package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func (cfg Config) create(ctx context.Context, caller identity.Caller, args createArgs) (repositoryObject, error) {
	if !store.ValidName(args.Name) {
		return repositoryObject{}, ruleRefusal("name: " + InvalidName)
	}
	var r store.Repo
	var out repositoryObject
	err := cfg.Store.Coordinate(ctx, func(ctx context.Context) error {
		var err error
		r, err = cfg.Store.Create(ctx, caller.UserID, args.Name)
		if err != nil {
			return err
		}
		size, err := cfg.Store.Size(ctx, r.ID)
		if err != nil {
			return err
		}
		out = objectFromValues(ctx, r, size, "")
		return nil
	})
	if errors.Is(err, store.ErrNameTaken) {
		return repositoryObject{}, ruleRefusal(takenLine(args.Name))
	}
	if err != nil {
		return repositoryObject{}, errUnreachable
	}
	cfg.Telemetry.Emit(ctx, "repo.created", telemetry.Attrs{"repo": r.ID, "owner": caller.UserID})
	return out, nil
}

func (cfg Config) rename(ctx context.Context, caller identity.Caller, args renameArgs) (repositoryObject, error) {
	validName := store.ValidName(args.Name)
	if !validName {
		_, err := cfg.Store.Find(ctx, caller.UserID, args.Repo)
		if errors.Is(err, store.ErrNotFound) {
			return repositoryObject{}, cfg.renameMissing(ctx, caller, args, false)
		}
		return repositoryObject{}, ruleRefusal("name: " + InvalidName)
	}
	var out repositoryObject
	var r, renamed store.Repo
	var refusal error
	err := cfg.Store.Coordinate(ctx, func(ctx context.Context) error {
		var err error
		r, err = cfg.Store.Find(ctx, caller.UserID, args.Repo)
		if errors.Is(err, store.ErrNotFound) {
			refusal = cfg.renameMissing(ctx, caller, args, true)
			return refusal
		}
		if err != nil {
			return err
		}
		renamed, err = cfg.Store.Rename(ctx, r.ID, args.Name)
		if errors.Is(err, store.ErrNotFound) {
			refusal = cfg.renameMissing(ctx, caller, args, true)
			return refusal
		}
		if errors.Is(err, store.ErrNameTaken) {
			refusal = ruleRefusal(takenLine(args.Name))
			return refusal
		}
		if err != nil {
			return err
		}
		out, err = cfg.repository(ctx, renamed)
		return err
	})
	if err != nil {
		if refusal != nil {
			return repositoryObject{}, refusal
		}
		return repositoryObject{}, errUnreachable
	}
	if renamed.Name != r.Name {
		cfg.Telemetry.Emit(ctx, "repo.renamed", telemetry.Attrs{"repo": renamed.ID, "owner": caller.UserID})
	}
	return out, nil
}

func (cfg Config) renameMissing(ctx context.Context, caller identity.Caller, args renameArgs, validName bool) error {
	repos, err := cfg.Store.List(ctx, caller.UserID)
	if err != nil {
		if !validName {
			return ruleRefusal("name: " + InvalidName)
		}
		return errUnreachable
	}
	lines := []string{missingLine(args.Repo)}
	if !validName {
		lines = append(lines, "name: "+InvalidName)
	} else {
		for _, r := range repos {
			if r.Name == args.Name {
				lines = append(lines, takenLine(args.Name))
				break
			}
		}
	}
	return ruleRefusal(lines...)
}

func (cfg Config) delete(ctx context.Context, caller identity.Caller, args repoArgs) (deleteOutput, error) {
	r, err := cfg.Store.Find(ctx, caller.UserID, args.Repo)
	if errors.Is(err, store.ErrNotFound) {
		return deleteOutput{}, ruleRefusal(missingLine(args.Repo))
	}
	if err != nil {
		return deleteOutput{}, errUnreachable
	}
	release, ok := cfg.Limits.TryHold(r.ID)
	if !ok {
		return deleteOutput{}, fmt.Errorf(Busy, r.Name)
	}
	defer release()
	if err := cfg.Store.Delete(ctx, r.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return deleteOutput{}, ruleRefusal(missingLine(args.Repo))
		}
		return deleteOutput{}, errUnreachable
	}
	cfg.Telemetry.Emit(ctx, "repo.deleted", telemetry.Attrs{"repo": r.ID, "owner": caller.UserID})
	return deleteOutput{ID: r.ID, Name: r.Name}, nil
}
