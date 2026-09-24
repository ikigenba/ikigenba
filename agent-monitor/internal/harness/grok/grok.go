// Package grok discovers live Grok Build CLI sessions through an injected filesystem.
package grok

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

	"github.com/ikigenba/ikigenba/agent-monitor/internal/proc"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

// List returns the live root sessions recorded by Grok.
func List(root fs.FS, home string) ([]session.Session, error) {
	index := path.Join("/", home, ".grok", "active_sessions.json")
	sessionsDir := path.Join("/", home, ".grok", "sessions")
	data, err := fs.ReadFile(root, strings.TrimPrefix(index, "/"))
	if errors.Is(err, fs.ErrNotExist) {
		return []session.Session{}, nil
	}
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return nil, &session.ReadError{Path: index, Err: err}
	}
	var entries []json.RawMessage
	if json.Unmarshal(data, &entries) != nil || entries == nil {
		return nil, &session.ReadError{Path: index, Err: session.ErrNotJSON}
	}
	for _, entry := range entries {
		if len(bytes.TrimSpace(entry)) == 0 || bytes.TrimSpace(entry)[0] != '{' {
			return nil, &session.ReadError{Path: index, Err: session.ErrNotJSON}
		}
	}

	var result []session.Session
	for _, raw := range entries {
		var entry map[string]json.RawMessage
		_ = json.Unmarshal(raw, &entry)
		id, ok := jsonString(entry["session_id"])
		if !ok || id == "" {
			continue
		}
		pid, ok := positiveInt(entry["pid"])
		if !ok {
			continue
		}
		started, startErr := proc.Start(root, pid)
		if startErr != nil {
			continue
		}
		if opened, valid := jsonTimestamp(entry["opened_at"]); valid && started.After(opened) {
			continue
		}
		s := session.Session{ID: id, Status: session.StatusUnknown}
		if cwd, valid := jsonString(entry["cwd"]); valid && cwd != "" {
			s.CWD = cwd
		} else {
			s.CWD, _ = proc.Cwd(root, pid)
		}
		if dir := findSessionDir(root, sessionsDir, id); dir != "" {
			s.Title = readTitle(root, dir)
			readEvents(root, dir, &s)
		}
		result = append(result, s)
	}
	return result, nil
}

func jsonString(raw json.RawMessage) (string, bool) {
	var value string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func jsonTimestamp(raw json.RawMessage) (time.Time, bool) {
	value, ok := jsonString(raw)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil
}

func positiveInt(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
		return 0, false
	}
	mantissa := string(raw)
	exponentText := ""
	if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
		exponentText = mantissa[i+1:]
		mantissa = mantissa[:i]
	}
	fractionDigits := 0
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		fractionDigits = len(mantissa) - i - 1
		mantissa = mantissa[:i] + mantissa[i+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		return 0, false
	}
	exponent := 0
	if exponentText != "" {
		negative := false
		if exponentText[0] == '+' || exponentText[0] == '-' {
			negative = exponentText[0] == '-'
			exponentText = exponentText[1:]
		}
		exponentText = strings.TrimLeft(exponentText, "0")
		if exponentText != "" {
			bound := fractionDigits + 19
			if negative {
				bound = len(digits)
			}
			boundText := strconv.Itoa(bound)
			if len(exponentText) > len(boundText) || len(exponentText) == len(boundText) && exponentText > boundText {
				return 0, false
			}
			var err error
			exponent, err = strconv.Atoi(exponentText)
			if err != nil {
				return 0, false
			}
			if negative {
				exponent = -exponent
			}
		}
	}
	shift := exponent - fractionDigits
	if shift < 0 {
		remove := -shift
		if remove > len(digits) || strings.Trim(digits[len(digits)-remove:], "0") != "" {
			return 0, false
		}
		digits = digits[:len(digits)-remove]
	} else if shift > 0 {
		digits += strings.Repeat("0", shift)
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || n == 0 || n > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(n), true
}

func findSessionDir(root fs.FS, sessionsDir, id string) string {
	if id == "." || id == ".." || strings.Contains(id, "/") {
		return ""
	}
	parents, err := fs.ReadDir(root, strings.TrimPrefix(sessionsDir, "/"))
	if err != nil {
		return ""
	}
	sort.Slice(parents, func(i, j int) bool { return parents[i].Name() < parents[j].Name() })
	for _, parent := range parents {
		if !parent.IsDir() {
			continue
		}
		parentPath := path.Join(sessionsDir, parent.Name())
		children, readErr := fs.ReadDir(root, strings.TrimPrefix(parentPath, "/"))
		if readErr != nil {
			continue
		}
		for _, child := range children {
			if child.Name() == id {
				return path.Join(parentPath, id)
			}
		}
	}
	return ""
}

func readTitle(root fs.FS, dir string) string {
	data, err := fs.ReadFile(root, strings.TrimPrefix(path.Join(dir, "summary.json"), "/"))
	if err != nil || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return ""
	}
	var summary map[string]json.RawMessage
	if json.Unmarshal(data, &summary) != nil {
		return ""
	}
	title, _ := jsonString(summary["generated_title"])
	return title
}

func readEvents(root fs.FS, dir string, s *session.Session) {
	data, err := fs.ReadFile(root, strings.TrimPrefix(path.Join(dir, "events.jsonl"), "/"))
	if err != nil {
		return
	}
	s.Status = session.StatusIdle
	started := false
	lastType := ""
	for _, line := range session.Lines(data) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(line) {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		if typ, ok := jsonString(record["type"]); ok {
			lastType = typ
			if typ == "turn_started" {
				started = true
			}
		} else {
			lastType = ""
		}
		if ts, ok := jsonTimestamp(record["ts"]); ok && (!s.HasLastActive || ts.After(s.LastActive)) {
			s.LastActive = ts
			s.HasLastActive = true
		}
	}
	if started && lastType != "turn_ended" {
		s.Status = session.StatusWorking
	}
}
