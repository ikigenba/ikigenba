// Package release reads the suite's release layout without depending on domain packages.
package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Host layout paths and package-relative release filenames.
const (
	Dir           = "/opt/ikigenba"
	ReleasesDir   = Dir + "/releases"
	CurrentLink   = Dir + "/current"
	PreviousLink  = Dir + "/previous"
	OpsctlLink    = "/usr/local/bin/opsctl"
	CurrentOpsctl = CurrentLink + "/opsctl/bin/opsctl"
	OpsctlPath    = "opsctl/bin/opsctl"
	MetadataName  = "release.json"
	LabelName     = "label"
)

// ErrNoMetadata identifies an absent or invalid release record.
var ErrNoMetadata = errors.New("release metadata is unavailable")

// Release identifies a suite commit and its optional display label.
type Release struct {
	SHA   string
	Label string
}

// Short returns up to seven SHA characters.
func (r Release) Short() string {
	if len(r.SHA) > 7 {
		return r.SHA[:7]
	}
	return r.SHA
}

// Display combines the label with the short SHA.
func (r Release) Display() string {
	if r.Label == "" {
		return r.Short()
	}
	return r.Label + " (" + r.Short() + ")"
}

// ValidSHA recognizes a full lowercase hexadecimal commit SHA.
func ValidSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := range len(s) {
		valid := s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f'
		if !valid {
			return false
		}
	}
	return true
}

// ValidLabel recognizes a nonempty ASCII release label.
func ValidLabel(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		valid := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.ContainsRune("._/-", rune(c))
		if !valid {
			return false
		}
	}
	return true
}

// Read decodes the release metadata and optional label.
func Read(root, sha string) (Release, error) {
	if !ValidSHA(sha) {
		return Release{}, ErrNoMetadata
	}
	data, err := readFile(root, ReleasesDir+"/"+sha+"/"+MetadataName)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %w", ErrNoMetadata, err)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return Release{}, ErrNoMetadata
	}
	var value string
	member, ok := object["sha"]
	if !ok || len(member) == 0 || member[0] != '"' || json.Unmarshal(member, &value) != nil {
		return Release{}, ErrNoMetadata
	}
	return Release{SHA: value, Label: readLabel(root, sha)}, nil
}
func readFile(root, name string) ([]byte, error) {
	resolved, err := resolve(root, name, ReleasesDir)
	if err != nil {
		return nil, err
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	base, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(base, resolved)
	if err != nil {
		return nil, err
	}
	return filesystem.ReadFile(relative)
}
func readLabel(root, sha string) string {
	data, err := readFile(root, ReleasesDir+"/"+sha+"/"+LabelName)
	if err != nil {
		return ""
	}
	label := strings.TrimSuffix(string(data), "\n")
	if !ValidLabel(label) {
		return ""
	}
	return label
}

// Current reads the running release link.
func Current(root string) (Release, bool, error) { return readLink(root, CurrentLink) }

// Previous reads the preceding release link.
func Previous(root string) (Release, bool, error) { return readLink(root, PreviousLink) }
func readLink(root, name string) (Release, bool, error) {
	directory, err := resolve(root, filepath.Dir(name), Dir)
	if errors.Is(err, os.ErrNotExist) {
		return Release{}, false, nil
	}
	if err != nil {
		return Release{}, false, err
	}
	filename := filepath.Join(directory, filepath.Base(name))
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return Release{}, false, nil
	}
	if err != nil {
		return Release{}, false, err
	}
	target, err := os.Readlink(filename)
	if info.Mode()&os.ModeSymlink == 0 || err != nil || !strings.HasPrefix(target, "releases/") || !ValidSHA(strings.TrimPrefix(target, "releases/")) {
		return Release{}, false, fmt.Errorf("%s does not name a release", name)
	}
	sha := strings.TrimPrefix(target, "releases/")
	return Release{SHA: sha, Label: readLabel(root, sha)}, true, nil
}

// Of identifies the release containing the resolved executable.
func Of(root, executable string) (Release, bool) {
	base, err := filepath.Abs(root)
	if err != nil {
		return Release{}, false
	}
	machine, err := filepath.Abs(executable)
	if err != nil {
		return Release{}, false
	}
	relative, err := filepath.Rel(base, machine)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Release{}, false
	}
	resolved, err := Resolve(base, relative)
	if err != nil {
		return Release{}, false
	}
	below, err := filepath.Rel(filepath.Join(base, ReleasesDir), resolved)
	if err != nil {
		return Release{}, false
	}
	parts := strings.Split(filepath.ToSlash(below), "/")
	if len(parts) != 4 || !ValidSHA(parts[0]) || strings.Join(parts[1:], "/") != OpsctlPath {
		return Release{}, false
	}
	r, err := Read(base, parts[0])
	if err != nil || r.SHA != parts[0] {
		return Release{}, false
	}
	return r, true
}

// Resolve follows host absolute and relative symlinks within root. A machine path
// already below root is also accepted. Missing entries and link loops fail.
func Resolve(root, name string) (string, error) {
	return resolve(root, name, "")
}

func resolve(root, name, scope string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(name) {
		switch {
		case name == base:
			name = "."
		case strings.HasPrefix(name, base+string(filepath.Separator)):
			name = strings.TrimPrefix(name, base+string(filepath.Separator))
		default:
			name = strings.TrimPrefix(name, string(filepath.Separator))
		}
	}
	pending := strings.Split(filepath.ToSlash(name), "/")
	var done []string
	links := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(done) == 0 {
				return "", fmt.Errorf("path escapes root")
			}
			done = done[:len(done)-1]
			continue
		}
		candidate := filepath.Join(append([]string{base}, append(done, part)...)...)
		info, statErr := os.Lstat(candidate)
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 255 {
				return "", fmt.Errorf("too many symbolic links")
			}
			target, readErr := os.Readlink(candidate)
			if readErr != nil {
				return "", readErr
			}
			if scope != "" {
				hostTarget := filepath.Join("/", filepath.Join(done...), target)
				if filepath.IsAbs(target) {
					hostTarget = filepath.Clean(target)
				}
				within := hostTarget == scope || strings.HasPrefix(hostTarget, scope+"/")
				finalTarget := filepath.Join(append([]string{hostTarget}, pending...)...)
				finalWithin := finalTarget == scope || strings.HasPrefix(finalTarget, scope+"/")
				ancestor := strings.HasPrefix(scope, strings.TrimSuffix(hostTarget, "/")+"/")
				if (!within && !ancestor) || !finalWithin {
					return "", fmt.Errorf("release path leaves %s", scope)
				}
			}
			if filepath.IsAbs(target) {
				done = nil
			}
			pending = append(strings.Split(filepath.ToSlash(target), "/"), pending...)
			continue
		}
		done = append(done, part)
	}
	resolved := filepath.Join(append([]string{base}, done...)...)
	if scope != "" {
		allowed := filepath.Join(base, scope)
		if resolved != allowed && !strings.HasPrefix(resolved, allowed+string(filepath.Separator)) {
			return "", fmt.Errorf("release path leaves %s", scope)
		}
	}
	return resolved, nil
}

// LinkOpsctl atomically replaces the host opsctl link.
func LinkOpsctl(root, target string) error {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	if err := filesystem.MkdirAll("usr/local/bin", 0o755); err != nil {
		return err
	}
	directory, err := Resolve(root, "/usr/local/bin")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".opsctl-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	if err = file.Close(); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err = os.Remove(temporary); err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary) }()
	if err = os.Symlink(target, temporary); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(directory, "opsctl"))
}
