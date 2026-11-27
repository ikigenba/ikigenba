// Package settings reads the service's runtime settings.
package settings

import (
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// WantSeconds describes accepted drain values.
const WantSeconds string = "a positive whole number of seconds"

// Settings holds startup configuration.
type Settings struct {
	DrainSeconds int64
	Sandbox      bool
	Hosts        Hosts
}

// Error identifies an invalid setting.
type Error struct{ Name, Value, Want string }

func (e *Error) Error() string { return e.Name + " is '" + e.Value + "', not " + e.Want }

// Defaults returns the settings used when variables are absent or empty.
func Defaults() Settings { return Settings{DrainSeconds: 5} }

// Read validates the drain setting and reads the host selection.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	if v, ok := lookup("DRAIN_SECONDS"); ok && v != "" {
		valid := v[0] >= '1' && v[0] <= '9'
		for _, b := range []byte(v) {
			if b < '0' || b > '9' {
				valid = false
			}
		}
		if !valid {
			return Settings{}, &Error{"DRAIN_SECONDS", v, WantSeconds}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		s.DrainSeconds = n
	}
	_, s.Sandbox = lookup("IKIGENBA_SANDBOX")
	get := func(key string) string {
		value, present := lookup(key)
		if !present {
			return ""
		}
		return value
	}
	s.Hosts = Hosts{
		Region:          get("AWS_REGION"),
		Domain:          get("IKIGENBA_HOSTS_DOMAIN"),
		ZoneID:          get("IKIGENBA_HOSTS_ZONE_ID"),
		LaunchTemplate:  get("IKIGENBA_HOSTS_LAUNCH_TEMPLATE"),
		InstanceProfile: get("IKIGENBA_HOSTS_INSTANCE_PROFILE"),
	}
	return s, nil
}

// Drain converts seconds to a saturating duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}

// Hosts names the AWS resources for the managed hosts.
type Hosts struct{ Region, Domain, ZoneID, LaunchTemplate, InstanceProfile string }

// BackupBucket returns the name of the hosts' backup bucket.
func (h Hosts) BackupBucket() string {
	return strings.ReplaceAll(h.Domain, ".", "-") + "-hosts-backups"
}

// BackupRole returns the name of the role hosts assume for backups.
func (h Hosts) BackupRole() string { return h.InstanceProfile + "-backup" }

// Configured reports whether all five settings are present.
func (h Hosts) Configured() bool {
	return h.Region != "" && h.Domain != "" && h.ZoneID != "" && h.LaunchTemplate != "" && h.InstanceProfile != ""
}

// Mode identifies the cloud accounts can use.
type Mode int

// Available cloud modes.
const (
	ModeNone Mode = iota
	ModeAWS
	ModeSandbox
)

// SandboxHosts names the memory cloud resources.
var SandboxHosts = Hosts{"sandbox", "hosts.test", "ZSANDBOX", "sandbox-hosts", "sandbox-hosts"}

// SandboxBinding is the daemon binding when the sandbox has no service URL.
const SandboxBinding = "accounts.sandbox.test"

// ErrNoAccountsURL reports that accounts has no usable service URL.
var ErrNoAccountsURL = errors.New("no accounts URL")

// Cloud selects the cloud from the settings.
func (s Settings) Cloud() Mode {
	if s.Hosts.Configured() {
		return ModeAWS
	}
	if s.Sandbox && s.Hosts == (Hosts{}) {
		return ModeSandbox
	}
	return ModeNone
}

// HostsInUse returns the settings of the selected cloud.
func (s Settings) HostsInUse() Hosts {
	switch s.Cloud() {
	case ModeAWS:
		return s.Hosts
	case ModeSandbox:
		return SandboxHosts
	default:
		return Hosts{}
	}
}

// AccountsURL reads the accounts entry on every call.
func (s Settings) AccountsURL(servicesPath string) (string, error) {
	if list, err := services.Read(servicesPath); err == nil {
		if entry, found := list.Find("accounts"); found {
			parsed, err := url.Parse(entry.URL)
			if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
				return strings.TrimRight(entry.URL, "/"), nil
			}
		}
	}
	if s.Cloud() == ModeSandbox {
		return "http://" + SandboxBinding, nil
	}
	return "", ErrNoAccountsURL
}

// Binding returns the ASCII-lowercase host and optional port of AccountsURL.
func (s Settings) Binding(servicesPath string) (string, error) {
	value, err := s.AccountsURL(servicesPath)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, parsed.Host), nil
}
