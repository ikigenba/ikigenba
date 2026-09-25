package claude

import (
	"encoding/json"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// Chat reads the named Claude agent's transcript.
func Chat(root fs.FS, home, sessionID, agentID string) (*chat.Transcript, []chat.Entry, error) {
	projects := path.Join("/", home, ".claude", "projects")
	registry := path.Join("/", home, ".claude", "sessions")
	name := strings.TrimPrefix(projects, "/")
	if _, err := fs.Stat(root, name); err != nil {
		return nil, nil, treeDirectoryError(projects, err)
	}
	entries, err := fs.ReadDir(root, name)
	if err != nil {
		return nil, nil, treeDirectoryError(projects, err)
	}
	if sessionID == "" || sessionID == "." || sessionID == ".." || strings.Contains(sessionID, "/") {
		return nil, nil, tree.ErrNotFound
	}
	project := treeProject(root, projects, sessionID, entries)
	_, liveState := treeRegistration(root, registry, sessionID)
	if project == "" && liveState != 1 {
		return nil, nil, tree.ErrNotFound
	}
	transcriptPath := ""
	if agentID == sessionID {
		if project != "" {
			transcriptPath = path.Join(project, sessionID+".jsonl")
		}
	} else {
		if project == "" {
			return nil, nil, chat.ErrAgentNotFound
		}
		subdir := path.Join(project, sessionID, "subagents")
		subs, err := fs.ReadDir(root, strings.TrimPrefix(subdir, "/"))
		if err != nil {
			return nil, nil, chat.ErrAgentNotFound
		}
		found := false
		for _, sub := range subs {
			if agentID != "" && (sub.Name() == "agent-"+agentID+".jsonl" || sub.Name() == "agent-"+agentID+".meta.json") {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, chat.ErrAgentNotFound
		}
		transcriptPath = path.Join(subdir, "agent-"+agentID+".jsonl")
	}
	tr := chat.NewTranscript(transcriptPath, chat.Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}, func() chat.Decoder {
		return &claudeChatDecoder{rootAgent: agentID == sessionID, maxima: make(map[string]chat.Usage)}
	})
	got, _, err := tr.Read(root)
	if err != nil {
		return nil, nil, err
	}
	return tr, got, nil
}

type claudeChatDecoder struct {
	rootAgent bool
	first     bool
	fork      bool
	directive bool
	maxima    map[string]chat.Usage
}

func (d *claudeChatDecoder) Decode(_ fs.FS, line []byte) ([]chat.Entry, chat.Usage) {
	var rec treeRecord
	if json.Unmarshal(line, &rec) != nil || rec == nil {
		return nil, chat.Usage{}
	}
	typ := treeField(rec, "type")
	if !d.first {
		d.first = true
		d.fork = typ == "fork-context-ref"
	}
	message := treeObject(rec["message"])
	blocks := treeArray(message["content"])
	isDirective := false
	if d.fork && !d.directive && typ == "user" {
		for _, raw := range blocks {
			block := treeObject(raw)
			if treeField(block, "type") == "text" && strings.HasPrefix(treeField(block, "text"), "<fork-boilerplate>") {
				isDirective = true
				break
			}
		}
	}
	if d.fork && !d.directive && !isDirective {
		return nil, chat.Usage{}
	}
	if isDirective {
		d.directive = true
	}
	stamp, hasStamp := treeTimestamp(rec)
	entry := func(kind chat.Kind, tool, value string) chat.Entry {
		return chat.Entry{Time: stamp, HasTime: hasStamp, Kind: kind, Tool: tool, Text: value}
	}
	var result []chat.Entry
	switch typ {
	case "user":
		if content, ok := stringField(message["content"]); ok {
			origin := treeObject(rec["origin"])
			kind := chat.Kind("")
			switch treeField(origin, "kind") {
			case "human":
				kind = chat.KindUser
			case "task-notification", "coordinator", "peer":
				kind = chat.KindAgent
			default:
				_, originPresent := rec["origin"]
				if !originPresent && !jsonBool(rec["isMeta"]) && !strings.HasPrefix(content, "<local-command-stdout>") && !strings.HasPrefix(content, "<local-command-stderr>") {
					kind = chat.KindAgent
					if d.rootAgent {
						kind = chat.KindUser
					}
				}
			}
			if kind != "" {
				result = append(result, entry(kind, "", promptText(content)))
			}
		} else {
			for _, raw := range blocks {
				block := treeObject(raw)
				switch treeField(block, "type") {
				case "tool_result":
					if !isDirective {
						kind := chat.KindResultOK
						if jsonBool(block["is_error"]) {
							kind = chat.KindResultError
						}
						result = append(result, entry(kind, "", ""))
					}
				case "text":
					if value, ok := stringField(block["text"]); isDirective && ok && strings.HasPrefix(value, "<fork-boilerplate>") {
						result = append(result, entry(chat.KindAgent, "", value))
					}
				}
			}
		}
	case "attachment":
		attachment := treeObject(rec["attachment"])
		if treeField(attachment, "type") == "queued_command" {
			if prompt, ok := stringField(attachment["prompt"]); ok {
				origin := treeField(treeObject(attachment["origin"]), "kind")
				kind := chat.Kind("")
				if origin == "human" {
					kind = chat.KindUser
				} else if treeField(attachment, "commandMode") == "task-notification" || origin == "coordinator" || origin == "peer" {
					kind = chat.KindAgent
				}
				if kind != "" {
					result = append(result, entry(kind, "", prompt))
				}
			}
		}
	case "assistant":
		for _, raw := range blocks {
			block := treeObject(raw)
			switch treeField(block, "type") {
			case "text":
				if value, ok := stringField(block["text"]); ok {
					result = append(result, entry(chat.KindAssistant, "", value))
				}
			case "thinking":
				if value, ok := stringField(block["thinking"]); ok {
					result = append(result, entry(chat.KindReasoning, "", value))
				}
			case "tool_use":
				if name, ok := stringField(block["name"]); ok {
					result = append(result, entry(chat.KindTool, name, string(block["input"])))
				}
			}
		}
	}
	if typ != "assistant" {
		return result, chat.Usage{}
	}
	id, ok := stringField(message["id"])
	usage := treeObject(message["usage"])
	if !ok || usage == nil || treeField(message, "model") == "<synthetic>" {
		return result, chat.Usage{}
	}
	counts := chat.Usage{In: count(usage["input_tokens"]), CacheWrite: count(usage["cache_creation_input_tokens"]), CacheRead: count(usage["cache_read_input_tokens"]), Out: count(usage["output_tokens"]), Reasoning: count(treeObject(usage["output_tokens_details"])["thinking_tokens"])}
	before, exists := d.maxima[id]
	after := before
	if counts.In > after.In {
		after.In = counts.In
	}
	if counts.CacheWrite > after.CacheWrite {
		after.CacheWrite = counts.CacheWrite
	}
	if counts.CacheRead > after.CacheRead {
		after.CacheRead = counts.CacheRead
	}
	if counts.Out > after.Out {
		after.Out = counts.Out
	}
	if counts.Reasoning > after.Reasoning {
		after.Reasoning = counts.Reasoning
	}
	d.maxima[id] = after
	delta := chat.Usage{In: after.In - before.In, CacheWrite: after.CacheWrite - before.CacheWrite, CacheRead: after.CacheRead - before.CacheRead, Out: after.Out - before.Out, Reasoning: after.Reasoning - before.Reasoning}
	if !exists {
		delta.Calls = 1
	}
	return result, delta
}

func promptText(value string) string {
	if !strings.HasPrefix(value, "<command-name>") && !strings.HasPrefix(value, "<command-message>") {
		return value
	}
	name, ok := tagAfter(value, 0, "command-name")
	if !ok {
		return value
	}
	args, _ := tagAfter(value, 0, "command-args")
	if args != "" {
		return name + " " + args
	}
	return name
}

func count(raw json.RawMessage) int64 {
	s := string(raw)
	if s == "" || s[0] == '"' || s == "null" {
		return 0
	}
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0
	}
	exponent := int64(0)
	if pos := strings.IndexAny(s, "eE"); pos >= 0 {
		var err error
		exponent, err = strconv.ParseInt(s[pos+1:], 10, 64)
		if err != nil {
			return 0
		}
		s = s[:pos]
	}
	fractionDigits := 0
	if pos := strings.IndexByte(s, '.'); pos >= 0 {
		fractionDigits = len(s) - pos - 1
		s = s[:pos] + s[pos+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return 0
	}
	if negative || exponent > int64(len(raw))+19 || exponent < -int64(len(raw))-19 {
		return 0
	}
	shift := int(exponent) - fractionDigits
	if shift >= 0 {
		if len(s)+shift > 19 {
			return 0
		}
		s += strings.Repeat("0", shift)
	} else {
		cut := -shift
		if cut > len(s) || strings.Trim(s[len(s)-cut:], "0") != "" {
			return 0
		}
		s = s[:len(s)-cut]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func jsonBool(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}
