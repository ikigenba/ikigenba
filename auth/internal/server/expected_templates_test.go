package server

import (
	"bytes"
	"html/template"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth"
)

func expectedAuthTemplates(t *testing.T) *template.Template {
	t.Helper()
	// R-3D6V-9YHK: this is the design's canonical template construction.
	set, err := page.Templates().Funcs(template.FuncMap{"splitNUL": splitNUL}).Parse(pageValueTemplate)
	if err != nil {
		t.Fatal(err)
	}
	set, err = set.ParseFS(auth.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func expectedAuthTemplate(t *testing.T, name string, data any) string {
	t.Helper()
	var out bytes.Buffer
	if err := expectedAuthTemplates(t).ExecuteTemplate(&out, name, data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func assertAuthTemplate(t *testing.T, body, name string, data any) {
	t.Helper()
	if want := expectedAuthTemplate(t, name, data); body != want {
		t.Fatalf("response differs from %s template: got %q, want %q", name, body, want)
	}
}

func TestTemplateDataShapesAndStates(t *testing.T) {
	// R-6ZCB-90OL R-70K7-MSFA R-71S4-0K5Z: unkeyed construction proves exact field order and count.
	signIn := signInPageData{"fixture.test", "", "encoded", "destination.test", "auth.fixture.test:443", "workspace.test", "email@fixture.test"}
	profile := profilePageData{"fixture.test", "email@fixture.test", "workspace.test", nil, tokenCreateData{Expiry: "90d"}, mcpClientsData{}}
	_ = authPageData{page.Banner{}, &signIn, &profile, nil, nil}
	// R-6T8T-C5Z4 R-3EER-NQ89 R-3FMO-1HYY
	set := expectedAuthTemplates(t)
	for _, name := range []string{"page", "chrome", "signIn", "alert", "profile", "tokenList", "tokenTime", "tokenLastUsed", "tokenNever", "elapsed", "tokenCreate", "plusIcon", "tokenCreated", "copyIcon", "mcp-clients", "approve", "value"} {
		if set.Lookup(name) == nil {
			t.Fatalf("missing template %s", name)
		}
	}
	execute := func(name string, data any) { t.Helper(); expectedAuthTemplate(t, name, data) }
	for _, refusal := range []string{"", "cancelled", "not_member"} {
		signIn.Refused = refusal
		execute("page", authPageData{SignIn: &signIn})
		execute("signIn", signIn)
		execute("alert", signIn)
	}
	execute("page", authPageData{Profile: &profile})
	execute("chrome", authPageData{Profile: &profile})
	execute("profile", profile)
	for _, unit := range []string{"now", "minute", "hour", "day"} {
		last := tokenLastUsedData{Datetime: "fixture-datetime", Title: "fixture-title", Elapsed: elapsedData{Unit: unit, Count: 2}}
		expires := tokenTimeData{Datetime: "expiry-datetime", Text: "expiry-text"}
		profile.Rows = []tokenRowData{{ID: "fixture-unused", Name: "unused", Created: expires}, {ID: "fixture-used", Name: "used", Created: expires, LastUsed: &last, Expires: &expires, Enabled: true}}
		profile.Clients = mcpClientsData{Clients: []mcpClientData{{ID: "client-unused", Name: "unused"}, {ID: "client-used", Name: "used", LastUsed: "last-datetime", LastUsedElapsed: last.Elapsed, Expired: true}}}
		execute("page", authPageData{Profile: &profile})
		execute("chrome", authPageData{Profile: &profile})
		execute("tokenList", profile.Rows)
		execute("tokenTime", expires)
		execute("tokenLastUsed", last)
		execute("mcp-clients", profile.Clients)
	}
	for _, nameError := range []bool{false, true} {
		for _, expiryError := range []bool{false, true} {
			create := tokenCreateData{Name: "submitted-name", Expiry: "90d", Rejected: true, NameError: nameError, ExpiryError: expiryError}
			execute("page", authPageData{Create: &create})
			execute("chrome", authPageData{Create: &create})
			execute("tokenCreate", create)
		}
	}
	created := tokenCreatedData{Name: "created-name", Secret: "fixture-secret"}
	execute("page", authPageData{Created: &created})
	execute("chrome", authPageData{Created: &created})
	execute("tokenCreated", created)
	execute("value", "fixture\x00value")
	execute("elapsed", elapsedData{Unit: "now"})
	for _, unit := range []string{"minute", "hour", "day"} {
		for _, count := range []int64{1, 2} {
			execute("elapsed", elapsedData{Unit: unit, Count: count})
		}
	}
	for _, name := range []string{"tokenNever", "plusIcon", "copyIcon"} {
		execute(name, nil)
	}
	// D09's declared approve data is compatible with the same set.
	execute("approve", map[string]any{"Banner": page.Banner{}, "ClientName": "fixture-client", "Gateway": "gateway.test", "Until": "fixture-until", "ReturnHost": "return.test", "ClientID": "fixture-id", "RedirectURI": "https://return.test/", "Challenge": "fixture-challenge", "State": "fixture-state", "Resource": "https://gateway.test/"})
}

func TestValueTemplatePreservesContextualEscaping(t *testing.T) {
	// R-6VOM-3PGI R-6WWI-HH77
	assertAuthTemplate(t, "fixture-value", "value", "fixture-value")
	split := func(fn func(string) []string) func(string) []string { return fn }(splitNUL)
	isolated, err := template.New("").Funcs(template.FuncMap{"splitNUL": split}).Parse(pageValueTemplate)
	if err != nil {
		t.Fatal(err)
	}
	for _, defined := range isolated.Templates() {
		if defined.Name() != "" && defined.Name() != "value" {
			t.Fatalf("unexpected template %q", defined.Name())
		}
	}
	if isolated.Lookup("value") == nil {
		t.Fatal("value missing")
	}
	for _, value := range []string{"", "plain", "<&\"'", "\x00", "a\x00\x00b", "<a\x00&b\"\x00", "é\xff\x00z"} {
		pieces := strings.Split(value, "\x00")
		if !reflect.DeepEqual(split(value), pieces) {
			t.Fatalf("split %q = %#v, want %#v", value, split(value), pieces)
		}
		// Synthetic fixtures provide the two required escaping contexts without asserting any asset markup.
		for _, context := range []struct{ before, after string }{{"", ""}, {`<fixture data-fixture="`, `"></fixture>`}} {
			render := func(action, value string) string {
				t.Helper()
				set := expectedAuthTemplates(t)
				set, err = set.New("context-fixture").Parse(context.before + action + context.after)
				if err != nil {
					t.Fatal(err)
				}
				var out bytes.Buffer
				if err := set.ExecuteTemplate(&out, "context-fixture", value); err != nil {
					t.Fatal(err)
				}
				return out.String()[len(context.before) : out.Len()-len(context.after)]
			}
			wantPieces := make([]string, len(pieces))
			for i, piece := range pieces {
				wantPieces[i] = render("{{.}}", piece)
			}
			if got, want := render(`{{template "value" .}}`, value), strings.Join(wantPieces, "\x00"); got != want {
				t.Fatalf("context escaped value %q = %q, want %q", value, got, want)
			}
		}
	}
}
