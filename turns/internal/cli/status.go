package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/config"
	"github.com/ikigenba/ikigenba/turns/internal/local"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

func runStatus(ctx context.Context, _ []string, _ io.Reader, stdout, stderr io.Writer, deps seam.Deps) int {
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		return diagnostic(stderr, err.Error(), "")
	}
	cfg, err := config.Load(p)
	if err != nil {
		return diagnostic(stderr, "could not read the configuration", err.Error())
	}
	found := local.Read(p)
	localCounts := make(map[session.Tool]statusTally)
	remoteCounts := make(map[session.Tool]statusTally)
	for _, s := range found.Sessions {
		tally := localCounts[s.Ref.Tool]
		tally.sessions++
		tally.turns += len(s.Turns)
		localCounts[s.Ref.Tool] = tally
	}
	var remoteErr error
	if cfg.Configured() {
		client := space.New(cfg.Space, cfg.Token, deps.HTTP)
		for start := 0; start < len(found.Sessions); start += space.MaxLatest {
			end := min(start+space.MaxLatest, len(found.Sessions))
			refs := make([]session.Ref, end-start)
			for i, s := range found.Sessions[start:end] {
				refs[i] = s.Ref
			}
			held, latestErr := client.Latest(ctx, refs)
			if latestErr != nil {
				remoteErr = latestErr
				break
			}
			for i, h := range held {
				if h.Latest.IsZero() {
					continue
				}
				tally := remoteCounts[h.Ref.Tool]
				tally.sessions++
				for _, turn := range found.Sessions[start+i].Turns {
					if !turn.Moment.After(h.Latest) {
						tally.turns++
					}
				}
				remoteCounts[h.Ref.Tool] = tally
			}
		}
	}
	spaceName := cfg.Space
	if spaceName == "" {
		spaceName = "none"
	}
	tokenState := "none"
	if cfg.Token != "" {
		tokenState = "set"
	}
	_, _ = fmt.Fprintf(stdout, "space: %s\ntoken: %s\nlast push: %s\n", spaceName, tokenState, statusLastPush(p.State))
	for _, tool := range session.Tools() {
		tally := localCounts[tool]
		_, _ = fmt.Fprintf(stdout, "%s: %d %s, %d %s here", tool, tally.sessions, statusPlural(tally.sessions, "session"), tally.turns, statusPlural(tally.turns, "turn"))
		if remoteErr != nil {
			_, _ = io.WriteString(stdout, "; unknown on the space")
		} else if cfg.Configured() {
			remote := remoteCounts[tool]
			_, _ = fmt.Fprintf(stdout, "; %d %s, %d %s on the space", remote.sessions, statusPlural(remote.sessions, "session"), remote.turns, statusPlural(remote.turns, "turn"))
		}
		_, _ = io.WriteString(stdout, "\n")
	}
	if remoteErr != nil {
		var failure *space.Error
		if errors.As(remoteErr, &failure) {
			if failure.Quoted {
				return diagnosticOutput(stderr, "could not read the space: "+failure.Reason, seam.QuoteOutput(failure.Detail))
			}
			return diagnostic(stderr, "could not read the space: "+failure.Reason, failure.Detail)
		}
		return diagnostic(stderr, "could not read the space: "+remoteErr.Error(), "")
	}
	return ExitOK
}

type statusTally struct{ sessions, turns int }

func statusPlural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
func statusLastPush(state string) string {
	root, err := os.OpenRoot(state)
	if err != nil {
		return "never"
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(paths.LastPushFile)
	if err != nil {
		return "never"
	}
	text := strings.TrimRight(string(data), " \t\r\n")
	const layout = "2006-01-02T15:04:05Z"
	if len(text) != len(layout) {
		return "never"
	}
	moment, err := time.Parse(layout, text)
	if err != nil || moment.Format(layout) != text {
		return "never"
	}
	return text
}
