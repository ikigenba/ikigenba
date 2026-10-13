package tools

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func (cfg Config) page(ctx context.Context, owner, tool string, args pageArgs) ([]store.Repo, *string, error) {
	n, err := paging.Window(args.Limit)
	if err != nil {
		return nil, nil, err
	}
	token := paging.Token{Tool: tool}
	var after *string
	if args.Page != nil && *args.Page != "" {
		var key string
		if err := token.Decode(*args.Page, &key); err != nil {
			return nil, nil, paging.ErrToken
		}
		after = &key
	}
	repos, err := cfg.Store.List(ctx, owner, after, n+1)
	if err != nil {
		return nil, nil, errUnreachable
	}
	var next *string
	if len(repos) > n {
		repos = repos[:n]
		value := token.Encode(repos[n-1].Name)
		next = &value
	}
	return repos, next, nil
}
