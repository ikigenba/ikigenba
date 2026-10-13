package tools

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

func (cfg Config) status(ctx context.Context, caller identity.Caller, args pageArgs) (statusOutput, error) {
	repos, next, err := cfg.page(ctx, caller.UserID, "status", args)
	if err != nil {
		return statusOutput{}, err
	}
	pressure := cfg.Limits.Pressure()
	out := statusOutput{
		Read:  usageOutput{Slots: pressure.Read.Slots, Active: pressure.Read.Active, Queued: pressure.Read.Queued},
		Write: usageOutput{Slots: pressure.Write.Slots, Active: pressure.Write.Active, Queued: pressure.Write.Queued},
		Repos: make([]statusEntry, 0, len(repos)),
		Next:  next,
	}
	limit := cfg.Limits.Settings().RepoMaxBytes
	for _, r := range repos {
		size, err := cfg.Store.Size(ctx, r.ID)
		if err != nil {
			return statusOutput{}, errUnreachable
		}
		out.Repos = append(out.Repos, statusEntry{ID: r.ID, Name: r.Name, SizeBytes: size, LimitBytes: limit, Available: r.Available, Busy: cfg.Limits.Busy(r.ID)})
	}
	return out, nil
}
