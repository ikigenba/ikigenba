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
	return Banner{Service: "notes", Icon: template.HTML("fixture-icon<>&banner"), Email: "member@example.test", ProfileURL: "/profile", LogoutURL: "/logout", Services: []Service{
		{Name: "notes", URL: "/notes", Icon: template.HTML("fixture-icon<>&notes"), Enabled: true, Current: true},
		{Name: "calendar", URL: "/calendar", Icon: template.HTML("fixture-icon<>&calendar"), Enabled: false},
	}}
}

func TestTemplatesSignature(t *testing.T) {
	// R-I900-VFBX
	if _, ok := any(Templates).(func() *template.Template); !ok {
		t.Fatalf("Templates has type %T, want func() *template.Template", Templates)
	}
}

func TestTemplatesEmbeddedDefinitions(t *testing.T) {
	// R-55GA-E3MI
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
	for _, name := range []string{"banner", "launcher", "footer", "preload"} {
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
	// R-56O6-RVD7
	set := Templates()
	allowed := []string{"appkit", "banner", "launcher", "footer", "preload"}
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
	if _, err := first.Parse(`{{define "consumer"}}custom{{end}}{{define "banner"}}changed{{end}}{{define "launcher"}}other{{end}}`); err != nil {
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
	// R-57W3-5N3W
	const page = `{{define "page"}}{{template "banner" .}}{{template "launcher" .}}{{template "footer" .}}{{template "preload"}}{{end}}`
	data := templateFixture()
	want := templateExecute(t, Templates(), "banner", data) + templateExecute(t, Templates(), "launcher", data) + templateExecute(t, Templates(), "footer", data) + templateExecute(t, Templates(), "preload", nil)
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
				t.Fatal("consumer did not render banner, launcher, footer, and preload")
			}
		})
	}
}

func TestBannerInvokesLauncherOnce(t *testing.T) {
	// R-IL70-P4QV
	data := templateFixture()
	data.Version = "consumer-build"
	set := Templates()
	if _, err := set.Parse(`{{define "launcher"}}sentinel:{{.Service}}/{{.Version}}/{{.Email}}/{{.ProfileURL}}/{{.LogoutURL}}{{range .Services}}/{{.Name}}/{{.URL}}/{{.Enabled}}/{{.Current}}/{{.Icon}}{{end}}:end{{end}}`); err != nil {
		t.Fatal(err)
	}
	launcher := templateExecute(t, set, "launcher", data)
	if got := templateExecute(t, set, "banner", data); strings.Count(got, launcher) != 1 {
		t.Fatal("launcher must execute once with identical data")
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
	// R-CHNB-5CO9 R-CIV7-J4EY R-CK33-WW5N R-CLB0-ANWC
	for _, services := range [][]Service{nil, templateFixture().Services} {
		data := templateFixture()
		data.Services = services
		data.Service = "consumer<service>&\"'"
		data.Email = "consumer<email>&\"'"
		output := templateExecute(t, Templates(), "banner", data)
		templateContains(t, output, templateText(t, data.Service), string(data.Icon), data.ProfileURL, templateText(t, data.Email), data.LogoutURL)
	}
}

func TestBannerScript(t *testing.T) {
	// R-CMIW-OFN1
	data := templateFixture()
	if output := templateExecute(t, Templates(), "banner", data); strings.Count(output, StaticPrefix+"launcher.js") != 1 {
		t.Fatal("launcher script URL must occur once")
	}
}

func TestBannerEmptyServices(t *testing.T) {
	// R-CNQT-27DQ
	for _, services := range [][]Service{nil, {}} {
		data := templateFixture()
		data.Services = services
		output := templateExecute(t, Templates(), "banner", data)
		launcher := templateExecute(t, Templates(), "launcher", data)
		if strings.Contains(output, launcher) || strings.Contains(output, StaticPrefix+"launcher.js") {
			t.Fatal("empty services emitted launcher output or script URL")
		}
		templateContains(t, output, data.Service, string(data.Icon), data.ProfileURL, data.Email, data.LogoutURL)
	}
}

func TestLauncherValues(t *testing.T) {
	// R-COYP-FZ4F R-IZTT-ADN7 R-CQ6L-TQV4 R-CREI-7ILT
	for _, current := range []bool{false, true} {
		data := Banner{Services: []Service{
			{Name: "first<name>&\"'", Icon: template.HTML("first<icon>&"), URL: "/first-consumer", Enabled: true, Current: current},
			{Name: "second<name>&\"'", Icon: template.HTML("second<icon>&"), URL: "/second-consumer", Current: current},
			{Name: "third<name>&\"'", Icon: template.HTML("third<icon>&"), URL: "/third-consumer", Enabled: true, Current: current},
		}}
		output := templateExecute(t, Templates(), "launcher", data)
		remaining := output
		for _, service := range data.Services {
			var found bool
			_, remaining, found = strings.Cut(remaining, string(service.Icon))
			if !found {
				t.Fatal("missing unaltered icon in service order")
			}
			_, remaining, found = strings.Cut(remaining, templateText(t, service.Name))
			if !found {
				t.Fatal("missing escaped name after icon")
			}
			if strings.Contains(output, service.URL) != service.Enabled {
				t.Fatalf("URL presence differs from enabled state: %+v", service)
			}
		}
	}
}

func TestTemplatesAutoescaping(t *testing.T) {
	// R-CSME-LACI
	const special = "<> &\"'+\x00"
	for _, url := range []string{"https://consumer.test/path?q=one&next=two", "javascript:consumer(1)"} {
		data := Banner{Service: "service" + special, Version: "version" + special, Email: "email" + special, ProfileURL: url, LogoutURL: url,
			Services: []Service{{Name: "enabled" + special, URL: url, Enabled: true}, {Name: "disabled" + special, URL: "/not-emitted"}}}
		banner := templateExecute(t, Templates(), "banner", data)
		launcher := templateExecute(t, Templates(), "launcher", data)
		footer := templateExecute(t, Templates(), "footer", data)
		templateContains(t, banner, templateText(t, data.Service), templateText(t, data.Email))
		for _, service := range data.Services {
			templateContains(t, launcher, templateText(t, service.Name))
		}
		templateContains(t, footer, templateText(t, data.Service), templateText(t, data.Version))
		wantURL := templateText(t, url)
		if strings.HasPrefix(url, "javascript:") {
			wantURL = "#ZgotmplZ"
			if strings.Contains(banner, url) || strings.Contains(launcher, url) {
				t.Fatal("unsafe URL emitted")
			}
		}
		for _, field := range []string{"ProfileURL", "LogoutURL", "ServiceURL"} {
			one := Banner{Services: []Service{{Enabled: true}}}
			switch field {
			case "ProfileURL":
				one.ProfileURL = url
			case "LogoutURL":
				one.LogoutURL = url
			case "ServiceURL":
				one.Services[0].URL = url
			}
			templateContains(t, templateExecute(t, Templates(), "banner", one), wantURL)
		}
	}
}

func TestFooter(t *testing.T) {
	// R-CTUA-Z237
	data := Banner{Service: "consumer<service>&\"'", Version: "consumer<version>&\"'"}
	output := templateExecute(t, Templates(), "footer", data)
	_, remaining, found := strings.Cut(output, templateText(t, data.Service))
	if !found || !strings.Contains(remaining, templateText(t, data.Version)) {
		t.Fatal("footer must emit escaped service then version")
	}
}

func TestPreloadTemplate(t *testing.T) {
	// R-CV27-CTTW
	set := template.Must(Templates().Parse(`{{define "head"}}{{template "preload"}}{{end}}`))
	templateContains(t, templateExecute(t, set, "head", nil), PreloadURL())
}
