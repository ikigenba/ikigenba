package opsctl

import (
	"context"
	"io"
	"testing"
)

func TestInitBoxCreatesWebGroupAndAddsNginxOnNormalAndSkipCertPaths(t *testing.T) {
	// R-AQMT-9M04
	for _, tc := range []struct {
		name     string
		skipCert bool
	}{
		{name: "normal", skipCert: false},
		{name: "skip-cert", skipCert: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			sysRoot := t.TempDir()
			sys := &stubSystem{}
			o := &Opsctl{
				Root:    root,
				SysRoot: sysRoot,
				System:  sys,
				Out:     io.Discard,
				Err:     io.Discard,
			}

			opts := InitBoxOptions{
				DefaultApp: "dashboard",
				Domain:     "int.ikigenba.com",
				Email:      "ops@example.com",
				ApexBlock:  "server_name __DOMAIN__;\n",
				SkipCert:   tc.skipCert,
			}
			if err := o.InitBox(context.Background(), opts); err != nil {
				t.Fatalf("init-box: %v", err)
			}

			ops := sys.opSeq()
			install := requireOp(t, ops, "install-packages:nginx,certbot")
			ensureWeb := requireOp(t, ops, "ensure-group:web")
			addNginx := requireOp(t, ops, "add-user-to-group:nginx:web")
			if ensureWeb <= install {
				t.Fatalf("ensure web group index = %d, want after package install index %d; ops = %v", ensureWeb, install, ops)
			}
			if addNginx <= install {
				t.Fatalf("add nginx to web group index = %d, want after package install index %d; ops = %v", addNginx, install, ops)
			}

			enableNginx := indexOp(ops, "enable-now:nginx")
			reloadNginx := indexOp(ops, "nginx-reload")
			if tc.skipCert {
				if enableNginx != -1 || reloadNginx != -1 {
					t.Fatalf("skip-cert nginx ops = enable %d reload %d, want both absent; ops = %v", enableNginx, reloadNginx, ops)
				}
				return
			}
			if enableNginx == -1 || reloadNginx == -1 {
				t.Fatalf("normal init-box ops missing nginx enable/reload; ops = %v", ops)
			}
			if ensureWeb >= enableNginx || ensureWeb >= reloadNginx {
				t.Fatalf("ensure web group index = %d, want before nginx enable %d and reload %d; ops = %v", ensureWeb, enableNginx, reloadNginx, ops)
			}
			if addNginx >= enableNginx || addNginx >= reloadNginx {
				t.Fatalf("add nginx to web group index = %d, want before nginx enable %d and reload %d; ops = %v", addNginx, enableNginx, reloadNginx, ops)
			}
		})
	}
}

func requireOp(t *testing.T, ops []string, want string) int {
	t.Helper()
	if idx := indexOp(ops, want); idx != -1 {
		return idx
	}
	t.Fatalf("ops missing %q: %v", want, ops)
	return -1
}

func indexOp(ops []string, want string) int {
	for i, op := range ops {
		if op == want {
			return i
		}
	}
	return -1
}
