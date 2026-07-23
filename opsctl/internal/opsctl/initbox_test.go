package opsctl

import (
	"context"
	"io"
	"testing"
)

func TestInitBoxDoesNotCreateServedTreeGroup(t *testing.T) {
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

			for _, op := range sys.opSeq() {
				if op == "ensure-group:web" || op == "add-user-to-group:nginx:web" {
					t.Fatalf("init-box requested retired served-tree group op %q; ops = %v", op, sys.opSeq())
				}
			}
		})
	}
}

func TestInitBoxInstallsBaselineCommandLineToolsOnDefaultAndSkipCertPaths(t *testing.T) {
	// R-WHC0-I9HL
	// R-JQGB-RYA2
	for _, tc := range []struct {
		name     string
		skipCert bool
	}{
		{name: "default"},
		{name: "skip-cert", skipCert: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys := &stubSystem{}
			o := &Opsctl{
				Root:    t.TempDir(),
				SysRoot: t.TempDir(),
				System:  sys,
				Out:     io.Discard,
				Err:     io.Discard,
			}

			err := o.InitBox(context.Background(), InitBoxOptions{
				DefaultApp: "dashboard",
				Domain:     "int.ikigenba.com",
				Email:      "ops@example.com",
				ApexBlock:  "server_name __DOMAIN__;\n",
				SkipCert:   tc.skipCert,
			})
			if err != nil {
				t.Fatalf("init-box: %v", err)
			}

			want := "install-packages:nginx,certbot,poppler-utils,git,sqlite,tar,curl-minimal"
			for _, op := range sys.opSeq() {
				if op == want {
					return
				}
			}
			t.Fatalf("init-box did not request %q; ops = %v", want, sys.opSeq())
		})
	}
}

func TestInitBoxInstallsOAuthCLIOnDefaultAndSkipCertPaths(t *testing.T) {
	// R-ML75-3NVZ
	const (
		packages = "install-packages:nginx,certbot,poppler-utils,git,sqlite,tar,curl-minimal"
		oauth    = "install-script:https://raw.githubusercontent.com/ikigenba/oauth/main/install.sh|env:BINDIR=/usr/local/bin"
	)
	for _, tc := range []struct {
		name     string
		skipCert bool
	}{
		{name: "default"},
		{name: "skip-cert", skipCert: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys := &stubSystem{}
			o := &Opsctl{
				Root:    t.TempDir(),
				SysRoot: t.TempDir(),
				System:  sys,
				Out:     io.Discard,
				Err:     io.Discard,
			}
			if err := o.InitBox(context.Background(), InitBoxOptions{
				DefaultApp: "dashboard",
				Domain:     "int.ikigenba.com",
				Email:      "ops@example.com",
				ApexBlock:  "server_name __DOMAIN__;\n",
				SkipCert:   tc.skipCert,
			}); err != nil {
				t.Fatalf("init-box: %v", err)
			}

			ops := sys.opSeq()
			if len(ops) < 2 {
				t.Fatalf("init-box ops = %v, want package and oauth installs first", ops)
			}
			if ops[0] != packages || ops[1] != oauth {
				t.Fatalf("init-box first ops = %v, want [%q %q]", ops[:2], packages, oauth)
			}
		})
	}
}
