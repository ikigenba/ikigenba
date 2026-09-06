package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agent-repl/internal/cli"
)

var sessionTime = time.Date(2031, 2, 3, 4, 5, 6, 0, time.FixedZone("test", -6*60*60))

// R-VXK1-CMU3 R-VZZU-46BH
func TestRunStopsBeforeInputWhenLogCreationFails(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".agent-repl"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), nil, failOnRead{t: t}, &stdout, &stderr, testDeps(t, home))
	if code != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "error: ") ||
		!strings.Contains(stderr.String(), "not a directory") {
		t.Fatalf("Run log failure = code %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}

// R-W17Q-HY26
func TestRunOpenFailureLeavesNewLogEmptyAndDoesNotReadInput(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	deps.Getenv = func(string) string { return "" }
	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), configuredArgs("http://provider.invalid"), failOnRead{t: t}, &stdout, &stderr, deps)
	if code != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "error: ") ||
		!strings.Contains(stderr.String(), "OPENAI_API_KEY") {
		t.Fatalf("Run open failure = code %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	data := readOnlyLog(t, home)
	if len(data) != 0 {
		t.Fatalf("log after Open failure = %q, want empty", data)
	}
}

// R-W4VF-N9A9
func TestRunConsumesTurnBeforeReadingNextLine(t *testing.T) {
	firstEvent := make(chan struct{})
	allowCompletion := make(chan struct{})
	var handlerComplete atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		prompt := lastUserText(t, decodeBody(t, request))
		if prompt == "first" {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"first event\"}}]}\n\n")
			writer.(http.Flusher).Flush()
			close(firstEvent)
			<-allowCompletion
			handlerComplete.Store(true)
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		writeChatSuccess(writer, "second answer")
	}))
	t.Cleanup(server.Close)

	reader := &completionAwareReader{
		lines:           [][]byte{[]byte("first\n"), []byte("second\n")},
		handlerComplete: &handlerComplete,
	}
	done := make(chan int, 1)
	go func() {
		done <- cli.Run(t.Context(), configuredArgs(server.URL), reader, io.Discard, io.Discard, testDeps(t, t.TempDir()))
	}()
	await(t, firstEvent)
	close(allowCompletion)
	if code := awaitValue(t, done); code != 0 {
		t.Fatalf("Run code = %d", code)
	}
	if reader.readBeforeComplete.Load() {
		t.Fatal("second stdin read began before the first provider stream completed")
	}
}

// R-VYRX-QEKS R-W2FM-VPSV R-W4VF-N9A9 R-W7B8-ESRN
// R-WDEQ-BNH4 R-WT9F-AO45
func TestRunDecoratedLoopCreatesPrivateCompleteLog(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := decodeBody(t, request)
		prompt := lastUserText(t, body)
		mu.Lock()
		prompts = append(prompts, prompt)
		mu.Unlock()
		writeChatSuccess(writer, "answer "+prompt)
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	input := strings.NewReader("\r\nfirst\r\nfinal without newline")
	code := cli.Run(t.Context(), configuredArgs(server.URL), input, &stdout, &stderr, testDeps(t, home))
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("Run = code %d stderr %q", code, stderr.String())
	}
	mu.Lock()
	gotPrompts := append([]string(nil), prompts...)
	mu.Unlock()
	if fmt.Sprint(gotPrompts) != "[first final without newline]" {
		t.Fatalf("provider prompts = %q", gotPrompts)
	}
	transcript := stdout.String()
	for _, part := range []string{"you › ", "assistant › answer first", "assistant › answer final without newline", "summary\n"} {
		if !strings.Contains(transcript, part) {
			t.Errorf("decorated output missing %q: %q", part, transcript)
		}
	}
	if got := strings.Count(transcript, "you › "); got != 4 {
		t.Errorf("prompt count = %d, want 4 (blank, two turns, EOF)", got)
	}

	logPath := onlyLogPath(t, home)
	assertMode(t, filepath.Dir(logPath), 0o700)
	assertMode(t, logPath, 0o600)
	if got, want := filepath.Base(logPath), "20310203T100506Z.jsonl"; got != want {
		t.Fatalf("log name = %q, want %q", got, want)
	}
	records := decodeRecords(t, readFile(t, logPath))
	if len(records) == 0 || records[len(records)-1]["type"] != "summary" {
		t.Fatalf("records do not end in summary: %#v", records)
	}
	for _, record := range records {
		if got := record["time"]; got != "2031-02-03T04:05:06-06:00" {
			t.Fatalf("record time = %v, want injected clock", got)
		}
	}
}

// R-W8J4-SKIC
func TestRunContinuesAfterProviderStreamError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = decodeBody(t, request)
		if calls.Add(1) == 1 {
			http.Error(writer, `{"error":{"message":"first turn rejected","type":"bad_request"}}`, http.StatusBadRequest)
			return
		}
		writeChatSuccess(writer, "second succeeded")
	}))
	t.Cleanup(server.Close)

	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), configuredArgs(server.URL), strings.NewReader("first\nsecond\n"), &stdout, &stderr, testDeps(t, t.TempDir()))
	if code != 0 || calls.Load() != 2 || !strings.Contains(stderr.String(), "error › ") || !strings.Contains(stdout.String(), "second succeeded") {
		t.Fatalf("continuation = code %d calls %d stdout %q stderr %q", code, calls.Load(), stdout.String(), stderr.String())
	}
}

// R-W8J4-SKIC
func TestRunContinuesAfterUnknownOptionInvalidConfig(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(writer, "unknown option must fail before this request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	args := append(configuredArgs(server.URL), "-c", "unknown_future_option=verbatim")
	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), args, strings.NewReader("first\nsecond\n"), &stdout, &stderr, testDeps(t, t.TempDir()))
	if code != 0 || requests.Load() != 0 {
		t.Fatalf("invalid-config continuation = code %d provider requests %d", code, requests.Load())
	}
	if got := strings.Count(stderr.String(), "error › "); got != 2 ||
		!strings.Contains(stderr.String(), "invalid configuration") ||
		!strings.Contains(stderr.String(), "unknown_future_option") {
		t.Fatalf("invalid-config errors = %q, want two rendered agentkit.ErrInvalidConfig errors", stderr.String())
	}
	if got := strings.Count(stdout.String(), "you › "); got != 3 {
		t.Fatalf("prompt count after two invalid turns = %d, want 3", got)
	}
}

// R-WT9F-AO45
func TestRunDecoratedLifecycleIsOrderedAndSummaryMatchesLogSink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ordered.txt"), []byte("ordered result"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := decodeBody(t, request)
		encoded, err := json.Marshal(body["messages"])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(encoded, []byte("ordered result")) {
			writeToolCallWithUsage(writer, "Read", `{"file_path":"ordered.txt"}`, 11, 7, 2, 3)
			return
		}
		writeChatSuccessWithUsage(writer, "ordered answer", 13, 5, 4, 1)
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	deps := testDeps(t, home)
	deps.Root = root
	var stdout, stderr bytes.Buffer
	if code := cli.Run(t.Context(), configuredArgs(server.URL), strings.NewReader("one turn\n"), &stdout, &stderr, deps); code != 0 || stderr.Len() != 0 {
		t.Fatalf("Run = code %d stderr %q", code, stderr.String())
	}

	want := "you › \n" +
		"tool › Read {\"file_path\":\"ordered.txt\"}\n\n" +
		"result › Read      1\tordered result\n\n" +
		"assistant › ordered answer\n\n" +
		"you › \n" +
		"summary\n" +
		"· tokens  in=18 cache(r=6 w=0) out=8 reasoning=4 total=36\n" +
		"· cost     $0.000453 session\n"
	if got := stdout.String(); got != want {
		t.Fatalf("decorated lifecycle output\ngot:  %q\nwant: %q", got, want)
	}
}

// R-WUHB-OFUU
func TestRunRawStdoutIsExactlyTheJSONLLogAndErrorsStayOnStderr(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = decodeBody(t, request)
		if calls.Add(1) == 1 {
			http.Error(writer, "rejected", http.StatusBadRequest)
			return
		}
		writeChatSuccess(writer, "raw answer")
	}))
	t.Cleanup(server.Close)
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := append([]string{"-raw"}, configuredArgs(server.URL)...)
	code := cli.Run(t.Context(), args, strings.NewReader("bad\ngood\n"), &stdout, &stderr, testDeps(t, home))
	if code != 0 || !strings.Contains(stderr.String(), "error › ") {
		t.Fatalf("raw run = code %d stderr %q", code, stderr.String())
	}
	fileData := readOnlyLog(t, home)
	want := []byte("" +
		`{"type":"turn_start","time":"2031-02-03T04:05:06-06:00","seq":0,"identity":{"Endpoint":"openai-chat","AuthMode":"api_key","Model":"gpt-5.6-sol"}}` + "\n" +
		`{"type":"error","time":"2031-02-03T04:05:06-06:00","seq":1,"error":{"Category":2,"Status":400,"Code":"","Message":"rejected\n","RetryAfter":0,"Endpoint":{"Endpoint":"","AuthMode":"","Model":""}}}` + "\n" +
		`{"type":"usage","time":"2031-02-03T04:05:06-06:00","seq":2,"usage":{"InputTokens":0,"CachedTokens":0,"CacheWrite5mTokens":0,"CacheWrite1hTokens":0,"OutputTokens":0,"ReasoningTokens":0},"cost":0}` + "\n" +
		`{"type":"turn_end","time":"2031-02-03T04:05:06-06:00","seq":3}` + "\n" +
		`{"type":"turn_start","time":"2031-02-03T04:05:06-06:00","seq":0,"identity":{"Endpoint":"openai-chat","AuthMode":"api_key","Model":"gpt-5.6-sol"}}` + "\n" +
		`{"type":"message","time":"2031-02-03T04:05:06-06:00","seq":1,"message":{"role":2,"blocks":[{"type":"text","text":"raw answer","provider":null}]}}` + "\n" +
		`{"type":"usage","time":"2031-02-03T04:05:06-06:00","seq":2,"usage":{"InputTokens":0,"CachedTokens":0,"CacheWrite5mTokens":0,"CacheWrite1hTokens":0,"OutputTokens":0,"ReasoningTokens":0},"cost":0}` + "\n" +
		`{"type":"turn_end","time":"2031-02-03T04:05:06-06:00","seq":3}` + "\n" +
		`{"type":"summary","time":"2031-02-03T04:05:06-06:00","seq":4,"usage":{"InputTokens":0,"CachedTokens":0,"CacheWrite5mTokens":0,"CacheWrite1hTokens":0,"OutputTokens":0,"ReasoningTokens":0},"cost":0}` + "\n")
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("raw stdout differs from independent expectation\nstdout=%q\nwant=%q", stdout.Bytes(), want)
	}
	if !bytes.Equal(stdout.Bytes(), fileData) {
		t.Fatalf("raw stdout differs from log\nstdout=%q\nlog=%q", stdout.Bytes(), fileData)
	}
	records := decodeRecords(t, stdout.Bytes())
	if len(records) == 0 || records[len(records)-1]["type"] != "summary" || strings.Contains(stdout.String(), "you › ") {
		t.Fatalf("raw stdout is not JSONL ending in summary: %q", stdout.String())
	}
}

// R-WAYX-K3ZQ R-WDEQ-BNH4
func TestRunPromptInterruptEndsBlockedInputAndClosesLog(t *testing.T) {
	reader := newBlockingReader()
	interrupts := make(chan struct{}, 1)
	deps := testDeps(t, t.TempDir())
	deps.Interrupts = interrupts
	done := make(chan int, 1)
	go func() { done <- cli.Run(t.Context(), nil, reader, io.Discard, io.Discard, deps) }()
	await(t, reader.started)
	interrupts <- struct{}{}
	if code := awaitValue(t, done); code != 0 {
		t.Fatalf("Run interrupt code = %d", code)
	}
	records := decodeRecords(t, readOnlyLog(t, deps.Home))
	if len(records) != 1 || records[0]["type"] != "summary" {
		t.Fatalf("prompt interrupt records = %#v", records)
	}
}

// R-WC6T-XVQF
func TestRunParentCancellationEndsBlockedInput(t *testing.T) {
	reader := newBlockingReader()
	ctx, cancel := context.WithCancel(t.Context())
	deps := testDeps(t, t.TempDir())
	done := make(chan int, 1)
	go func() { done <- cli.Run(ctx, nil, reader, io.Discard, io.Discard, deps) }()
	await(t, reader.started)
	cancel()
	if code := awaitValue(t, done); code != 0 {
		t.Fatalf("Run cancellation code = %d", code)
	}
}

// R-W9R1-6C91
func TestRunInflightInterruptCancelsTurnThenSendsNextLine(t *testing.T) {
	firstStarted := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		prompt := lastUserText(t, decodeBody(t, request))
		calls.Add(1)
		if prompt == "cancel me" {
			close(firstStarted)
			<-request.Context().Done()
			return
		}
		writeChatSuccess(writer, "continued")
	}))
	t.Cleanup(server.Close)
	interrupts := make(chan struct{}, 1)
	deps := testDeps(t, t.TempDir())
	deps.Interrupts = interrupts
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- cli.Run(t.Context(), configuredArgs(server.URL), strings.NewReader("cancel me\nthen continue\n"), &stdout, &stderr, deps)
	}()
	await(t, firstStarted)
	interrupts <- struct{}{}
	if code := awaitValue(t, done); code != 0 || calls.Load() != 2 || !strings.Contains(stdout.String(), "continued") {
		t.Fatalf("in-flight interrupt = code %d calls %d stdout %q stderr %q", code, calls.Load(), stdout.String(), stderr.String())
	}
}

// R-U2HK-B91W
func TestRunMapsOptionsAndInjectedDependenciesIntoRealSessionBehavior(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "root-marker.txt"), []byte("rooted-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer injected-credential" {
			t.Errorf("Authorization = %q", got)
		}
		body := decodeBody(t, request)
		if body["model"] != "gpt-5.6-sol" || body["temperature"] != float64(0.25) {
			t.Errorf("mapped model/settings = %#v", body)
		}
		requests.Add(1)
		encoded, err := json.Marshal(body["messages"])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(encoded, []byte("rooted-content")) {
			writeToolCall(writer, "Read", `{"file_path":"root-marker.txt"}`)
			return
		}
		writeChatSuccess(writer, "tool complete")
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	deps := testDeps(t, home)
	deps.Root = root
	deps.Getenv = func(name string) string {
		if name != "OPENAI_API_KEY" {
			t.Fatalf("Getenv(%q), want OPENAI_API_KEY", name)
		}
		return "injected-credential"
	}
	args := configuredArgs(server.URL)
	args = append(args, "-c", "temperature=0.25", "-c", "auth_file=/unused-but-mapped")
	if code := cli.Run(t.Context(), args, strings.NewReader("use the rooted file\n"), io.Discard, io.Discard, deps); code != 0 {
		t.Fatalf("Run code = %d", code)
	}
	if requests.Load() != 2 {
		t.Fatalf("provider requests = %d, want tool call and completion", requests.Load())
	}
	logPath := onlyLogPath(t, home)
	wantLogPath := filepath.Join(home, ".agent-repl", "logs", "20310203T100506Z.jsonl")
	if logPath != wantLogPath {
		t.Fatalf("log path = %q, want injected Home and Now path %q", logPath, wantLogPath)
	}
}

func configuredArgs(baseURL string) []string {
	return []string{
		"-c", "provider=openai",
		"-c", "model=gpt-5.6-sol",
		"-c", "wire=chat",
		"-c", "auth=api_key",
		"-c", "base_url=" + baseURL,
	}
}

func testDeps(t *testing.T, home string) cli.Deps {
	t.Helper()
	return cli.Deps{
		Home:   home,
		Getenv: func(string) string { return "test-api-key" },
		Now:    func() time.Time { return sessionTime },
		Root:   t.TempDir(),
	}
}

func writeChatSuccess(writer http.ResponseWriter, text string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	data, _ := json.Marshal(text)
	_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", data)
}

func writeChatSuccessWithUsage(writer http.ResponseWriter, text string, prompt, completion, cached, reasoning int) {
	writer.Header().Set("Content-Type", "text/event-stream")
	data, _ := json.Marshal(text)
	_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: {\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"prompt_tokens_details\":{\"cached_tokens\":%d},\"completion_tokens_details\":{\"reasoning_tokens\":%d}}}\n\ndata: [DONE]\n\n", data, prompt, completion, cached, reasoning)
}

func writeToolCall(writer http.ResponseWriter, name, arguments string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-root\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", name, arguments)
}

func writeToolCallWithUsage(writer http.ResponseWriter, name, arguments string, prompt, completion, cached, reasoning int) {
	writer.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-ordered\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: {\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"prompt_tokens_details\":{\"cached_tokens\":%d},\"completion_tokens_details\":{\"reasoning_tokens\":%d}}}\n\ndata: [DONE]\n\n", name, arguments, prompt, completion, cached, reasoning)
}

func decodeBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	defer func() {
		if err := request.Body.Close(); err != nil {
			t.Errorf("close provider request: %v", err)
		}
	}()
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Errorf("decode provider request: %v", err)
	}
	return body
}

func lastUserText(t *testing.T, body map[string]any) string {
	t.Helper()
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("messages = %#v", body["messages"])
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		t.Fatalf("last message = %#v", messages[len(messages)-1])
	}
	text, _ := last["content"].(string)
	return text
}

func onlyLogPath(t *testing.T, home string) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(home, ".agent-repl", "logs", "*.jsonl"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("log paths = %q, error %v", paths, err)
	}
	return paths[0]
}

func readOnlyLog(t *testing.T, home string) []byte {
	t.Helper()
	return readFile(t, onlyLogPath(t, home))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	file, err := root.Open(filepath.Base(path))
	if closeErr := root.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeRecords(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		return nil
	}
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("invalid JSONL record %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

type blockingReader struct {
	started chan struct{}
	once    sync.Once
}

// completionAwareReader serves stdin lines and records whether the second
// line was requested before the provider handler had completed the first
// stream, so the ordering claim is checked at the read itself rather than by
// waiting out a window.
type completionAwareReader struct {
	lines              [][]byte
	reads              atomic.Int32
	handlerComplete    *atomic.Bool
	readBeforeComplete atomic.Bool
}

func (reader *completionAwareReader) Read(buffer []byte) (int, error) {
	index := int(reader.reads.Add(1)) - 1
	if index >= len(reader.lines) {
		return 0, io.EOF
	}
	line := reader.lines[index]
	if bytes.Equal(line, []byte("second\n")) && !reader.handlerComplete.Load() {
		reader.readBeforeComplete.Store(true)
	}
	return copy(buffer, line), nil
}

func newBlockingReader() *blockingReader {
	return &blockingReader{started: make(chan struct{})}
}

func (reader *blockingReader) Read([]byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	select {}
}

func await(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for synchronization point")
	}
}

func awaitValue(t *testing.T, value <-chan int) int {
	t.Helper()
	select {
	case result := <-value:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run")
		return -1
	}
}
