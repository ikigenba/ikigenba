// Package codex reads content-free turns from Codex rollout files.
package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/klauspost/compress/zstd"
)

const maxCount int64 = 1_000_000_000_000

var momentForm = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

type object map[string]json.RawMessage

// Read reads the live and archived rollout sources below root without changing them.
func Read(root string) session.Found {
	var result session.Found
	seen := make(map[string]bool)
	for _, name := range []string{"sessions", "archived_sessions"} {
		base := filepath.Join(root, name)
		var paths []string
		_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil || (name == "sessions" && len(strings.Split(rel, string(filepath.Separator))) != 4) {
				return nil
			}
			if !strings.HasPrefix(entry.Name(), "rollout-") || (!strings.HasSuffix(entry.Name(), ".jsonl") && !strings.HasSuffix(entry.Name(), ".jsonl.zst")) {
				return nil
			}
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() {
				paths = append(paths, path)
			}
			return nil
		})
		sort.Strings(paths)
		for _, path := range paths {
			value, err := readSource(path)
			if err != nil {
				result.Skipped++
			} else if len(value.Turns) > 0 && !seen[value.Ref.ID] {
				seen[value.Ref.ID] = true
				result.Sessions = append(result.Sessions, value)
			}
		}
	}
	sort.Slice(result.Sessions, func(i, j int) bool { return result.Sessions[i].Ref.ID < result.Sessions[j].Ref.ID })
	return result
}

func readSource(path string) (session.Session, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return session.Session{}, err
	}
	if strings.HasSuffix(path, ".zst") {
		if len(data) == 0 {
			return session.Session{}, errors.New("empty zstd stream")
		}
		decoder, decodeErr := zstd.NewReader(bytes.NewReader(data))
		if decodeErr != nil {
			return session.Session{}, decodeErr
		}
		data, err = io.ReadAll(decoder)
		decoder.Close()
		if err != nil {
			return session.Session{}, err
		}
	}
	return parse(data)
}

func parse(data []byte) (session.Session, error) {
	var records []object
	var meta object
	for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
		if len(line) == 0 || line[len(line)-1] != '\n' || len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record object
		if err := json.Unmarshal(line, &record); err != nil || record == nil {
			return session.Session{}, errors.New("invalid JSON object line")
		}
		records = append(records, record)
		if meta == nil && text(record["type"]) == "session_meta" {
			meta = nested(record["payload"])
			if meta == nil {
				meta = object{}
			}
		}
	}
	id := text(meta["id"])
	value := session.Session{Ref: session.Ref{Tool: session.ToolCodex, ID: id}, Directory: text(meta["cwd"]), Headless: text(meta["originator"]) == "codex_exec"}
	if text(meta["thread_source"]) == "subagent" {
		parent := text(meta["parent_thread_id"])
		if parent == "" {
			parent = text(nested(nested(nested(meta["source"])["subagent"])["thread_spawn"])["parent_thread_id"])
		}
		if parent != "" && parent != id {
			value.Parent = &session.Ref{Tool: session.ToolCodex, ID: parent}
		}
	}
	threshold, hasThreshold := ordinal(meta["subagent_history_start_ordinal"])
	type candidate struct {
		turn     session.Turn
		usage    object
		position int
		usable   bool
	}
	var candidates []candidate
	responses := make(map[string]int)
	model := ""
	for position, record := range records {
		if positionOrdinal, ok := ordinal(record["ordinal"]); hasThreshold && ok && positionOrdinal.Cmp(threshold) < 0 {
			continue
		}
		payload := nested(record["payload"])
		kind := text(record["type"])
		if kind == "turn_context" {
			model = text(payload["model"])
			continue
		}
		moment, usable := timestamp(record["timestamp"])
		current := candidate{turn: session.Turn{Moment: moment}, position: position, usable: usable}
		switch {
		case kind == "token_usage_record":
			var thread string
			if json.Unmarshal(payload["thread_id"], &thread) == nil && len(payload["thread_id"]) > 0 && string(payload["thread_id"]) != "null" && thread != id {
				continue
			}
			current.turn.Whose, current.turn.Model = session.WhoseAgent, model
			current.usage = nested(payload["usage"])
			response := text(payload["response_id"])
			if response != "" {
				if previous, ok := responses[response]; ok {
					old := &candidates[previous]
					old.turn.Model, old.usage = model, current.usage
					if !old.usable && usable {
						old.turn.Moment, old.position, old.usable = moment, position, true
					}
					continue
				}
				responses[response] = len(candidates)
			}
		case kind == "event_msg" && text(payload["type"]) == "item_completed" && text(nested(payload["item"])["type"]) == "UserMessage":
			current.turn.Whose = session.WhosePerson
		case kind == "response_item" && (text(payload["type"]) == "function_call_output" || text(payload["type"]) == "custom_tool_call_output"):
			current.turn.Whose = session.WhoseTool
		default:
			continue
		}
		candidates = append(candidates, current)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].position < candidates[j].position })
	type key struct {
		moment time.Time
		whose  session.Whose
	}
	merged := make(map[key]int)
	for _, candidate := range candidates {
		if !candidate.usable {
			continue
		}
		turn := candidate.turn
		if turn.Whose == session.WhoseAgent {
			var err error
			turn.Tokens, err = counts(candidate.usage)
			if err != nil {
				return session.Session{}, err
			}
		}
		identity := key{turn.Moment, turn.Whose}
		if previous, ok := merged[identity]; ok {
			old := &value.Turns[previous]
			old.Model = turn.Model
			old.Tokens.Input += turn.Tokens.Input
			old.Tokens.Cached += turn.Tokens.Cached
			old.Tokens.Output += turn.Tokens.Output
			old.Tokens.Reasoning += turn.Tokens.Reasoning
			if old.Tokens.Input > maxCount || old.Tokens.Cached > maxCount || old.Tokens.Output > maxCount || old.Tokens.Reasoning > maxCount {
				return session.Session{}, errors.New("merged token count out of range")
			}
		} else {
			merged[identity] = len(value.Turns)
			value.Turns = append(value.Turns, turn)
		}
	}
	if len(value.Turns) == 0 {
		return session.Session{}, nil
	}
	if id == "" || !filepath.IsAbs(value.Directory) {
		return session.Session{}, errors.New("missing session identity or absolute directory")
	}
	value.Directory = filepath.Clean(value.Directory)
	sort.SliceStable(value.Turns, func(i, j int) bool { return value.Turns[i].Moment.Before(value.Turns[j].Moment) })
	return value, nil
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

func ordinal(raw json.RawMessage) (*big.Int, bool) {
	return new(big.Int).SetString(string(raw), 10)
}

func timestamp(raw json.RawMessage) (time.Time, bool) {
	value := text(raw)
	if !momentForm.MatchString(value) {
		return time.Time{}, false
	}
	moment, err := time.Parse(time.RFC3339Nano, value)
	moment = moment.UTC().Truncate(time.Millisecond)
	return moment, err == nil && !moment.Before(time.Unix(0, 0))
}

func counts(usage object) (session.Tokens, error) {
	var result session.Tokens
	for key, field := range map[string]*int64{"input_tokens": &result.Input, "cached_input_tokens": &result.Cached, "output_tokens": &result.Output, "reasoning_output_tokens": &result.Reasoning} {
		raw, present := usage[key]
		if !present {
			continue
		}
		value, valid := integer(raw)
		if !valid || value < 0 || value > maxCount {
			return session.Tokens{}, errors.New("invalid token count")
		}
		*field = value
	}
	return result, nil
}
