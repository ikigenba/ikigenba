package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
)

func (cfg Config) publish(ctx context.Context, caller identity.Caller, a PublishArgs) (Site, error) {
	s, err := cfg.find(ctx, caller.UserID, a.Name)
	if err != nil {
		return Site{}, err
	}
	info, err := os.Stat(cfg.Cache.RepoDir(s.Repo))
	if err != nil || !info.IsDir() {
		return Site{}, fmt.Errorf("repository '%s' is unavailable", s.Repo)
	}
	ref := s.Ref
	if a.Ref != nil {
		ref = *a.Ref
	}
	sha, err := cfg.Cache.Resolve(ctx, s.Repo, ref)
	if err != nil {
		if errors.Is(err, cache.ErrNoCommit) {
			return Site{}, fmt.Errorf("no commit for '%s'", ref)
		}
		return Site{}, cfg.gitError(ctx, err)
	}
	dir := cfg.Cache.Dir(s.ID, sha)
	_, treeErr := os.Lstat(dir)
	_, siteErr := os.Lstat(filepath.Dir(dir))
	if err = cfg.Cache.Unpack(ctx, s.ID, s.Repo, sha); err != nil {
		return Site{}, cfg.gitError(ctx, err)
	}
	x, err := cfg.Store.Publish(ctx, s.ID, sha)
	if err != nil {
		if os.IsNotExist(treeErr) {
			_ = os.RemoveAll(dir)
		}
		if os.IsNotExist(siteErr) {
			_ = os.Remove(filepath.Dir(dir))
		}
		return Site{}, catalogError(err)
	}
	_ = cfg.Cache.Prune(s.ID, sha)
	cfg.emit(ctx, "site.published", telemetry.Attrs{"site": x.ID, "commit": x.Commit, "ref": ref})
	return siteObject(ctx, x), nil
}
