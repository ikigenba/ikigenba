package linux

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// Clone paths, identity, timeout and size limits.
const (
	TemplateDir        = "/var/lib/accountsd/template"
	CloneDir           = TemplateDir + "/clone"
	CloneUser          = "nobody"
	CloneTimeout       = 2 * time.Minute
	MaxTemplateBytes   = 16 << 20
	MaxTemplateEntries = 1000
)

// ErrTemplate indicates an invalid template setting or repository.
var ErrTemplate = errors.New("unusable team template")

// TemplateError identifies a failed template clone and its cause.
type TemplateError struct {
	URL, Ref, Line string
	Err            error
}

func (e *TemplateError) Error() string {
	detail := e.Line
	if detail == "" && e.Err != nil {
		detail = e.Err.Error()
	}
	return fmt.Sprintf("team template %s at %s: %s", e.URL, e.Ref, detail)
}

func (e *TemplateError) Unwrap() error { return e.Err }

type templateClone struct {
	url, ref, commit string
	failed           error
	skippedUsers     map[string]bool
}

func cloneFor(s protocol.DesiredState) templateClone {
	return templateClone{
		url: cmp.Or(s.TemplateURL, protocol.DefaultTemplateURL),
		ref: cmp.Or(s.TemplateRef, protocol.DefaultTemplateRef),
	}
}

func (c *templateClone) ensure(ctx context.Context, h *Host) (bool, error) {
	if c.commit != "" || c.failed != nil {
		return c.commit != "", nil
	}
	commit, line, err := h.cloneTemplate(ctx, c.url, c.ref)
	if err == nil {
		c.commit = commit
		return true, nil
	}
	failure := &TemplateError{URL: c.url, Ref: c.ref, Line: line, Err: err}
	var command *CommandError
	if errors.Is(err, ErrTemplate) ||
		(errors.As(err, &command) && command.Label == "runuser" &&
			(command.Err == nil || errors.Is(command.Err, context.DeadlineExceeded) && ctx.Err() == nil)) {
		c.failed = failure
		return false, nil
	}
	return false, failure
}

func (h *Host) templateParent() (bool, error) {
	prefix := "/"
	for component := range strings.SplitSeq(strings.TrimPrefix(TemplateDir, "/"), "/") {
		prefix = filepath.Join(prefix, component)
		info, err := os.Lstat(h.path(prefix))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false, nil
		case err != nil:
			return false, hostError(prefix, err)
		case !info.IsDir():
			return false, hostError(prefix, fs.ErrInvalid)
		}
	}
	return true, nil
}

func (h *Host) removeClone() error {
	exists, err := h.templateParent()
	if err != nil {
		return hostError(CloneDir, err)
	}
	if !exists {
		return nil
	}
	root, err := os.OpenRoot(h.env.Root)
	if err != nil {
		return hostError(CloneDir, err)
	}
	defer func() { _ = root.Close() }()
	if err := root.RemoveAll(strings.TrimPrefix(CloneDir, "/")); err != nil {
		return hostError(CloneDir, err)
	}
	return nil
}

func (h *Host) prepareClone(ctx context.Context) error {
	if _, err := h.templateParent(); err != nil {
		return err
	}
	if _, err := h.dir(TemplateDir, 0o711); err != nil {
		return err
	}
	directory := h.path(TemplateDir)
	info, err := os.Lstat(directory)
	if err != nil {
		return hostError(TemplateDir, err)
	}
	if !ownerIs(info, 0, 0) {
		if _, err := h.run(ctx, "chown", "--no-dereference", "root:root", directory); err != nil {
			return err
		}
	}
	if err := h.removeClone(); err != nil {
		return err
	}
	if err := os.Mkdir(h.path(CloneDir), 0o700); err != nil {
		return hostError(CloneDir, err)
	}
	if err := h.mode(CloneDir, 0o700); err != nil {
		return err
	}
	_, err = h.run(ctx, "chown", CloneUser+":"+CloneUser, h.path(CloneDir))
	return err
}

func (h *Host) cloneTemplate(ctx context.Context, url, ref string) (string, string, error) {
	if valid.TemplateURL(url) != nil || valid.TemplateRef(ref) != nil {
		return "", "", ErrTemplate
	}
	if err := h.prepareClone(ctx); err != nil {
		return "", "", err
	}
	deadline, cancel := context.WithTimeout(ctx, CloneTimeout)
	defer cancel()
	cloneContext := context.WithValue(deadline, cloneProcessGroup{}, true)

	dir := h.path(CloneDir)
	limitedGit := []string{"prlimit", "--fsize=" + strconv.Itoa(MaxTemplateBytes), "--", "git", "-c", "core.hooksPath=/dev/null"}
	clone := append(slices.Clone(limitedGit),
		"-c", "credential.helper=", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "advice.detachedHead=false", "clone", "--quiet", "--no-checkout", "--depth", "1",
		"--single-branch", "--no-tags", "--no-recurse-submodules", "--branch", ref, "--", url, dir)
	checkout := append(slices.Clone(limitedGit), "-C", dir, "checkout", "--quiet", "--force", "HEAD")
	var tree templateTree
	var commit string
	for step, args := range [][]string{
		clone, {"git", "-C", dir, "rev-parse", "HEAD"}, {"git", "-C", dir, "ls-tree", "-r", "-l", "-z", "HEAD"}, checkout,
	} {
		command := Command{
			Name: "runuser", Args: append([]string{"-u", CloneUser, "--"}, args...),
			Env: []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"},
		}
		if step == 2 {
			command.Stdout = &tree
		}
		result, err := h.query(cloneContext, command, 0)
		if err != nil {
			line := ""
			if result.ExitCode != 0 {
				line = lastTemplateDiagnostic(result.Stderr)
			}
			return "", line, err
		}
		if step == 1 {
			commit = templateCommit(result.Stdout)
			if commit == "" {
				return "", "", ErrTemplate
			}
		}
		if step == 2 && (tree.invalid || tree.used != 0) {
			return "", "", ErrTemplate
		}
	}
	if _, err := h.run(ctx, "chown", "-R", "--no-dereference", "root:root", dir); err != nil {
		return "", "", err
	}
	return commit, "", h.checkTemplateFiles()
}

func lastTemplateDiagnostic(data []byte) string {
	for {
		last := bytes.LastIndexByte(data, '\n')
		line := data[last+1:]
		if len(bytes.Trim(line, " \t\r")) != 0 {
			return string(bytes.TrimSuffix(line, []byte{'\r'}))
		}
		if last < 0 {
			return ""
		}
		data = data[:last]
	}
}

func templateCommit(out []byte) string {
	if (len(out) != 41 && len(out) != 65) || out[len(out)-1] != '\n' {
		return ""
	}
	commit := out[:len(out)-1]
	for _, b := range commit {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return ""
		}
	}
	return string(commit)
}

// templateTree keeps a single record as git emits its NUL-delimited listing.
type templateTree struct {
	record  [MaxCapture]byte
	used    int
	count   int
	size    int
	invalid bool
}

func (t *templateTree) Write(p []byte) (int, error) {
	length := len(p)
	for len(p) > 0 && !t.invalid {
		end := bytes.IndexByte(p, 0)
		if end < 0 {
			end = len(p)
		}
		if t.used+end > MaxCapture {
			t.invalid = true
			break
		}
		t.used += copy(t.record[t.used:], p[:end])
		if end == len(p) {
			break
		}
		t.acceptRecord()
		t.used = 0
		p = p[end+1:]
	}
	return length, nil
}

func (t *templateTree) acceptRecord() {
	t.count++
	size, ok := templateRecordSize(t.record[:t.used])
	if !ok || t.count > MaxTemplateEntries || size > MaxTemplateBytes-t.size {
		t.invalid = true
		return
	}
	t.size += size
}

func templateRecordSize(record []byte) (int, bool) {
	header, path, ok := bytes.Cut(record, []byte{'\t'})
	base := path[bytes.LastIndexByte(path, '/')+1:]
	if !ok || len(path) == 0 || bytes.Equal(base, []byte(".gitattributes")) {
		return 0, false
	}
	mode, header, _ := bytes.Cut(header, []byte{' '})
	kind, header, _ := bytes.Cut(header, []byte{' '})
	object, size, _ := bytes.Cut(header, []byte{' '})
	if len(mode) == 0 || len(kind) == 0 || len(object) == 0 {
		return 0, false
	}
	if (bytes.HasPrefix(path, []byte("seed/")) || bytes.HasPrefix(path, []byte("home/"))) &&
		!bytes.Equal(mode, []byte("100644")) && !bytes.Equal(mode, []byte("100755")) {
		return 0, false
	}
	size = bytes.TrimLeft(size, " ")
	if bytes.Equal(size, []byte{'-'}) {
		return 0, true
	}
	var value int
	for _, digit := range size {
		if digit < '0' || digit > '9' || value > MaxTemplateBytes {
			return 0, false
		}
		value = 10*value + int(digit-'0')
	}
	return value, len(size) != 0
}

// templateFiles opens the relative names returned by a walk of the checked clone.
type templateFiles string

func (source templateFiles) Open(name string) (fs.File, error) {
	// Git and Linux filenames need not be UTF-8, unlike paths into os.DirFS.
	return os.OpenInRoot(string(source), name)
}

func (h *Host) checkTemplateFiles() error {
	var hasSeed bool
	for _, name := range []string{"seed", "home"} {
		dir := filepath.Join(CloneDir, name)
		full := h.path(dir)
		err := filepath.WalkDir(full, func(path string, _ fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return ErrTemplate
			}
			hostPath := filepath.Join(dir, strings.TrimPrefix(path, full))
			if err != nil {
				return hostError(hostPath, err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				return hostError(hostPath, err)
			}
			if path == full && !info.IsDir() || !info.IsDir() && !info.Mode().IsRegular() ||
				name == "seed" && info.Name() == ".git" {
				return ErrTemplate
			}
			if name == "seed" && info.Mode().IsRegular() {
				hasSeed = true
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if !hasSeed {
		return ErrTemplate
	}
	return nil
}
