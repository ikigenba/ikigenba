package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func (cfg Config) list(ctx context.Context, caller identity.Caller, _ emptyArgs) (listOutput, error) {
	repos, err := cfg.Store.List(ctx, caller.UserID)
	if err != nil {
		return listOutput{}, errUnreachable
	}
	out := listOutput{Repos: make([]listingEntry, 0, len(repos))}
	for _, r := range repos {
		size, err := cfg.Store.Size(ctx, r.ID)
		if err != nil {
			return listOutput{}, errUnreachable
		}
		head, err := cfg.Store.Head(ctx, r.ID)
		if err != nil {
			return listOutput{}, errUnreachable
		}
		out.Repos = append(out.Repos, listingEntry{ID: r.ID, Name: r.Name, SizeBytes: size, Head: optionalHead(head), Available: r.Available})
	}
	return out, nil
}

func (cfg Config) show(ctx context.Context, caller identity.Caller, args repoArgs) (repositoryObject, error) {
	r, err := cfg.Store.Find(ctx, caller.UserID, args.Repo)
	if errors.Is(err, store.ErrNotFound) {
		return repositoryObject{}, ruleRefusal(missingLine(args.Repo))
	}
	if err != nil {
		return repositoryObject{}, errUnreachable
	}
	return cfg.repository(ctx, r)
}

func (cfg Config) repository(ctx context.Context, r store.Repo) (repositoryObject, error) {
	size, err := cfg.Store.Size(ctx, r.ID)
	if err != nil {
		return repositoryObject{}, errUnreachable
	}
	head, err := cfg.Store.Head(ctx, r.ID)
	if err != nil {
		return repositoryObject{}, errUnreachable
	}
	return objectFromValues(ctx, r, size, head), nil
}
