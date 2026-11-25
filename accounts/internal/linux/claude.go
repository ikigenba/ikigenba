package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

// Claude Code's pinned repository key, state paths and pacing.
const (
	ClaudeKeyURL          = "https://downloads.claude.ai/keys/claude-code.asc"
	ClaudeKeyFingerprint  = "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"
	ClaudeKeyFile         = "/var/lib/accountsd/claude-code.asc"
	GnupgHome             = "/var/lib/accountsd/gnupg"
	ClaudeStamp           = "/var/lib/accountsd/claude-code-upgraded"
	MaxKeyBytes           = 64 << 10
	FetchTimeout          = time.Minute
	ClaudeUpgradeInterval = 24 * time.Hour
	ClaudeRetry           = time.Hour
)

// DiagClaudeCode introduces a failure of the optional Claude Code part.
const DiagClaudeCode = "cannot install or update Claude Code"

// ErrClaudeKey means the download did not prove it contains only the pinned key.
var ErrClaudeKey = errors.New("the Claude Code signing key is not the pinned key")

const (
	claudeSettingsDir = "/etc/claude-code"
	claudeShareDir    = "/usr/local/share/accountsd"
	claudeMarketDir   = claudeShareDir + "/claude"
	claudePluginsDir  = claudeMarketDir + "/plugins"
	claudePluginDir   = claudePluginsDir + "/accounts"
	claudeRepo        = "/etc/yum.repos.d/claude-code.repo"
	claudeRepoBytes   = "[claude-code]\nname=Claude Code\nbaseurl=https://downloads.claude.ai/claude-code/rpm/stable\nenabled=1\ngpgcheck=1\ngpgkey=" + ClaudeKeyURL + "\n"

	claudeFetchUnavailable = "no signing key download configured"
	claudeHTTPStatus       = "GET: HTTP status %d"
)

// Get makes a single GET and leaves a successful response body to the caller.
func Get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusOK && ctx.Err() == nil {
		return res.Body, nil
	}
	_ = res.Body.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf(claudeHTTPStatus, res.StatusCode)
}

func (h *Host) applyClaude(ctx context.Context) {
	err := h.configureClaude(ctx)
	if err != nil {
		// Configuration can fail before the key step gets to remove a stale download.
		_, _ = h.remove(ClaudeKeyFile)
	} else {
		now := h.env.Now()
		if h.claudeFailed && !now.Before(h.claudeFailAt) && now.Before(h.claudeFailAt.Add(ClaudeRetry)) {
			return
		}
		err = h.ensureClaudeKey(ctx)
		if err == nil {
			err = h.ensureClaudePackage(ctx)
		}
	}
	if err == nil {
		h.claudeFailed = false
		return
	}
	h.claudeFailed, h.claudeFailAt = true, h.env.Now()
	if h.env.Stderr != nil {
		line := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
		_, _ = fmt.Fprintf(h.env.Stderr, "accountsd: %s: %s\n", DiagClaudeCode, line)
	}
}

func (h *Host) configureClaude(ctx context.Context) error {
	for _, dir := range []string{
		claudeSettingsDir, claudeShareDir, claudeMarketDir, claudeMarketDir + "/.claude-plugin",
		claudePluginsDir, claudePluginDir, claudePluginDir + "/.claude-plugin",
	} {
		if _, err := h.dir(dir, 0o755); err != nil {
			return err
		}
		if err := h.secure(ctx, dir); err != nil {
			return err
		}
	}
	for _, file := range []struct{ path, text string }{
		{claudeSettingsDir + "/managed-settings.json", `{"extraKnownMarketplaces":{"ikigenba":{"source":{"source":"directory","path":"` + claudeMarketDir + `"}}},"enabledPlugins":{"accounts@ikigenba":true}}`},
		{claudeMarketDir + "/.claude-plugin/marketplace.json", `{"name":"ikigenba","owner":{"name":"ikigenba"},"plugins":[{"name":"accounts","source":"./plugins/accounts"}]}`},
		{claudePluginDir + "/.claude-plugin/plugin.json", `{"name":"accounts","description":"Manage your SSH keys and, as an admin, your account's users."}`},
		{claudePluginDir + "/.mcp.json", `{"mcpServers":{"accounts":{"command":"` + MCPHelper + `"}}}`},
	} {
		if err := h.writeClaudeFile(ctx, file.path, file.text); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) writeClaudeFile(ctx context.Context, path, text string) error {
	if _, err := h.write(path, []byte(text), 0o644); err != nil {
		return err
	}
	return h.secure(ctx, path)
}

func (h *Host) ensureClaudeKey(ctx context.Context) (err error) {
	defer func() {
		if _, removeErr := h.remove(ClaudeKeyFile); err == nil {
			err = removeErr
		}
	}()
	keyID := strings.ToLower(ClaudeKeyFingerprint[len(ClaudeKeyFingerprint)-8:])
	result, err := h.query(ctx, Command{Name: "rpm", Args: []string{"-q", "gpg-pubkey-" + keyID}}, 0, 1)
	if err != nil || result.ExitCode == 0 {
		return err
	}
	if _, err := h.dir(GnupgHome, 0o700); err != nil {
		return err
	}
	if err := h.secure(ctx, GnupgHome); err != nil {
		return err
	}
	if err := h.downloadClaudeKey(ctx); err != nil {
		return err
	}
	keyPath := h.path(ClaudeKeyFile)
	listing, err := h.run(ctx, "gpg", "--homedir", h.path(GnupgHome), "--batch", "--show-keys", "--with-colons", keyPath)
	if err != nil {
		return err
	}
	if !pinnedClaudeKey(listing.Stdout) {
		return ErrClaudeKey
	}
	_, err = h.run(ctx, "rpmkeys", "--import", keyPath)
	return err
}

func pinnedClaudeKey(listing []byte) bool {
	// Exec caps stdout at MaxCapture. A full capture may omit a second key.
	if len(listing) >= MaxCapture {
		return false
	}
	seenPrimary, seenFingerprint, pinned := false, false, false
	for line := range bytes.SplitSeq(listing, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("pub:")) {
			if seenPrimary {
				return false
			}
			seenPrimary = true
		}
		if seenPrimary && !seenFingerprint && bytes.HasPrefix(line, []byte("fpr:")) {
			seenFingerprint = true
			fields := bytes.SplitN(line, []byte{':'}, 11)
			pinned = len(fields) >= 10 && string(fields[9]) == ClaudeKeyFingerprint
		}
	}
	return pinned
}

func (h *Host) downloadClaudeKey(ctx context.Context) (err error) {
	if h.env.Fetch == nil {
		return errors.New(claudeFetchUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	body, err := h.env.Fetch(ctx, ClaudeKeyURL)
	if err != nil {
		if body != nil {
			_ = body.Close()
		}
		return err
	}
	defer func() {
		if closeErr := body.Close(); err == nil {
			err = closeErr
		}
	}()
	return h.writeStream(ClaudeKeyFile, 0o600, func(dst *os.File) (bool, error) {
		var buffer [32 << 10]byte
		n, readErr := io.CopyBuffer(struct{ io.Writer }{dst}, io.LimitReader(body, MaxKeyBytes+1), buffer[:])
		switch {
		case n > MaxKeyBytes:
			return false, machine.ErrTooLarge
		case readErr != nil:
			return false, readErr
		}
		return true, nil
	})
}

func (h *Host) ensureClaudePackage(ctx context.Context) error {
	if err := h.writeClaudeFile(ctx, claudeRepo, claudeRepoBytes); err != nil {
		return err
	}
	result, err := h.query(ctx, Command{Name: "rpm", Args: []string{"-q", "claude-code"}}, 0, 1)
	if err != nil {
		return err
	}
	verb := "install"
	if result.ExitCode == 0 {
		if !h.claudeUpgradeDue() {
			return nil
		}
		verb = "upgrade"
	}
	if verb == "install" {
		if _, err := h.run(ctx, "dnf", verb, "-y", "--setopt=claude-code.gpgkey=", "claude-code"); err != nil {
			return err
		}
	}
	var upgradeErr error
	stampErr := h.writeStream(ClaudeStamp, 0o644, func(dst *os.File) (bool, error) {
		previous := h.claudeStampText()
		if verb == "upgrade" {
			// Prepare ownership before dnf, since the stamp must be finished even if it fails.
			// Keep the existing inode when the clock changes and the resulting bytes match.
			if previous != "" {
				if err := h.secure(ctx, ClaudeStamp); err != nil {
					return false, err
				}
			}
			staged := filepath.Join(filepath.Dir(ClaudeStamp), filepath.Base(dst.Name()))
			if err := h.secure(ctx, staged); err != nil {
				return false, err
			}
			_, upgradeErr = h.run(ctx, "dnf", verb, "-y", "--setopt=claude-code.gpgkey=", "claude-code")
		}
		stamp := h.env.Now().UTC().Truncate(time.Second).Format(time.RFC3339) + "\n"
		if previous == stamp {
			return false, nil
		}
		_, err := io.WriteString(dst, stamp)
		return true, err
	})
	if upgradeErr != nil {
		return upgradeErr
	}
	if stampErr != nil || verb == "upgrade" {
		return stampErr
	}
	return h.secure(ctx, ClaudeStamp)
}

func (h *Host) claudeStampText() string {
	info, err := os.Lstat(h.path(ClaudeStamp))
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxCapture {
		return ""
	}
	return h.boundedText(ClaudeStamp)
}

func (h *Host) claudeUpgradeDue() bool {
	stamp, ok := strings.CutSuffix(h.claudeStampText(), "\n")
	if !ok {
		return true
	}
	switch len(stamp) {
	case len("2006-01-02T15:04:05Z"):
		if stamp[19] != 'Z' {
			return true
		}
	case len("2006-01-02T15:04:05+00:00"):
		if stamp[19] != '+' && stamp[19] != '-' {
			return true
		}
		// Parse the offset as a clock to reject out-of-range hours and minutes.
		// time.Parse's RFC3339 parser also accepts offsets such as +24:00.
		if _, err := time.Parse("15:04", stamp[20:]); err != nil {
			return true
		}
	default:
		return true
	}
	previous, err := time.Parse(time.RFC3339, stamp)
	now := h.env.Now()
	return err != nil || now.Before(previous) || !now.Before(previous.Add(ClaudeUpgradeInterval))
}
