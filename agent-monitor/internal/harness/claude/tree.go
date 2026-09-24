package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

type treeRecord map[string]json.RawMessage

type treeLog struct {
	records []treeRecord
	ok      bool
}

func readTreeLog(root fs.FS, name string) treeLog {
	var log session.Log
	lines, _, err := log.Read(root, strings.TrimPrefix(name, "/"))
	if err != nil {
		return treeLog{}
	}
	result := treeLog{ok: true}
	for _, line := range lines {
		if !json.Valid(line) || len(bytes.TrimSpace(line)) == 0 || bytes.TrimSpace(line)[0] != '{' {
			continue
		}
		var record treeRecord
		if json.Unmarshal(line, &record) == nil && record != nil {
			result.records = append(result.records, record)
		}
	}
	return result
}

func treeField(record treeRecord, name string) string {
	value, _ := stringField(record[name])
	return value
}

func treeObject(raw json.RawMessage) treeRecord {
	var object treeRecord
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	return object
}

func treeArray(raw json.RawMessage) []json.RawMessage {
	var array []json.RawMessage
	if json.Unmarshal(raw, &array) != nil {
		return nil
	}
	return array
}

func treeTimestamp(record treeRecord) (time.Time, bool) {
	value, ok := stringField(record["timestamp"])
	if !ok {
		return time.Time{}, false
	}
	stamp, err := time.Parse(time.RFC3339Nano, value)
	return stamp, err == nil
}

func readTreeObject(root fs.FS, name string) treeRecord {
	data, err := fs.ReadFile(root, strings.TrimPrefix(name, "/"))
	if err != nil {
		return nil
	}
	var object treeRecord
	if json.Unmarshal(data, &object) != nil {
		return nil
	}
	return object
}

func treeRegistration(root fs.FS, registry, id string) (treeRecord, int) {
	entries, err := fs.ReadDir(root, strings.TrimPrefix(registry, "/"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0
		}
		return nil, -1
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fields := readTreeObject(root, path.Join(registry, entry.Name()))
		sessionID, idOK := stringField(fields["sessionId"])
		pidValue, pidOK := numberField(fields["pid"], false)
		if !idOK || sessionID != id || !pidOK || pidValue == 0 || pidValue > uint64(int(^uint(0)>>1)) {
			continue
		}
		if treeLive(root, int(pidValue), fields) {
			return fields, 1
		}
	}
	return nil, 0
}

func treeLive(root fs.FS, pid int, fields treeRecord) bool {
	start, ok := stringField(fields["procStart"])
	if ok {
		valid := start != ""
		for i := 0; i < len(start); i++ {
			if start[i] < '0' || start[i] > '9' {
				valid = false
				break
			}
		}
		if _, err := strconv.ParseUint(start, 10, 64); err != nil {
			valid = false
		}
		if !valid {
			copyFields := make(treeRecord, len(fields))
			for key, value := range fields {
				copyFields[key] = value
			}
			delete(copyFields, "procStart")
			return live(root, pid, copyFields)
		}
	}
	return live(root, pid, fields)
}

func treeProject(root fs.FS, projects, id string, entries []fs.DirEntry) string {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := path.Join(projects, entry.Name(), id+".jsonl")
		info, err := fs.Stat(root, strings.TrimPrefix(name, "/"))
		if err == nil && !info.IsDir() {
			return path.Join(projects, entry.Name())
		}
	}
	return ""
}

func treeTitle(log treeLog) string {
	if !log.ok {
		return ""
	}
	var custom, ai string
	for _, record := range log.records {
		switch treeField(record, "type") {
		case "custom-title":
			if value := treeField(record, "customTitle"); value != "" {
				custom = value
			}
		case "ai-title":
			if value := treeField(record, "aiTitle"); value != "" {
				ai = value
			}
		}
	}
	if custom != "" {
		return custom
	}
	return ai
}

func notification(record treeRecord, root bool) string {
	switch treeField(record, "type") {
	case "queue-operation":
		if root && treeField(record, "operation") == "enqueue" {
			value := treeField(record, "content")
			if strings.HasPrefix(value, "<task-notification>") {
				return value
			}
		}
	case "user":
		if !root && treeField(treeObject(record["origin"]), "kind") == "task-notification" {
			value := treeField(treeObject(record["message"]), "content")
			if strings.Contains(value, "<task-notification>") {
				return value
			}
		}
	case "attachment":
		if !root && treeField(treeObject(record["attachment"]), "commandMode") == "task-notification" {
			value := treeField(treeObject(record["attachment"]), "prompt")
			if strings.Contains(value, "<task-notification>") {
				return value
			}
		}
	}
	return ""
}

func tagAfter(value string, start int, tag string) (string, bool) {
	i := strings.Index(value[start:], "<"+tag+">")
	if i < 0 {
		return "", false
	}
	i += start + len(tag) + 2
	j := strings.Index(value[i:], "</"+tag+">")
	if j < 0 {
		return "", false
	}
	return value[i : i+j], true
}

func notificationResult(record treeRecord, root bool, id string) (tree.Status, bool) {
	value := notification(record, root)
	start := strings.Index(value, "<task-notification>")
	if start < 0 {
		return "", false
	}
	agent, agentOK := tagAfter(value, start, "task-id")
	word, wordOK := tagAfter(value, start, "status")
	if !agentOK || agent != id || !wordOK {
		return "", false
	}
	switch word {
	case "completed":
		return tree.StatusDone, true
	case "failed":
		return tree.StatusFailed, true
	case "killed":
		return tree.StatusKilled, true
	default:
		return tree.StatusUnknown, true
	}
}

func toolResult(record treeRecord, useID string) bool {
	if useID == "" || treeField(record, "type") != "user" {
		return false
	}
	for _, raw := range treeArray(treeObject(record["message"])["content"]) {
		item := treeObject(raw)
		if treeField(item, "type") == "tool_result" && treeField(item, "tool_use_id") == useID {
			return true
		}
	}
	return false
}

func sentMessage(record treeRecord, id string) bool {
	if treeField(record, "type") != "assistant" {
		return false
	}
	for _, raw := range treeArray(treeObject(record["message"])["content"]) {
		item := treeObject(raw)
		if treeField(item, "type") == "tool_use" && treeField(item, "name") == "SendMessage" && treeField(treeObject(item["input"]), "to") == id {
			return true
		}
	}
	return false
}

func subagentStatus(id, starter, rootName string, meta treeRecord, logs map[string]treeLog, liveState int) tree.Status {
	rootLog := logs[rootName]
	starterLog := logs[starter]
	var rootResult, starterResult tree.Status
	var hasRoot, hasStarter, hasTimedResult, hasMessage bool
	var latestResult, latestMessage time.Time
	counted := []string{rootName}
	if starter != "" && starter != rootName {
		counted = append(counted, starter)
	}
	for _, name := range counted {
		log := logs[name]
		if !log.ok {
			continue
		}
		for _, record := range log.records {
			status, result := notificationResult(record, name == rootName, id)
			if name == starter && treeField(meta, "requestShape") == "foreground" && toolResult(record, treeField(meta, "toolUseId")) {
				status, result = tree.StatusDone, true
			}
			if result {
				if name == rootName && notification(record, true) != "" {
					rootResult, hasRoot = status, true
				} else if name == starter {
					starterResult, hasStarter = status, true
				}
				if stamp, ok := treeTimestamp(record); ok && (!hasTimedResult || stamp.After(latestResult)) {
					latestResult, hasTimedResult = stamp, true
				}
			}
			if sentMessage(record, id) {
				if stamp, ok := treeTimestamp(record); ok && (!hasMessage || stamp.After(latestMessage)) {
					latestMessage, hasMessage = stamp, true
				}
			}
		}
	}
	if hasTimedResult && hasMessage && latestMessage.After(latestResult) {
		if liveState == 1 {
			return tree.StatusWorking
		}
		return tree.StatusUnknown
	}
	if rootLog.ok && hasRoot {
		return rootResult
	}
	if starter != "" && starterLog.ok && hasStarter {
		return starterResult
	}
	if starter != "" && rootLog.ok && starterLog.ok {
		if liveState == 1 {
			return tree.StatusWorking
		}
	}
	return tree.StatusUnknown
}

// Tree returns the root session and subagents recorded by Claude Code.
func Tree(root fs.FS, home, id string) (tree.Tree, error) {
	projects := path.Join("/", home, ".claude", "projects")
	registry := path.Join("/", home, ".claude", "sessions")
	projectName := strings.TrimPrefix(projects, "/")
	_, err := fs.Stat(root, projectName)
	if err != nil {
		return tree.Tree{}, treeDirectoryError(projects, err)
	}
	entries, err := fs.ReadDir(root, projectName)
	if err != nil {
		return tree.Tree{}, treeDirectoryError(projects, err)
	}
	if id == "" || id == "." || id == ".." || strings.Contains(id, "/") {
		return tree.Tree{}, tree.ErrNotFound
	}
	project := treeProject(root, projects, id, entries)
	registration, liveState := treeRegistration(root, registry, id)
	if project == "" && liveState != 1 {
		return tree.Tree{}, tree.ErrNotFound
	}
	result := tree.Tree{Root: tree.Node{ID: id}}
	switch liveState {
	case 1:
		switch treeField(registration, "status") {
		case "busy":
			result.Root.Status = tree.StatusWorking
		case "idle":
			result.Root.Status = tree.StatusIdle
		default:
			result.Root.Status = tree.StatusUnknown
		}
	case 0:
		result.Root.Status = tree.StatusEnded
	default:
		result.Root.Status = tree.StatusUnknown
	}
	rootName := ""
	logs := make(map[string]treeLog)
	if project != "" {
		rootName = path.Join(project, id+".jsonl")
		logs[rootName] = readTreeLog(root, rootName)
	}
	result.Root.Label = treeField(registration, "name")
	if result.Root.Label == "" {
		result.Root.Label = treeTitle(logs[rootName])
	}
	if project == "" {
		return result, nil
	}
	subdir := path.Join(project, id, "subagents")
	subentries, err := fs.ReadDir(root, strings.TrimPrefix(subdir, "/"))
	if err != nil {
		return result, nil
	}
	ids := map[string]bool{}
	for _, entry := range subentries {
		name := entry.Name()
		if !strings.HasPrefix(name, "agent-") {
			continue
		}
		var agent string
		if strings.HasSuffix(name, ".meta.json") {
			agent = strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".meta.json")
		} else if strings.HasSuffix(name, ".jsonl") {
			agent = strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".jsonl")
		}
		if agent != "" {
			ids[agent] = true
		}
	}
	ordered := make([]string, 0, len(ids))
	for agent := range ids {
		ordered = append(ordered, agent)
	}
	sort.Strings(ordered)
	metas := make(map[string]treeRecord, len(ids))
	starters := make(map[string]string, len(ids))
	for _, agent := range ordered {
		meta := readTreeObject(root, path.Join(subdir, "agent-"+agent+".meta.json"))
		metas[agent] = meta
		starter := ""
		parent := ""
		if meta != nil {
			raw, exists := meta["parentAgentId"]
			if !exists {
				starter = rootName
			} else if value, ok := stringField(raw); ok && value != "" && value != "." && value != ".." && value != agent && !strings.Contains(value, "/") {
				parent = value
				starter = path.Join(subdir, "agent-"+value+".jsonl")
			}
		}
		starters[agent] = starter
		result.Subagents = append(result.Subagents, tree.Node{ID: agent, Parent: parent, Label: treeField(meta, "description")})
	}
	for _, agent := range ordered {
		own := path.Join(subdir, "agent-"+agent+".jsonl")
		if _, ok := logs[own]; !ok {
			logs[own] = readTreeLog(root, own)
		}
		if starter := starters[agent]; starter != "" {
			if _, ok := logs[starter]; !ok {
				logs[starter] = readTreeLog(root, starter)
			}
		}
	}
	for i, agent := range ordered {
		own := logs[path.Join(subdir, "agent-"+agent+".jsonl")]
		if own.ok {
			for _, record := range own.records {
				if stamp, ok := treeTimestamp(record); ok {
					result.Subagents[i].Started = stamp
					result.Subagents[i].HasStarted = true
					break
				}
			}
		}
		result.Subagents[i].Status = subagentStatus(agent, starters[agent], rootName, metas[agent], logs, liveState)
	}
	return result, nil
}

func treeDirectoryError(projects string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return tree.ErrNotFound
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return &session.ReadError{Path: projects, Err: err}
}
