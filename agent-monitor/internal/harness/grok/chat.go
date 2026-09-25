package grok

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// Chat reads the named Grok agent's transcript.
func Chat(root fs.FS, home, sessionID, agentID string) (*chat.Transcript, []chat.Entry, error) {
	locating := path.Join("/", home, ".grok", "sessions")
	parents, err := chatLocations(root, locating)
	if err != nil {
		return nil, nil, err
	}
	if !validSessionID(sessionID) {
		return nil, nil, tree.ErrNotFound
	}
	dir, isDir := treeSessionDir(root, locating, parents, sessionID)
	if !isDir {
		dir = ""
	}
	live, _ := indexLiveness(root, path.Join("/", home, ".grok", "active_sessions.json"), sessionID)
	if dir == "" && !live {
		return nil, nil, tree.ErrNotFound
	}
	if dir != "" {
		summary := readJSONObject(root, path.Join(dir, "summary.json"))
		kind, _ := jsonString(summary["session_kind"])
		if strings.HasPrefix(kind, "subagent") && !live {
			return nil, nil, tree.ErrNotFound
		}
	}

	rootAgent := agentID == sessionID
	ownedID := sessionID
	transcriptDir := dir
	if !rootAgent {
		if dir == "" {
			return nil, nil, chat.ErrAgentNotFound
		}
		entries, err := fs.ReadDir(root, strings.TrimPrefix(path.Join(dir, "subagents"), "/"))
		if err != nil {
			return nil, nil, chat.ErrAgentNotFound
		}
		found := false
		for _, entry := range entries {
			if entry.Name() == agentID {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, chat.ErrAgentNotFound
		}
		meta := readJSONObject(root, path.Join(dir, "subagents", agentID, "meta.json"))
		ownedID, _ = jsonString(meta["child_session_id"])
		transcriptDir = ""
		if validSessionID(ownedID) {
			if childDir, childIsDir := treeSessionDir(root, locating, parents, ownedID); childIsDir {
				transcriptDir = childDir
			}
		}
	}
	transcriptPath := ""
	if transcriptDir != "" {
		transcriptPath = path.Join(transcriptDir, "updates.jsonl")
	}
	recorded := chat.Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}
	tr := chat.NewTranscript(transcriptPath, recorded, func() chat.Decoder {
		return &grokDecoder{rootAgent: rootAgent, ownID: ownedID, locating: locating, children: make(map[string]*grokChild), spawns: make(map[string]string), prompts: make(map[string]bool)}
	})
	entries, _, err := tr.Read(root)
	if err != nil {
		return nil, nil, err
	}
	return tr, entries, nil
}

func chatLocations(root fs.FS, locating string) ([]fs.DirEntry, error) {
	name := strings.TrimPrefix(locating, "/")
	if _, err := fs.Stat(root, name); err != nil {
		if isNotExist(err) {
			return nil, tree.ErrNotFound
		}
		return nil, treeReadError(locating, err)
	}
	parents, err := fs.ReadDir(root, name)
	if err != nil {
		if isNotExist(err) {
			return nil, tree.ErrNotFound
		}
		return nil, treeReadError(locating, err)
	}
	return parents, nil
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

type grokChild struct {
	log        session.Log
	current    chat.Usage
	subtracted chat.Usage
}

type grokDecoder struct {
	rootAgent bool
	ownID     string
	locating  string
	spawns    map[string]string
	prompts   map[string]bool
	finished  map[string]bool
	children  map[string]*grokChild
}

func (d *grokDecoder) Decode(fsys fs.FS, record []byte) ([]chat.Entry, chat.Usage) {
	var top map[string]json.RawMessage
	if json.Unmarshal(record, &top) != nil {
		return nil, chat.Usage{}
	}
	params := grokObject(top["params"])
	meta := grokObject(params["_meta"])
	if eventID, ok := jsonString(meta["eventId"]); ok && !strings.HasPrefix(eventID, d.ownID+"-") {
		return nil, chat.Usage{}
	}
	update := grokObject(params["update"])
	kind, _ := jsonString(update["sessionUpdate"])
	defer func() {
		if prompt, ok := jsonString(meta["promptId"]); ok {
			d.prompts[prompt] = true
		}
	}()
	if kind == "subagent_spawned" {
		if id, ok := jsonString(update["subagent_id"]); ok {
			prompt, _ := jsonString(update["parent_prompt_id"])
			d.spawns[id] = prompt
		}
	}
	if kind == "subagent_finished" {
		id, ok := jsonString(update["subagent_id"])
		if !ok || !d.direct(id) {
			return nil, chat.Usage{}
		}
		childID, _ := jsonString(update["child_session_id"])
		if validSessionID(childID) && childID != d.ownID {
			if d.finished == nil {
				d.finished = make(map[string]bool)
			}
			d.finished[childID] = true
		}
		output, ok := jsonString(update["output"])
		if !ok {
			output, _ = jsonString(update["error"])
		}
		return d.one(top, chat.KindAgent, "", output), chat.Usage{}
	}
	switch kind {
	case "user_message_chunk", "agent_message_chunk", "agent_thought_chunk":
		if kind == "user_message_chunk" {
			var hidden bool
			_ = json.Unmarshal(grokObject(update["_meta"])["hideFromScrollback"], &hidden)
			if hidden {
				return nil, chat.Usage{}
			}
		}
		text, ok := jsonString(grokObject(update["content"])["text"])
		if !ok {
			return nil, chat.Usage{}
		}
		entryKind := chat.KindUser
		switch kind {
		case "agent_message_chunk":
			entryKind = chat.KindAssistant
		case "agent_thought_chunk":
			entryKind = chat.KindReasoning
		case "user_message_chunk":
			if !d.rootAgent {
				entryKind = chat.KindAgent
			}
		}
		return d.one(top, entryKind, "", text), chat.Usage{}
	case "tool_call":
		tool, _ := jsonString(grokObject(grokObject(update["_meta"])["x.ai/tool"])["name"])
		if tool == "" {
			tool, _ = jsonString(update["title"])
		}
		return d.one(top, chat.KindTool, tool, string(update["rawInput"])), chat.Usage{}
	case "tool_call_update":
		status, _ := jsonString(update["status"])
		if status != "failed" && status != "completed" {
			return nil, chat.Usage{}
		}
		entryKind := chat.KindResultOK
		output := grokObject(update["rawOutput"])
		outputKind, _ := jsonString(output["type"])
		if status == "failed" || outputKind == "Bash" && grokNonzero(output["exit_code"]) {
			entryKind = chat.KindResultError
		}
		return d.one(top, entryKind, "", ""), chat.Usage{}
	case "turn_completed":
		usage := grokTurnUsage(update)
		for childID := range d.finished {
			child := d.children[childID]
			if child == nil {
				child = &grokChild{}
				d.children[childID] = child
			}
			if childDir := findSessionDir(fsys, d.locating, childID); childDir != "" {
				lines, reset, err := child.log.Read(fsys, strings.TrimPrefix(path.Join(childDir, "updates.jsonl"), "/"))
				if err == nil {
					if reset {
						child.current = chat.Usage{}
					}
					for _, line := range lines {
						var childTop map[string]json.RawMessage
						if !json.Valid(line) || json.Unmarshal(line, &childTop) != nil || childTop == nil {
							continue
						}
						childParams := grokObject(childTop["params"])
						childMeta := grokObject(childParams["_meta"])
						if eventID, ok := jsonString(childMeta["eventId"]); ok && !strings.HasPrefix(eventID, childID+"-") {
							continue
						}
						childUpdate := grokObject(childParams["update"])
						childKind, _ := jsonString(childUpdate["sessionUpdate"])
						if childKind == "turn_completed" {
							child.current = grokAdd(child.current, grokTurnUsage(childUpdate))
						}
					}
				}
			}
			usage = grokSub(usage, grokSub(child.current, child.subtracted))
			child.subtracted = child.current
		}
		d.finished = nil
		return nil, usage
	}
	return nil, chat.Usage{}
}

func (d *grokDecoder) direct(id string) bool {
	prompt, ok := d.spawns[id]
	return ok && d.prompts[prompt]
}

func (d *grokDecoder) one(top map[string]json.RawMessage, kind chat.Kind, tool, text string) []chat.Entry {
	e := chat.Entry{Kind: kind, Tool: tool, Text: text}
	if n, ok := grokInt(top["timestamp"]); ok {
		e.Time, e.HasTime = time.Unix(n, 0), true
	}
	return []chat.Entry{e}
}

func grokObject(raw json.RawMessage) map[string]json.RawMessage {
	var obj map[string]json.RawMessage
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	return obj
}

func grokInt(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || raw[0] == '"' || raw[0] == '{' || raw[0] == '[' || raw[0] == 'n' || raw[0] == 't' || raw[0] == 'f' {
		return 0, false
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	return n, err == nil
}

func grokNonzero(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] == '"' || raw[0] == '{' || raw[0] == '[' || raw[0] == 'n' || raw[0] == 't' || raw[0] == 'f' {
		return false
	}
	for _, digit := range string(raw) {
		if digit == 'e' || digit == 'E' {
			break
		}
		if digit >= '1' && digit <= '9' {
			return true
		}
	}
	return false
}

func grokTurnUsage(update map[string]json.RawMessage) chat.Usage {
	u := grokObject(update["usage"])
	in, _ := grokInt(u["inputTokens"])
	read, _ := grokInt(u["cachedReadTokens"])
	write, _ := grokInt(u["cacheCreationTokens"])
	out, _ := grokInt(u["outputTokens"])
	reasoning, _ := grokInt(u["reasoningTokens"])
	calls, _ := grokInt(u["modelCalls"])
	return chat.Usage{In: in - read - write, CacheWrite: write, CacheRead: read, Out: out, Reasoning: reasoning, Calls: calls}
}

func grokAdd(a, b chat.Usage) chat.Usage {
	return chat.Usage{In: a.In + b.In, CacheWrite: a.CacheWrite + b.CacheWrite, CacheRead: a.CacheRead + b.CacheRead, Out: a.Out + b.Out, Reasoning: a.Reasoning + b.Reasoning, Calls: a.Calls + b.Calls}
}

func grokSub(a, b chat.Usage) chat.Usage {
	return chat.Usage{In: a.In - b.In, CacheWrite: a.CacheWrite - b.CacheWrite, CacheRead: a.CacheRead - b.CacheRead, Out: a.Out - b.Out, Reasoning: a.Reasoning - b.Reasoning, Calls: a.Calls - b.Calls}
}

var _ chat.Decoder = (*grokDecoder)(nil)
