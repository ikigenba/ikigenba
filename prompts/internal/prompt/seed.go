package prompt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"prompts/internal/version"
)

const (
	defaultSeedRetryWindow = 30 * time.Second
	seedRetryDelay         = 100 * time.Millisecond
)

// SeedDefinitions copies legacy definition columns into their repositories.
// A repository with a head already exists at a higher source of truth and is
// therefore never overwritten.
func (s *Service) SeedDefinitions(ctx context.Context) (seeded int, err error) {
	if s == nil || s.store == nil {
		return 0, fmt.Errorf("prompt: seed definitions: no store")
	}
	if s.Version == nil {
		return 0, fmt.Errorf("prompt: seed definitions: version plane is not configured")
	}

	definitions, err := s.store.listSeedDefinitions(ctx)
	if err != nil {
		return 0, err
	}
	taken := make(map[string]bool, len(definitions))
	for _, d := range definitions {
		if d.NameKey != "" {
			taken[d.NameKey] = true
		}
	}

	for i := range definitions {
		d := &definitions[i]
		if d.NameKey == "" {
			base := NameKey(d.Name, d.ID)
			d.NameKey = availableSeedNameKey(base, taken)
			if err := s.store.setPromptNameKey(ctx, d.ID, d.NameKey); err != nil {
				return seeded, err
			}
			taken[d.NameKey] = true
		}

		actor := "prompts:" + d.ID
		if err := s.retryVersionUnavailable(ctx, func() error {
			return s.Version.Create(ctx, d.NameKey, actor)
		}); err != nil {
			return seeded, versionPlaneError("seed create repository", err)
		}
		var head string
		if err := s.retryVersionUnavailable(ctx, func() error {
			var headErr error
			head, headErr = s.Version.Head(ctx, d.NameKey)
			return headErr
		}); err != nil {
			return seeded, versionPlaneError("seed read head", err)
		}
		if head != "" {
			continue
		}

		files := []version.File{
			{Path: "prompt.md", Data: []byte(d.UserPrompt)},
			{Path: "config.json", Data: []byte(d.ConfigJSON)},
		}
		if d.SystemPrompt != "" {
			files = append(files, version.File{Path: "system.md", Data: []byte(d.SystemPrompt)})
		}
		if err := s.retryVersionUnavailable(ctx, func() error {
			_, commitErr := s.Version.Commit(ctx, d.NameKey, files, "seed "+d.Name, actor)
			return commitErr
		}); err != nil {
			return seeded, versionPlaneError("seed commit", err)
		}
		seeded++
	}
	return seeded, nil
}

func availableSeedNameKey(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if !taken[candidate] {
			return candidate
		}
	}
}

func (s *Service) retryVersionUnavailable(ctx context.Context, operation func() error) error {
	window := s.seedRetryWindow
	if window <= 0 {
		window = defaultSeedRetryWindow
	}
	sleep := s.seedSleep
	if sleep == nil {
		sleep = sleepSeedRetry
	}
	delay := seedRetryDelay
	remaining := window
	for {
		err := operation()
		if !errors.Is(err, version.ErrUnavailable) {
			return err
		}
		if remaining <= 0 {
			return err
		}
		if delay > remaining {
			delay = remaining
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		remaining -= delay
		if delay < window/2 {
			delay *= 2
		} else {
			delay = window
		}
	}
}

func sleepSeedRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
