package cache

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

// Tree returns an existing tree or shares a request-independent rebuild.
func (c *Cache) Tree(ctx context.Context, site, repo, sha string) (string, error) {
	if err := validTree(site, sha); err != nil {
		return "", err
	}
	dir := c.Dir(site, sha)
	key := site + "/" + sha
	c.mu.Lock()
	if isDir(dir) {
		c.mu.Unlock()
		return dir, nil
	}
	b, joined := c.builds[key]
	if !joined {
		if c.cfg.Limits.Draining() {
			c.mu.Unlock()
			return "", limits.ErrDraining
		}
		b = &build{done: make(chan struct{}), site: site, sha: sha}
		c.builds[key] = b
		go c.rebuild(context.WithoutCancel(ctx), b, site, repo, sha, key, dir)
	}
	c.mu.Unlock()
	if joined && c.cfg.Joined != nil {
		c.cfg.Joined(site, sha)
	}
	select {
	case <-ctx.Done():
		return "", context.Cause(ctx)
	case <-b.done:
		if b.err != nil {
			return "", b.err
		}
		return dir, nil
	}
}

func (c *Cache) rebuild(ctx context.Context, b *build, site, repo, sha, key, dir string) {
	_, err := c.Resolve(ctx, repo, sha)
	if err == nil {
		err = c.unpack(ctx, site, repo, sha, b)
	}
	c.mu.Lock()
	if isDir(dir) {
		err = nil
	}
	b.err = err
	delete(c.builds, key)
	close(b.done)
	c.mu.Unlock()
}

// Prune removes obsolete trees while retaining active staging.
func (c *Cache) Prune(site, keep string) error { return c.remove(site, keep, false) }

// Remove removes a site's trees while allowing active unpacks to finish.
func (c *Cache) Remove(site string) error { return c.remove(site, "", true) }
func (c *Cache) remove(site, keep string, all bool) error {
	if !store.ValidID(site) {
		return os.ErrInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	parent := filepath.Join(c.cfg.Root, site)
	for _, b := range c.builds {
		if b.site == site && (all || b.sha != keep) {
			b.discard = true
		}
	}
	entries, err := os.ReadDir(parent)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	live := make(map[string]bool)
	for u := range c.active {
		if u.site == site {
			live[filepath.Base(u.stage)] = true
			if all || u.sha != keep {
				u.discard = true
			}
		}
	}
	for _, e := range entries {
		if live[e.Name()] || (!all && e.Name() == keep) {
			continue
		}
		if err = writable(parent); err != nil {
			return err
		}
		if err = os.RemoveAll(filepath.Join(parent, e.Name())); err != nil {
			return err
		}
	}
	if all && len(live) == 0 {
		if err = writable(filepath.Dir(parent)); err != nil {
			return err
		}
		return os.Remove(parent)
	}
	return nil
}
