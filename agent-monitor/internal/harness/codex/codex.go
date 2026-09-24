// Package codex discovers live Codex threads from their writer locks.
package codex

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/proc"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

// List returns the live root threads under home.
func List(root fs.FS, home string) ([]session.Session, error) {
	base := path.Join("/", home, ".codex")
	lockDir := path.Join(base, "thread-writer-locks")
	entries, err := fs.ReadDir(root, name(lockDir))
	if errors.Is(err, fs.ErrNotExist) {
		return []session.Session{}, nil
	}
	if err != nil {
		return nil, readError(lockDir, err)
	}
	holders, err := proc.LockHolders(root)
	if err != nil {
		return nil, readError("/proc/locks", err)
	}
	sessionsDir := path.Join(base, "sessions")
	indexPath := path.Join(base, "session_index.jsonl")
	var out []session.Session
	for _, entry := range entries {
		filename := entry.Name()
		if !strings.HasSuffix(filename, ".lock") || strings.HasPrefix(filename, ".") || len(filename) <= len(".lock") {
			continue
		}
		threadID := strings.TrimSuffix(filename, ".lock")
		info, statErr := fs.Stat(root, name(path.Join(lockDir, filename)))
		if statErr != nil {
			continue
		}
		fileID, ok := proc.FileIDOf(info)
		if !ok {
			continue
		}
		pid, live := holders[fileID]
		if !live {
			continue
		}
		s := session.Session{ID: threadID, Status: session.StatusUnknown}
		rollout := firstRollout(root, sessionsDir, threadID)
		if rollout != "" {
			data, readErr := fs.ReadFile(root, name(rollout))
			if readErr == nil {
				records := logRecords(data)
				if len(records) != 0 {
					meta := firstMeta(records)
					if parentID(meta) != "" {
						continue
					}
					s.Status = session.StatusIdle
					for _, record := range records {
						if stringField(record, "type") == "event_msg" {
							switch stringField(objectField(record, "payload"), "type") {
							case "task_started":
								s.Status = session.StatusWorking
							case "task_complete", "turn_aborted":
								s.Status = session.StatusIdle
							}
						}
						if raw, exists := record["timestamp"]; exists {
							var value string
							if json.Unmarshal(raw, &value) == nil {
								if stamp, parseErr := time.Parse(time.RFC3339Nano, value); parseErr == nil && (!s.HasLastActive || stamp.After(s.LastActive)) {
									s.LastActive = stamp
									s.HasLastActive = true
								}
							}
						}
					}
					s.CWD = stringField(objectField(meta, "payload"), "cwd")
				}
			}
		}
		if s.CWD == "" {
			s.CWD, _ = proc.Cwd(root, pid)
		}
		out = append(out, s)
	}
	if len(out) > 0 {
		titles := indexTitles(root, indexPath)
		for i := range out {
			out[i].Title = titles[out[i].ID]
		}
	}
	return out, nil
}

func name(absolute string) string { return strings.TrimPrefix(absolute, "/") }

func readError(absolute string, err error) *session.ReadError {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return &session.ReadError{Path: absolute, Err: err}
}

func firstRollout(root fs.FS, directory, threadID string) string {
	for _, year := range directories(root, directory) {
		yearPath := path.Join(directory, year)
		for _, month := range directories(root, yearPath) {
			monthPath := path.Join(yearPath, month)
			for _, day := range directories(root, monthPath) {
				dayPath := path.Join(monthPath, day)
				entries, err := fs.ReadDir(root, name(dayPath))
				if err != nil {
					continue
				}
				sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
				for _, entry := range entries {
					file := entry.Name()
					if !entry.IsDir() && strings.HasPrefix(file, "rollout-") && strings.HasSuffix(file, "-"+threadID+".jsonl") {
						return path.Join(dayPath, file)
					}
				}
			}
		}
	}
	return ""
}

func directories(root fs.FS, directory string) []string {
	entries, err := fs.ReadDir(root, name(directory))
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func logRecords(data []byte) []map[string]json.RawMessage {
	var records []map[string]json.RawMessage
	for _, line := range session.Lines(data) {
		if !json.Valid(line) {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) == nil && record != nil {
			records = append(records, record)
		}
	}
	return records
}

func firstMeta(records []map[string]json.RawMessage) map[string]json.RawMessage {
	if len(records) > 0 && stringField(records[0], "type") == "session_meta" {
		return records[0]
	}
	return nil
}

func parentID(meta map[string]json.RawMessage) string {
	payload := objectField(meta, "payload")
	if id := stringField(payload, "parent_thread_id"); id != "" {
		return id
	}
	source := objectField(payload, "source")
	subagent := objectField(source, "subagent")
	spawn := objectField(subagent, "thread_spawn")
	return stringField(spawn, "parent_thread_id")
}

func objectField(record map[string]json.RawMessage, field string) map[string]json.RawMessage {
	var object map[string]json.RawMessage
	_ = json.Unmarshal(record[field], &object)
	return object
}

func stringField(record map[string]json.RawMessage, field string) string {
	var value string
	if json.Unmarshal(record[field], &value) != nil {
		return ""
	}
	return value
}

func indexTitles(root fs.FS, indexPath string) map[string]string {
	titles := make(map[string]string)
	data, err := fs.ReadFile(root, name(indexPath))
	if err != nil {
		return titles
	}
	for _, record := range logRecords(data) {
		if id := stringField(record, "id"); id != "" {
			titles[id] = stringField(record, "thread_name")
		}
	}
	return titles
}
