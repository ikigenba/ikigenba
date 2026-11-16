package nginx_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/domain"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

// R-97TC-Q273 R-9919-3TXS R-9A95-HLOH R-9BH1-VDF6 R-9COY-955V R-5JD4-BTQO
// R-8Y25-NW9J R-8ZA2-1O08 R-90HY-FFQX R-91PU-T7HM R-945N-KQZ0 R-95DJ-YIPP R-96LG-CAGE
func TestCustomDomainsExactBlocksLeaveFrameAndServicesUnchanged(t *testing.T) {
	for _, state := range []string{"absent", "data-only", "unrouted", "active", "disabled"} {
		for _, authenticated := range []bool{false, true} {
			for _, attributes := range []string{"", "guests = true\ndefault = true\nmcp = true\ndescription = 'tools'\n"} {
				t.Run(fmt.Sprintf("%s/auth=%v/attributes=%q", state, authenticated, attributes), func(t *testing.T) {
					root := t.TempDir()
					if authenticated {
						writeManifest(t, root, "auth", "app = 'auth'\n")
					}
					switch state {
					case "data-only":
						mkdir(t, filepath.Join(root, "var/opt/ikigenba/sites/state"))
					case "unrouted":
						writeManifest(t, root, "sites", "guests = true\n")
					case "active", "disabled":
						writeManifest(t, root, "sites", "app = 'sites'\n"+attributes)
					}
					env := host.Env{Root: root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
						if state == "disabled" && c.Args[3] == "ikigenba-sites.socket" {
							return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
						}
						return host.Result{}, nil
					}}
					base, err := nginx.Render(context.Background(), env, "space.example.test", "")
					if err != nil {
						t.Fatal(err)
					}
					store := config.Store{Root: root}
					if err := domain.Save(store, []domain.Domain{{Name: "z.example"}, {Name: "a.example", Primary: "z.example"}, {Name: "m.example"}, {Name: "b.example", Primary: "m.example"}}); err != nil {
						t.Fatal(err)
					}
					before := snapshotTree(t, root)
					candidate, err := nginx.Render(context.Background(), env, "space.example.test", "")
					if err != nil {
						t.Fatal(err)
					}
					want := string(base)
					for _, d := range []domain.Domain{{Name: "a.example", Primary: "z.example"}, {Name: "b.example", Primary: "m.example"}, {Name: "m.example"}, {Name: "z.example"}} {
						block := customBlock(d, state)
						want += "\n" + block
						actual := serverBlockFor(t, string(candidate), d.Name)
						if actual != block {
							t.Fatalf("domain %s block = %q, want %q", d.Name, actual, block)
						}
						for _, forbidden := range []string{"add_header", "if ", "include ", "auth_request", "error_page", "$ikigenba_", "proxy_set_header Origin", "/mcp", "/api", "info/refs", "git-upload-pack", "git-receive-pack"} {
							if strings.Contains(actual, forbidden) {
								t.Fatalf("custom block contains %q", forbidden)
							}
						}
					}
					if string(candidate) != want {
						t.Fatalf("custom domains changed frame/services or order\ngot: %s\nwant: %s", candidate, want)
					}
					if !reflect.DeepEqual(before, snapshotTree(t, root)) {
						t.Fatal("Render changed root")
					}
				})
			}
		}
	}
}

func customBlock(d domain.Domain, state string) string {
	block := "server {\n    listen              443 ssl;\n    server_name         " + d.Name + ";\n" +
		"    ssl_certificate     /etc/letsencrypt/live/" + d.Name + "/fullchain.pem;\n" +
		"    ssl_certificate_key /etc/letsencrypt/live/" + d.Name + "/privkey.pem;\n" +
		"    access_log          /var/log/nginx/access.log ikigenba;\n"
	switch {
	case d.Primary != "":
		block += "    return              301 https://" + d.Primary + "$request_uri;\n"
	case state == "disabled":
		block += "    return              503;\n"
	case state != "active":
		block += "    return              404;\n"
	default:
		block += "\n    location / {\n" +
			"        proxy_pass       http://unix:/run/ikigenba/sites.sock:;\n" +
			"        proxy_set_header Host              $host;\n" +
			"        proxy_set_header X-Real-IP         $remote_addr;\n" +
			"        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;\n" +
			"        proxy_set_header X-Forwarded-Proto $scheme;\n" +
			"        proxy_set_header X-Request-Id      $request_id;\n" +
			"        proxy_set_header X-User-Id         \"\";\n" +
			"        proxy_set_header X-User-Email      \"\";\n" +
			"    }\n"
	}
	return block + "}\n"
}

// R-9K0C-JRM1
func TestHTTPChallengeLocationPrecedesRedirectWithoutProxy(t *testing.T) {
	root := t.TempDir()
	if err := domain.Save(config.Store{Root: root}, []domain.Domain{{Name: "custom.example"}}); err != nil {
		t.Fatal(err)
	}
	candidate, err := nginx.Render(context.Background(), enabledEnv(root), "space.example.test", "")
	if err != nil {
		t.Fatal(err)
	}
	servers := strings.Split(string(candidate), "server {\n")
	if len(servers) < 2 {
		t.Fatal("missing HTTP server")
	}
	block := "server {\n" + strings.TrimSuffix(servers[1], "\n")
	want := "server {\n    listen      80 default_server;\n    listen      [::]:80 default_server;\n    server_name _;\n    access_log  /var/log/nginx/access.log ikigenba;\n\n" +
		"    location ^~ /.well-known/acme-challenge/ {\n        root " + cert.Webroot + ";\n    }\n\n" +
		"    location / {\n        return 301 https://$host$request_uri;\n    }\n}\n"
	if block != want {
		t.Fatalf("HTTP block = %q, want %q", block, want)
	}
	if strings.Contains(block, root) || strings.Contains(block, "proxy_") {
		t.Fatalf("HTTP host path/proxy: %s", block)
	}
}

// R-5GXB-KA9A
func TestDomainListFailurePrecedesEveryCommandAndMutation(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		root := t.TempDir()
		store := config.Store{Root: root}
		if err := store.Set(domain.Key, "example.com,Example.org"); err != nil {
			t.Fatal(err)
		}
		if corrupt {
			if err := os.WriteFile(filepath.Join(root, "etc/ikigenba/config.json"), []byte("corrupt"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		writeManifest(t, root, "sites", "app = 'sites'\n")
		mkdir(t, filepath.Join(root, "etc/nginx/conf.d"))
		if err := os.WriteFile(filepath.Join(root, "etc/nginx/conf.d/ikigenba.conf"), []byte("previous"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, cause := domain.List(store)
		if cause == nil {
			t.Fatal("fixture has no error")
		}
		before := snapshotTree(t, root)
		env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("domain failure executed a command")
			return host.Result{}, nil
		}}
		candidate, err := nginx.Render(context.Background(), env, "space.example.test", "")
		if candidate != nil {
			t.Fatalf("failed Render candidate: %s", candidate)
		}
		results := []error{err, nginx.Apply(context.Background(), env, "space.example.test", ""), nginx.Write(context.Background(), env, "space.example.test", "")}
		changed, err := nginx.Update(context.Background(), env, "space.example.test", "")
		if changed {
			t.Fatal("failed Update returned true")
		}
		results = append(results, err)
		for _, err := range results {
			if err == nil || err.Error() != cause.Error() {
				t.Fatalf("error = %v, want %v", err, cause)
			}
			for _, sentinel := range []error{domain.ErrMalformed, config.ErrCorrupt} {
				if errors.Is(err, sentinel) != errors.Is(cause, sentinel) {
					t.Fatalf("error identity lost: %v", err)
				}
			}
		}
		if !reflect.DeepEqual(before, snapshotTree(t, root)) {
			t.Fatal("domain failure mutated root")
		}
	}
}

// R-NSP6-V1KA R-NTX3-8TAZ
func TestWritersCreateWebrootAndPreserveExistingParents(t *testing.T) {
	// This test is deliberately sequential: umask belongs to the process.
	previousMask := syscall.Umask(0o077)
	defer syscall.Umask(previousMask)
	for _, op := range []string{"Apply", "Write", "Update"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%v", op, existing), func(t *testing.T) {
				root := t.TempDir()
				mkdir(t, filepath.Join(root, "etc/nginx/conf.d"))
				webroot := filepath.Join(root, cert.Webroot)
				parent := filepath.Join(root, "var")
				var owner *syscall.Stat_t
				if existing {
					mkdir(t, webroot)
					if err := syscall.Chmod(parent, 0o711); err != nil {
						t.Fatal(err)
					}
					if err := syscall.Chmod(webroot, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(webroot, "keep"), []byte("contents"), 0o600); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(webroot)
					if err != nil {
						t.Fatal(err)
					}
					owner = info.Sys().(*syscall.Stat_t)
				}
				commands := 0
				env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
					commands++
					info, err := os.Stat(webroot)
					if err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
						t.Fatalf("command before webroot ready: %v", err)
					}
					return host.Result{}, nil
				}}
				if err := runWriter(op, env); err != nil {
					t.Fatal(err)
				}
				wantCommands := 2
				if op == "Write" {
					wantCommands = 0
				}
				if commands != wantCommands {
					t.Fatalf("commands=%d", commands)
				}
				for _, path := range []string{parent, filepath.Join(root, "var/www"), webroot} {
					info, err := os.Lstat(path)
					if err != nil {
						t.Fatal(err)
					}
					want := os.FileMode(0o755)
					if existing && path != webroot {
						want = 0o700
						if path == parent {
							want = 0o711
						}
					}
					if !info.IsDir() || info.Mode().Perm() != want {
						t.Fatalf("%s mode=%v, want %v", path, info.Mode(), want)
					}
				}
				if existing {
					info, err := os.Stat(webroot)
					if err != nil {
						t.Fatal(err)
					}
					after := info.Sys().(*syscall.Stat_t)
					if owner.Uid != after.Uid || owner.Gid != after.Gid {
						t.Fatal("webroot owner changed")
					}
					rootFS, err := os.OpenRoot(webroot)
					if err != nil {
						t.Fatal(err)
					}
					contents, err := rootFS.ReadFile("keep")
					if closeErr := rootFS.Close(); closeErr != nil {
						t.Fatal(closeErr)
					}
					if err != nil || string(contents) != "contents" {
						t.Fatalf("contents changed: %q %v", contents, err)
					}
				}
			})
		}
	}
}

func runWriter(op string, env host.Env) error {
	switch op {
	case "Apply":
		return nginx.Apply(context.Background(), env, "space.example.test", "")
	case "Write":
		return nginx.Write(context.Background(), env, "space.example.test", "")
	default:
		changed, err := nginx.Update(context.Background(), env, "space.example.test", "")
		if err == nil && !changed {
			return errors.New("Update returned unchanged")
		}
		return err
	}
}

// R-NTX3-8TAZ
func TestUnchangedUpdateLeavesWebrootAbsentOrItsModeAlone(t *testing.T) {
	for _, exists := range []bool{false, true} {
		root := t.TempDir()
		env := enabledEnv(root)
		candidate, err := nginx.Render(context.Background(), env, "space.example.test", "")
		if err != nil {
			t.Fatal(err)
		}
		mkdir(t, filepath.Join(root, "etc/nginx/conf.d"))
		if err := os.WriteFile(filepath.Join(root, "etc/nginx/conf.d/ikigenba.conf"), candidate, 0o600); err != nil {
			t.Fatal(err)
		}
		if exists {
			mkdir(t, filepath.Join(root, cert.Webroot))
		}
		before := snapshotTree(t, root)
		changed, err := nginx.Update(context.Background(), env, "space.example.test", "")
		if err != nil || changed {
			t.Fatalf("Update = %v, %v", changed, err)
		}
		if !reflect.DeepEqual(before, snapshotTree(t, root)) {
			t.Fatal("unchanged Update changed root")
		}
	}
}

// R-G20E-74Q1
func TestWebrootFailurePreservesConfigurationWithoutExecuting(t *testing.T) {
	for _, op := range []string{"Apply", "Write", "Update"} {
		for _, blocked := range []string{"var", "var/www", "var/www/acme"} {
			for _, prior := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/prior=%v", op, blocked, prior), func(t *testing.T) {
					root := t.TempDir()
					directory := filepath.Join(root, "etc/nginx/conf.d")
					mkdir(t, directory)
					path := filepath.Join(root, blocked)
					mkdir(t, filepath.Dir(path))
					if err := os.WriteFile(path, []byte("block"), 0o600); err != nil {
						t.Fatal(err)
					}
					if prior {
						if err := os.WriteFile(filepath.Join(directory, "ikigenba.conf"), []byte("old bytes"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
						t.Fatal("webroot failure executed")
						return host.Result{}, nil
					}}
					var err error
					if op == "Update" {
						var changed bool
						changed, err = nginx.Update(context.Background(), env, "space.example.test", "")
						if changed {
							t.Fatalf("Update returned changed=true on webroot failure: %v", err)
						}
					} else {
						err = runWriter(op, env)
					}
					if !errors.Is(err, syscall.ENOTDIR) {
						t.Fatalf("error = %v, want wrapped ENOTDIR", err)
					}
					if prior {
						assertPublishedConfiguration(t, directory, []byte("old bytes"), 0o600)
					} else {
						entries, err := os.ReadDir(directory)
						if err != nil || len(entries) != 0 {
							t.Fatalf("left candidate/temporary: %v %v", entries, err)
						}
					}
				})
			}
		}
	}
}

// R-NSP6-V1KA R-G20E-74Q1
func TestWebrootFollowsDirectorySymlinkAndRejectsFileSymlink(t *testing.T) {
	for _, directory := range []bool{false, true} {
		root := t.TempDir()
		mkdir(t, filepath.Join(root, "etc/nginx/conf.d"))
		mkdir(t, filepath.Join(root, "var/www"))
		target := filepath.Join(root, "target")
		if directory {
			mkdir(t, target)
		} else if err := os.WriteFile(target, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, cert.Webroot)); err != nil {
			t.Fatal(err)
		}
		err := nginx.Write(context.Background(), enabledEnv(root), "space.example.test", "")
		if !directory {
			if !errors.Is(err, syscall.ENOTDIR) {
				t.Fatalf("error=%v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("target mode=%v, %v", info, err)
		}
		info, err = os.Lstat(filepath.Join(root, cert.Webroot))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlink replaced: %v %v", info, err)
		}
	}
}
