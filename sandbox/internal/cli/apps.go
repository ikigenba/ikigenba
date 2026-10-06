// Package cli implements the sandbox command interface.
package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

type appInfo struct {
	Name         string
	Default      bool
	Description  string
	MCP          bool
	Guests       bool
	Env          map[string]string
	Secrets      []string
	SecretValues map[string]string
	Icon         []byte
	HasIcon      bool
	Placement    string
	Delegate     bool
}

func appOSReason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
func appPrinted(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c < 32 || c == 127 {
			fmt.Fprintf(&b, "\\x%02x", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
func manifestError(name, reason string) error {
	return fmt.Errorf("%s: etc/manifest.toml: %s", name, reason)
}
func variableName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range []byte(s) {
		if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}
func sandboxVariable(s string) bool {
	switch s {
	case "DRAIN_SECONDS", "LISTEN_FDS", "LISTEN_FDNAMES", "LISTEN_PID", "NOTIFY_SOCKET":
		return true
	}
	return strings.HasPrefix(s, "IKIGENBA_")
}
func invalidEnvCharacter(r rune) bool {
	return r == 0 || r == 0xfeff || r >= 0xfdd0 && r <= 0xfdef || r&0xffff == 0xfffe || r&0xffff == 0xffff
}
func invalidValue(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' || invalidEnvCharacter(r) {
			return true
		}
	}
	return false
}

func discoverApps(worktree string) ([]appInfo, error) {
	entries, err := os.ReadDir(worktree)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", worktree, appOSReason(err))
	}
	var apps []appInfo
	for _, entry := range entries {
		name := entry.Name()
		if _, err := os.Stat(filepath.Join(worktree, name, "etc/manifest.toml")); err != nil {
			continue
		}
		apps = append(apps, appInfo{Name: name})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	if len(apps) == 0 {
		return nil, fmt.Errorf("no apps in %s\n\nan app is a directory holding etc/manifest.toml", worktree)
	}
	for i := range apps {
		if err := readManifest(worktree, &apps[i]); err != nil {
			return nil, err
		}
	}
	var defaults []string
	for _, app := range apps {
		if app.Default {
			defaults = append(defaults, app.Name)
		}
	}
	if len(defaults) > 1 {
		return nil, fmt.Errorf("more than one default app: %s", strings.Join(defaults, ", "))
	}
	return apps, nil
}
func readManifest(worktree string, a *appInfo) error {
	if !usableAppName(a.Name) {
		return fmt.Errorf("'%s' is not a usable app name", appPrinted(a.Name))
	}
	raw, err := appReadFile(filepath.Join(worktree, a.Name, "etc/manifest.toml"))
	if err != nil {
		return manifestError(a.Name, appOSReason(err))
	}
	var m map[string]any
	if _, err = toml.Decode(string(raw), &m); err != nil {
		return manifestError(a.Name, strings.TrimPrefix(err.Error(), "toml: "))
	}
	if _, ok := m["port"]; ok {
		return manifestError(a.Name, "'port' is not allowed; the sandbox gives the app its socket")
	}
	types := []struct{ k, kind string }{{"app", "a string"}, {"description", "a string"}, {"default", "a boolean"}, {"mcp", "a boolean"}, {"guests", "a boolean"}, {"secrets", "an array of strings"}, {"env", "a table of strings"}, {"resources", "a table"}}
	for _, item := range types {
		v, ok := m[item.k]
		if !ok {
			continue
		}
		valid := false
		switch item.k {
		case "app", "description":
			_, valid = v.(string)
		case "default", "mcp", "guests":
			_, valid = v.(bool)
		case "secrets":
			if arr, ok := v.([]any); ok {
				valid = true
				for _, x := range arr {
					if _, ok := x.(string); !ok {
						valid = false
					}
				}
			}
		case "env":
			if table, ok := v.(map[string]any); ok {
				valid = true
				for _, x := range table {
					if _, ok := x.(string); !ok {
						valid = false
					}
				}
			}
		case "resources":
			_, valid = v.(map[string]any)
		}
		if !valid {
			return manifestError(a.Name, fmt.Sprintf("'%s' must be %s", item.k, item.kind))
		}
	}
	app, ok := m["app"]
	if !ok {
		return manifestError(a.Name, "'app' is missing")
	}
	if app.(string) != a.Name {
		return manifestError(a.Name, fmt.Sprintf("app '%s' does not match its directory '%s'", appPrinted(app.(string)), a.Name))
	}
	a.Description, _ = m["description"].(string)
	a.Default, _ = m["default"].(bool)
	a.MCP, _ = m["mcp"].(bool)
	a.Guests, _ = m["guests"].(bool)
	for _, r := range a.Description {
		if r < 32 || r == 127 {
			return manifestError(a.Name, "'description' must be one line of text")
		}
	}
	a.Env = map[string]string{}
	if table, ok := m["env"].(map[string]any); ok {
		for k, v := range table {
			a.Env[k] = v.(string)
		}
	}
	if arr, ok := m["secrets"].([]any); ok {
		for _, v := range arr {
			a.Secrets = append(a.Secrets, v.(string))
		}
	}
	keys := make([]string, 0, len(a.Env))
	for k := range a.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keys = append(keys, a.Secrets...)
	for _, k := range keys {
		reason := ""
		switch {
		case !variableName(k):
			reason = fmt.Sprintf("'%s' is not a variable name", appPrinted(k))
		case sandboxVariable(k):
			reason = fmt.Sprintf("'%s' is set by the sandbox", k)
		default:
			_, env := a.Env[k]
			secret := false
			for _, s := range a.Secrets {
				if s == k {
					secret = true
				}
			}
			if env && secret {
				reason = fmt.Sprintf("'%s' is both in [env] and in secrets", k)
			} else if env && invalidValue(a.Env[k]) {
				reason = fmt.Sprintf("'%s' holds a character an env file cannot hold", k)
			}
		}
		if reason != "" {
			return manifestError(a.Name, reason)
		}
	}
	a.Placement = "apps"
	a.Delegate = false
	if resources, ok := m["resources"].(map[string]any); ok {
		if err := checkResources(a.Name, resources); err != nil {
			return err
		}
		if resources["slice"] == "core" {
			a.Placement = "core"
		}
		a.Delegate, _ = resources["delegate"].(bool)
	}
	if a.MCP && strings.TrimFunc(a.Description, unicode.IsSpace) == "" {
		return manifestError(a.Name, "'mcp' is true but 'description' is empty; an MCP service must say what it offers")
	}
	return readAppIcon(worktree, a)
}

func checkResources(name string, resources map[string]any) error {
	keys := []string{"slice", "memory_max", "go_memory_limit", "cpu_weight", "delegate", "oom_policy"}
	var unknown []string
	for key := range resources {
		if key != "slice" && key != "memory_max" && key != "go_memory_limit" && key != "cpu_weight" && key != "delegate" && key != "oom_policy" {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	keys = append(keys, unknown...)
	for _, key := range keys {
		value, present := resources[key]
		if !present {
			continue
		}
		switch key {
		case "slice":
			if value != "core" && value != "apps" {
				return manifestError(name, `'resources.slice' must be "core" or "apps"`)
			}
		case "cpu_weight":
			weight, ok := value.(int64)
			if !ok || weight < 1 || weight > 10000 {
				return manifestError(name, fmt.Sprintf("'resources.%s' must be a whole number from 1 to 10000", key))
			}
		case "memory_max", "go_memory_limit":
			memory, ok := value.(string)
			count, valid := memoryByteCount(memory)
			if !ok || !valid {
				return manifestError(name, fmt.Sprintf("'resources.%s' must be a whole number of bytes, optionally followed by K, M, or G", key))
			}
			if key == "go_memory_limit" {
				maximum := uint64(134217728)
				if configured, ok := resources["memory_max"].(string); ok {
					maximum, _ = memoryByteCount(configured)
				}
				if count > maximum {
					return manifestError(name, "'resources.go_memory_limit' must not be larger than 'resources.memory_max'")
				}
			}
		case "delegate":
			if _, ok := value.(bool); !ok {
				return manifestError(name, "'resources.delegate' must be true or false")
			}
		case "oom_policy":
			if value != "continue" {
				return manifestError(name, `'resources.oom_policy' must be "continue"`)
			}
		default:
			return manifestError(name, fmt.Sprintf("'resources.%s' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy", appPrinted(key)))
		}
	}
	return nil
}

func memoryByteCount(memory string) (uint64, bool) {
	if memory == "" {
		return 0, false
	}
	multiplier := uint64(1)
	switch memory[len(memory)-1] {
	case 'K':
		multiplier = 1024
	case 'M':
		multiplier = 1048576
	case 'G':
		multiplier = 1073741824
	}
	if multiplier != 1 {
		memory = memory[:len(memory)-1]
	}
	for _, digit := range []byte(memory) {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	count, err := strconv.ParseUint(memory, 10, 63)
	if err != nil || count == 0 || count > (1<<63-1)/multiplier {
		return 0, false
	}
	return count * multiplier, true
}

func readAppIcon(worktree string, a *appInfo) error {
	p := filepath.Join(worktree, a.Name, "share/icon.svg")
	st, err := os.Lstat(p)
	if err != nil {
		return nil
	}
	a.HasIcon = true
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s: share/icon.svg is not a regular file", a.Name)
	}
	a.Icon, err = appReadFile(p)
	if err != nil {
		return fmt.Errorf("%s: share/icon.svg: %s", a.Name, appOSReason(err))
	}
	if len(a.Icon) > 65536 {
		return fmt.Errorf("%s: share/icon.svg is larger than 64 KiB", a.Name)
	}
	if !utf8.Valid(a.Icon) {
		return fmt.Errorf("%s: share/icon.svg is not valid UTF-8", a.Name)
	}
	if !svgImage(a.Icon) {
		return fmt.Errorf("%s: share/icon.svg is not an SVG image", a.Name)
	}
	return nil
}
func svgImage(b []byte) bool {
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	d := xml.NewDecoder(bytes.NewReader(b))
	depth, roots := 0, 0
	for {
		t, err := d.Token()
		if errors.Is(err, io.EOF) {
			return depth == 0 && roots == 1
		}
		if err != nil {
			return false
		}
		switch x := t.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 || x.Name.Local != "svg" {
					return false
				}
			}
			seen := map[xml.Name]bool{}
			for _, a := range x.Attr {
				if seen[a.Name] {
					return false
				}
				seen[a.Name] = true
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 {
				for _, c := range x {
					if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
						return false
					}
				}
			}
		}
	}
}
func secretSource(secret string) string {
	switch secret {
	case "GOOGLE_CLIENT_ID":
		return "GOOGLE_LOCALHOST_CLIENT_ID"
	case "GOOGLE_CLIENT_SECRET":
		return "GOOGLE_LOCALHOST_CLIENT_SECRET"
	default:
		return secret
	}
}

func checkSecrets(getenv func(string) string, apps []appInfo) error {
	var missing []string
	for i := range apps {
		a := &apps[i]
		a.SecretValues = map[string]string{}
		keys := append([]string(nil), a.Secrets...)
		sort.Strings(keys)
		last := ""
		for _, k := range keys {
			if k == last {
				continue
			}
			last = k
			source := secretSource(k)
			value := getenv(source)
			if invalidValue(value) {
				return fmt.Errorf("environment variable %s, read for %s's %s, holds a character an env file cannot hold", source, a.Name, k)
			}
			if value == "" {
				missing = append(missing, a.Name+" "+k+" from "+source)
			} else {
				a.SecretValues[k] = value
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("secrets missing from the environment\n\n%s", strings.Join(missing, "\n"))
	}
	return nil
}
func renderAppEnv(data, name string, port int, a appInfo) []byte {
	entry := registryEntry{Name: name, Port: port}
	env := map[string]string{"DRAIN_SECONDS": "5", "IKIGENBA_CALLBACK_URL": callbackOrigin(entry), "IKIGENBA_PUBLIC_URL": appOrigin(entry, a.Name), "IKIGENBA_SANDBOX": name, "IKIGENBA_SERVICES": filepath.Join(data, "services.json")}
	for k, v := range a.Env {
		env[k] = v
	}
	for k, v := range a.SecretValues {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	esc := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=\"%s\"\n", k, esc.Replace(env[k]))
	}
	return []byte(b.String())
}
func appJSONString(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
func renderServices(name string, port, euid int, apps []appInfo) []byte {
	var b strings.Builder
	b.WriteString("{\n  \"services\": [\n")
	entry := registryEntry{Name: name, Port: port}
	for i, a := range apps {
		fmt.Fprintf(&b, "    { \"name\": %s, \"url\": %s, \"description\": %s, \"socket\": %s, \"enabled\": true, \"mcp\": %s", appJSONString(a.Name), appJSONString(appOrigin(entry, a.Name)), appJSONString(a.Description), appJSONString(socketPath(euid, port, a.Name)), strconv.FormatBool(a.MCP))
		if a.HasIcon {
			fmt.Fprintf(&b, ", \"icon\": %s", appJSONString(string(a.Icon)))
		}
		b.WriteString(" }")
		if i+1 < len(apps) {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]\n}\n")
	return []byte(b.String())
}

func appReadFile(p string) (b []byte, err error) {
	file, err := os.Open(filepath.Clean(p))
	if err != nil {
		return nil, err
	}
	b, err = io.ReadAll(file)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return b, err
}
