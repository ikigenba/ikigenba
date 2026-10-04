package cache

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
)

func writable(p string) error {
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0222 == 0 {
		return &os.PathError{Op: "write", Path: p, Err: os.ErrPermission}
	}
	return nil
}

// Unpack atomically installs a bounded archive, retaining an existing tree.
func (c *Cache) Unpack(ctx context.Context, site, repo, sha string) error {
	return c.unpack(ctx, site, repo, sha, nil)
}

func (c *Cache) unpack(ctx context.Context, site, repo, sha string, b *build) error {
	if err := validTree(site, sha); err != nil {
		return err
	}
	c.mu.Lock()
	if isDir(c.Dir(site, sha)) {
		c.mu.Unlock()
		return nil
	}
	if err := c.repository(repo); err != nil {
		c.mu.Unlock()
		return err
	}
	parent := filepath.Join(c.cfg.Root, site)
	parentExisted := isDir(parent)
	parentInfo, _ := os.Stat(parent)
	check := parent
	if !parentExisted {
		check = c.cfg.Root
	}
	if isDir(check) {
		if err := writable(check); err != nil {
			c.mu.Unlock()
			return err
		}
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		c.mu.Unlock()
		return err
	}
	stage, err := os.MkdirTemp(parent, ".unpack-")
	if err != nil {
		if !parentExisted {
			_ = os.Remove(parent)
		}
		c.mu.Unlock()
		return err
	}
	u := &unpack{site: site, sha: sha, stage: stage}
	if b != nil {
		u.discard = b.discard
	}
	c.active[u] = struct{}{}
	c.mu.Unlock()
	err = c.archive(ctx, u, repo)
	if err == nil && c.cfg.Unpacked != nil {
		c.cfg.Unpacked(site, sha)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.active, u)
	defer func() {
		_ = os.RemoveAll(stage)
		if !parentExisted || u.discard {
			_ = os.Remove(parent)
		} else if err != nil && parentInfo != nil {
			_ = os.Chtimes(parent, parentInfo.ModTime(), parentInfo.ModTime())
		}
	}()
	if isDir(c.Dir(site, sha)) {
		return nil
	}
	if err != nil {
		return err
	}
	if u.discard {
		return nil
	}
	err = os.Rename(stage, c.Dir(site, sha))
	return err
}
func (c *Cache) archive(ctx context.Context, u *unpack, repo string) error {
	op, cancel := c.cfg.Limits.Operation(ctx)
	defer cancel()
	cmd := c.cfg.Git.Command(op, "", "--git-dir="+c.RepoDir(repo), "archive", "--format=tar", u.sha)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		if cause := context.Cause(op); cause != nil {
			return cause
		}
		return git.ErrNotFound
	}
	extractErr := c.extract(out, u.stage)
	if extractErr != nil && !errors.Is(extractErr, io.ErrUnexpectedEOF) && !errors.Is(extractErr, io.EOF) {
		cancel()
	}
	waitErr := cmd.Wait()
	if extractErr != nil && !errors.Is(extractErr, io.ErrUnexpectedEOF) && !errors.Is(extractErr, io.EOF) {
		return extractErr
	}
	if cause := context.Cause(op); cause != nil {
		return cause
	}
	if waitErr != nil {
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			return &git.Error{Status: exit.ExitCode(), Stderr: stderr.String()}
		}
		return waitErr
	}
	return extractErr
}
func (c *Cache) extract(reader io.Reader, stage string) error {
	root, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	tr := tar.NewReader(reader)
	seen := make(map[string]byte)
	var size int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return errors.New("unsafe archive path")
		}
		if _, ok := seen[name]; ok {
			return errors.New("duplicate archive path")
		}
		for p := path.Dir(name); p != "."; p = path.Dir(p) {
			if kind, ok := seen[p]; ok && kind != tar.TypeDir {
				return errors.New("archive entry below a non-directory")
			}
		}
		// Implicit parents also prevent a later link or file replacing a directory.
		for p := range seen {
			if strings.HasPrefix(p, name+"/") && h.Typeflag != tar.TypeDir {
				return errors.New("archive non-directory replaces parent")
			}
		}
		seen[name] = h.Typeflag
		dest := filepath.FromSlash(name)
		if h.Typeflag == tar.TypeReg {
			if h.Size < 0 || h.Size > c.cfg.Limits.Settings().SiteMaxBytes-size {
				return limits.ErrTooLarge
			}
			size += h.Size
		}
		if err = root.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = root.MkdirAll(dest, 0700)
		case tar.TypeSymlink:
			err = root.Symlink(h.Linkname, dest)
		case tar.TypeReg:
			var f *os.File
			f, err = root.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(h.Mode&0777))
			if err == nil {
				_, err = io.CopyN(f, tr, h.Size)
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
			}
		default:
			return errors.New("unsupported archive entry")
		}
		if err != nil {
			return err
		}
	}
}
