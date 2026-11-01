// Package local gathers and links the sessions recorded by every supported tool.
package local

import (
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/claude"
	"github.com/ikigenba/ikigenba/turns/internal/codex"
	"github.com/ikigenba/ikigenba/turns/internal/dispatch"
	"github.com/ikigenba/ikigenba/turns/internal/grok"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

// Found holds linked sessions and a skip count for every supported tool.
type Found struct {
	Sessions []session.Session
	Skipped  map[session.Tool]int
}

// Read gathers all three tools' sessions without changing their files.
func Read(p paths.Paths, since time.Time) Found {
	jobs := dispatch.Jobs(p.Jobs)
	dirs := make([]string, len(jobs))
	for i, job := range jobs {
		dirs[i] = job.Dir
	}
	claudeFound, mentions := claude.Read(p.Claude, dirs, since)
	mentioned := make(map[string]bool, len(mentions))
	for _, mention := range mentions {
		mentioned[mention.Path] = true
	}
	var ids []string
	for _, job := range jobs {
		if mentioned[job.Dir] {
			ids = append(ids, job.SessionID)
		}
	}
	codexFound := codex.Read(p.Codex, since, ids)
	grokFound := grok.Read(p.Grok, since, ids)
	sessions := claudeFound.Sessions
	sessions = append(sessions, codexFound.Sessions...)
	sessions = append(sessions, grokFound.Sessions...)
	return Found{
		Sessions: dispatch.Link(sessions, jobs, mentions),
		Skipped: map[session.Tool]int{
			session.ToolClaude: claudeFound.Skipped,
			session.ToolCodex:  codexFound.Skipped,
			session.ToolGrok:   grokFound.Skipped,
		},
	}
}
