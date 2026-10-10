package services_test

import (
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/services"
)

const validEntry = `{"name":"alpha","url":"/alpha","description":"Alpha service","socket":"alpha.sock","enabled":true,"mcp":false}`

func fixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "services.json")
	writeFile(t, path, content)
	t.Setenv(services.Variable, path)
	return os.Getenv(services.Variable)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, content string) services.List {
	t.Helper()
	list, err := services.Read(fixture(t, content))
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPublicReader(t *testing.T) {
	// R-HN1T-ZJZF R-JKK3-SH90 R-JMZW-K0QE R-JPFP-BK7S R-JQNL-PBYH
	const variable string = services.Variable
	if variable != "IKIGENBA_SERVICES" {
		t.Fatalf("variable = %q", variable)
	}
	list, err := services.Read(fixture(t, `{"services":[`+validEntry+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	var entries []services.Entry = list
	entry, ok := services.List(entries).Find("alpha")
	if !ok || entry.Name != "alpha" {
		t.Fatalf("Find = %#v, %v", entry, ok)
	}
}

func TestReadRefreshesPath(t *testing.T) {
	// R-JRVI-33P6
	dir := t.TempDir()
	path := filepath.Join(dir, "services.json")
	t.Setenv(services.Variable, path)
	if list, err := services.Read(path); list != nil || err == nil {
		t.Fatalf("absent file: %#v, %v", list, err)
	}
	writeFile(t, path, `{"services":[`+validEntry+`]}`)
	if list, err := services.Read(path); err != nil || len(list) != 1 {
		t.Fatalf("appeared file: %#v, %v", list, err)
	}
	writeFile(t, path, `{"services":[]}`)
	if list, err := services.Read(path); err != nil || list == nil || len(list) != 0 {
		t.Fatalf("changed file: %#v, %v", list, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if list, err := services.Read(path); list != nil || err == nil {
		t.Fatalf("removed file: %#v, %v", list, err)
	}
	first, second := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(first, "relative.json"), `{"services":[`+validEntry+`]}`)
	writeFile(t, filepath.Join(second, "relative.json"), `{"services":[]}`)
	t.Chdir(first)
	if list, err := services.Read("relative.json"); err != nil || len(list) != 1 {
		t.Fatalf("first directory: %#v, %v", list, err)
	}
	t.Chdir(second)
	if list, err := services.Read("relative.json"); err != nil || len(list) != 0 {
		t.Fatalf("second directory: %#v, %v", list, err)
	}
}

func TestReadFileErrors(t *testing.T) {
	// R-6VB6-GFYF R-LE5S-3K72
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "unreadable")
	writeFile(t, unreadable, `{"services":[]}`)
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "absent")
	brokenLink := filepath.Join(dir, "broken")
	if err := os.Symlink(missing, brokenLink); err != nil {
		t.Fatal(err)
	}
	directoryLink := filepath.Join(dir, "directory-link")
	if err := os.Symlink(dir, directoryLink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", dir, unreadable, fifo, missing, brokenLink, directoryLink} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			list, err := services.Read(path)
			if list != nil || err == nil {
				t.Fatalf("Read(%q) = %#v, %v", path, list, err)
			}
			if (path == missing || path == brokenLink) && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("missing path error = %v", err)
			}
		})
	}
	regular := fixture(t, `{"services":[]}`)
	for _, path := range []string{
		dir + "//absent", dir + "/./absent", dir + "/named/../absent", dir + "/",
		filepath.Dir(regular) + "//services.json",
		filepath.Dir(regular) + "/./services.json",
	} {
		list, err := services.Read(path)
		if list != nil || err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("unclean path %q = %#v, %v", path, list, err)
		}
	}
	link := filepath.Join(dir, "regular-link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if list, err := services.Read(link); err != nil || list == nil {
		t.Fatalf("regular symlink: %#v, %v", list, err)
	}
	linkedDirectory := filepath.Join(dir, "linked-directory")
	if err := os.Symlink(filepath.Dir(regular), linkedDirectory); err != nil {
		t.Fatal(err)
	}
	list, err := services.Read(linkedDirectory + "/../services.json")
	if list != nil || err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("symlink parent path = %#v, %v", list, err)
	}
}

func TestReadDocumentErrors(t *testing.T) {
	// R-JVJ7-8EX9
	for _, content := range []string{
		"", "\xff", "\xef\xbb\xbf" + `{"services":[]}`, `{"services":[`,
		`{"services":[]} {}`, `{"services":[]} trailing`, `{"services":[],}`,
		`{"services":[NaN]}`, `{"services":[01]}`, `[]`, `null`, `true`, `1`, `"object"`,
		`{}`, `{"services":null}`, `{"services":{}}`, `{"services":true}`, `{"services":0}`, `{"services":"[]"}`,
		`{"services":[],"extra":"` + string([]byte{0xff}) + `"}`,
	} {
		t.Run(content, func(t *testing.T) {
			list, err := services.Read(fixture(t, content))
			if list != nil || err == nil {
				t.Fatalf("invalid document: %#v, %v", list, err)
			}
		})
	}
}

func TestUsableEntries(t *testing.T) {
	// R-JWR3-M6NY
	for _, value := range []string{`null`, `true`, `false`, `1`, `"entry"`, `[]`, `{}`} {
		assertSkipped(t, value)
	}
	for _, key := range []string{"name", "url", "description", "socket", "enabled", "mcp"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(validEntry), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, key)
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		assertSkipped(t, string(encoded))
		for _, wrong := range []string{`null`, `0`, `[]`, `{}`, `true`, `false`, `""`, `"value"`} {
			isBool := key == "enabled" || key == "mcp"
			if isBool && (wrong == "true" || wrong == "false") || !isBool && wrong == `"value"` || !isBool && key != "name" && wrong == `""` {
				continue
			}
			fields[key] = json.RawMessage(wrong)
			encoded, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			assertSkipped(t, string(encoded))
		}
	}
	list := readFile(t, `{"services":[{"name":" ","url":"","description":"","socket":"","enabled":false,"mcp":true}]}`)
	if len(list) != 1 || list[0].Name != " " || list[0].Enabled || !list[0].MCP {
		t.Fatalf("usable empty strings and boolean values: %#v", list)
	}
}

func assertSkipped(t *testing.T, entry string) {
	t.Helper()
	list := readFile(t, `{"services":[`+validEntry+`,`+entry+`,`+validEntry+`]}`)
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "alpha" {
		t.Fatalf("unusable entry %s affected usable entries: %#v", entry, list)
	}
}

func TestReadDecodedOrder(t *testing.T) {
	// R-6WJ2-U7P4
	list := readFile(t, `{"services":[{"name":"A\u03b1","url":"/a?x=1&y=2","description":"line\nnext","socket":"s\\t","enabled":false,"mcp":true},`+validEntry+`,`+validEntry+`]}`)
	want := []services.Entry{
		{Name: "Aα", URL: "/a?x=1&y=2", Description: "line\nnext", Socket: "s\\t", MCP: true},
		{Name: "alpha", URL: "/alpha", Description: "Alpha service", Socket: "alpha.sock", Enabled: true},
		{Name: "alpha", URL: "/alpha", Description: "Alpha service", Socket: "alpha.sock", Enabled: true},
	}
	if len(list) != len(want) {
		t.Fatalf("list length = %d", len(list))
	}
	for i, entry := range list {
		if entry.Name != want[i].Name || entry.URL != want[i].URL || entry.Description != want[i].Description ||
			entry.Socket != want[i].Socket || entry.Enabled != want[i].Enabled || entry.MCP != want[i].MCP {
			t.Fatalf("entry %d = %#v, want %#v", i, entry, want[i])
		}
	}
}

func TestReadIcons(t *testing.T) {
	// R-BCGP-K6RD
	for _, icon := range []string{`""`, `"<svg>\n\u03b1</svg>"`, `null`, `true`, `0`, `[]`, `{}`} {
		entry := strings.TrimSuffix(validEntry, "}") + `,"icon":` + icon + `}`
		list := readFile(t, `{"services":[`+entry+`]}`)
		if len(list) != 1 {
			t.Fatalf("optional icon %s removed entry", icon)
		}
		var expected template.HTML
		hasIcon := strings.HasPrefix(icon, `"`)
		if hasIcon {
			if err := json.Unmarshal([]byte(icon), &expected); err != nil {
				t.Fatal(err)
			}
		}
		if list[0].HasIcon != hasIcon || list[0].Icon != expected {
			t.Fatalf("icon %s: %#v", icon, list[0])
		}
	}
	list := readFile(t, `{"services":[`+validEntry+`]}`)
	if list[0].HasIcon || list[0].Icon != "" {
		t.Fatalf("absent icon: %#v", list[0])
	}
}

func TestEmptyList(t *testing.T) {
	// R-K0ES-RHW1
	for _, content := range []string{`{"services":[]}`, `{"services":[null,{},false,"x"]}`} {
		list := readFile(t, content)
		if list == nil || len(list) != 0 {
			t.Fatalf("empty list = %#v", list)
		}
	}
}

func TestReadGroups(t *testing.T) {
	// R-SHT9-7MU0
	for _, group := range []struct {
		value string
		want  string
	}{
		{`"core"`, "core"},
		{`"c\u006fre"`, "core"},
		{`"application"`, "application"},
		{`""`, "application"},
		{`"Core"`, "application"},
		{`"CORE"`, "application"},
		{`" core"`, "application"},
		{`"core "`, "application"},
		{`"future"`, "application"},
		{`null`, "application"},
		{`true`, "application"},
		{`false`, "application"},
		{`0`, "application"},
		{`[]`, "application"},
		{`{}`, "application"},
		{"", "application"},
	} {
		t.Run(group.value, func(t *testing.T) {
			entry := validEntry
			if group.value != "" {
				entry = strings.TrimSuffix(entry, "}") + `,"group":` + group.value + `}`
			}
			list := readFile(t, `{"services":[`+entry+`]}`)
			if len(list) != 1 || list[0].Group != group.want {
				t.Fatalf("group %s: %#v, want %q", group.value, list, group.want)
			}
		})
	}
}

func TestUnknownMembers(t *testing.T) {
	// R-SJ15-LEKP
	for _, known := range []string{validEntry, strings.TrimSuffix(validEntry, "}") + `,"icon":"icon","group":"core"}`} {
		baseline := readFile(t, `{"services":[`+known+`]}`)
		for _, extra := range []string{`null`, `true`, `false`, `1`, `"extra"`, `[]`, `{"nested":{"services":false,"group":"application","icon":null}}`} {
			entry := strings.TrimSuffix(known, "}") + `,"unknown":` + extra + `}`
			list := readFile(t, `{"unknown":`+extra+`,"services":[`+entry+`]}`)
			if len(list) != 1 || list[0] != baseline[0] {
				t.Fatalf("unknown member %s changed list: %#v", extra, list)
			}
		}
	}
}

func TestCaseSensitiveMembers(t *testing.T) {
	// R-SK91-Z6BE
	if list, err := services.Read(fixture(t, `{"Services":[]}`)); list != nil || err == nil {
		t.Fatalf("Services recognized: %#v, %v", list, err)
	}
	for _, key := range []string{"name", "url", "description", "socket", "enabled", "mcp"} {
		upper := strings.ToUpper(key[:1]) + key[1:]
		assertSkipped(t, strings.Replace(validEntry, `"`+key+`":`, `"`+upper+`":`, 1))
	}
	entry := strings.TrimSuffix(validEntry, "}") + `,"Icon":"ignored","Name":"ignored","URL":false,"Description":null,"Socket":0,"Enabled":false,"MCP":true,"Group":"core"}`
	list := readFile(t, `{"Services":false,"services":[`+entry+`]}`)
	baseline := readFile(t, `{"services":[`+validEntry+`]}`)
	if len(list) != 1 || list[0] != baseline[0] {
		t.Fatalf("case variants changed entry: %#v", list)
	}
	for _, key := range []string{"Group", "GROUP", "gRoup"} {
		entry := strings.TrimSuffix(validEntry, "}") + `,"group":"core","` + key + `":"application"}`
		list := readFile(t, `{"services":[`+entry+`]}`)
		if len(list) != 1 || list[0].Group != "core" {
			t.Fatalf("case variant %s replaced group: %#v", key, list)
		}
	}
}

func TestLastDuplicateMember(t *testing.T) {
	// R-K42H-WT44
	list := readFile(t, `{"services":null,"services":[`+validEntry+`]}`)
	if len(list) != 1 {
		t.Fatalf("last top-level value ignored: %#v", list)
	}
	if list, err := services.Read(fixture(t, `{"services":[],"services":null}`)); list != nil || err == nil {
		t.Fatalf("last invalid top-level value ignored: %#v, %v", list, err)
	}
	entry := strings.TrimSuffix(validEntry, "}") + `,"name":"last","url":"last-url","description":"last-description","socket":"last-socket","enabled":false,"mcp":true,"icon":null,"icon":"last-icon"}`
	list = readFile(t, `{"services":[`+entry+`]}`)
	want := services.Entry{Name: "last", URL: "last-url", Description: "last-description", Socket: "last-socket", MCP: true, Icon: "last-icon", HasIcon: true}
	if len(list) != 1 || list[0].Name != want.Name || list[0].URL != want.URL || list[0].Description != want.Description ||
		list[0].Socket != want.Socket || list[0].Enabled != want.Enabled || list[0].MCP != want.MCP ||
		list[0].Icon != want.Icon || list[0].HasIcon != want.HasIcon {
		t.Fatalf("last entry members: %#v", list)
	}
	for _, key := range []string{"name", "url", "description", "socket", "enabled", "mcp"} {
		assertSkipped(t, strings.TrimSuffix(validEntry, "}")+`,"`+key+`":null}`)
	}
	list = readFile(t, `{"services":[`+strings.TrimSuffix(validEntry, "}")+`,"icon":"first","icon":null}]}`)
	if len(list) != 1 || list[0].HasIcon || list[0].Icon != "" {
		t.Fatalf("last null icon: %#v", list)
	}
	for _, group := range []struct {
		members string
		want    string
	}{
		{`,"group":"application","group":"core"`, "core"},
		{`,"group":"core","group":"application"`, "application"},
		{`,"group":"core","group":null`, "application"},
	} {
		entry := strings.TrimSuffix(validEntry, "}") + group.members + `}`
		list := readFile(t, `{"services":[`+entry+`]}`)
		if len(list) != 1 || list[0].Group != group.want {
			t.Fatalf("last group %s: %#v", group.members, list)
		}
	}
}

func TestFind(t *testing.T) {
	// R-K5AE-AKUT
	list := services.List{{Name: "alpha", URL: "first"}, {Name: "beta"}, {Name: "alpha", URL: "second"}}
	for _, name := range []string{"alpha", "beta"} {
		entry, ok := list.Find(name)
		want := list[0]
		if name == "beta" {
			want = list[1]
		}
		if !ok || entry != want {
			t.Fatalf("Find(%q) = %#v, %v", name, entry, ok)
		}
	}
	for _, candidate := range []services.List{list, nil, {}} {
		for _, missing := range []string{"absent", "Alpha", ""} {
			if entry, ok := candidate.Find(missing); ok || entry != (services.Entry{}) {
				t.Fatalf("missing Find(%q) = %#v, %v", missing, entry, ok)
			}
		}
	}
}

func TestReadDoesNotPanic(t *testing.T) {
	// R-9UZH-ILUK
	for _, path := range []string{"", "\x00", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		_, _ = services.Read(path)
	}
	for _, content := range []string{"", "\xff", `null`, `{"services":[{},[],null,1,true,"x"]}`, `{"services":[` + validEntry + `]}`} {
		_, _ = services.Read(fixture(t, content))
	}
}

func TestConcurrentRead(t *testing.T) {
	// R-K7Q7-24C7
	path := fixture(t, `{"services":[`+validEntry+`]}`)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				list, err := services.Read(path)
				if err != nil || len(list) != 1 || list[0].Name != "alpha" {
					t.Errorf("concurrent Read = %#v, %v", list, err)
					return
				}
				list[0].Name = "local mutation"
			}
		})
	}
	wg.Wait()
}
