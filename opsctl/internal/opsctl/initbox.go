package opsctl

import (
	"context"
	"fmt"
	"os"
)

// InitBoxOptions parameterises `opsctl init-box` — the box-GLOBAL substrate (PLAN
// §D1, ADR "init-box vs setup"). It carries the apex routing the old
// dashboard/bin/setup sourced from etc/manifest.env + etc/deploy.env so init-box
// can emit a byte-identical apex server block.
type InitBoxOptions struct {
	// DefaultApp is the apex/DEFAULT app's name (today "dashboard"). The apex
	// server block lands at conf.d/<DefaultApp>.conf; the apex app's loopback port
	// is a literal in that block (its fixed registry port).
	DefaultApp string
	Domain     string // apex domain (e.g. int.ikigenba.com) — __DOMAIN__ in the block
	Email      string // CERTBOT_EMAIL — for HTTP-01 cert issuance
	ApexBlock  string // the apex nginx server{} SOURCE (committed dashboard
	// etc/nginx.conf, with the __DOMAIN__ placeholder).
	// SkipCert short-circuits the certbot call (the box ops are still recorded);
	// init-box is otherwise idempotent and certbot reuses a live cert.
	SkipCert bool
}

// InitBox provisions the one-time, box-global substrate the apex used to
// bootstrap inside dashboard/bin/setup — split out so per-app setup never reaches
// for global state (PLAN §D1):
//
//  1. install the box-baseline packages and oauth CLI (seam),
//  2. create the conf.d/locations/ include dir + the letsencrypt webroot,
//  3. write the apex nginx server{} block (with /_authn + the locations include),
//  4. nginx -t + enable+reload nginx (seam),
//  5. obtain the apex TLS cert via certbot HTTP-01 webroot (seam),
//  6. write + enable-now the certbot renewal timer.
//
// Config artifacts (the apex block, the renew timer/service) are WRITTEN to
// SysRoot-rooted paths so tests byte-assert them; the imperative box ops (package
// install, nginx validate/reload, certbot, timer enable) go through the System
// seam. It is idempotent and per-box, not per-app.
func (o *Opsctl) InitBox(ctx context.Context, opts InitBoxOptions) error {
	if err := validateInitBoxOptions(opts); err != nil {
		return err
	}
	l := o.layout(opts.DefaultApp)

	// 1. Packages: nginx + certbot (front door), poppler-utils (the
	// box-baseline PDF tooling — pdftotext/pdftoppm/pdfinfo — that sandboxed
	// prompt runs rely on), git, sqlite, and the tools needed by release
	// installers.
	o.logf("install nginx + certbot + poppler-utils + git + sqlite + tar + curl-minimal")
	if err := o.System.InstallPackages(ctx, "nginx", "certbot", "poppler-utils", "git", "sqlite", "tar", "curl-minimal"); err != nil {
		return fmt.Errorf("init-box: install packages: %w", err)
	}
	o.logf("install oauth CLI")
	if err := o.System.InstallScript(ctx, "https://raw.githubusercontent.com/ikigenba/oauth/main/install.sh", "BINDIR=/usr/local/bin"); err != nil {
		return fmt.Errorf("init-box: install oauth: %w", err)
	}

	// 2. The box-global include dir + the HTTP-01 webroot.
	o.logf("create %s + %s", l.LocationsDir(), l.LetsEncryptWebroot())
	if err := mkdirAll755(l.NginxConfDir(), l.LocationsDir(), l.LetsEncryptWebroot()); err != nil {
		return fmt.Errorf("init-box: create dirs: %w", err)
	}

	// 3. The apex server block (carries /_authn + the locations include).
	o.logf("write apex nginx block %s", l.ApexBlockPath())
	block := renderApexBlock(opts.ApexBlock, opts.Domain)
	if err := writeFileAtomic(l.ApexBlockPath(), []byte(block)); err != nil {
		return fmt.Errorf("init-box: write apex block: %w", err)
	}

	// 4 + 5. Bring nginx up and obtain the apex TLS cert — UNLESS --skip-cert.
	//
	// The apex block's 443 server references the apex cert by path, so `nginx -t`
	// (and therefore enable/reload) cannot succeed until that cert EXISTS. This is
	// a chicken-and-egg on a greenfield box: nginx can't validate without the
	// cert, but the usual HTTP-01 webroot issuance needs nginx already serving
	// :80. We break it by bootstrapping the FIRST cert via certbot --standalone
	// (certbot binds :80 itself, nginx not running) BEFORE `nginx -t`. Once the
	// cert is on disk, nginx -t passes and we enable+reload. On reruns the cert
	// already exists, so we skip the standalone bootstrap and just (re)validate +
	// reload nginx; the renewal timer owns ongoing renewals via webroot.
	//
	// --skip-cert stages the block WITHOUT validating/starting nginx or issuing a
	// cert (the block + locations dir are still written above); used to defer the
	// whole cert/nginx bring-up.
	if err := o.configureApexCert(ctx, opts, l); err != nil {
		return err
	}

	// 6. The suite-owned timers, enabled now: certbot renewal and the nightly
	//    backup sweep.
	if err := o.installBoxTimers(ctx, l); err != nil {
		return err
	}

	o.logf("init-box complete — next: opsctl setup <app> per service")
	return nil
}

func validateInitBoxOptions(opts InitBoxOptions) error {
	if opts.DefaultApp == "" {
		return fmt.Errorf("init-box: default app name is required")
	}
	if opts.Domain == "" {
		return fmt.Errorf("init-box: domain is required")
	}
	if opts.ApexBlock == "" {
		return fmt.Errorf("init-box: apex nginx block source is required")
	}
	return nil
}

func (o *Opsctl) configureApexCert(ctx context.Context, opts InitBoxOptions, l Layout) error {
	if opts.SkipCert {
		o.logf("skip-cert: staging apex block only (nginx not validated/started; cert issued later)")
		return nil
	}
	if opts.Email == "" {
		return fmt.Errorf("init-box: certbot email is required (set --email or --skip-cert)")
	}
	if !o.System.CertExists(opts.Domain) {
		o.logf("bootstrap first apex cert for %s (certbot --standalone, nginx not yet up)", opts.Domain)
		if err := o.System.ObtainCertStandalone(ctx, opts.Domain, opts.Email); err != nil {
			return fmt.Errorf("init-box: bootstrap apex cert: %w", err)
		}
	} else {
		o.logf("apex cert for %s already present — skipping standalone bootstrap", opts.Domain)
	}
	if err := o.System.NginxTest(ctx); err != nil {
		return fmt.Errorf("init-box: nginx -t: %w", err)
	}
	if err := o.System.EnableUnit(ctx, "nginx", true); err != nil {
		return fmt.Errorf("init-box: enable nginx: %w", err)
	}
	if err := o.System.NginxReload(ctx); err != nil {
		return fmt.Errorf("init-box: nginx reload: %w", err)
	}
	o.logf("reconcile apex cert renewal to webroot for %s", opts.Domain)
	if err := o.System.ObtainCert(ctx, opts.Domain, opts.Email, l.LetsEncryptWebroot()); err != nil {
		return fmt.Errorf("init-box: reconcile cert renewal: %w", err)
	}
	return nil
}

func (o *Opsctl) installBoxTimers(ctx context.Context, l Layout) error {
	files := []struct {
		name, path, body string
	}{
		{"renew service", l.RenewServicePath(), renewService},
		{"renew timer", l.RenewTimerPath(), renewTimer},
		{"backup service", l.BackupServicePath(), backupService},
		{"backup timer", l.BackupTimerPath(), backupTimer},
	}
	for _, file := range files {
		if err := writeFileAtomic(file.path, []byte(file.body)); err != nil {
			return fmt.Errorf("init-box: write %s: %w", file.name, err)
		}
	}
	if err := o.System.DaemonReload(ctx); err != nil {
		return fmt.Errorf("init-box: daemon-reload: %w", err)
	}
	for _, unit := range []string{"ikigenba-certbot-renew.timer", "ikigenba-backup.timer"} {
		if err := o.System.EnableUnit(ctx, unit, true); err != nil {
			return fmt.Errorf("init-box: enable %s: %w", unit, err)
		}
	}
	return nil
}

// LoadApexBlockFile reads the apex nginx block source (the committed dashboard
// etc/nginx.conf) for the CLI path.
func LoadApexBlockFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("init-box: --apex-block <path> is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("init-box: read apex block %s: %w", path, err)
	}
	return string(b), nil
}
