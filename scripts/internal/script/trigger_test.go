package script

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateTriggerWellFormedness(t *testing.T) {
	// R-7UZ2-4KOT
	for _, filter := range []string{"create/bills/**", ":create/**", "*:create/**", "drop*:create/**", "github:push/**"} {
		_, err := validateTrigger(filter)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("%q: %v", filter, err)
		}
	}
	_, err := validateTrigger("github:push/**")
	for _, source := range []string{"cron", "crm", "ledger", "dropbox", "prompts", "repos"} {
		if !strings.Contains(err.Error(), source) {
			t.Fatalf("unknown source error does not name %q: %v", source, err)
		}
	}
	if strings.Contains(err.Error(), "scripts") {
		t.Fatalf("unknown source error = %v", err)
	}
	if source, err := validateTrigger("dropbox:create/bills/**"); err != nil || source != "dropbox" {
		t.Fatalf("valid filter = %q, %v", source, err)
	}
}

func TestValidateReposTrigger(t *testing.T) {
	// R-2TS6-VE9D
	for _, filter := range []string{"repos:push/scripts/nightly-export", "repos:push/**"} {
		if source, err := validateTrigger(filter); err != nil || source != "repos" {
			t.Fatalf("validateTrigger(%q) = %q, %v; want repos, nil", filter, source, err)
		}
	}
	if _, err := validateTrigger("repos:nosuchkind/**"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown repos kind error = %v, want ErrValidation", err)
	}

	_, err := validateTrigger("github:push/**")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown source error = %v, want ErrValidation", err)
	}
	for _, source := range []string{"cron", "crm", "ledger", "dropbox", "prompts", "repos"} {
		if !strings.Contains(err.Error(), source) {
			t.Errorf("unknown source error does not name %q: %v", source, err)
		}
	}
	if strings.Contains(err.Error(), "scripts") {
		t.Fatalf("unknown source error names excluded self-chaining source scripts: %v", err)
	}
}

func TestValidateWebhooksTrigger(t *testing.T) {
	// R-IM5H-6CKN
	for _, filter := range []string{"webhooks:received/mg-dev-track", "webhooks:received/**"} {
		if source, err := validateTrigger(filter); err != nil || source != "webhooks" {
			t.Fatalf("validateTrigger(%q) = %q, %v; want webhooks, nil", filter, source, err)
		}
	}
	if _, err := validateTrigger("webhooks:nosuchkind/**"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown webhooks kind error = %v, want ErrValidation", err)
	}

	_, err := validateTrigger("github:push/**")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown source error = %v, want ErrValidation", err)
	}
	for _, source := range []string{"cron", "crm", "dropbox", "ledger", "prompts", "repos", "webhooks"} {
		if !strings.Contains(err.Error(), source) {
			t.Errorf("unknown source error does not name %q: %v", source, err)
		}
	}
	if strings.Contains(err.Error(), "scripts") {
		t.Fatalf("unknown source error names excluded self-chaining source scripts: %v", err)
	}
}

func TestValidateTriggerFamilies(t *testing.T) {
	// R-7W6Y-ICFI
	for _, filter := range []string{"dropbox:create/bills/**/*.pdf", "dropbox:*", "cron:tick/some-schedule-nobody-declared"} {
		if _, err := validateTrigger(filter); err != nil {
			t.Fatalf("%q: %v", filter, err)
		}
	}
	for _, filter := range []string{"dropbox:nosuchkind/**", "dropbox:create/["} {
		if _, err := validateTrigger(filter); !errors.Is(err, ErrValidation) {
			t.Fatalf("%q: %v", filter, err)
		}
	}
}
