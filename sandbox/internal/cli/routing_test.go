package cli

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type nginxNode struct {
	words    []string
	block    bool
	children []nginxNode
}

// parseRoutingConfig implements nginx's lexical rules for the generated
// configuration, rather than depending on its spelling or indentation.
func parseRoutingConfig(t *testing.T, text string) []nginxNode {
	t.Helper()
	pos := 0
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
	var parse func(bool) []nginxNode
	parse = func(nested bool) []nginxNode {
		var nodes []nginxNode
		var words []string
		for pos < len(text) {
			c := text[pos]
			if space(c) {
				pos++
				continue
			}
			if c == '#' {
				for pos < len(text) && text[pos] != '\n' {
					pos++
				}
				continue
			}
			if c == '}' {
				if !nested || len(words) != 0 {
					t.Fatal("unexpected closing block")
				}
				pos++
				return nodes
			}
			if c == ';' || c == '{' {
				if len(words) == 0 {
					t.Fatal("empty directive")
				}
				pos++
				node := nginxNode{words: words, block: c == '{'}
				words = nil
				if node.block {
					node.children = parse(true)
				}
				nodes = append(nodes, node)
				continue
			}
			var word strings.Builder
			var quote byte
			if c == '"' || c == '\'' {
				quote = c
				pos++
			}
			closed := quote == 0
			for pos < len(text) {
				c = text[pos]
				if quote != 0 && c == quote {
					pos++
					closed = true
					if pos < len(text) && !space(text[pos]) && text[pos] != ';' && text[pos] != '{' {
						t.Fatal("quoted word not followed by delimiter")
					}
					break
				}
				if quote == 0 && (space(c) || c == ';' || (c == '{' && (word.Len() == 0 || text[pos-1] != '$'))) {
					break
				}
				if c == '\\' {
					pos++
					if pos == len(text) {
						t.Fatal("unfinished escape")
					}
					c = text[pos]
					switch c {
					case 't':
						word.WriteByte('\t')
					case 'r':
						word.WriteByte('\r')
					case 'n':
						word.WriteByte('\n')
					case '"', '\'', '\\':
						word.WriteByte(c)
					default:
						word.WriteByte('\\')
						word.WriteByte(c)
					}
				} else {
					word.WriteByte(c)
				}
				pos++
			}
			if !closed {
				t.Fatal("unclosed quote")
			}
			words = append(words, word.String())
		}
		if nested || len(words) > 0 {
			t.Fatal("unfinished configuration")
		}
		return nodes
	}
	nodes := parse(false)
	var checkSyntax func([]nginxNode)
	checkSyntax = func(nodes []nginxNode) {
		for _, node := range nodes {
			key := node.words[0]
			wantBlock := key == "events" || key == "http" || key == "upstream" || key == "location" || (key == "server" && len(node.words) == 1)
			if node.block != wantBlock {
				t.Fatalf("%q: block syntax %t, want %t", node.words, node.block, wantBlock)
			}
			checkSyntax(node.children)
		}
	}
	checkSyntax(nodes)
	return nodes
}

func routingFind(t *testing.T, nodes []nginxNode, words ...string) nginxNode {
	t.Helper()
	var found []nginxNode
	for _, n := range nodes {
		if reflect.DeepEqual(n.words, words) {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		t.Fatalf("wanted exactly one %q, found %d in %+v", words, len(found), nodes)
	}
	return found[0]
}

func routingKeys(t *testing.T, nodes []nginxNode, keys ...string) {
	t.Helper()
	got := make(map[string]int)
	want := make(map[string]int)
	for _, n := range nodes {
		got[n.words[0]]++
	}
	for _, k := range keys {
		want[k]++
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("directives: got %v want %v", got, want)
	}
}

func routingServer(t *testing.T, http nginxNode, names ...string) nginxNode {
	t.Helper()
	var found []nginxNode
	for _, n := range http.children {
		if !reflect.DeepEqual(n.words, []string{"server"}) {
			continue
		}
		for _, d := range n.children {
			if reflect.DeepEqual(d.words, append([]string{"server_name"}, names...)) {
				found = append(found, n)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("server names %v: got %d matches", names, len(found))
	}
	return found[0]
}

func routingHeaders(t *testing.T, loc nginxNode, id, email string) {
	t.Helper()
	for _, pair := range [][2]string{{"Host", "$http_host"}, {"X-Real-IP", "$remote_addr"}, {"X-Forwarded-For", "$remote_addr"}, {"X-Forwarded-Proto", "http"}, {"X-Request-Id", "$request_id"}, {"X-User-Id", id}, {"X-User-Email", email}} {
		routingFind(t, loc.children, "proxy_set_header", pair[0], pair[1])
		count := 0
		for _, n := range loc.children {
			if len(n.words) > 1 && n.words[0] == "proxy_set_header" && n.words[1] == pair[0] {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("header %s set %d times", pair[0], count)
		}
	}
}

func TestRoutingRuntimeAndContexts(t *testing.T) {
	// R-SWXP-LN7R R-4O92-QDQB R-XKZH-R9GD R-SY5L-ZEYG
	// R-RPMC-4P2O R-YRNZ-58EO R-4QOV-HX7P R-4T4O-9GP3
	// R-4UCK-N8FS R-4VKH-106H R-4WSD-ERX6
	data := "/tmp/a b%c$d/ikigenba/sandbox/wip"
	config := string(renderNginxConfig(data, "/checkout", "wip", 7001, 1234, []appInfo{{Name: "auth"}, {Name: "dummy"}}))
	nodes := parseRoutingConfig(t, config)
	routingKeys(t, nodes, "pid", "error_log", "events", "http")
	routingFind(t, nodes, "pid", data+"/nginx/nginx.pid")
	routingFind(t, nodes, "error_log", "stderr")
	if len(routingFind(t, nodes, "events").children) != 0 {
		t.Fatal("events is not empty")
	}
	http := routingFind(t, nodes, "http")
	routingKeys(t, http.children, "access_log", "client_body_temp_path", "proxy_temp_path", "fastcgi_temp_path", "uwsgi_temp_path", "scgi_temp_path", "server_names_hash_bucket_size", "upstream", "upstream", "server", "server", "server", "server")
	routingFind(t, http.children, "access_log", "off")
	routingFind(t, http.children, "server_names_hash_bucket_size", "256")
	for _, pair := range [][2]string{{"client_body_temp_path", "client_body"}, {"proxy_temp_path", "proxy"}, {"fastcgi_temp_path", "fastcgi"}, {"uwsgi_temp_path", "uwsgi"}, {"scgi_temp_path", "scgi"}} {
		routingFind(t, http.children, pair[0], data+"/nginx/"+pair[1])
		if !strings.Contains(config, pair[0]+` "`+data+"/nginx/"+pair[1]+`";`) {
			t.Fatalf("path not quoted for %s", pair[0])
		}
	}
	if !strings.Contains(config, `pid "/tmp/a b%c$d/ikigenba/sandbox/wip/nginx/nginx.pid";`) {
		t.Fatal("pid path not quoted unchanged")
	}
	for _, name := range []string{"auth", "dummy"} {
		up := routingFind(t, http.children, "upstream", "app_"+name)
		routingKeys(t, up.children, "server")
		routingFind(t, up.children, "server", "unix:/run/user/1234/sandbox/7001/"+name+".sock")
	}
	var inspect func([]nginxNode)
	inspect = func(ns []nginxNode) {
		for _, n := range ns {
			if (n.words[0] == "access_log" || n.words[0] == "error_log") && len(n.words) != 2 {
				t.Fatal("extra log argument")
			}
			if n.words[0] == "access_log" && !reflect.DeepEqual(n.words, []string{"access_log", "off"}) {
				t.Fatal("extra access log")
			}
			if n.words[0] == "error_log" && !reflect.DeepEqual(n.words, []string{"error_log", "stderr"}) {
				t.Fatal("extra error log")
			}
			inspect(n.children)
		}
	}
	inspect(nodes)
}

func TestRoutingServersAndUngated(t *testing.T) {
	// R-4Y09-SJNV R-T0LE-QYFU R-T1TB-4Q6J R-RN6J-D5LA
	// R-ROEF-QXBZ R-52VV-BMMN R-T5H0-A1EM R-RLYM-ZDUL
	// R-56JK-GXUQ R-T94P-FCMP R-TACL-T4DE R-4H5B-RWN4 R-5A79-M92T R-GPR8-TLS0
	for _, auth := range []bool{false, true} {
		for _, defaultApp := range []bool{false, true} {
			for _, guests := range []bool{false, true} {
				t.Run(fmt.Sprintf("auth=%t,default=%t,guests=%t", auth, defaultApp, guests), func(t *testing.T) {
					f := newUpFixture(t, "dummy")
					setting := ""
					if guests {
						setting = "guests = true\n"
					}
					f.put(filepath.Join(f.worktree, "dummy", "etc", "manifest.toml"), fmt.Sprintf("app = \"dummy\"\ndefault = %t\n", defaultApp)+setting, 0644)
					if auth {
						f.put(filepath.Join(f.worktree, "auth", "etc", "manifest.toml"), "app = \"auth\"\n"+setting, 0644)
					}
					if code, _, diagnostic := f.run("up"); code != 0 || diagnostic != "" {
						t.Fatalf("up: %d %q", code, diagnostic)
					}
					nodes := parseRoutingConfig(t, upRead(t, filepath.Join(f.data, "nginx", "nginx.conf")))
					http := routingFind(t, nodes, "http")
					defaults := 0
					for _, n := range http.children {
						if n.words[0] != "server" {
							continue
						}
						listens := 0
						for _, d := range n.children {
							if d.words[0] != "listen" {
								continue
							}
							listens++
							if reflect.DeepEqual(d.words, []string{"listen", "127.0.0.1:7400", "default_server"}) {
								defaults++
								routingKeys(t, n.children, "listen", "return")
								routingFind(t, n.children, "return", "404")
							} else if !reflect.DeepEqual(d.words, []string{"listen", "127.0.0.1:7400"}) {
								t.Fatalf("unexpected listen %v", d.words)
							}
						}
						if listens != 1 {
							t.Fatalf("server holds %d listens", listens)
						}
					}
					if defaults != 1 {
						t.Fatalf("default servers %d", defaults)
					}
					var checkListen func([]nginxNode, string)
					checkListen = func(ns []nginxNode, parent string) {
						for _, n := range ns {
							if n.words[0] == "listen" && parent != "server" {
								t.Fatal("listen outside server")
							}
							checkListen(n.children, n.words[0])
						}
					}
					checkListen(nodes, "")
					if auth {
						local := routingServer(t, http, "localhost")
						routingKeys(t, local.children, "listen", "server_name", "return")
						routingFind(t, local.children, "return", "302", "http://auth.wip.localhost:7400$request_uri")
					} else {
						for _, n := range http.children {
							for _, d := range n.children {
								if d.words[0] == "server_name" {
									for _, v := range d.words[1:] {
										if v == "localhost" {
											t.Fatal("localhost server without auth")
										}
									}
								}
							}
						}
					}
					names := []string{"dummy.wip.localhost"}
					if defaultApp {
						names = append(names, "wip.localhost")
					}
					dummy := routingServer(t, http, names...)
					if !defaultApp {
						for _, n := range http.children {
							for _, d := range n.children {
								if d.words[0] == "server_name" {
									for _, v := range d.words[1:] {
										if v == "wip.localhost" {
											t.Fatal("bare sandbox name without default")
										}
									}
								}
							}
						}
					}
					var ungated nginxNode
					if auth {
						ungated = routingServer(t, http, "auth.wip.localhost")
						routingFind(t, dummy.children, "location", "=", "/_sandbox/auth")
					} else {
						ungated = dummy
					}
					keys := []string{"listen", "server_name", "include", "location"}
					if auth {
						keys = append(keys, "location", "location")
						check := routingFind(t, ungated.children, "location", "=", "/check")
						routingKeys(t, check.children, "return")
						routingFind(t, check.children, "return", "404")
						open := routingFind(t, ungated.children, "location", "=", "/check/open")
						routingKeys(t, open.children, "return")
						routingFind(t, open.children, "return", "404")
					}
					routingKeys(t, ungated.children, keys...)
					loc := routingFind(t, ungated.children, "location", "/")
					routingKeys(t, loc.children, "proxy_pass", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header")
					appName := "dummy"
					if auth {
						appName = "auth"
					}
					routingFind(t, loc.children, "proxy_pass", "http://app_"+appName)
					routingHeaders(t, loc, "", "")
				})
			}
		}
	}
}

func TestRoutingFragmentEscaping(t *testing.T) {
	// R-40PK-FUYY R-SWXP-LN7R
	root := t.TempDir()
	worktree := root + `/a"b'c*d?e[f]g$h\i j;{#`
	wantTree := root + `/a"b'c\*d\?e\[f]g$h\\i j;{#`
	for _, apps := range [][]appInfo{{{Name: "dummy"}}, {{Name: "auth"}, {Name: "dummy", Default: true}}} {
		http := routingFind(t, parseRoutingConfig(t, string(renderNginxConfig("/data", worktree, "wip", 7002, 100, apps))), "http")
		for _, app := range apps {
			names := []string{app.Name + ".wip.localhost"}
			if app.Default {
				names = append(names, "wip.localhost")
			}
			server := routingServer(t, http, names...)
			routingFind(t, server.children, "include", wantTree+"/"+app.Name+"/etc/nginx.conf*")
			includes := 0
			locationSeen := false
			for _, n := range server.children {
				if n.words[0] == "location" {
					locationSeen = true
				}
				if n.words[0] == "include" {
					includes++
					if locationSeen {
						t.Fatal("fragment after location")
					}
				}
			}
			if includes != 1 {
				t.Fatalf("fragment count %d", includes)
			}
		}
	}
}

func TestRoutingGatedLocations(t *testing.T) {
	// R-EBCD-K7ZY R-GS71-L59E R-4ID8-5ODT R-GVUQ-QGHH R-GX2N-4886 R-GQZ5-7DIP R-GKVN-AIT8 R-GH7Y-57L5
	// R-8JN0-I3HC R-8KUW-VV81 R-8M2T-9MYQ
	// R-5GAR-J3SA R-TF87-C7C6 R-5IQK-AN9O R-56JK-GXUQ R-T94P-FCMP
	for _, setting := range []struct {
		name, manifest     string
		guests, defaultApp bool
	}{
		{"unset", "", false, false}, {"false", "mcp = false\n", false, false}, {"true", "mcp = true\n", false, false},
		{"guests-false", "guests = false\n", false, false},
		{"guests-true", "guests = true\n", true, false},
		{"guests-mcp-false", "guests = true\nmcp = false\n", true, false},
		{"guests-mcp-true", "guests = true\nmcp = true\n", true, false},
		{"guests-default", "guests = true\ndefault = true\n", true, true},
	} {
		t.Run(setting.name, func(t *testing.T) {
			f := newUpFixture(t, "auth", "dummy")
			f.put(filepath.Join(f.worktree, "dummy", "etc", "manifest.toml"), "app = \"dummy\"\ndescription = \"Demo\"\n"+setting.manifest, 0644)
			if code, _, diagnostic := f.run("up"); code != 0 || diagnostic != "" {
				t.Fatalf("up: code %d, diagnostic %q", code, diagnostic)
			}
			http := routingFind(t, parseRoutingConfig(t, upRead(t, filepath.Join(f.data, "nginx", "nginx.conf"))), "http")
			names := []string{"dummy.wip.localhost"}
			if setting.defaultApp {
				names = append(names, "wip.localhost")
			}
			server := routingServer(t, http, names...)
			keys := []string{"listen", "server_name", "include", "location", "location", "location", "location", "location", "location", "location", "location", "location", "location", "location"}
			if setting.guests {
				keys = append(keys, "location")
			}
			routingKeys(t, server.children, keys...)
			routingFind(t, server.children, "listen", "127.0.0.1:7400")
			routingFind(t, server.children, "include", filepath.Join(f.worktree, "dummy", "etc", "nginx.conf*"))
			checks := [][2]string{{"/_sandbox/auth", "/check"}}
			if setting.guests {
				checks = append(checks, [2]string{"/_sandbox/auth_open", "/check/open"})
			}
			for _, pair := range checks {
				check := routingFind(t, server.children, "location", "=", pair[0])
				routingKeys(t, check.children, "internal", "proxy_pass", "proxy_pass_request_body", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header")
				routingFind(t, check.children, "internal")
				routingFind(t, check.children, "proxy_pass", "http://app_auth"+pair[1])
				routingFind(t, check.children, "proxy_pass_request_body", "off")
				routingFind(t, check.children, "proxy_set_header", "Content-Length", "")
				routingFind(t, check.children, "proxy_set_header", "X-Original-Method", "$request_method")
				routingFind(t, check.children, "proxy_set_header", "X-Original-Host", "$host")
				routingFind(t, check.children, "proxy_set_header", "X-Original-URI", "$request_uri")
				routingHeaders(t, check, "", "")
			}
			for _, spec := range []struct {
				args    []string
				handler string
			}{{[]string{"location", "/"}, "signin"}, {[]string{"location", "=", "/mcp"}, "bearer"}, {[]string{"location", "^~", "/mcp/"}, "bearer"}, {[]string{"location", "~", "/(info/refs|git-upload-pack|git-receive-pack)$"}, "git"}} {
				loc := routingFind(t, server.children, spec.args...)
				open := setting.guests && spec.handler == "signin"
				keys := []string{"auth_request", "auth_request_set", "auth_request_set", "proxy_pass", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header", "proxy_set_header"}
				checkPath := "/_sandbox/auth"
				if open {
					checkPath = "/_sandbox/auth_open"
				} else {
					keys = append(keys, "error_page")
				}
				routingKeys(t, loc.children, keys...)
				routingFind(t, loc.children, "auth_request", checkPath)
				routingFind(t, loc.children, "auth_request_set", "$sandbox_user_id", "$upstream_http_x_user_id")
				routingFind(t, loc.children, "auth_request_set", "$sandbox_user_email", "$upstream_http_x_user_email")
				if !open {
					routingFind(t, loc.children, "error_page", "401", "=", "@sandbox_"+spec.handler)
				}
				routingFind(t, loc.children, "proxy_pass", "http://app_dummy")
				routingHeaders(t, loc, "$sandbox_user_id", "$sandbox_user_email")
			}
			for _, handler := range []string{"signin", "bearer", "git"} {
				loc := routingFind(t, server.children, "location", "@sandbox_"+handler)
				routingKeys(t, loc.children, "satisfy", "allow", "try_files")
				routingFind(t, loc.children, "satisfy", "any")
				routingFind(t, loc.children, "allow", "all")
				routingFind(t, loc.children, "try_files", "/.sandbox-none", "@sandbox_"+handler+"_reply")
			}
			signin := routingFind(t, server.children, "location", "@sandbox_signin_reply")
			routingKeys(t, signin.children, "return")
			routingFind(t, signin.children, "return", "302", "http://auth.wip.localhost:7400/?return=$scheme://$http_host$request_uri")
			bearer := routingFind(t, server.children, "location", "@sandbox_bearer_reply")
			routingKeys(t, bearer.children, "default_type", "add_header", "return")
			routingFind(t, bearer.children, "default_type", "text/plain")
			routingFind(t, bearer.children, "add_header", "WWW-Authenticate", `Bearer realm="ikigenba"`, "always")
			routingFind(t, bearer.children, "return", "401", "authentication required: send Authorization: Bearer <token>\n")
			git := routingFind(t, server.children, "location", "@sandbox_git_reply")
			routingKeys(t, git.children, "default_type", "add_header", "return")
			routingFind(t, git.children, "default_type", "text/plain")
			routingFind(t, git.children, "add_header", "WWW-Authenticate", `Basic realm="ikigenba"`, "always")
			routingFind(t, git.children, "return", "401", "authentication required: send your token as the password\n")
		})
	}
}

func TestRoutingNginxUnit(t *testing.T) {
	// R-5DLC-QFHT R-ZUPX-F7J2 R-ZVXT-SZ9R R-XPV3-ACF5 R-XON6-WKOG R-ZZLI-YAHU
	data := "/tmp/a b%c$d/ikigenba/sandbox/wip"
	text := string(renderNginxUnit(data, "wip"))
	sections := make(map[string][]string)
	var section string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			if _, exists := sections[section]; exists {
				t.Fatal("duplicate section")
			}
			sections[section] = nil
			continue
		}
		sections[section] = append(sections[section], line)
	}
	if len(sections) != 2 || len(sections["[Unit]"]) != 1 || sections["[Unit]"][0] != "Description=sandbox wip: nginx" {
		t.Fatalf("unit sections: %v", sections)
	}
	settings := make(map[string][]string)
	for _, line := range sections["[Service]"] {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("invalid setting: %s", line)
		}
		settings[key] = append(settings[key], value)
	}
	if len(settings) != 4 {
		t.Fatalf("service keys: %v", settings)
	}
	for key, want := range map[string]string{"Type": "forking", "PIDFile": "/tmp/a b%%c$d/ikigenba/sandbox/wip/nginx/nginx.pid"} {
		if !reflect.DeepEqual(settings[key], []string{want}) {
			t.Fatalf("%s: %v", key, settings[key])
		}
	}
	if len(settings["ExecStart"]) != 1 {
		t.Fatalf("start settings: %v", settings["ExecStart"])
	}
	start := settings["ExecStart"][0]
	words := parseRoutingConfig(t, "exec "+start+";")[0].words[1:]
	if len(words) != 7 || words[0] != "nginx" {
		t.Fatalf("start command: %v", words)
	}
	pairs := make(map[string]string)
	for n := 1; n < len(words); n += 2 {
		if _, exists := pairs[words[n]]; exists {
			t.Fatal("duplicate start flag")
		}
		pairs[words[n]] = words[n+1]
	}
	wantPairs := map[string]string{"-p": "/tmp/a b%%c$$d/ikigenba/sandbox/wip/nginx", "-c": "/tmp/a b%%c$$d/ikigenba/sandbox/wip/nginx/nginx.conf", "-e": "stderr"}
	if !reflect.DeepEqual(pairs, wantPairs) {
		t.Fatalf("start pairs got %v want %v", pairs, wantPairs)
	}
	for _, path := range []string{wantPairs["-p"], wantPairs["-c"]} {
		if !strings.Contains(start, `"`+path+`"`) {
			t.Fatalf("unit path not quoted: %s", path)
		}
	}
	if !reflect.DeepEqual(settings["ExecReload"], []string{start + " -t", start + " -s reload"}) {
		t.Fatalf("reload order/commands: %v", settings["ExecReload"])
	}
}
