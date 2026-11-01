// Package claude reads content-free turns from Claude Code session files.
package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/turns/internal/session"
)

const maxTokens int64 = 1000000000000

type object map[string]json.RawMessage

func obj(raw json.RawMessage) object { var v object; _ = json.Unmarshal(raw, &v); return v }
func str(raw json.RawMessage) string { var v string; _ = json.Unmarshal(raw, &v); return v }
func truth(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("true")) }
func array(raw json.RawMessage) []json.RawMessage {
	var v []json.RawMessage
	_ = json.Unmarshal(raw, &v)
	return v
}
func moment(raw json.RawMessage) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, str(raw))
	t = t.UTC().Truncate(time.Millisecond)
	return t, err == nil && !t.Before(time.Unix(0, 0))
}

type source struct{ path, relative, id, parent string }

func regular(entry os.DirEntry) bool { return entry.Type().IsRegular() }
func sources(root string) []source {
	base := filepath.Join(root, "projects")
	dirs, _ := os.ReadDir(base)
	var out []source
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		project := filepath.Join(base, dir.Name())
		entries, _ := os.ReadDir(project)
		for _, entry := range entries {
			if regular(entry) && strings.HasSuffix(entry.Name(), ".jsonl") {
				out = append(out, source{path: filepath.Join(project, entry.Name()), relative: filepath.Join(dir.Name(), entry.Name()), id: strings.TrimSuffix(entry.Name(), ".jsonl")})
			}
			if !entry.IsDir() {
				continue
			}
			sub := filepath.Join(project, entry.Name(), "subagents")
			appendAgents := func(path string) {
				agents, _ := os.ReadDir(path)
				for _, agent := range agents {
					name := agent.Name()
					if regular(agent) && strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") && len(name) > len("agent-.jsonl") {
						file := filepath.Join(path, name)
						relative, _ := filepath.Rel(base, file)
						out = append(out, source{file, relative, entry.Name() + "/" + strings.TrimSuffix(name, ".jsonl"), entry.Name()})
					}
				}
			}
			appendAgents(sub)
			workflows, _ := os.ReadDir(filepath.Join(sub, "workflows"))
			for _, workflow := range workflows {
				if workflow.IsDir() {
					appendAgents(filepath.Join(sub, "workflows", workflow.Name()))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.Count(out[i].relative, string(filepath.Separator)), strings.Count(out[j].relative, string(filepath.Separator))
		if a != b {
			return a < b
		}
		return out[i].relative < out[j].relative
	})
	return out
}

type candidate struct {
	turn  session.Turn
	valid bool
	usage object
}

func count(usage object, key string) (int64, bool) {
	raw, present := usage[key]
	if !present {
		return 0, true
	}
	var n int64
	err := json.Unmarshal(raw, &n)
	return n, err == nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && n >= 0 && n <= maxTokens
}
func tokens(usage object) (session.Tokens, bool) {
	in, a := count(usage, "input_tokens")
	creation, b := count(usage, "cache_creation_input_tokens")
	cached, c := count(usage, "cache_read_input_tokens")
	output, d := count(usage, "output_tokens")
	reasoning, e := count(usage, "thinking_tokens")
	if _, present := usage["thinking_tokens"]; !present {
		reasoning, e = count(obj(usage["output_tokens_details"]), "thinking_tokens")
	}
	return session.Tokens{Input: in + creation, Cached: cached, Output: output, Reasoning: reasoning}, a && b && c && d && e && in+creation <= maxTokens
}
func classify(line object, message object) session.Whose {
	switch str(line["type"]) {
	case "assistant":
		return session.WhoseAgent
	case "user":
		switch str(obj(line["origin"])["kind"]) {
		case "task-notification", "peer", "coordinator":
			return session.WhoseTool
		}
		for _, raw := range array(message["content"]) {
			if str(obj(raw)["type"]) == "tool_result" {
				return session.WhoseTool
			}
		}
		var content string
		if json.Unmarshal(message["content"], &content) == nil && !bytes.Equal(bytes.TrimSpace(message["content"]), []byte("null")) && !truth(line["isMeta"]) {
			return session.WhosePerson
		}
	}
	return ""
}
func between(text, start, end string) (string, bool) {
	_, after, ok := strings.Cut(text, start)
	if !ok {
		return "", false
	}
	v, _, ok := strings.Cut(after, end)
	if !ok {
		return "", false
	}
	return v, true
}
func argument(arg string) string {
	arg, _, _ = strings.Cut(arg, "\n")
	if len(arg) > 256 {
		end := 256
		for end > 0 && !utf8.RuneStart(arg[end]) {
			end--
		}
		arg = arg[:end]
	}
	return arg
}
func command(content string) *session.Command {
	for {
		_, after, ok := strings.Cut(content, "<command-name>")
		if !ok {
			return nil
		}
		name, rest, ok := strings.Cut(after, "</command-name>")
		if !ok {
			return nil
		}
		content = rest
		trimmed := strings.TrimPrefix(name, "/")
		if trimmed == "" || trimmed == "clear" || trimmed == "exit" {
			continue
		}
		arg, _ := between(rest, "<command-args>", "</command-args>")
		return &session.Command{Name: name, Argument: argument(arg)}
	}
}
func skillCommand(content json.RawMessage) *session.Command {
	for _, raw := range array(content) {
		item := obj(raw)
		if str(item["type"]) != "tool_use" || str(item["name"]) != "Skill" {
			continue
		}
		input := obj(item["input"])
		if name := str(input["skill"]); name != "" {
			return &session.Command{Name: "/" + name, Argument: argument(str(input["args"]))}
		}
	}
	return nil
}

func texts(content json.RawMessage) []string {
	var out []string
	var text string
	if json.Unmarshal(content, &text) == nil {
		return []string{text}
	}
	for _, raw := range array(content) {
		item := obj(raw)
		if json.Unmarshal(item["text"], &text) == nil {
			out = append(out, text)
		}
		if json.Unmarshal(item["content"], &text) == nil {
			out = append(out, text)
		} else {
			for _, nested := range array(item["content"]) {
				if json.Unmarshal(obj(nested)["text"], &text) == nil {
					out = append(out, text)
				}
			}
		}
	}
	return out
}
func boundary(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-'
}
func occurs(text, path string) bool {
	for start := 0; start <= len(text); {
		index := strings.Index(text[start:], path)
		if index < 0 {
			return false
		}
		index += start
		end := index + len(path)
		if (index == 0 || !boundary(text[index-1])) && (end == len(text) || !boundary(text[end])) {
			return true
		}
		start = index + 1
	}
	return false
}

// matcher finds every job path in a text in one pass per parent directory.
// A path whose final segment is wholly in the boundary alphabet is grouped
// under its parent (everything through the last '/'); any other path falls
// back to a direct boundary search.
type matcher struct {
	parents map[string]map[string][]string
	order   []string
	direct  []struct{ form, job string }
}

func newMatcher(jobs []string) *matcher {
	m := &matcher{parents: make(map[string]map[string][]string)}
	seen := make(map[string]bool)
	add := func(form, job string) {
		slash := strings.LastIndexByte(form, '/')
		name := form[slash+1:]
		simple := slash >= 0 && name != ""
		for i := 0; simple && i < len(name); i++ {
			simple = boundary(name[i])
		}
		if !simple {
			m.direct = append(m.direct, struct{ form, job string }{form, job})
			return
		}
		parent := form[:slash+1]
		names, ok := m.parents[parent]
		if !ok {
			names = make(map[string][]string)
			m.parents[parent] = names
			m.order = append(m.order, parent)
		}
		names[name] = append(names[name], job)
	}
	for _, job := range jobs {
		if job == "" || seen[job] {
			continue
		}
		seen[job] = true
		add(job, job)
		if index := strings.Index(job, "/.local/state/dispatch/jobs/"); index >= 0 {
			add("~"+job[index:], job)
		}
	}
	return m
}

func (m *matcher) find(text string, hit func(job string)) {
	for _, parent := range m.order {
		names := m.parents[parent]
		for start := 0; start < len(text); {
			index := strings.Index(text[start:], parent)
			if index < 0 {
				break
			}
			index += start
			start = index + 1
			if index > 0 && boundary(text[index-1]) {
				continue
			}
			from := index + len(parent)
			end := from
			for end < len(text) && boundary(text[end]) {
				end++
			}
			for _, job := range names[text[from:end]] {
				hit(job)
			}
		}
	}
	for _, d := range m.direct {
		if occurs(text, d.form) {
			hit(d.job)
		}
	}
}

func read(src source, jobs *matcher) (session.Session, []session.Mention, bool) {
	s := session.Session{Ref: session.Ref{Tool: session.ToolClaude, ID: src.id}, Headless: src.parent != ""}
	if src.parent != "" && src.parent != src.id {
		s.Parent = &session.Ref{Tool: session.ToolClaude, ID: src.parent}
	}
	file, err := os.Open(src.path)
	if err != nil {
		return s, nil, false
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 1<<20)
	var candidates []*candidate
	agents := make(map[string]*candidate)
	mentions := make(map[string]time.Time)
	entrypointSeen := false
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			if readErr != io.EOF {
				return s, nil, false
			}
			break
		}
		lineBytes = bytes.TrimSpace(lineBytes)
		if len(lineBytes) == 0 {
			continue
		}
		var line object
		if json.Unmarshal(lineBytes, &line) != nil || line == nil {
			return s, nil, false
		}
		if s.Directory == "" {
			s.Directory = str(line["cwd"])
		}
		if ep := str(line["entrypoint"]); !entrypointSeen && ep != "" {
			entrypointSeen = true
			if src.parent == "" {
				s.Headless = ep == "sdk-cli"
			}
		}
		message := obj(line["message"])
		whose := classify(line, message)
		if whose == "" {
			continue
		}
		t, valid := moment(line["timestamp"])
		if whose == session.WhoseAgent {
			id := str(message["id"])
			if id == "" {
				continue
			}
			c, exists := agents[id]
			if !exists {
				c = &candidate{turn: session.Turn{Moment: t, Whose: whose}, valid: valid}
				agents[id] = c
				candidates = append(candidates, c)
			}
			c.turn.Model = str(message["model"])
			c.usage = obj(message["usage"])
			if c.valid && s.Command == nil {
				s.Command = skillCommand(message["content"])
			}
			continue
		}
		if !valid {
			continue
		}
		candidates = append(candidates, &candidate{turn: session.Turn{Moment: t, Whose: whose}, valid: true})
		if whose == session.WhosePerson && s.Command == nil {
			s.Command = command(str(message["content"]))
		}
		if whose != session.WhoseTool {
			continue
		}
		record := func(job string) {
			if old, present := mentions[job]; !present || t.Before(old) {
				mentions[job] = t
			}
		}
		for _, text := range texts(message["content"]) {
			jobs.find(text, record)
		}
	}
	type key struct {
		moment time.Time
		whose  session.Whose
	}
	merged := make(map[key]int)
	for _, c := range candidates {
		if !c.valid {
			continue
		}
		if c.turn.Whose == session.WhoseAgent {
			var ok bool
			c.turn.Tokens, ok = tokens(c.usage)
			if !ok {
				return s, nil, false
			}
		}
		k := key{c.turn.Moment, c.turn.Whose}
		if index, exists := merged[k]; exists {
			old := &s.Turns[index]
			old.Tokens.Input += c.turn.Tokens.Input
			old.Tokens.Cached += c.turn.Tokens.Cached
			old.Tokens.Output += c.turn.Tokens.Output
			old.Tokens.Reasoning += c.turn.Tokens.Reasoning
			if old.Tokens.Input > maxTokens || old.Tokens.Cached > maxTokens || old.Tokens.Output > maxTokens || old.Tokens.Reasoning > maxTokens {
				return s, nil, false
			}
			old.Model = c.turn.Model
		} else {
			merged[k] = len(s.Turns)
			s.Turns = append(s.Turns, c.turn)
		}
	}
	if len(s.Turns) == 0 {
		return s, nil, true
	}
	if s.Ref.ID == "" || !filepath.IsAbs(s.Directory) {
		return s, nil, false
	}
	s.Directory = filepath.Clean(s.Directory)
	sort.SliceStable(s.Turns, func(i, j int) bool { return s.Turns[i].Moment.Before(s.Turns[j].Moment) })
	var result []session.Mention
	for path, t := range mentions {
		result = append(result, session.Mention{Path: path, By: s.Ref, Moment: t})
	}
	return s, result, true
}

// Read returns sessions and dispatch-directory mentions without changing their sources.
func Read(root string, jobs []string, since time.Time) (session.Found, []session.Mention) {
	var found session.Found
	var mentions []session.Mention
	seen := make(map[string]bool)
	match := newMatcher(jobs)
	for _, src := range sources(root) {
		if !since.IsZero() {
			info, err := os.Stat(src.path)
			if err == nil && info.ModTime().Before(since) {
				continue
			}
		}
		s, m, ok := read(src, match)
		if !ok {
			found.Skipped++
			continue
		}
		if len(s.Turns) == 0 || seen[s.Ref.ID] {
			continue
		}
		seen[s.Ref.ID] = true
		found.Sessions = append(found.Sessions, s)
		mentions = append(mentions, m...)
	}
	sort.Slice(found.Sessions, func(i, j int) bool { return found.Sessions[i].Ref.ID < found.Sessions[j].Ref.ID })
	sort.Slice(mentions, func(i, j int) bool {
		if mentions[i].Path != mentions[j].Path {
			return mentions[i].Path < mentions[j].Path
		}
		return mentions[i].By.ID < mentions[j].By.ID
	})
	return found, mentions
}
