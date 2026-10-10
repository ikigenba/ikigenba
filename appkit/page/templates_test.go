package page

import (
	"bytes"
	"html/template"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func templateExecute(t *testing.T, set *template.Template, name string, data any) string {
	t.Helper()
	var output bytes.Buffer
	if err := set.ExecuteTemplate(&output, name, data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func templateFixture() Banner {
	return Banner{Service: "notes", Icon: template.HTML("fixture-icon<>&banner"), Release: "consumer-release", Commit: "consumer-commit", Email: "member@example.test", ProfileURL: "/profile", LogoutURL: "/logout"}
}

func TestTemplatesSignature(t *testing.T) {
	// R-I900-VFBX
	if _, ok := any(Templates).(func() *template.Template); !ok {
		t.Fatalf("Templates has type %T, want func() *template.Template", Templates)
	}
}

func TestTemplatesEmbeddedDefinitions(t *testing.T) {
	// R-J8UH-3M49
	markup, err := assetsFS.ReadFile("assets/banner.html")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := template.New("reference").Funcs(template.FuncMap{"preloadURL": PreloadURL}).Parse(string(markup))
	if err != nil {
		t.Fatal(err)
	}
	actual := Templates()
	if actual == nil {
		t.Fatal("nil set")
	}
	for _, name := range []string{"banner", "footer", "preload"} {
		if actual.Lookup(name) == nil {
			t.Fatalf("missing %s", name)
		}
		for _, data := range []Banner{{}, templateFixture()} {
			if got, want := templateExecute(t, actual, name, data), templateExecute(t, expected, name, data); got != want {
				t.Fatalf("%s differs from embedded asset", name)
			}
		}
	}
}

func TestTemplatesNames(t *testing.T) {
	// R-JA2D-HDUY
	set := Templates()
	allowed := []string{"appkit", "banner", "footer", "preload"}
	// R-YHYW-CTC1
	if set.Name() != "appkit" {
		t.Fatalf("root name %q", set.Name())
	}
	for _, member := range set.Templates() {
		if !slices.Contains(allowed, member.Name()) {
			t.Fatalf("template name %q", member.Name())
		}
	}
}

func TestTemplatesIndependent(t *testing.T) {
	// R-ICNQ-0QK0
	first, second := Templates(), Templates()
	if first == second {
		t.Fatal("shared set")
	}
	data := templateFixture()
	baseline := templateExecute(t, second, "banner", data)
	if _, err := first.Parse(`{{define "consumer"}}custom{{end}}{{define "banner"}}changed{{end}}`); err != nil {
		t.Fatal(err)
	}
	templateExecute(t, first, "banner", data)
	if second.Lookup("consumer") != nil {
		t.Fatal("consumer definition leaked")
	}
	if got := templateExecute(t, second, "banner", data); got != baseline {
		t.Fatal("redefinition or execution affected other set")
	}
	third := Templates()
	if _, err := third.Parse(`{{define "later"}}ok{{end}}`); err != nil {
		t.Fatalf("other execution made fresh set unparseable: %v", err)
	}
	if got := templateExecute(t, third, "banner", data); got != baseline {
		t.Fatal("future set affected")
	}
}

func TestTemplatesConsumerParse(t *testing.T) {
	// R-JBA9-V5LN
	const page = `{{define "page"}}{{template "banner" .}}{{template "footer" .}}{{template "preload"}}{{end}}`
	data := templateFixture()
	want := templateExecute(t, Templates(), "banner", data) + templateExecute(t, Templates(), "footer", data) + templateExecute(t, Templates(), "preload", nil)
	for _, mode := range []string{"Parse", "ParseFS"} {
		t.Run(mode, func(t *testing.T) {
			set := Templates()
			var err error
			if mode == "Parse" {
				_, err = set.Parse(page)
			} else {
				_, err = set.ParseFS(fstest.MapFS{"pages/page.html": &fstest.MapFile{Data: []byte(page)}}, "pages/*.html")
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := templateExecute(t, set, "page", data); got != want {
				t.Fatal("consumer did not render banner, footer, and preload")
			}
		})
	}
}

func TestTemplatesPreloadURLFunction(t *testing.T) {
	// R-5ABV-X6LA
	for _, mode := range []string{"Parse", "ParseFS"} {
		set := Templates()
		const markup = `{{define "consumerURL"}}{{preloadURL}}{{end}}`
		var err error
		if mode == "Parse" {
			_, err = set.Parse(markup)
		} else {
			_, err = set.ParseFS(fstest.MapFS{"consumer.html": &fstest.MapFile{Data: []byte(markup)}}, "consumer.html")
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := templateExecute(t, set, "consumerURL", nil); got != PreloadURL() {
			t.Fatalf("preloadURL = %q, want %q", got, PreloadURL())
		}
	}
}

func templateContains(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Fatalf("output does not contain supplied value %q: %q", value, output)
		}
	}
}

func templateText(t *testing.T, value string) string {
	t.Helper()
	set := template.Must(template.New("escape").Parse("{{.}}"))
	return templateExecute(t, set, "escape", value)
}

func TestBannerValues(t *testing.T) {
	// R-JDQ2-MP31 R-CIV7-J4EY R-JEXZ-0GTQ R-JG5V-E8KF
	data := templateFixture()
	data.Service = "consumer<service>&\"'"
	data.Email = "consumer<email>&\"'"
	output := templateExecute(t, Templates(), "banner", data)
	templateContains(t, output, templateText(t, data.Service), string(data.Icon), data.ProfileURL, templateText(t, data.Email), data.LogoutURL)
}

func TestTemplatesAutoescaping(t *testing.T) {
	// R-JCI6-8XCC
	const special = "<> &\"'+\x00"
	for _, url := range []string{"https://consumer.test/path?q=one&next=two", "https://consumer.test/path?q=<>\"'", "javascript:consumer(1)"} {
		data := Banner{Service: "service" + special, Release: "release" + special, Commit: "commit" + special, Email: "email" + special, ProfileURL: url, LogoutURL: url}
		banner := templateExecute(t, Templates(), "banner", data)
		footer := templateExecute(t, Templates(), "footer", data)
		templateContains(t, banner, templateText(t, data.Service), templateText(t, data.Email))
		templateContains(t, footer, templateText(t, data.Release), templateText(t, data.Commit))
		wantURL := templateText(t, url)
		if strings.HasPrefix(url, "javascript:") {
			wantURL = "#ZgotmplZ"
		} else if strings.Contains(url, "<>") {
			wantURL = "https://consumer.test/path?q=%3c%3e%22%27"
		}
		for _, field := range []string{"ProfileURL", "LogoutURL"} {
			one := Banner{}
			switch field {
			case "ProfileURL":
				one.ProfileURL = url
			case "LogoutURL":
				one.LogoutURL = url
			}
			output := templateExecute(t, Templates(), "banner", one)
			templateContains(t, output, wantURL)
			if strings.Contains(output, url) {
				t.Fatal("URL requiring escaping emitted unchanged")
			}
		}
	}
}

func TestFooterValues(t *testing.T) {
	// R-JL1G-XBJ7
	for _, release := range []string{"", "consumer<release>&\"'"} {
		for _, commit := range []string{"", "consumer<commit>&\"'"} {
			data := Banner{Release: release, Commit: commit}
			output := templateExecute(t, Templates(), "footer", data)
			for _, value := range []string{release, commit} {
				if value != "" {
					templateContains(t, output, templateText(t, value))
				}
			}
		}
	}
}

func TestFooterValueOrder(t *testing.T) {
	// R-JM9D-B39W
	data := Banner{Release: "consumer-release.value", Commit: "consumer-commit.value"}
	baseline := templateExecute(t, Templates(), "footer", Banner{})
	if strings.Contains(baseline, data.Release) || strings.Contains(baseline, data.Commit) || strings.Contains(data.Release, data.Commit) || strings.Contains(data.Commit, data.Release) {
		t.Fatal("fixture fails ordering preconditions")
	}
	output := templateExecute(t, Templates(), "footer", data)
	release, commit := strings.Index(output, data.Release), strings.Index(output, data.Commit)
	if release < 0 || commit <= release {
		t.Fatal("footer must emit release then commit")
	}
}

func TestFooterOtherFieldsDoNotAffectOutput(t *testing.T) {
	// R-JNH9-OV0L
	for _, release := range []string{"", "consumer-release"} {
		for _, commit := range []string{"", "consumer-commit"} {
			first := Banner{Release: release, Commit: commit}
			second := Banner{Service: "consumer-service", Icon: template.HTML("consumer-icon<>&"), Release: release, Commit: commit, Email: "consumer-email", ProfileURL: "/consumer-profile", LogoutURL: "/consumer-logout", Home: "/consumer-home", Tools: true, Trail: []Level{{Name: "consumer-level", URL: "/consumer-level"}}}
			if templateExecute(t, Templates(), "footer", first) != templateExecute(t, Templates(), "footer", second) {
				t.Fatal("other Banner fields affect footer")
			}
		}
	}
}

func TestPreloadTemplate(t *testing.T) {
	// R-CV27-CTTW
	set := template.Must(Templates().Parse(`{{define "head"}}{{template "preload"}}{{end}}`))
	templateContains(t, templateExecute(t, set, "head", nil), PreloadURL())
}
