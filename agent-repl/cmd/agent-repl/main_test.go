package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// R-U4XD-2SJA
func TestBuiltBinaryWiresProcessEnvironmentAndStreams(t *testing.T) {
	const (
		prompt = "answer through the real binary"
		reply  = "binary wiring confirmed"
	)
	apiKey := strings.Repeat("x", 20)
	server := newTestProvider(t, apiKey, prompt, reply)
	binary := buildAgentREPL(t)
	home := t.TempDir()
	command := runAgentREPL(t, binary, server.URL, home, apiKey, prompt)

	assertSuccessfulRun(t, command, reply)
	assertOneLogFile(t, home)
}

func newTestProvider(t *testing.T, apiKey, prompt, reply string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.Header.Get("Authorization"), "Bearer "+apiKey; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
			return
		}
		if !bytes.Contains(body["input"], []byte(prompt)) {
			t.Errorf("provider input = %q, want prompt %q", body["input"], prompt)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", reply)
		_, _ = io.WriteString(writer, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{}}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

func buildAgentREPL(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "agent-repl")
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find Go tool: %v", err)
	}
	build := runProcess(t, goTool, []string{"build", "-o", binary, "./cmd/agent-repl"}, filepath.Join("..", ".."), os.Environ(), "")
	if !build.success {
		t.Fatalf("build binary: %s\n%s", build.stdout, build.stderr)
	}
	return binary
}

func runAgentREPL(t *testing.T, binary, baseURL, home, apiKey, prompt string) processResult {
	t.Helper()
	return runProcess(t, binary, []string{
		"-c", "provider=openai",
		"-c", "auth=api_key",
		"-c", "base_url=" + baseURL,
	}, "", overrideEnvironment(os.Environ(), map[string]string{
		"HOME":           home,
		"OPENAI_API_KEY": apiKey,
	}), prompt+"\n")
}

func assertSuccessfulRun(t *testing.T, command processResult, reply string) {
	t.Helper()
	if !command.success {
		t.Fatalf("run binary\nstdout: %s\nstderr: %s", command.stdout, command.stderr)
	}
	wantStdout := "you › \n" +
		"assistant › " + reply + "\n\n" +
		"you › \n" +
		"summary\n" +
		"· tokens  in=0 cache(r=0 w=0) out=0 reasoning=0 total=0\n" +
		"· cost     $0.000000 session\n"
	if got := command.stdout; got != wantStdout {
		t.Errorf("stdout = %q, want %q", got, wantStdout)
	}
}

func assertOneLogFile(t *testing.T, home string) {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(home, ".agent-repl", "logs", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("log files = %q, want exactly one JSONL file", logs)
	}
}

type processResult struct {
	stdout  string
	stderr  string
	success bool
}

func runProcess(t *testing.T, executable string, args []string, dir string, environment []string, stdin string) processResult {
	t.Helper()

	processDir := t.TempDir()
	stdinFile := createProcessPipe(t, stdin)
	stdoutFile := createProcessFile(t, processDir, "")
	stderrFile := createProcessFile(t, processDir, "")

	process, err := os.StartProcess(executable, append([]string{executable}, args...), &os.ProcAttr{
		Dir:   dir,
		Env:   environment,
		Files: []*os.File{stdinFile, stdoutFile, stderrFile},
	})
	if err != nil {
		t.Fatalf("start %s: %v", filepath.Base(executable), err)
	}
	state, err := process.Wait()
	if err != nil {
		t.Fatalf("wait for %s: %v", filepath.Base(executable), err)
	}

	return processResult{
		stdout:  readProcessFile(t, stdoutFile),
		stderr:  readProcessFile(t, stderrFile),
		success: state.Success(),
	}
}

func createProcessPipe(t *testing.T, contents string) *os.File {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create process stdin pipe: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if _, err := io.WriteString(writer, contents); err != nil {
		_ = writer.Close()
		t.Fatalf("write process stdin pipe: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close process stdin pipe: %v", err)
	}
	return reader
}

func createProcessFile(t *testing.T, dir, contents string) *os.File {
	t.Helper()
	file, err := os.CreateTemp(dir, "process-*")
	if err != nil {
		t.Fatalf("create process file: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := io.WriteString(file, contents); err != nil {
		t.Fatalf("write process file: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind process file: %v", err)
	}
	return file
}

func readProcessFile(t *testing.T, file *os.File) string {
	t.Helper()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind process output: %v", err)
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read process output: %v", err)
	}
	return string(contents)
}

func overrideEnvironment(parent []string, overrides map[string]string) []string {
	environment := make([]string, 0, len(parent)+len(overrides))
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[name]; !replaced {
			environment = append(environment, entry)
		}
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}
