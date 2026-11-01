// Package dispatch reads job identities and links their sessions to dispatchers.
package dispatch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/turns/internal/session"
)

// Job identifies the child session recorded in a dispatch job directory.
type Job struct {
	Dir       string
	SessionID string
}

// Jobs returns readable job identities in path order without changing any files.
func Jobs(dir string) []Job {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var jobs []Job
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			continue
		}
		name := filepath.Join(path, "session-id")
		info, statErr = os.Stat(name)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Clean(name))
		if readErr == nil {
			if id := strings.TrimSpace(string(data)); id != "" {
				jobs = append(jobs, Job{Dir: path, SessionID: id})
			}
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Dir < jobs[j].Dir })
	return jobs
}

// Link returns independent sessions with eligible dispatch parents filled in.
func Link(sessions []session.Session, jobs []Job, mentions []session.Mention) []session.Session {
	linked := make([]session.Session, len(sessions))
	for i, value := range sessions {
		linked[i] = value
		if value.Parent != nil {
			parent := *value.Parent
			linked[i].Parent = &parent
		}
		if value.Command != nil {
			command := *value.Command
			linked[i].Command = &command
		}
		if value.Turns != nil {
			linked[i].Turns = append([]session.Turn(nil), value.Turns...)
		}
		if value.Parent != nil || (value.Ref.Tool != session.ToolCodex && value.Ref.Tool != session.ToolGrok) {
			continue
		}
		var best session.Mention
		found := false
		for _, job := range jobs {
			if job.SessionID != value.Ref.ID {
				continue
			}
			for _, mention := range mentions {
				if mention.Path != job.Dir || mention.By.ID == "" || mention.By == value.Ref {
					continue
				}
				if !found || mention.Moment.Before(best.Moment) || (mention.Moment.Equal(best.Moment) && (mention.By.ID < best.By.ID || (mention.By.ID == best.By.ID && mention.By.Tool < best.By.Tool))) {
					best, found = mention, true
				}
			}
		}
		if found {
			parent := best.By
			linked[i].Parent = &parent
		}
	}
	return linked
}
