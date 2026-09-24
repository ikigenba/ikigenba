package grok

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/proc"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// Tree describes a Grok root session and the subagents recorded under it.
func Tree(root fs.FS, home, id string) (tree.Tree, error) {
	locating := path.Join("/", home, ".grok", "sessions")
	name := strings.TrimPrefix(locating, "/")
	if _, err := fs.Stat(root, name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return tree.Tree{}, tree.ErrNotFound
		}
		return tree.Tree{}, treeReadError(locating, err)
	}
	parents, err := fs.ReadDir(root, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return tree.Tree{}, tree.ErrNotFound
		}
		return tree.Tree{}, treeReadError(locating, err)
	}
	if !validSessionID(id) {
		return tree.Tree{}, tree.ErrNotFound
	}

	dir, isDir := treeSessionDir(root, locating, parents, id)
	if !isDir {
		dir = ""
	}
	index := path.Join("/", home, ".grok", "active_sessions.json")
	live, known := indexLiveness(root, index, id)
	if dir == "" && !live {
		return tree.Tree{}, tree.ErrNotFound
	}
	var rootSummary map[string]json.RawMessage
	if dir != "" {
		rootSummary = readJSONObject(root, path.Join(dir, "summary.json"))
		if kind, ok := jsonString(rootSummary["session_kind"]); ok && strings.HasPrefix(kind, "subagent") && !live {
			return tree.Tree{}, tree.ErrNotFound
		}
	}

	out := tree.Tree{Root: tree.Node{ID: id, Status: tree.StatusEnded}}
	if !known {
		out.Root.Status = tree.StatusUnknown
	} else if live {
		out.Root.Status = tree.StatusUnknown
	}
	if dir == "" {
		return out, nil
	}
	out.Root.Label, _ = jsonString(rootSummary["generated_title"])
	if live && known {
		s := session.Session{Status: session.StatusUnknown}
		readTreeEvents(root, dir, &s)
		out.Root.Status = tree.Status(s.Status)
	}

	subdir := path.Join(dir, "subagents")
	entries, err := fs.ReadDir(root, strings.TrimPrefix(subdir, "/"))
	if err != nil {
		return out, nil
	}
	spawns, rootPrompts := readTreeUpdates(root, dir)
	children := make(map[string]spawnInfo, len(entries))
	for _, entry := range entries {
		id := entry.Name()
		node := tree.Node{ID: id, Status: tree.StatusUnknown}
		meta := readJSONObject(root, path.Join(subdir, id, "meta.json"))
		node.Label, _ = jsonString(meta["description"])
		if t, ok := jsonTimestamp(meta["started_at"]); ok {
			node.Started, node.HasStarted = t, true
		}
		switch status, _ := jsonString(meta["status"]); status {
		case "completed":
			node.Status = tree.StatusDone
		case "failed":
			node.Status = tree.StatusFailed
		case "cancelled":
			node.Status = tree.StatusKilled
		case "running":
			if live && known {
				node.Status = tree.StatusWorking
			}
		}
		children[id] = spawns[id]
		out.Subagents = append(out.Subagents, node)
	}

	childPrompts := make(map[string]map[string]bool)
	readPrompts := map[string]map[string]bool{dir: rootPrompts}
	for _, candidate := range out.Subagents {
		childID := children[candidate.ID].session
		if !validSessionID(childID) {
			continue
		}
		childDir, _ := treeSessionDir(root, locating, parents, childID)
		if childDir == "" {
			continue
		}
		prompts, seen := readPrompts[childDir]
		if !seen {
			_, prompts = readTreeUpdates(root, childDir)
			readPrompts[childDir] = prompts
		}
		childPrompts[candidate.ID] = prompts
	}
	for i := range out.Subagents {
		node := &out.Subagents[i]
		prompt := children[node.ID].prompt
		if prompt == "" || rootPrompts[prompt] {
			continue
		}
		parent := ""
		matches := 0
		for _, candidate := range out.Subagents {
			if candidate.ID != node.ID && childPrompts[candidate.ID][prompt] {
				parent = candidate.ID
				matches++
			}
		}
		if matches == 1 {
			node.Parent = parent
		}
	}
	return out, nil
}

func validSessionID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.Contains(id, "/")
}

func treeReadError(location string, err error) error {
	var pe *fs.PathError
	for errors.As(err, &pe) {
		err = pe.Err
	}
	return &session.ReadError{Path: location, Err: err}
}

func treeSessionDir(root fs.FS, locating string, parents []fs.DirEntry, id string) (string, bool) {
	if !validSessionID(id) {
		return "", false
	}
	ordered := append([]fs.DirEntry(nil), parents...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name() < ordered[j].Name() })
	for _, parent := range ordered {
		if !parent.IsDir() {
			continue
		}
		p := path.Join(locating, parent.Name())
		entries, err := fs.ReadDir(root, strings.TrimPrefix(p, "/"))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.Name() == id {
				return path.Join(p, id), entry.IsDir()
			}
		}
	}
	return "", false
}

func indexLiveness(root fs.FS, index, id string) (live, known bool) {
	data, err := fs.ReadFile(root, strings.TrimPrefix(index, "/"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, true
	}
	if err != nil {
		return false, false
	}
	var entries []json.RawMessage
	if json.Unmarshal(data, &entries) != nil || entries == nil {
		return false, false
	}
	for _, raw := range entries {
		if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
			return false, false
		}
	}
	for _, raw := range entries {
		var entry map[string]json.RawMessage
		_ = json.Unmarshal(raw, &entry)
		entryID, ok := jsonString(entry["session_id"])
		if !ok || entryID != id {
			continue
		}
		pid, ok := positiveInt(entry["pid"])
		if !ok {
			continue
		}
		started, err := proc.Start(root, pid)
		if err != nil {
			continue
		}
		if opened, ok := jsonTimestamp(entry["opened_at"]); ok && started.After(opened) {
			continue
		}
		return true, true
	}
	return false, true
}

func readJSONObject(root fs.FS, absolute string) map[string]json.RawMessage {
	data, err := fs.ReadFile(root, strings.TrimPrefix(absolute, "/"))
	if err != nil || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil {
		return nil
	}
	return object
}

func treeRecords(root fs.FS, absolute string) []map[string]json.RawMessage {
	var log session.Log
	lines, _, err := log.Read(root, strings.TrimPrefix(absolute, "/"))
	if err != nil {
		return nil
	}
	var records []map[string]json.RawMessage
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(line) {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) == nil {
			records = append(records, record)
		}
	}
	return records
}

func readTreeEvents(root fs.FS, dir string, s *session.Session) {
	// The event log is read through one fresh incremental-log pass.
	var log session.Log
	lines, _, err := log.Read(root, strings.TrimPrefix(path.Join(dir, "events.jsonl"), "/"))
	if err != nil {
		return
	}
	s.Status = session.StatusIdle
	started, lastType := false, ""
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(line) {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		lastType, _ = jsonString(record["type"])
		if lastType == "turn_started" {
			started = true
		}
	}
	if started && lastType != "turn_ended" {
		s.Status = session.StatusWorking
	}
}

type spawnInfo struct{ prompt, session string }

func readTreeUpdates(root fs.FS, dir string) (map[string]spawnInfo, map[string]bool) {
	spawns := make(map[string]spawnInfo)
	prompts := make(map[string]bool)
	for _, record := range treeRecords(root, path.Join(dir, "updates.jsonl")) {
		var params map[string]json.RawMessage
		if json.Unmarshal(record["params"], &params) != nil || params == nil {
			continue
		}
		var meta map[string]json.RawMessage
		if json.Unmarshal(params["_meta"], &meta) == nil && meta != nil {
			if prompt, ok := jsonString(meta["promptId"]); ok {
				prompts[prompt] = true
			}
		}
		var update map[string]json.RawMessage
		if json.Unmarshal(params["update"], &update) != nil || update == nil {
			continue
		}
		kind, _ := jsonString(update["sessionUpdate"])
		id, _ := jsonString(update["subagent_id"])
		if kind != "subagent_spawned" || id == "" {
			continue
		}
		prompt, _ := jsonString(update["parent_prompt_id"])
		child, _ := jsonString(update["child_session_id"])
		spawns[id] = spawnInfo{prompt: prompt, session: child}
	}
	return spawns, prompts
}
