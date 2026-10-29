package website

import (
	"net/http"
	"testing"
	"time"
)

func TestDeclaredShapes(t *testing.T) {
	// R-6DLM-RKIJ R-6ETJ-5C98 R-2EOR-MW1G
	result := Result{"state", "url", "title", 201, 123, "error", "input", []Miss{{"miss", 404, 0, FailStatus}}}
	if result.State != "state" || result.URL != "url" || result.Title != "title" || result.StatusCode != 201 || result.Size != 123 || result.Error != "error" || result.From != "input" || len(result.Misses) != 1 || result.Misses[0] != (Miss{"miss", 404, 0, FailStatus}) {
		t.Fatal(result)
	}
	client := &http.Client{}
	timer := make(chan time.Time)
	after := func(time.Duration) <-chan time.Time { return timer }
	checker := Checker{client, after}
	if checker.Client != client || checker.After(Timeout) != timer {
		t.Fatal("checker shape")
	}
}
