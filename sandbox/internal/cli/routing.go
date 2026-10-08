package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

// nginxWord quotes an argument for nginx's configuration parser. Backslashes
// protecting glob metacharacters survive parsing through their doubled form.
func nginxWord(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func renderNginxConfig(data, worktree, name string, port, euid int, apps []appInfo) []byte {
	var b strings.Builder
	directive := func(indent, key string, args ...string) {
		fmt.Fprintf(&b, "%s%s %s;\n", indent, key, strings.Join(args, " "))
	}
	nginxDir := filepath.Join(data, "nginx")
	directive("", "pid", nginxWord(filepath.Join(nginxDir, "nginx.pid")))
	directive("", "error_log", "stderr")
	b.WriteString("events {}\nhttp {\n")
	directive("  ", "access_log", "off")
	for _, temp := range []struct{ key, dir string }{
		{"client_body_temp_path", "client_body"}, {"proxy_temp_path", "proxy"},
		{"fastcgi_temp_path", "fastcgi"}, {"uwsgi_temp_path", "uwsgi"}, {"scgi_temp_path", "scgi"},
	} {
		directive("  ", temp.key, nginxWord(filepath.Join(nginxDir, temp.dir)))
	}
	directive("  ", "server_names_hash_bucket_size", "256")
	sitesOrigin := fmt.Sprintf("http://sites.%s.localhost:%d", name, port)
	b.WriteString("  map $http_origin $sandbox_cors_origin {\n")
	directive("    ", "default", nginxWord(""))
	directive("    ", "~^"+strings.ReplaceAll(sitesOrigin, ".", `\.`)+"$", "$http_origin")
	b.WriteString("  }\n  map $sandbox_cors_origin $sandbox_cors_credentials {\n")
	directive("    ", nginxWord(""), nginxWord(""))
	directive("    ", "default", "true")
	b.WriteString("  }\n")
	for _, header := range [][2]string{
		{"methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS"},
		{"headers", "Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID"},
		{"max_age", "600"},
		{"expose", ""},
	} {
		fmt.Fprintf(&b, "  map %s $sandbox_cors_%s {\n", nginxWord("$request_method $sandbox_cors_credentials"), header[0])
		directive("    ", "default", nginxWord(""))
		directive("    ", nginxWord("OPTIONS true"), nginxWord(header[1]))
		if header[0] == "expose" {
			directive("    ", nginxWord("~ true$"), nginxWord("Mcp-Session-Id, WWW-Authenticate"))
		}
		b.WriteString("  }\n")
	}
	corsHeaders := func(indent string, preflight bool) {
		for _, header := range [][2]string{
			{"Access-Control-Allow-Origin", "$sandbox_cors_origin"},
			{"Access-Control-Allow-Credentials", "$sandbox_cors_credentials"},
		} {
			directive(indent, "add_header", header[0], header[1], "always")
		}
		if preflight {
			for _, header := range [][2]string{
				{"Access-Control-Allow-Methods", "$sandbox_cors_methods"},
				{"Access-Control-Allow-Headers", "$sandbox_cors_headers"},
				{"Access-Control-Max-Age", "$sandbox_cors_max_age"},
			} {
				directive(indent, "add_header", header[0], header[1], "always")
			}
		}
		directive(indent, "add_header", "Access-Control-Expose-Headers", "$sandbox_cors_expose", "always")
		directive(indent, "add_header", "Vary", "Origin", "always")
	}
	hasAuth := false
	for _, app := range apps {
		hasAuth = hasAuth || app.Name == "auth"
		fmt.Fprintf(&b, "  upstream app_%s {\n", app.Name)
		directive("    ", "server", nginxWord(fmt.Sprintf("unix:/run/user/%d/sandbox/%d/%s.sock", euid, port, app.Name)))
		b.WriteString("  }\n")
	}
	listen := fmt.Sprintf("127.0.0.1:%d", port)
	b.WriteString("  server {\n")
	directive("    ", "listen", listen, "default_server")
	directive("    ", "return", "404")
	b.WriteString("  }\n")
	authOrigin := fmt.Sprintf("http://auth.%s.localhost:%d", name, port)
	mcpMetadata := fmt.Sprintf("http://mcp.%s.localhost:%d/.well-known/oauth-protected-resource", name, port)
	if hasAuth {
		b.WriteString("  server {\n")
		directive("    ", "listen", listen)
		directive("    ", "server_name", "localhost")
		directive("    ", "return", "302", nginxWord(authOrigin+"$request_uri"))
		b.WriteString("  }\n")
	}
	forward := func(userID, email string) {
		for _, header := range [][2]string{
			{"Host", "$http_host"}, {"X-Real-IP", "$remote_addr"}, {"X-Forwarded-For", "$remote_addr"},
			{"X-Forwarded-Proto", "http"}, {"X-Request-Id", "$request_id"},
			{"X-User-Id", userID}, {"X-User-Email", email},
		} {
			directive("      ", "proxy_set_header", header[0], header[1])
		}
	}
	for _, app := range apps {
		b.WriteString("  server {\n")
		directive("    ", "listen", listen)
		names := []string{app.Name + "." + name + ".localhost"}
		if app.Default {
			names = append(names, name+".localhost")
		}
		directive("    ", "server_name", names...)
		corsHeaders("    ", true)
		b.WriteString("    if ($request_method = OPTIONS) {\n      return 204;\n    }\n")
		escapedTree := strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`).Replace(worktree)
		directive("    ", "include", nginxWord(filepath.Join(escapedTree, app.Name, "etc", "nginx.conf*")))
		if !hasAuth || app.Name == "auth" {
			b.WriteString("    location / {\n")
			directive("      ", "proxy_pass", "http://app_"+app.Name)
			forward(`""`, `""`)
			b.WriteString("    }\n")
			if app.Name == "auth" {
				b.WriteString("    location = /check {\n      return 404;\n    }\n    location = /check/open {\n      return 404;\n    }\n")
			}
		} else {
			checks := []struct{ location, path string }{{"/_sandbox/auth", "/check"}}
			if app.Guests {
				checks = append(checks, struct{ location, path string }{"/_sandbox/auth_open", "/check/open"})
			}
			for _, check := range checks {
				fmt.Fprintf(&b, "    location = %s {\n      internal;\n", check.location)
				directive("      ", "proxy_pass", "http://app_auth"+check.path)
				directive("      ", "proxy_pass_request_body", "off")
				directive("      ", "proxy_set_header", "Content-Length", `""`)
				directive("      ", "proxy_set_header", "X-Original-Method", "$request_method")
				directive("      ", "proxy_set_header", "X-Original-Host", "$host")
				directive("      ", "proxy_set_header", "X-Original-URI", "$request_uri")
				forward(`""`, `""`)
				b.WriteString("    }\n")
			}
			for _, location := range []struct{ path, handler string }{
				{"/", "signin"}, {"= /mcp", "bearer"}, {"^~ /mcp/", "bearer"},
				{"= /api", "api"}, {"^~ /api/", "api"},
				{"~ /(info/refs|git-upload-pack|git-receive-pack)$", "git"},
			} {
				fmt.Fprintf(&b, "    location %s {\n", location.path)
				check := "/_sandbox/auth"
				open := app.Guests && location.path == "/"
				if open {
					check = "/_sandbox/auth_open"
				}
				directive("      ", "auth_request", check)
				directive("      ", "auth_request_set", "$sandbox_user_id", "$upstream_http_x_user_id")
				directive("      ", "auth_request_set", "$sandbox_user_email", "$upstream_http_x_user_email")
				if !open {
					directive("      ", "error_page", "401", "=", "@sandbox_"+location.handler)
				}
				if location.handler == "bearer" {
					directive("      ", "error_page", "403", "=", "@sandbox_invalid_token")
				}
				directive("      ", "proxy_pass", "http://app_"+app.Name)
				forward("$sandbox_user_id", "$sandbox_user_email")
				b.WriteString("    }\n")
			}
			for _, handler := range []string{"signin", "bearer", "git", "api"} {
				fmt.Fprintf(&b, "    location @sandbox_%s {\n", handler)
				directive("      ", "satisfy", "any")
				directive("      ", "allow", "all")
				directive("      ", "try_files", "/.sandbox-none", "@sandbox_"+handler+"_reply")
				b.WriteString("    }\n")
			}
			b.WriteString("    location @sandbox_signin_reply {\n")
			directive("      ", "return", "302", nginxWord(authOrigin+"/?return=$scheme://$http_host$request_uri"))
			b.WriteString("    }\n    location @sandbox_bearer_reply {\n")
			directive("      ", "default_type", "text/plain")
			directive("      ", "add_header", "WWW-Authenticate", nginxWord(`Bearer realm="ikigenba", resource_metadata="`+mcpMetadata+`"`), "always")
			corsHeaders("      ", false)
			directive("      ", "return", "401", `"authentication required: send Authorization: Bearer <token>\n"`)
			b.WriteString("    }\n    location @sandbox_invalid_token {\n")
			directive("      ", "default_type", "text/plain")
			directive("      ", "add_header", "WWW-Authenticate", nginxWord(`Bearer error="invalid_token", resource_metadata="`+mcpMetadata+`"`), "always")
			corsHeaders("      ", false)
			directive("      ", "return", "401", `"authentication required: send Authorization: Bearer <token>\n"`)
			b.WriteString("    }\n    location @sandbox_git_reply {\n")
			directive("      ", "default_type", "text/plain")
			directive("      ", "add_header", "WWW-Authenticate", nginxWord(`Basic realm="ikigenba"`), "always")
			corsHeaders("      ", false)
			directive("      ", "return", "401", `"authentication required: send your token as the password\n"`)
			b.WriteString("    }\n    location @sandbox_api_reply {\n")
			directive("      ", "default_type", "text/plain")
			directive("      ", "add_header", "WWW-Authenticate", nginxWord(`Bearer realm="ikigenba"`), "always")
			corsHeaders("      ", false)
			directive("      ", "return", "401", `"authentication required: sign in or send Authorization: Bearer <token>\n"`)
			b.WriteString("    }\n")
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func renderNginxUnit(data, name string) []byte {
	nginxDir := filepath.Join(data, "nginx")
	argument := func(path string) string {
		return `"` + strings.NewReplacer("%", "%%", "$", "$$").Replace(path) + `"`
	}
	start := "nginx -p " + argument(nginxDir) + " -c " + argument(filepath.Join(nginxDir, "nginx.conf")) + " -e stderr"
	return []byte(fmt.Sprintf("[Unit]\nDescription=sandbox %s: nginx\n\n[Service]\nType=forking\nPIDFile=%s\nExecStart=%s\nExecReload=%s -t\nExecReload=%s -s reload\nSlice=%s\n", name,
		strings.ReplaceAll(filepath.Join(nginxDir, "nginx.pid"), "%", "%%"), start, start, start, sandboxSliceName(name, "core")))
}
