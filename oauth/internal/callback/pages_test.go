package callback_test

import (
	"bytes"
	"context"
	"html/template"
	"net/url"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/oauth"
)

// R-GT3R-0PA9 R-GUBN-EH0Y R-6C1K-3M98
func TestTemplateSetExecutesDeclaredPages(t *testing.T) {
	pages := callbackTemplates(t)
	for _, test := range []struct {
		name string
		data any
	}{
		{"success", nil},
		{"failure", struct{ Description string }{}},
		{"failure", struct{ Description string }{"distinguishing supplied description"}},
	} {
		var body bytes.Buffer
		if err := pages.ExecuteTemplate(&body, test.name, test.data); err != nil {
			t.Errorf("ExecuteTemplate(%q) = %v", test.name, err)
		}
	}
}

// R-GVJJ-S8RN R-GWRG-60IC R-GZ78-XJZQ
func TestCallbackBodiesEqualNamedTemplates(t *testing.T) {
	for _, test := range callbackPageCases() {
		t.Run(test.name, func(t *testing.T) {
			page := servePage(t, test.query)
			var expected bytes.Buffer
			if err := callbackTemplates(t).ExecuteTemplate(&expected, test.template, test.data); err != nil {
				t.Fatal(err)
			}
			if page.body != expected.String() {
				t.Errorf("callback body = %q, template output = %q", page.body, expected.String())
			}
		})
	}
}

// R-TPZN-04BM
func TestCallbackPagesAreSelfContained(t *testing.T) {
	for _, test := range callbackPageCases() {
		t.Run(test.name, func(t *testing.T) {
			if err := readSelfContainedHTML(servePage(t, test.query).body); err != nil {
				t.Error(err)
			}
		})
	}
}

func callbackTemplates(t *testing.T) *template.Template {
	t.Helper()
	pages, err := template.ParseFS(oauth.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return pages
}

type pageCase struct {
	name, template string
	query          url.Values
	data           any
}

func callbackPageCases() []pageCase {
	empty := struct{ Description string }{}
	described := struct{ Description string }{"provider supplied distinguishing text"}
	return []pageCase{
		{"success", "success", url.Values{"state": {"expected-state"}, "code": {"code"}}, nil},
		{"state absent", "failure", url.Values{}, empty},
		{"state empty", "failure", url.Values{"state": {""}}, empty},
		{"state mismatch with provider description", "failure", url.Values{"state": {"wrong"}, "error": {"denied"}, "error_description": {"untrusted supplied description"}}, empty},
		{"code absent", "failure", url.Values{"state": {"expected-state"}}, empty},
		{"code empty", "failure", url.Values{"state": {"expected-state"}, "code": {""}, "error": {""}}, empty},
		{"provider description absent", "failure", url.Values{"state": {"expected-state"}, "error": {"denied"}}, empty},
		{"provider description empty", "failure", url.Values{"state": {"expected-state"}, "error": {"denied"}, "error_description": {""}}, empty},
		{"provider description supplied", "failure", url.Values{"state": {"expected-state"}, "error": {"denied"}, "error_description": {described.Description}}, described},
	}
}

func servePage(t *testing.T, query url.Values) callbackPage {
	t.Helper()
	server := listenForWait(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcomes := startWait(ctx, server, "/callback", "expected-state")
	page := getCallbackPage(ctx, t, server.Port(), "/callback", query)
	requireWaitOutcome(ctx, t, outcomes)
	return page
}
