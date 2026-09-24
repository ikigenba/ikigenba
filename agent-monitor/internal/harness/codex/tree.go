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
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

type treeRollout struct {
	found    bool
	readable bool
	records  []map[string]json.RawMessage
	started  []startedItem
}

type startedItem struct {
	id      string
	label   string
	started time.Time
	hasTime bool
}

// Tree returns the Codex root and the threads named by its started items.
func Tree(root fs.FS, home, id string) (tree.Tree, error) {
	base := path.Join("/", home, ".codex")
	sessionsDir := path.Join(base, "sessions")
	dirName := name(sessionsDir)
	if _, err := fs.Stat(root, dirName); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return tree.Tree{}, tree.ErrNotFound
		}
		return tree.Tree{}, readError(sessionsDir, err)
	}
	if _, err := fs.ReadDir(root, dirName); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return tree.Tree{}, tree.ErrNotFound
		}
		return tree.Tree{}, readError(sessionsDir, err)
	}
	if id == "" || id == "." || id == ".." || strings.Contains(id, "/") {
		return tree.Tree{}, tree.ErrNotFound
	}

	rollouts := make(map[string]treeRollout)
	load := func(threadID string) treeRollout {
		if previous, ok := rollouts[threadID]; ok {
			return previous
		}
		file := firstTreeRollout(root, sessionsDir, threadID)
		result := treeRollout{found: file != ""}
		if result.found {
			var log session.Log
			lines, _, err := log.Read(root, name(file))
			if err == nil {
				result.readable = true
				result.records = recordsOf(lines)
				result.started = startedItems(result.records, threadID)
			}
		}
		rollouts[threadID] = result
		return result
	}

	rootRollout := load(id)
	subagent := len(rootRollout.records) > 0 && parentID(firstMeta(rootRollout.records)) != ""
	lockFile := path.Join(base, "thread-writer-locks", id+".lock")
	liveness := rootLiveness(root, lockFile)
	if subagent || (!rootRollout.found && liveness != live) {
		return tree.Tree{}, tree.ErrNotFound
	}

	result := tree.Tree{Root: tree.Node{ID: id, Status: tree.StatusUnknown}}
	if liveness == dead {
		result.Root.Status = tree.StatusEnded
	} else if liveness == live && len(rootRollout.records) > 0 {
		result.Root.Status = rootStatus(rootRollout.records)
	}
	result.Root.Label = indexTitlesFromLog(root, path.Join(base, "session_index.jsonl"))[id]

	// Expand each thread once, including cycles. The root's own started items
	// take priority when several rollouts name the same child.
	queue := []string{id}
	seen := map[string]bool{id: true}
	namers := make(map[string]map[string]startedItem)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, item := range load(current).started {
			if item.id == id {
				continue
			}
			if namers[item.id] == nil {
				namers[item.id] = make(map[string]startedItem)
			}
			if _, exists := namers[item.id][current]; !exists {
				namers[item.id][current] = item
			}
			if !seen[item.id] {
				seen[item.id] = true
				queue = append(queue, item.id)
			}
		}
	}

	for childID, parents := range namers {
		starter := id
		if _, rootNames := parents[id]; !rootNames {
			starter = ""
			for candidate := range parents {
				if starter == "" || candidate < starter {
					starter = candidate
				}
			}
		}
		item := parents[starter]
		child := tree.Node{ID: childID, Label: item.label, Started: item.started, HasStarted: item.hasTime, Status: childStatus(load(childID).records, liveness)}
		if starter != id && len(parents) == 1 {
			child.Parent = starter
		}
		result.Subagents = append(result.Subagents, child)
	}
	sort.Slice(result.Subagents, func(i, j int) bool { return result.Subagents[i].ID < result.Subagents[j].ID })
	return result, nil
}

type liveState uint8

const (
	unknown liveState = iota
	dead
	live
)

func rootLiveness(root fs.FS, lockFile string) liveState {
	info, err := fs.Stat(root, name(lockFile))
	if errors.Is(err, fs.ErrNotExist) {
		return dead
	}
	if err != nil {
		return unknown
	}
	fileID, ok := proc.FileIDOf(info)
	if !ok {
		return unknown
	}
	holders, err := proc.LockHolders(root)
	if err != nil {
		return unknown
	}
	if _, held := holders[fileID]; held {
		return live
	}
	return dead
}

func firstTreeRollout(root fs.FS, directory, threadID string) string {
	for _, year := range directories(root, directory) {
		for _, month := range directories(root, path.Join(directory, year)) {
			for _, day := range directories(root, path.Join(directory, year, month)) {
				dayPath := path.Join(directory, year, month, day)
				entries, err := fs.ReadDir(root, name(dayPath))
				if err != nil {
					continue
				}
				sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
				for _, entry := range entries {
					if !entry.IsDir() && treeRolloutName(entry.Name(), threadID) {
						return path.Join(dayPath, entry.Name())
					}
				}
			}
		}
	}
	return ""
}

func treeRolloutName(file, id string) bool {
	const prefix = "rollout-"
	const stampLength = 19
	if len(file) != len(prefix)+stampLength+1+len(id)+len(".jsonl") || !strings.HasPrefix(file, prefix) || !strings.HasSuffix(file, "-"+id+".jsonl") {
		return false
	}
	stamp := file[len(prefix) : len(prefix)+stampLength]
	for i := range stampLength {
		switch i {
		case 4, 7, 13, 16:
			if stamp[i] != '-' {
				return false
			}
		case 10:
			if stamp[i] != 'T' {
				return false
			}
		default:
			if stamp[i] < '0' || stamp[i] > '9' {
				return false
			}
		}
	}
	return true
}

func recordsOf(lines [][]byte) []map[string]json.RawMessage {
	var records []map[string]json.RawMessage
	for _, line := range lines {
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) == nil && record != nil {
			records = append(records, record)
		}
	}
	return records
}

func startedItems(records []map[string]json.RawMessage, ownID string) []startedItem {
	var out []startedItem
	for _, record := range records {
		if stringField(record, "type") != "event_msg" {
			continue
		}
		payload := objectField(record, "payload")
		if copiedFrom, isString := jsonString(payload, "thread_id"); isString && copiedFrom != ownID {
			continue
		}
		item := objectField(payload, "item")
		if stringField(item, "type") != "SubAgentActivity" || stringField(item, "kind") != "started" {
			continue
		}
		childID := stringField(item, "agent_thread_id")
		if childID == "" {
			continue
		}
		stamp, hasTime := recordTimestamp(record, "timestamp")
		label := stringField(item, "agent_path")
		if index := strings.LastIndexByte(label, '/'); index >= 0 {
			label = label[index+1:]
		}
		out = append(out, startedItem{id: childID, label: label, started: stamp, hasTime: hasTime})
	}
	return out
}

func recordTimestamp(record map[string]json.RawMessage, key string) (time.Time, bool) {
	var value string
	if json.Unmarshal(record[key], &value) != nil {
		return time.Time{}, false
	}
	stamp, err := time.Parse(time.RFC3339Nano, value)
	return stamp, err == nil
}

func jsonString(record map[string]json.RawMessage, key string) (string, bool) {
	var value string
	if err := json.Unmarshal(record[key], &value); err != nil {
		return "", false
	}
	if len(record[key]) == 0 || record[key][0] != '"' {
		return "", false
	}
	return value, true
}

func rootStatus(records []map[string]json.RawMessage) tree.Status {
	status := tree.StatusIdle
	for _, record := range records {
		if stringField(record, "type") == "event_msg" {
			switch stringField(objectField(record, "payload"), "type") {
			case "task_started":
				status = tree.StatusWorking
			case "task_complete", "turn_aborted":
				status = tree.StatusIdle
			}
		}
	}
	return status
}

func childStatus(records []map[string]json.RawMessage, liveness liveState) tree.Status {
	status := tree.StatusUnknown
	for _, record := range records {
		if stringField(record, "type") == "event_msg" {
			switch stringField(objectField(record, "payload"), "type") {
			case "task_started":
				status = tree.StatusUnknown
				if liveness == live {
					status = tree.StatusWorking
				}
			case "task_complete":
				status = tree.StatusDone
			case "turn_aborted":
				status = tree.StatusKilled
			}
		}
	}
	return status
}

func indexTitlesFromLog(root fs.FS, indexPath string) map[string]string {
	titles := make(map[string]string)
	var log session.Log
	lines, _, err := log.Read(root, name(indexPath))
	if err != nil {
		return titles
	}
	for _, record := range recordsOf(lines) {
		if id := stringField(record, "id"); id != "" {
			titles[id] = stringField(record, "thread_name")
		}
	}
	return titles
}
