package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func renderFixture(t *testing.T, root, name, manifest string, binary bool, icon []byte) string {
	t.Helper()
	base := filepath.Join(root, "opt", name)
	if err := os.MkdirAll(filepath.Join(base, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(base, "etc", "manifest.toml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if binary {
		if err := os.MkdirAll(filepath.Join(base, "bin"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "bin", name), []byte("binary"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if icon != nil {
		if err := os.MkdirAll(filepath.Join(base, "share"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "share", "icon.svg"), icon, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(base, "share", "icon.svg")
}

func renderEnv(root string, disabled map[string]bool, queries *[]string) host.Env {
	return host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name != "systemctl" || len(command.Args) != 4 || command.Args[0] != "show" {
			panic("unexpected command in render: " + command.Name)
		}
		unit := command.Args[3]
		name := strings.TrimSuffix(strings.TrimPrefix(unit, "ikigenba-"), ".socket")
		*queries = append(*queries, name)
		state := "enabled"
		if disabled[name] {
			state = "disabled"
		}
		return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\n")}, nil
	}}
}

func renderWriteEnv(t *testing.T, root string, disabled map[string]bool, commands *[]host.Command) host.Env {
	t.Helper()
	var queries []string
	query := renderEnv(root, disabled, &queries).Execute
	return host.Env{Root: root, Execute: func(ctx context.Context, command host.Command) (host.Result, error) {
		if commands != nil {
			*commands = append(*commands, command)
		}
		switch {
		case command.Name == "systemctl":
			return query(ctx, command)
		case command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--user", "ikigenba"}):
			return host.Result{Stdout: []byte("1000\n")}, nil
		case command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--group", "--name", "ikigenba"}):
			return host.Result{Stdout: []byte("ikigenba\n")}, nil
		case command.Name == "chown":
			return host.Result{}, nil
		default:
			t.Fatalf("unexpected command: %+v", command)
			return host.Result{}, nil
		}
	}}
}

func readPublishedServices(t *testing.T, root string) []byte {
	return readRootFile(t, root, "var/lib/ikigenba/services.json")
}

func readRootFile(t *testing.T, root, relative string) []byte {
	t.Helper()
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := filesystem.Close(); err != nil {
			t.Error(err)
		}
	}()
	data, err := filesystem.ReadFile(relative)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type publishedEntry struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Socket      string `json:"socket"`
	Icon        string `json:"icon"`
	Enabled     bool   `json:"enabled"`
	MCP         bool   `json:"mcp"`
}

func publishedServices(t *testing.T, root string) []publishedEntry {
	t.Helper()
	var document struct {
		Services []publishedEntry `json:"services"`
	}
	if err := json.Unmarshal(readPublishedServices(t, root), &document); err != nil {
		t.Fatal(err)
	}
	return document.Services
}

// R-YG5R-NNZ2
func TestRenderSelectsExactlyListedServices(t *testing.T) {
	root := t.TempDir()
	icon := []byte("<svg/>\n")
	renderFixture(t, root, "routed", "app = \"routed\"\n", true, icon)
	renderFixture(t, root, "unrouted", "", true, icon)
	renderFixture(t, root, "no-binary", "app = \"no-binary\"\n", false, icon)
	renderFixture(t, root, "empty-app", "description = \"Not routed\"\n", true, icon)
	renderFixture(t, root, "no-icon", "app = \"no-icon\"\n", true, nil)
	for _, name := range []string{"directory-binary", "symlink-binary"} {
		renderFixture(t, root, name, "app = \""+name+"\"\n", false, icon)
		if err := os.MkdirAll(filepath.Join(root, "opt", name, "bin"), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "opt", "directory-binary", "bin", "directory-binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "opt", "routed", "bin", "routed"), filepath.Join(root, "opt", "symlink-binary", "bin", "symlink-binary")); err != nil {
		t.Fatal(err)
	}
	_, err := Write(context.Background(), renderWriteEnv(t, root, map[string]bool{"routed": true, "no-icon": true}, nil), "host.example")
	if err != nil {
		t.Fatal(err)
	}
	services := publishedServices(t, root)
	if len(services) != 2 || services[0].Name != "no-icon" || services[1].Name != "routed" {
		t.Fatalf("published launcher names = %#v; want no-icon and routed", services)
	}
}

// R-YHDO-1FPR
func TestRenderEntryValuesIncludeDefaultAndApex(t *testing.T) {
	root := t.TempDir()
	icon := []byte("<svg>full</svg>\r\n")
	renderFixture(t, root, "alpha", "app = \"alpha\"\ndefault = true\ndescription = \"  A full description ☃  \"\nmcp = true\n", true, icon)
	renderFixture(t, root, "apex", "app = \"apex\"\n", true, nil) // app named by host.apex
	renderFixture(t, root, "empty-icon", "app = \"empty-icon\"\n", true, []byte{})
	_, err := Write(context.Background(), renderWriteEnv(t, root, map[string]bool{"apex": true}, nil), "box.example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []publishedEntry{
		{Name: "alpha", URL: "https://alpha.box.example.com", Description: "  A full description ☃  ", Socket: "/run/ikigenba/alpha.sock", Icon: string(icon), Enabled: true, MCP: true},
		{Name: "apex", URL: "https://apex.box.example.com", Socket: "/run/ikigenba/apex.sock", Enabled: false},
		{Name: "empty-icon", URL: "https://empty-icon.box.example.com", Socket: "/run/ikigenba/empty-icon.sock", Enabled: true},
	}
	if got := publishedServices(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("published entries = %#v, want %#v", got, want)
	}
	var document struct {
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal(readPublishedServices(t, root), &document); err != nil {
		t.Fatal(err)
	}
	for _, item := range document.Services {
		iconValue, present := item["icon"]
		switch item["name"] {
		case "alpha":
			if !present || iconValue != string(icon) {
				t.Fatalf("full icon: %#v", item)
			}
		case "apex":
			if present {
				t.Fatalf("absent icon: %#v", item)
			}
		case "empty-icon":
			if !present || iconValue != "" {
				t.Fatalf("empty icon: %#v", item)
			}
		}
	}

}

// R-YILK-F7GG
func TestRenderExactLayoutAndBytewiseOrder(t *testing.T) {
	root := t.TempDir()
	env := renderWriteEnv(t, root, map[string]bool{"zeta": true}, nil)
	if _, err := Write(context.Background(), env, "box.example"); err != nil {
		t.Fatal(err)
	}
	empty := readPublishedServices(t, root)
	if string(empty) != "{\n  \"services\": []\n}\n" {
		t.Fatalf("empty published file = %q", empty)
	}
	renderFixture(t, root, "zeta", "app = \"zeta\"\ndescription = \"Z\"\nmcp = true\n", true, nil)
	renderFixture(t, root, "alpha", "app = \"alpha\"\n", true, []byte("a"))
	if _, err := Write(context.Background(), env, "box.example"); err != nil {
		t.Fatal(err)
	}
	got := readPublishedServices(t, root)
	want := "{\n  \"services\": [\n" +
		"    { \"name\": \"alpha\", \"url\": \"https://alpha.box.example\", \"description\": \"\", \"socket\": \"/run/ikigenba/alpha.sock\", \"enabled\": true, \"mcp\": false, \"icon\": \"a\" },\n" +
		"    { \"name\": \"zeta\", \"url\": \"https://zeta.box.example\", \"description\": \"Z\", \"socket\": \"/run/ikigenba/zeta.sock\", \"enabled\": false, \"mcp\": true }\n" +
		"  ]\n}\n"
	if string(got) != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

// R-YJTG-SZ75
func TestRenderMinimalJSONEscapesAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	var controls []byte
	for character := byte(0); character < 32; character++ {
		controls = append(controls, character)
	}
	value := string(controls) + "\"\\<>/&" + "\u007f☃\u2028\u2029"
	renderFixture(t, root, "a", "app = \"a\"\n", true, []byte(value))
	var commands []host.Command
	hostName := "h\"\\<>/&\u007f☃\u2028\u2029.example"
	if _, err := Write(context.Background(), renderWriteEnv(t, root, nil, &commands), hostName); err != nil {
		t.Fatal(err)
	}
	data := readPublishedServices(t, root)
	want := `\u0000\u0001\u0002\u0003\u0004\u0005\u0006\u0007\b\t\n\u000b\f\r\u000e\u000f\u0010\u0011\u0012\u0013\u0014\u0015\u0016\u0017\u0018\u0019\u001a\u001b\u001c\u001d\u001e\u001f\"\\<>/&` + "\u007f☃" + `\u2028\u2029`
	if !strings.Contains(string(data), want) {
		t.Fatalf("encoded icon absent from %q; want %q", data, want)
	}
	if !strings.Contains(string(data), `"url": "https://a.h\"\\<>/&`+"\u007f☃"+`\u2028\u2029.example"`) {
		t.Fatalf("minimal URL escape absent from %q", data)
	}
	nameData := encodeEntries([]entry{{Name: value, URL: value, Description: value, Socket: value, Icon: value, HasIcon: true}})
	for _, field := range []string{"name", "url", "description", "socket", "icon"} {
		if !strings.Contains(string(nameData), `"`+field+`": "`+want+`"`) {
			t.Fatalf("minimal %s escape absent from %q", field, nameData)
		}
	}
	var decoded struct {
		Services []struct {
			Name        string `json:"name"`
			URL         string `json:"url"`
			Icon        string `json:"icon"`
			Description string `json:"description"`
			Socket      string `json:"socket"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Services) != 1 || decoded.Services[0].Name != "a" || decoded.Services[0].URL != "https://a."+hostName || decoded.Services[0].Icon != value {
		t.Fatalf("round trip = %#v, %v", decoded, err)
	}
	if err := json.Unmarshal(nameData, &decoded); err != nil || decoded.Services[0].Name != value || decoded.Services[0].URL != value || decoded.Services[0].Description != value || decoded.Services[0].Socket != value || decoded.Services[0].Icon != value {
		t.Fatalf("name round trip = %#v, %v", decoded, err)
	}
}

// R-HKOW-0O97
func TestRenderRejectsUnembeddableIcons(t *testing.T) {
	for _, test := range []struct {
		name     string
		makeIcon func(*testing.T, string)
		want     string
	}{
		{"directory", func(t *testing.T, icon string) {
			t.Helper()
			if err := os.MkdirAll(icon, 0o750); err != nil {
				t.Fatal(err)
			}
		}, "bad: share/icon.svg is not a regular file"},
		{"symlink", func(t *testing.T, icon string) {
			t.Helper()
			if err := os.Symlink("target", icon); err != nil {
				t.Fatal(err)
			}
		}, "bad: share/icon.svg is not a regular file"},
		{"invalid-utf8", func(t *testing.T, icon string) {
			t.Helper()
			if err := os.WriteFile(icon, []byte{0xff}, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "bad: share/icon.svg is not valid UTF-8"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			icon := renderFixture(t, root, "bad", "app = \"bad\"\n", true, nil)
			if err := os.MkdirAll(filepath.Dir(icon), 0o750); err != nil {
				t.Fatal(err)
			}
			test.makeIcon(t, icon)
			assertIconWriteFailure(t, root, test.want)
		})
	}
}

func assertIconWriteFailure(t *testing.T, root, want string) {
	t.Helper()
	// An unsuccessful regeneration must also preserve an absent publication.
	var absentCommands []host.Command
	changes, writeErr := Write(context.Background(), renderWriteEnv(t, root, nil, &absentCommands), "box.example")
	if changes != nil || writeErr == nil || writeErr.Error() != want {
		t.Fatalf("Write without publication = %v, %v; want nil and %q", changes, writeErr, want)
	}
	for _, command := range absentCommands {
		if command.Name != "systemctl" {
			t.Fatalf("command after failed icon: %+v", command)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "var", "lib", "ikigenba")); !os.IsNotExist(err) {
		t.Fatalf("services directory created after icon failure: %v", err)
	}
	file := filepath.Join(root, "var", "lib", "ikigenba", "services.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	var commands []host.Command
	changes, err := Write(context.Background(), renderWriteEnv(t, root, nil, &commands), "box.example")
	if changes != nil || err == nil || err.Error() != want {
		t.Fatalf("Write = %v, %v; want nil and %q", changes, err, want)
	}
	for _, command := range commands {
		if command.Name != "systemctl" {
			t.Fatalf("command after failed icon: %+v", command)
		}
	}
	data := readPublishedServices(t, root)
	if string(data) != "before" {
		t.Fatalf("file after failed Write = %q", data)
	}
	info, err := os.Lstat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode after failed Write = %v, %v", info, err)
	}
	directory, err := os.Lstat(filepath.Dir(file))
	if err != nil || directory.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode after failed Write = %v, %v", directory, err)
	}
}

// R-HKOW-0O97
func TestWritePreservesFileOnIconReadFailure(t *testing.T) {
	root := t.TempDir()
	icon := renderFixture(t, root, "bad", "app = \"bad\"\n", true, []byte("<svg/>"))
	if err := os.Chmod(icon, 0); err != nil {
		t.Fatal(err)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := filesystem.ReadFile(filepath.Join("opt", "bad", "share", "icon.svg"))
	_ = filesystem.Close()
	if readErr == nil {
		t.Fatal("fixture must make the icon unreadable")
	}
	want := fmt.Sprintf("bad: share/icon.svg: %s", readErr)
	assertIconWriteFailure(t, root, want)
}

// R-HKOW-0O97
func TestRenderDoesNotApplyInstallIconChecks(t *testing.T) {
	root := t.TempDir()
	icon := []byte(strings.Repeat("x", 65537))
	renderFixture(t, root, "large", "app = \"large\"\n", true, icon)
	if _, err := Write(context.Background(), renderWriteEnv(t, root, nil, nil), "box.example"); err != nil {
		t.Fatal(err)
	}
	entries := publishedServices(t, root)
	if len(entries) != 1 || entries[0].Icon != string(icon) {
		t.Fatalf("published large non-SVG icon = %#v", entries)
	}
}
