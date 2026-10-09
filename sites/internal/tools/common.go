package tools

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

func catalogError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(store.Unreachable)
}
func (cfg Config) find(ctx context.Context, user, name string) (store.Site, error) {
	if !store.ValidName(name) {
		return store.Site{}, fmt.Errorf(MissingSite, name)
	}
	s, err := cfg.Store.Find(ctx, user, name)
	if errors.Is(err, store.ErrNotFound) {
		return store.Site{}, fmt.Errorf(MissingSite, name)
	}
	return s, catalogError(err)
}
func siteObject(ctx context.Context, s store.Site) Site {
	base, _ := urls.FromContext(ctx)
	o := Site{ID: s.ID, Name: s.Name, Slug: s.Slug, URL: urls.SiteURL(base, s.Slug), Repo: s.Repo, Ref: s.Ref, Visibility: s.Visibility, Listed: s.Listed, Created: s.Created.UTC().Format("2006-01-02T15:04:05Z")}
	if s.Commit != "" {
		commit := s.Commit
		published := s.Published.UTC().Format("2006-01-02T15:04:05Z")
		o.Commit = &commit
		o.Published = &published
	}
	return o
}
func gitFailure(err error) string {
	var ge *git.Error
	if !errors.As(err, &ge) {
		return GitFailed
	}
	stderr := strings.TrimSuffix(ge.Stderr, "\n")
	if stderr == "" {
		return GitFailed
	}
	return GitFailed + "\n\n> " + strings.ReplaceAll(stderr, "\n", "\n> ")
}
func (cfg Config) gitError(ctx context.Context, err error) error {
	if errors.Is(err, limits.ErrHalted) || context.Cause(ctx) != nil && errors.Is(err, context.Cause(ctx)) {
		runtime.Goexit()
	}
	if errors.Is(err, limits.ErrTimedOut) {
		return fmt.Errorf(TimedOut, cfg.Limits.Settings().OperationSeconds)
	}
	if errors.Is(err, limits.ErrTooLarge) {
		return fmt.Errorf(TooLarge, cfg.Limits.Settings().SiteMaxBytes)
	}
	return errors.New(gitFailure(err))
}
func (cfg Config) emit(ctx context.Context, name string, attrs telemetry.Attrs) {
	cfg.Telemetry.Emit(ctx, name, attrs)
}
func (cfg Config) list(ctx context.Context, c identity.Caller, _ ListArgs) (SiteList, error) {
	ss, err := cfg.Store.List(ctx, c.UserID)
	if err != nil {
		return SiteList{}, catalogError(err)
	}
	out := SiteList{Sites: make([]ListedSite, 0, len(ss))}
	for _, s := range ss {
		o := siteObject(ctx, s)
		out.Sites = append(out.Sites, ListedSite{ID: o.ID, Name: o.Name, Slug: o.Slug, URL: o.URL, Visibility: o.Visibility, Listed: o.Listed, Commit: o.Commit})
	}
	return out, nil
}
func (cfg Config) show(ctx context.Context, c identity.Caller, a ShowArgs) (Site, error) {
	s, err := cfg.find(ctx, c.UserID, a.Name)
	if err != nil {
		return Site{}, err
	}
	return siteObject(ctx, s), nil
}
