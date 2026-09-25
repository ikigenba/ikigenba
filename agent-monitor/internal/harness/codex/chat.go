package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// Chat reads the conversation of a Codex thread belonging to sessionID.
func Chat(root fs.FS, home, sessionID, agentID string) (*chat.Transcript, []chat.Entry, error) {
	directory := path.Join("/", home, ".codex", "sessions")
	if _, err := fs.Stat(root, name(directory)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, tree.ErrNotFound
		}
		return nil, nil, readError(directory, err)
	}
	if _, err := fs.ReadDir(root, name(directory)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, tree.ErrNotFound
		}
		return nil, nil, readError(directory, err)
	}
	if sessionID == "" || sessionID == "." || sessionID == ".." || strings.Contains(sessionID, "/") {
		return nil, nil, tree.ErrNotFound
	}
	rootPath := firstTreeRollout(root, directory, sessionID)
	liveness := rootLiveness(root, path.Join("/", home, ".codex", "thread-writer-locks", sessionID+".lock"))
	if agentID == sessionID {
		var decoder *chatDecoder
		tr := chat.NewTranscript(rootPath, codexRecorded, func() chat.Decoder {
			decoder = &chatDecoder{ownID: sessionID}
			return decoder
		})
		entries, _, err := tr.Read(root)
		if decoder != nil && decoder.subagent || rootPath == "" && liveness != live {
			return nil, nil, tree.ErrNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		return tr, entries, nil
	}
	var rootRecords []map[string]json.RawMessage
	if rootPath != "" {
		var log session.Log
		lines, _, err := log.Read(root, name(rootPath))
		if err == nil {
			rootRecords = recordsOf(lines)
		}
	}
	if len(rootRecords) > 0 && parentID(firstMeta(rootRecords)) != "" || rootPath == "" && liveness != live {
		return nil, nil, tree.ErrNotFound
	}
	queue := []string{sessionID}
	seen := map[string]bool{sessionID: true}
	var found bool
	for len(queue) > 0 && !found {
		id := queue[0]
		queue = queue[1:]
		records := rootRecords
		if id != sessionID {
			file := firstTreeRollout(root, directory, id)
			if file == "" {
				continue
			}
			var log session.Log
			lines, _, err := log.Read(root, name(file))
			if err != nil {
				continue
			}
			records = recordsOf(lines)
		}
		for _, item := range startedItems(records, id) {
			if item.id == sessionID {
				continue
			}
			if item.id == agentID {
				found = true
				break
			}
			if !seen[item.id] {
				seen[item.id] = true
				queue = append(queue, item.id)
			}
		}
	}
	if !found {
		return nil, nil, chat.ErrAgentNotFound
	}
	file := firstTreeRollout(root, directory, agentID)
	tr := chat.NewTranscript(file, codexRecorded, func() chat.Decoder { return &chatDecoder{ownID: agentID} })
	entries, _, err := tr.Read(root)
	if err != nil {
		return nil, nil, err
	}
	return tr, entries, nil
}

var codexRecorded = chat.Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}

type chatDecoder struct {
	ownID    string
	count    int
	copying  bool
	subagent bool
}

func (d *chatDecoder) Decode(_ fs.FS, raw []byte) ([]chat.Entry, chat.Usage) {
	var record map[string]json.RawMessage
	if json.Unmarshal(raw, &record) != nil {
		return nil, chat.Usage{}
	}
	d.count++
	if d.count == 1 {
		d.subagent = parentID(firstMeta([]map[string]json.RawMessage{record})) != ""
	}
	typ := stringField(record, "type")
	if d.count == 2 && typ == "session_meta" {
		d.copying = true
	}
	if d.copying {
		payload := objectField(record, "payload")
		if d.count > 2 && typ == "event_msg" && stringField(payload, "type") == "thread_settings_applied" && stringField(payload, "thread_id") == d.ownID {
			d.copying = false
		} else {
			return nil, chat.Usage{}
		}
	}
	if typ == "token_usage_record" {
		payload := objectField(record, "payload")
		if stringField(payload, "thread_id") != d.ownID {
			return nil, chat.Usage{}
		}
		usage := objectField(payload, "usage")
		if usage == nil {
			return nil, chat.Usage{}
		}
		i := intField(usage, "input_tokens")
		c := intField(usage, "cached_input_tokens")
		w := intField(usage, "cache_write_input_tokens")
		return nil, chat.Usage{In: i - c - w, CacheWrite: w, CacheRead: c, Out: intField(usage, "output_tokens"), Reasoning: intField(usage, "reasoning_output_tokens"), Calls: 1}
	}
	if typ != "response_item" {
		return nil, chat.Usage{}
	}
	payload := objectField(record, "payload")
	stamp, hasTime := recordTimestamp(record, "timestamp")
	entry := chat.Entry{Time: stamp, HasTime: hasTime}
	switch stringField(payload, "type") {
	case "message":
		switch stringField(payload, "role") {
		case "user":
			entry.Kind = chat.KindUser
			entry.Text = userText(payload)
		case "assistant":
			entry.Kind = chat.KindAssistant
			entry.Text = contentText(payload["content"], "output_text")
		}
	case "agent_message":
		entry.Kind = chat.KindAgent
		entry.Text = agentText(payload["content"])
	case "reasoning":
		var summary []map[string]json.RawMessage
		_ = json.Unmarshal(payload["summary"], &summary)
		var entries []chat.Entry
		for _, part := range summary {
			if stringField(part, "type") == "summary_text" {
				if s := stringField(part, "text"); s != "" {
					entries = append(entries, chat.Entry{Time: stamp, HasTime: hasTime, Kind: chat.KindReasoning, Text: s})
				}
			}
		}
		return entries, chat.Usage{}
	case "function_call", "custom_tool_call":
		entry.Kind = chat.KindTool
		entry.Tool = stringField(payload, "name")
		field := "arguments"
		if stringField(payload, "type") == "custom_tool_call" {
			field = "input"
		} else if collaborationTool(entry.Tool) {
			entry.Text = redactMessage(stringField(payload, field))
		}
		if entry.Text == "" {
			entry.Text = stringField(payload, field)
		}
	case "function_call_output", "custom_tool_call_output":
		entry.Kind = chat.KindResultOK
		if failedOutput(payload["output"]) {
			entry.Kind = chat.KindResultError
		}
	}
	if entry.Kind == "" || entry.Text == "" && (entry.Kind == chat.KindUser || entry.Kind == chat.KindAssistant || entry.Kind == chat.KindAgent) {
		return nil, chat.Usage{}
	}
	return []chat.Entry{entry}, chat.Usage{}
}

func intField(object map[string]json.RawMessage, key string) int64 {
	var n int64
	_ = json.Unmarshal(object[key], &n)
	return n
}

func contentText(raw json.RawMessage, kind string) string {
	var content []map[string]json.RawMessage
	_ = json.Unmarshal(raw, &content)
	var text strings.Builder
	for _, item := range content {
		if stringField(item, "type") == kind {
			text.WriteString(stringField(item, "text"))
		}
	}
	return text.String()
}

func userText(payload map[string]json.RawMessage) string {
	var content []map[string]json.RawMessage
	var kinds []json.RawMessage
	_ = json.Unmarshal(payload["content"], &content)
	_ = json.Unmarshal(objectField(payload, "internal_chat_message_metadata_passthrough")["content_item_kinds"], &kinds)
	var text strings.Builder
	for i, item := range content {
		if i < len(kinds) && stringField(item, "type") == "input_text" {
			var kind string
			if json.Unmarshal(kinds[i], &kind) == nil && kind == "user.text" {
				text.WriteString(stringField(item, "text"))
			}
		}
	}
	return text.String()
}

func agentText(raw json.RawMessage) string {
	var content []map[string]json.RawMessage
	_ = json.Unmarshal(raw, &content)
	var text strings.Builder
	var encrypted bool
	for _, item := range content {
		switch stringField(item, "type") {
		case "input_text":
			text.WriteString(stringField(item, "text"))
		case "encrypted_content":
			encrypted = true
		}
	}
	s := text.String()
	if encrypted && strings.HasSuffix(s, "Payload:\n") {
		at := len(s) - len("Payload:\n")
		if at == 0 || s[at-1] == '\n' {
			s = s[:at]
			s = strings.TrimSuffix(s, "\n")
		}
	}
	return s
}

func collaborationTool(name string) bool {
	switch name {
	case "spawn_agent", "send_message", "followup_task", "wait_agent", "list_agents", "interrupt_agent":
		return true
	}
	return false
}

func redactMessage(raw string) string {
	if !json.Valid([]byte(raw)) {
		return ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || fields == nil {
		return ""
	}
	var out bytes.Buffer
	out.WriteByte('{')
	first := true
	// Decoder.Token preserves the input member order, including duplicate keys.
	dec := json.NewDecoder(strings.NewReader(raw))
	_, _ = dec.Token()
	for dec.More() {
		keyStart := dec.InputOffset()
		token, err := dec.Token()
		if err != nil {
			return ""
		}
		key := token.(string)
		keyEnd := dec.InputOffset()
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return ""
		}
		if key == "message" {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		keyBytes := []byte(raw[keyStart:keyEnd])
		out.Write(keyBytes[bytes.IndexByte(keyBytes, '"'):])
		out.WriteByte(':')
		var compact bytes.Buffer
		_ = json.Compact(&compact, value)
		out.Write(compact.Bytes())
	}
	out.WriteByte('}')
	return out.String()
}

func failedOutput(raw json.RawMessage) bool {
	var texts []string
	var single string
	if json.Unmarshal(raw, &single) == nil {
		texts = append(texts, single)
	} else {
		var parts []map[string]json.RawMessage
		if json.Unmarshal(raw, &parts) == nil {
			for _, part := range parts {
				if s, ok := jsonString(part, "text"); ok {
					texts = append(texts, s)
				}
			}
		}
	}
	if len(texts) > 0 {
		for _, prefix := range []string{"Script failed", "collab spawn failed:", "collab tool failed:", "failed to parse function arguments:"} {
			if strings.HasPrefix(texts[0], prefix) {
				return true
			}
		}
	}
	for _, s := range texts {
		if !json.Valid([]byte(s)) {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(s), &obj) != nil || obj == nil {
			continue
		}
		number := bytes.TrimSpace(obj["exit_code"])
		if len(number) == 0 || number[0] != '-' && (number[0] < '0' || number[0] > '9') {
			continue
		}
		for _, digit := range number {
			if digit == 'e' || digit == 'E' {
				break
			}
			if digit >= '1' && digit <= '9' {
				return true
			}
		}
	}
	return false
}
