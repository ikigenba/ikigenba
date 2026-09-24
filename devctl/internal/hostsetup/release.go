package hostsetup

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	// ReleasesURL is the published GitHub releases endpoint.
	ReleasesURL = "https://api.github.com/repos/ikigenba/ikigenba/releases"
	// DownloadURL is the published GitHub release asset base.
	DownloadURL = "https://github.com/ikigenba/ikigenba/releases/download"
	// InstallerPath is the temporary installer location on a host.
	InstallerPath = "/tmp/opsctl-install"
	// DNSProvider is the opsctl provider name for Route 53.
	DNSProvider = "route53"
)

// Release identifies an opsctl release and its published installer.
type Release struct {
	Version      string
	InstallerURL string
}

// ProcessError reports a release-discovery command that exited unsuccessfully.
type ProcessError struct {
	Label  string
	Status int
	Stderr string
}

// Error returns the command label and exit status.
func (e *ProcessError) Error() string {
	return fmt.Sprintf("%s: exit status %d", e.Label, e.Status)
}

// Detail returns the command's standard error as quoted diagnostic detail.
func (e *ProcessError) Detail() string { return seam.QuoteOutput(e.Stderr) }

// ExitCode returns the ordinary command failure status.
func (e *ProcessError) ExitCode() int { return 1 }

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt string        `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Latest discovers the newest published opsctl release.
func Latest(ctx context.Context, deps seam.Deps) (Release, error) {
	var selected *githubRelease
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s?per_page=100&page=%d", ReleasesURL, page)
		result, err := deps.Exec(ctx, seam.Cmd{
			Path: "curl",
			Args: []string{"-fsSL", url},
			Dir:  deps.Dir,
		})
		if err != nil {
			return Release{}, fmt.Errorf("list opsctl releases: %w", err)
		}
		if result.ExitCode != 0 {
			return Release{}, &ProcessError{
				Label:  "curl",
				Status: result.ExitCode,
				Stderr: string(result.Stderr),
			}
		}

		var releases []githubRelease
		if err := json.Unmarshal(result.Stdout, &releases); err != nil {
			return Release{}, fmt.Errorf("decode GitHub releases: %w", err)
		}
		for i := range releases {
			candidate := &releases[i]
			_, ok := opsctlVersion(candidate.TagName)
			if candidate.Draft || candidate.Prerelease || !ok {
				continue
			}
			if selected == nil || candidate.PublishedAt > selected.PublishedAt ||
				candidate.PublishedAt == selected.PublishedAt && candidate.TagName < selected.TagName {
				candidateCopy := *candidate
				selected = &candidateCopy
			}
		}
		if len(releases) < 100 {
			break
		}
	}

	if selected == nil {
		return Release{}, fmt.Errorf("no published opsctl release found")
	}
	version, _ := opsctlVersion(selected.TagName)
	for _, asset := range selected.Assets {
		if asset.Name == "install.sh" {
			return Release{Version: version, InstallerURL: asset.BrowserDownloadURL}, nil
		}
	}
	return Release{}, fmt.Errorf("opsctl release %s has no install.sh asset", selected.TagName)
}

func opsctlVersion(tag string) (string, bool) {
	const prefix = "opsctl/"
	if len(tag) <= len(prefix) || tag[:len(prefix)] != prefix {
		return "", false
	}
	version := tag[len(prefix):]
	return version, appref.ValidVersion(version)
}
