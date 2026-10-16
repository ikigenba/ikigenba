package website

import (
	"net/http"
	"testing"
	"time"
)

func TestDeclaredShapes(t *testing.T) {
	// R-HK5A-WQ18 R-HLD7-AHRX
	result := Result{"state", "url", "title", 201, 123, "error"}
	if result.State != "state" || result.URL != "url" || result.Title != "title" || result.StatusCode != 201 || result.Size != 123 || result.Error != "error" {
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
