package repos

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"eventplane/correlation"
)

func TestPutContentCreatesOneAttributedCommitAndRoundTripsBytes(t *testing.T) {
	// R-JQWG-D4IU
	service, repository, _ := newWriteTestService(t, "put-shape")
	seedCommits(t, repository, 2)
	before := gitText(t, repository, "rev-parse", "main")
	beforeCount := gitText(t, repository, "rev-list", "--count", "main")
	body := []byte("hello from the commit API\n")
	response := serveWrite(t, PutContentHandler(service), http.MethodPut,
		"/content?kind=code&name=put-shape&path=docs%2Fhello.txt&message="+url.QueryEscape("write greeting")+"&actor=sites:acme", body)
	if response.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%q", response.Code, response.Body.String())
	}
	var result CommitResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	head := gitText(t, repository, "rev-parse", "main")
	if result.Rev != head || gitText(t, repository, "rev-parse", "main^") != before {
		t.Fatalf("put rev=%q head=%q parent=%q want parent=%q", result.Rev, head, gitText(t, repository, "rev-parse", "main^"), before)
	}
	if count := gitText(t, repository, "rev-list", "--count", "main"); count != incrementDecimal(t, beforeCount) {
		t.Fatalf("commit count=%s before=%s", count, beforeCount)
	}
	format := "%an%x00%cn%x00%s"
	if got := gitText(t, repository, "show", "-s", "--format="+format, "main"); got != "sites:acme\x00ikigenba\x00write greeting" {
		t.Fatalf("commit attribution=%q", got)
	}
	read := serveRead(t, ContentHandler(service), "/content?kind=code&name=put-shape&path=docs%2Fhello.txt")
	if read.Code != http.StatusOK || !bytes.Equal(read.Body.Bytes(), body) {
		t.Fatalf("read after put status=%d body=%q", read.Code, read.Body.Bytes())
	}
}

func TestPutContentRejectsStaleRevisionBeforeWriting(t *testing.T) {
	// R-JS4C-QW9J
	service, repository, _ := newWriteTestService(t, "stale")
	seedCommits(t, repository, 2)
	head := gitText(t, repository, "rev-parse", "main")
	count := gitText(t, repository, "rev-list", "--count", "main")
	response := serveWrite(t, PutContentHandler(service), http.MethodPut,
		"/content?kind=code&name=stale&path=nope.txt&message=stale&actor=client&rev="+strings.Repeat("a", 40), []byte("must not land"))
	if response.Code != http.StatusConflict {
		t.Fatalf("stale put status=%d body=%q", response.Code, response.Body.String())
	}
	if got := gitText(t, repository, "rev-parse", "main"); got != head {
		t.Fatalf("head moved to %s, want %s", got, head)
	}
	if got := gitText(t, repository, "rev-list", "--count", "main"); got != count {
		t.Fatalf("commit count moved to %s, want %s", got, count)
	}
}

func TestDeleteContentCommitsPresentPathButAbsentPathIsEventlessNoOp(t *testing.T) {
	// R-JTC9-4O08
	service, repository, conn := newWriteTestService(t, "delete")
	if _, err := service.PutContent(context.Background(), "code", "delete", "remove.txt", "seed", "seed", "", []byte("gone")); err != nil {
		t.Fatal(err)
	}
	clearOutbox(t, conn)
	beforeCount := gitText(t, repository, "rev-list", "--count", "main")
	deleted := serveWrite(t, DeleteContentHandler(service), http.MethodDelete, "/content?kind=code&name=delete&path=remove.txt&message=remove&actor=client", nil)
	if deleted.Code != http.StatusOK || gitText(t, repository, "rev-list", "--count", "main") != incrementDecimal(t, beforeCount) {
		t.Fatalf("delete status=%d body=%q count=%s", deleted.Code, deleted.Body.String(), gitText(t, repository, "rev-list", "--count", "main"))
	}
	if output, err := service.custody.git.Run(context.Background(), repository, "ls-tree", "main", "--", "remove.txt"); err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("deleted path remains: output=%q err=%v", output, err)
	}
	clearOutbox(t, conn)
	head := gitText(t, repository, "rev-parse", "main")
	count := gitText(t, repository, "rev-list", "--count", "main")
	absent := serveWrite(t, DeleteContentHandler(service), http.MethodDelete, "/content?kind=code&name=delete&path=absent.txt&message=replay&actor=client", nil)
	var result CommitResult
	_ = json.Unmarshal(absent.Body.Bytes(), &result)
	if absent.Code != http.StatusOK || result.Rev != head || gitText(t, repository, "rev-list", "--count", "main") != count || outboxCount(t, conn) != 0 {
		t.Fatalf("absent delete status=%d result=%#v head=%s count=%s events=%d", absent.Code, result, gitText(t, repository, "rev-parse", "main"), gitText(t, repository, "rev-list", "--count", "main"), outboxCount(t, conn))
	}
}

func TestCommitBatchAppliesAllChangesInOneCommitAndIdenticalReplayIsNoOp(t *testing.T) {
	// R-JUK5-IFQX
	service, repository, conn := newWriteTestService(t, "batch")
	if _, err := service.PutContent(context.Background(), "code", "batch", "old.css", "seed", "seed", "", []byte("old")); err != nil {
		t.Fatal(err)
	}
	clearOutbox(t, conn)
	before := gitText(t, repository, "rev-list", "--count", "main")
	changes := []Change{
		{Op: "put", Path: "index.html", Content: []byte("index")},
		{Op: "put", Path: "assets/a.css", Content: []byte("a")},
		{Op: "put", Path: "assets/b.js", Content: []byte("b")},
		{Op: "delete", Path: "old.css"},
	}
	body, _ := json.Marshal(CommitBatchRequest{Kind: "code", Name: "batch", Message: "sync", Actor: "sites:batch", Changes: []BatchChange{
		{Op: "put", Path: "index.html", ContentB64: base64.StdEncoding.EncodeToString([]byte("index"))},
		{Op: "put", Path: "assets/a.css", ContentB64: base64.StdEncoding.EncodeToString([]byte("a"))},
		{Op: "put", Path: "assets/b.js", ContentB64: base64.StdEncoding.EncodeToString([]byte("b"))},
		{Op: "delete", Path: "old.css"},
	}})
	response := serveWrite(t, CommitHandler(service), http.MethodPost, "/commit", body)
	if response.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%q", response.Code, response.Body.String())
	}
	var result CommitResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if got := gitText(t, repository, "rev-list", "--count", "main"); got != incrementDecimal(t, before) {
		t.Fatalf("batch commit count=%s before=%s", got, before)
	}
	if got := treeFiles(t, repository); !reflect.DeepEqual(got, []string{"assets/a.css", "assets/b.js", "index.html"}) {
		t.Fatalf("batch tree=%v", got)
	}
	count := gitText(t, repository, "rev-list", "--count", "main")
	replayed, err := service.CommitBatch(context.Background(), "code", "batch", "replay", "sites:batch", "", changes)
	if err != nil || replayed.Rev != result.Rev || gitText(t, repository, "rev-list", "--count", "main") != count || outboxCount(t, conn) != 1 {
		t.Fatalf("replay result=%#v err=%v count=%s events=%d", replayed, err, gitText(t, repository, "rev-list", "--count", "main"), outboxCount(t, conn))
	}
}

func TestFirstPutAndBatchCreateParentlessMain(t *testing.T) {
	// R-JVS1-W7HM
	for _, fixture := range []struct {
		name  string
		write func(*Service, string) (CommitResult, error)
	}{
		{name: "initial-put", write: func(service *Service, name string) (CommitResult, error) {
			return service.PutContent(context.Background(), "code", name, "one.txt", "first put", "owner", "", []byte("one"))
		}},
		{name: "initial-batch", write: func(service *Service, name string) (CommitResult, error) {
			return service.CommitBatch(context.Background(), "code", name, "first batch", "owner", "", []Change{{Op: "put", Path: "one.txt", Content: []byte("one")}, {Op: "put", Path: "two.txt", Content: []byte("two")}})
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			service, repository, _ := newWriteTestService(t, fixture.name)
			result, err := fixture.write(service, fixture.name)
			if err != nil {
				t.Fatal(err)
			}
			if result.Rev != gitText(t, repository, "rev-parse", "refs/heads/main") || gitText(t, repository, "rev-list", "--count", "main") != "1" {
				t.Fatalf("initial result=%#v count=%s", result, gitText(t, repository, "rev-list", "--count", "main"))
			}
			if _, err := service.custody.git.Run(context.Background(), repository, "rev-parse", "main^"); err == nil {
				t.Fatal("initial commit unexpectedly has a parent")
			}
		})
	}
}

func TestPutContentBodyLimitRejectsBeforeGitAndAcceptsOneByteUnder(t *testing.T) {
	// R-JY7U-NQZ0
	service, repository, _ := newWriteTestService(t, "limit")
	service.SetMaxCommitBytes(5)
	beforeObjects := gitText(t, repository, "count-objects", "-v")
	over := serveWrite(t, PutContentHandler(service), http.MethodPut, "/content?kind=code&name=limit&path=a&message=over&actor=client", []byte("123456"))
	if over.Code != http.StatusRequestEntityTooLarge || !strings.Contains(over.Body.String(), "too_large") {
		t.Fatalf("over limit status=%d body=%q", over.Code, over.Body.String())
	}
	if after := gitText(t, repository, "count-objects", "-v"); after != beforeObjects {
		t.Fatalf("over-limit request wrote git objects:\nbefore %s\nafter %s", beforeObjects, after)
	}
	if _, err := service.custody.git.Run(context.Background(), repository, "rev-parse", "main"); err == nil {
		t.Fatal("over-limit request created main")
	}
	under := serveWrite(t, PutContentHandler(service), http.MethodPut, "/content?kind=code&name=limit&path=a&message=under&actor=client", []byte("1234"))
	if under.Code != http.StatusOK || gitText(t, repository, "show", "main:a") != "1234" {
		t.Fatalf("under limit status=%d body=%q", under.Code, under.Body.String())
	}
}

func TestCommitPublishesOnePushWithRealHeadAndRequestActor(t *testing.T) {
	// R-JFXC-X6UL
	service, repository, conn := newWriteTestService(t, "demo")
	firstResponse := serveWriteWithContext(t, PutContentHandler(service), context.Background(), http.MethodPut, "/content?kind=code&name=demo&path=a&message=first&actor=client:first", []byte("a"), "client:first")
	var first CommitResult
	if firstResponse.Code != http.StatusOK || json.Unmarshal(firstResponse.Body.Bytes(), &first) != nil {
		t.Fatalf("first commit status=%d body=%q", firstResponse.Code, firstResponse.Body.String())
	}
	assertOnlyPush(t, conn, first.Rev, "", "client:first", "demo")
	clearOutbox(t, conn)
	secondResponse := serveWriteWithContext(t, PutContentHandler(service), context.Background(), http.MethodPut, "/content?kind=code&name=demo&path=b&message=second&actor=client:second", []byte("b"), "client:second")
	var second CommitResult
	if secondResponse.Code != http.StatusOK || json.Unmarshal(secondResponse.Body.Bytes(), &second) != nil {
		t.Fatalf("second commit status=%d body=%q", secondResponse.Code, secondResponse.Body.String())
	}
	if second.Rev != gitText(t, repository, "rev-parse", "main") {
		t.Fatalf("reported rev=%q real head=%q", second.Rev, gitText(t, repository, "rev-parse", "main"))
	}
	assertOnlyPush(t, conn, second.Rev, first.Rev, "client:second", "demo")
}

func TestRealFeedSubscriberReceivesPushCommitEnvelope(t *testing.T) {
	// R-JID5-OQBZ
	service, _, _ := newWriteTestService(t, "demo")
	server := httptest.NewServer(service.producer.FeedHandler())
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL+"?from=tail", nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	correlationID := correlation.New()
	ctx := correlation.WithContext(context.Background(), correlationID)
	commitResponse := serveWriteWithContext(t, PutContentHandler(service), ctx, http.MethodPut, "/content?kind=code&name=demo&path=index&message=wire+commit&actor=loopback:client", []byte("wire"), "loopback:client")
	var result CommitResult
	if commitResponse.Code != http.StatusOK || json.Unmarshal(commitResponse.Body.Bytes(), &result) != nil {
		t.Fatalf("wire commit status=%d body=%q", commitResponse.Code, commitResponse.Body.String())
	}
	eventName, envelope := readPushFrame(t, response.Body)
	if eventName != "repos:push/code/demo" || envelope.Source != "repos" || envelope.Kind != "push" || envelope.Subject != "/code/demo" || envelope.CorrelationID == "" || envelope.CorrelationID != correlationID {
		t.Fatalf("wire event=%q envelope=%#v", eventName, envelope)
	}
	var payload PushPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.SHA != result.Rev {
		t.Fatalf("wire payload=%#v err=%v result=%#v", payload, err, result)
	}
}

type wireEnvelope struct {
	Source        string          `json:"source"`
	Kind          string          `json:"kind"`
	Subject       string          `json:"subject"`
	CorrelationID string          `json:"correlation_id"`
	Payload       json.RawMessage `json:"payload"`
}

func readPushFrame(t *testing.T, body io.Reader) (string, wireEnvelope) {
	t.Helper()
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		if !strings.HasPrefix(scanner.Text(), "event: repos:push/") {
			continue
		}
		eventName := strings.TrimPrefix(scanner.Text(), "event: ")
		if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "data: ") {
			t.Fatal("push frame missing data line")
		}
		var envelope wireEnvelope
		if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &envelope); err != nil {
			t.Fatal(err)
		}
		return eventName, envelope
	}
	t.Fatalf("feed ended before push: %v", scanner.Err())
	return "", wireEnvelope{}
}

func newWriteTestService(t *testing.T, name string) (*Service, string, *sql.DB) {
	t.Helper()
	conn, store := newTestStore(t)
	custody, _ := newTestCustody(t)
	if err := custody.Init(context.Background(), "code", name); err != nil {
		t.Fatal(err)
	}
	service := serviceWithOutbox(t, conn, store, custody)
	service.SetMaxCommitBytes(1024 * 1024)
	repository, _ := custody.Path("code", name)
	return service, repository, conn
}

func serveWrite(t *testing.T, handler http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return serveWriteWithContext(t, handler, context.Background(), method, target, body, "")
}

func serveWriteWithContext(t *testing.T, handler http.Handler, ctx context.Context, method, target string, body []byte, clientID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body)).WithContext(ctx)
	if clientID != "" {
		request.Header.Set("X-Client-Id", clientID)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func gitText(t *testing.T, repository string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, repository, args...))
}

func incrementDecimal(t *testing.T, value string) string {
	t.Helper()
	var number int
	if _, err := fmt.Sscan(value, &number); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(number + 1)
}

func treeFiles(t *testing.T, repository string) []string {
	t.Helper()
	output := gitText(t, repository, "ls-tree", "-r", "--name-only", "main")
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func clearOutbox(t *testing.T, conn *sql.DB) {
	t.Helper()
	if _, err := conn.Exec("DELETE FROM outbox"); err != nil {
		t.Fatal(err)
	}
}

func outboxCount(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM outbox").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertOnlyPush(t *testing.T, conn *sql.DB, sha, oldSHA, actor, name string) {
	t.Helper()
	var count int
	var kind, subject, raw string
	if err := conn.QueryRow("SELECT COUNT(*), kind, subject, payload FROM outbox").Scan(&count, &kind, &subject, &raw); err != nil {
		t.Fatal(err)
	}
	var payload PushPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	want := PushPayload{Kind: "code", Name: name, Branch: "main", SHA: sha, OldSHA: oldSHA, Actor: actor}
	if count != 1 || kind != "push" || subject != "/code/"+name || payload != want {
		t.Fatalf("event count=%d kind=%q subject=%q payload=%#v want=%#v", count, kind, subject, payload, want)
	}
}
