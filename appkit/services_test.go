package appkit_test

import (
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit"
)

func servicesWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func servicesKit(t *testing.T, content string) *appkit.Kit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "services.json")
	servicesWrite(t, path, content)
	t.Setenv("IKIGENBA_SERVICES", path)
	return appkit.New("app")
}

func TestServicesPublicTypes(t *testing.T) {
	// R-69LO-BOP5 R-6ATK-PGFU R-6D9D-GZX8
	for _, tc := range []struct {
		value any
		names []string
		types []reflect.Type
	}{
		{appkit.User{}, []string{"Email", "ProfileURL", "LogoutURL"}, []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[string](), reflect.TypeFor[string]()}},
		{appkit.Service{}, []string{"Name", "URL", "Icon", "Enabled", "Current"}, []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[string](), reflect.TypeFor[template.HTML](), reflect.TypeFor[bool](), reflect.TypeFor[bool]()}},
		{appkit.Banner{}, []string{"Service", "Email", "ProfileURL", "LogoutURL", "Services"}, []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[string](), reflect.TypeFor[string](), reflect.TypeFor[string](), reflect.TypeFor[[]appkit.Service]()}},
	} {
		typ := reflect.TypeOf(tc.value)
		if typ.Kind() != reflect.Struct || typ.NumField() != len(tc.names) {
			t.Fatalf("unexpected fields in %v", typ)
		}
		for i, name := range tc.names {
			field := typ.Field(i)
			if field.Name != name || field.Type != tc.types[i] || !field.IsExported() {
				t.Fatalf("%v field %d: %+v", typ, i, field)
			}
		}
	}
	// R-6EH9-URNX
	if reflect.TypeOf(appkit.New) != reflect.TypeFor[func(string) *appkit.Kit]() {
		t.Fatal("unexpected constructor signature")
	}
	if reflect.TypeOf((*appkit.Kit).Banner) != reflect.TypeFor[func(*appkit.Kit, appkit.User) appkit.Banner]() {
		t.Fatal("unexpected Banner method signature")
	}
	typ := reflect.TypeFor[appkit.Kit]()
	if typ.Kind() != reflect.Struct {
		t.Fatal("Kit is not a struct")
	}
	for i := range typ.NumField() {
		if typ.Field(i).IsExported() {
			t.Fatal("exported Kit field")
		}
	}
}

func TestServicesBannerValues(t *testing.T) {
	// R-6GX2-MB5B
	t.Setenv("IKIGENBA_SERVICES", "")
	u := appkit.User{Email: "  person+tag@example.test ", ProfileURL: "?profile=<>&", LogoutURL: "../exit?x=1"}
	banner := appkit.New(" App ").Banner(u)
	if banner.Service != " App " || banner.Email != u.Email || banner.ProfileURL != u.ProfileURL || banner.LogoutURL != u.LogoutURL {
		t.Fatalf("values altered: %+v", banner)
	}
}

func TestServicesEnvironmentSnapshot(t *testing.T) {
	// R-6FP6-8JEM
	kit := servicesKit(t, `{"services":[{"name":"first","url":"","icon":"","enabled":true}]}`)
	second := filepath.Join(t.TempDir(), "other.json")
	servicesWrite(t, second, `{"services":[{"name":"second","url":"","icon":"","enabled":false}]}`)
	for _, path := range []string{second, ""} {
		t.Setenv("IKIGENBA_SERVICES", path)
		if got := kit.Banner(appkit.User{}).Services; len(got) != 1 || got[0].Name != "first" {
			t.Fatalf("snapshot changed: %+v", got)
		}
	}
	if err := os.Unsetenv("IKIGENBA_SERVICES"); err != nil {
		t.Fatal(err)
	}
	if got := kit.Banner(appkit.User{}).Services; len(got) != 1 || got[0].Name != "first" {
		t.Fatalf("unset changed snapshot: %+v", got)
	}
	empty := appkit.New("app")
	t.Setenv("IKIGENBA_SERVICES", second)
	if len(empty.Banner(appkit.User{}).Services) != 0 {
		t.Fatal("later environment setting affected kit")
	}
}

func TestServicesFreshReadAndRelativePath(t *testing.T) {
	// R-6I4Z-02W0
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("IKIGENBA_SERVICES", "services.json")
	kit := appkit.New("app")
	if len(kit.Banner(appkit.User{}).Services) != 0 {
		t.Fatal("missing file yielded services")
	}
	path := filepath.Join(dir, "services.json")
	for _, name := range []string{"appeared", "changed"} {
		servicesWrite(t, path, fmt.Sprintf(`{"services":[{"name":%q,"url":"","icon":"","enabled":true}]}`, name))
		if got := kit.Banner(appkit.User{}).Services; len(got) != 1 || got[0].Name != name {
			t.Fatalf("file change not reflected: %+v", got)
		}
	}
	secondDir := t.TempDir()
	servicesWrite(t, filepath.Join(secondDir, "services.json"), `{"services":[{"name":"other-directory","url":"","icon":"","enabled":true}]}`)
	t.Chdir(secondDir)
	if got := kit.Banner(appkit.User{}).Services; len(got) != 1 || got[0].Name != "other-directory" {
		t.Fatalf("relative path was frozen: %+v", got)
	}
	if err := os.Remove(filepath.Join(secondDir, "services.json")); err != nil {
		t.Fatal(err)
	}
	if len(kit.Banner(appkit.User{}).Services) != 0 {
		t.Fatal("removed file retained services")
	}
}

func TestServicesPathAsGiven(t *testing.T) {
	// R-6I4Z-02W0
	path := filepath.Join(t.TempDir(), "services.json")
	servicesWrite(t, path, `{"services":[{"name":"app","url":"","icon":"","enabled":true}]}`)
	t.Setenv("IKIGENBA_SERVICES", path+"/")
	if got := appkit.New("app").Banner(appkit.User{}).Services; len(got) != 0 {
		t.Fatalf("trailing slash was removed from the file path: %+v", got)
	}
}

func servicesInvalidDocuments() []string {
	usable := `{"name":"app","url":"","icon":"","enabled":true}`
	return []string{"", "{", "\xff", "\xef\xbb\xbf{}", `{} {}`, `{} trailing`, `[]`, `null`, `true`, `"object"`, `42`, `{}`, `{"Services":[]}`, `{"services":null}`, `{"services":{}}`, `{"services":"[]"}`, `{"services":false}`, `{"services":[]}`, `{"services":[null,{},7]}`,
		`{"services":[` + usable + `],"unknown":"` + "\xff" + `"}`,
		`{"services":[{"name":"` + "\xff" + `","url":"","icon":"","enabled":true}]}`,
	}
}

func TestServicesEmptyFallback(t *testing.T) {
	// R-Z24E-CK72
	for _, content := range servicesInvalidDocuments() {
		t.Run(fmt.Sprintf("%q", content), func(t *testing.T) {
			if got := servicesKit(t, content).Banner(appkit.User{}).Services; len(got) != 0 {
				t.Fatalf("invalid document yielded %+v", got)
			}
		})
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		t.Setenv("IKIGENBA_SERVICES", path)
		if len(appkit.New("app").Banner(appkit.User{}).Services) != 0 {
			t.Fatalf("unreadable path %q yielded services", path)
		}
	}
	if err := os.Unsetenv("IKIGENBA_SERVICES"); err != nil {
		t.Fatal(err)
	}
	if len(appkit.New("app").Banner(appkit.User{}).Services) != 0 {
		t.Fatal("unset path yielded services")
	}
}

func servicesInvalidEntries() []string {
	entries := []string{`null`, `1`, `false`, `"entry"`, `[]`, `{}`, `{"name":"","url":"","icon":"","enabled":true}`}
	valid := map[string]any{"name": "name", "url": "", "icon": "", "enabled": true}
	for _, field := range []string{"name", "url", "icon", "enabled"} {
		for _, replacement := range []string{"", "null", "[]", "{}", "123", `"wrong"`, "true", "false"} {
			if field != "enabled" && (replacement == `"wrong"`) || field == "enabled" && (replacement == "true" || replacement == "false") {
				continue
			}
			parts := []string{}
			for _, member := range []string{"name", "url", "icon", "enabled"} {
				if member == field {
					if replacement != "" {
						parts = append(parts, fmt.Sprintf("%q:%s", member, replacement))
					}
				} else {
					parts = append(parts, fmt.Sprintf("%q:%#v", member, valid[member]))
				}
			}
			entries = append(entries, "{"+strings.Join(parts, ",")+"}")
		}
	}
	entries = append(entries, `{"Name":"name","url":"","icon":"","enabled":true}`, `{"name":"name","URL":"","icon":"","enabled":true}`, `{"name":"name","url":"","Icon":"","enabled":true}`, `{"name":"name","url":"","icon":"","Enabled":true}`)
	return entries
}

func TestServicesElementValidation(t *testing.T) {
	// R-6KKR-RMDE
	for _, entry := range servicesInvalidEntries() {
		t.Run(entry, func(t *testing.T) {
			content := `{"services":[{"name":"before","url":"","icon":"","enabled":true},` + entry + `,{"name":"after","url":"","icon":"","enabled":false}]}`
			got := servicesKit(t, content).Banner(appkit.User{}).Services
			if len(got) != 2 || got[0].Name != "before" || got[1].Name != "after" {
				t.Fatalf("invalid entry affected usable entries: %+v", got)
			}
		})
	}
}

func TestServicesUnknownAndDuplicateMembers(t *testing.T) {
	// R-Z4K7-43OG
	base := `{"name":"app","url":"/url","icon":"icon","enabled":true}`
	expected := []appkit.Service{{Name: "app", URL: "/url", Icon: "icon", Enabled: true, Current: true}}
	for _, extra := range []string{`null`, `true`, `123`, `"text"`, `[]`, `{"nested":[false]}`} {
		content := `{"unknown":` + extra + `,"services":[` + strings.TrimSuffix(base, "}") + `,"unknown":` + extra + `}]}`
		if got := servicesKit(t, content).Banner(appkit.User{}).Services; !reflect.DeepEqual(got, expected) {
			t.Fatalf("unknown member changed services: %+v", got)
		}
	}
	for _, content := range []string{
		`{"services":false,"services":[` + base + `]}`,
		`{"services":[{"name":null,"name":"app","url":false,"url":"/url","icon":[],"icon":"icon","enabled":null,"enabled":true}]}`,
	} {
		if got := servicesKit(t, content).Banner(appkit.User{}).Services; !reflect.DeepEqual(got, expected) {
			t.Fatalf("last member not used: %+v", got)
		}
	}
	for _, content := range []string{`{"services":[` + base + `],"services":null}`, `{"services":[{"name":"app","name":null,"url":"/url","icon":"icon","enabled":true}]}`} {
		if got := servicesKit(t, content).Banner(appkit.User{}).Services; len(got) != 0 {
			t.Fatalf("earlier member used: %+v", got)
		}
	}
}

func TestServicesDecodedOrderAndCurrent(t *testing.T) {
	// R-6N0K-J5US
	got := servicesKit(t, `{"services":[{"name":"app","url":"/a?x=1&y=2","icon":"<svg>\n&\"é</svg>","enabled":false},{"name":"APP","url":"","icon":"","enabled":true},{"name":"app","url":"different","icon":"other","enabled":true},{"name":" ","url":"","icon":"","enabled":false}]}`).Banner(appkit.User{}).Services
	expected := []appkit.Service{
		{Name: "app", URL: "/a?x=1&y=2", Icon: template.HTML("<svg>\n&\"é</svg>"), Current: true},
		{Name: "APP", Enabled: true},
		{Name: "app", URL: "different", Icon: "other", Enabled: true, Current: true},
		{Name: " "},
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("decoded services differ: got %+v; want %+v", got, expected)
	}
}

func TestServicesSilentFailures(t *testing.T) {
	// R-Z5S3-HVF5
	capture, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := capture.Close(); err != nil {
			t.Error(err)
		}
	}()
	stdout, stderr, logger := os.Stdout, os.Stderr, log.Writer()
	os.Stdout, os.Stderr = capture, capture
	log.SetOutput(capture)
	defer func() { os.Stdout, os.Stderr = stdout, stderr; log.SetOutput(logger) }()
	contents := append(servicesInvalidDocuments(), `{"services":[`+strings.Join(servicesInvalidEntries(), ",")+`]}`)
	for _, content := range contents {
		_ = servicesKit(t, content).Banner(appkit.User{})
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "absent"), t.TempDir()} {
		t.Setenv("IKIGENBA_SERVICES", path)
		_ = appkit.New("app").Banner(appkit.User{})
	}
	if err := os.Unsetenv("IKIGENBA_SERVICES"); err != nil {
		t.Fatal(err)
	}
	_ = appkit.New("app").Banner(appkit.User{})
	info, err := capture.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatal("reader wrote output")
	}
}

func TestServicesZeroKit(t *testing.T) {
	// R-6PGD-APC6
	t.Setenv("IKIGENBA_SERVICES", "")
	if err := os.Unsetenv("IKIGENBA_SERVICES"); err != nil {
		t.Fatal(err)
	}
	u := appkit.User{Email: "email", ProfileURL: "profile", LogoutURL: "logout"}
	var zero appkit.Kit
	got, expected := zero.Banner(u), appkit.New("").Banner(u)
	if !reflect.DeepEqual(got, expected) || got.Service != "" || len(got.Services) != 0 {
		t.Fatalf("zero Kit differs: %+v versus %+v", got, expected)
	}
}

func TestServicesConcurrentBanner(t *testing.T) {
	// R-6QO9-OH2V
	kit := servicesKit(t, `{"services":[{"name":"app","url":"/","icon":"<svg/>","enabled":true}]}`)
	expected := kit.Banner(appkit.User{Email: "person"})
	results := make(chan appkit.Banner, 64)
	var workers sync.WaitGroup
	for range 64 {
		workers.Go(func() { results <- kit.Banner(appkit.User{Email: "person"}) })
	}
	workers.Wait()
	close(results)
	for got := range results {
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("concurrent result differs: %+v", got)
		}
	}
}
