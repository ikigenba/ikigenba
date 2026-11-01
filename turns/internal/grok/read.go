// Package grok reads content-free turns from Grok session directories.
package grok

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/session"
)

const maxCount int64 = 1_000_000_000_000

type object map[string]json.RawMessage

// Read returns sessions below root, without changing their sources.
func Read(root string) session.Found {
	var found session.Found
	base := filepath.Join(root, "sessions")
	groups, err := os.ReadDir(base)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			found.Skipped = 1
		}
		return found
	}
	var sources []string
	parents := make(map[string]string)
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		groupPath := filepath.Join(base, group.Name())
		entries, readErr := os.ReadDir(groupPath)
		if readErr != nil {
			found.Skipped++
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(groupPath, entry.Name())
			sources = append(sources, path)
			children, _ := os.ReadDir(filepath.Join(path, "subagents"))
			for _, child := range children {
				if !child.IsDir() || child.Name() == entry.Name() {
					continue
				}
				if previous, ok := parents[child.Name()]; !ok || entry.Name() < previous {
					parents[child.Name()] = entry.Name()
				}
			}
		}
	}
	sort.Strings(sources)
	seen := make(map[string]bool)
	for _, path := range sources {
		value, readErr := readSource(path, parents)
		if readErr != nil {
			found.Skipped++
		} else if len(value.Turns) > 0 && !seen[value.Ref.ID] {
			seen[value.Ref.ID] = true
			found.Sessions = append(found.Sessions, value)
		}
	}
	sort.Slice(found.Sessions, func(i, j int) bool { return found.Sessions[i].Ref.ID < found.Sessions[j].Ref.ID })
	return found
}

func readSource(path string, parents map[string]string) (session.Session, error) {
	if _, err := os.ReadDir(path); err != nil {
		return session.Session{}, err
	}
	summaryData, summaryErr := os.ReadFile(filepath.Clean(filepath.Join(path, "summary.json")))
	var summary object
	if summaryErr == nil {
		if err := json.Unmarshal(summaryData, &summary); err != nil || summary == nil {
			return session.Session{}, errors.New("invalid summary object")
		}
	} else if !errors.Is(summaryErr, os.ErrNotExist) {
		return session.Session{}, summaryErr
	}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(path, "updates.jsonl")))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && errors.Is(summaryErr, os.ErrNotExist) {
			return session.Session{}, nil
		}
		return session.Session{}, err
	}
	turns, err := parseTurns(data)
	if err != nil || len(turns) == 0 {
		return session.Session{}, err
	}
	if summaryErr != nil {
		return session.Session{}, summaryErr
	}
	info := nested(summary["info"])
	id := text(info["id"])
	if id == "" {
		id = filepath.Base(path)
	}
	directory := text(info["cwd"])
	if directory == "" {
		directory, err = url.PathUnescape(filepath.Base(filepath.Dir(path)))
		if err != nil {
			return session.Session{}, err
		}
	}
	if id == "" || !filepath.IsAbs(directory) {
		return session.Session{}, errors.New("missing identity or absolute directory")
	}
	kind := text(summary["session_kind"])
	value := session.Session{Ref: session.Ref{Tool: session.ToolGrok, ID: id}, Directory: filepath.Clean(directory), Headless: kind == "headless" || kind == "subagent" || kind == "subagent_resume", Turns: turns}
	parent := text(summary["parent_session_id"])
	if parent == "" {
		parent = parents[id]
	}
	if parent != "" && parent != id {
		value.Parent = &session.Ref{Tool: session.ToolGrok, ID: parent}
	}
	return value, nil
}

func parseTurns(data []byte) ([]session.Turn, error) {
	type key struct {
		moment time.Time
		whose  session.Whose
	}
	var turns []session.Turn
	merged := make(map[key]int)
	for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
		if len(line) == 0 || line[len(line)-1] != '\n' || len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record object
		if err := json.Unmarshal(line, &record); err != nil || record == nil {
			return nil, errors.New("invalid JSON object line")
		}
		params := nested(record["params"])
		update := nested(params["update"])
		var turn session.Turn
		switch text(update["sessionUpdate"]) {
		case "turn_completed":
			turn.Whose = session.WhoseAgent
		case "user_message_chunk":
			turn.Whose = session.WhosePerson
		case "tool_call_update":
			status := text(update["status"])
			if status != "completed" && status != "failed" {
				continue
			}
			turn.Whose = session.WhoseTool
		default:
			continue
		}
		moment, ok := timestamp(record, params)
		if !ok {
			continue
		}
		turn.Moment = moment
		if turn.Whose == session.WhoseAgent {
			usage := nested(update["usage"])
			var err error
			turn.Tokens, err = counts(usage)
			if err != nil {
				return nil, err
			}
			models := nested(usage["modelUsage"])
			if len(models) == 1 {
				for model := range models {
					turn.Model = strings.TrimSuffix(model, "-build")
				}
			}
		}
		identity := key{moment: turn.Moment, whose: turn.Whose}
		if previous, exists := merged[identity]; exists {
			old := &turns[previous]
			old.Model = turn.Model
			old.Tokens.Input += turn.Tokens.Input
			old.Tokens.Cached += turn.Tokens.Cached
			old.Tokens.Output += turn.Tokens.Output
			old.Tokens.Reasoning += turn.Tokens.Reasoning
			if old.Tokens.Input > maxCount || old.Tokens.Cached > maxCount || old.Tokens.Output > maxCount || old.Tokens.Reasoning > maxCount {
				return nil, errors.New("merged token count out of range")
			}
		} else {
			merged[identity] = len(turns)
			turns = append(turns, turn)
		}
	}
	sort.SliceStable(turns, func(i, j int) bool { return turns[i].Moment.Before(turns[j].Moment) })
	return turns, nil
}

func text(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func nested(raw json.RawMessage) object {
	var value object
	_ = json.Unmarshal(raw, &value)
	return value
}

func integer(raw json.RawMessage) (int64, bool) {
	value, err := strconv.ParseInt(string(raw), 10, 64)
	return value, err == nil
}

func timestamp(record, params object) (time.Time, bool) {
	if value, ok := integer(nested(params["_meta"])["agentTimestampMs"]); ok && value >= 0 && value <= 253402300799999 {
		return time.UnixMilli(value).UTC(), true
	}
	if value, ok := integer(record["timestamp"]); ok && value >= 0 && value <= 253402300799 {
		return time.Unix(value, 0).UTC(), true
	}
	return time.Time{}, false
}

func counts(usage object) (session.Tokens, error) {
	var result session.Tokens
	for name, target := range map[string]*int64{"inputTokens": &result.Input, "cachedReadTokens": &result.Cached, "outputTokens": &result.Output, "reasoningTokens": &result.Reasoning} {
		raw, exists := usage[name]
		if !exists {
			continue
		}
		value, ok := integer(raw)
		if !ok || value < 0 || value > maxCount {
			return session.Tokens{}, errors.New("invalid token count")
		}
		*target = value
	}
	return result, nil
}
