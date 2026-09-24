// Package claude reads Claude Code's root-session registrations.
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

	"github.com/ikigenba/ikigenba/agent-monitor/internal/proc"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

// List returns live root sessions registered by Claude Code.
func List(root fs.FS, home string) ([]session.Session, error) {
	registry := path.Join("/", home, ".claude", "sessions")
	projects := path.Join("/", home, ".claude", "projects")
	entries, err := fs.ReadDir(root, strings.TrimPrefix(registry, "/"))
	if errors.Is(err, fs.ErrNotExist) {
		return []session.Session{}, nil
	}
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return nil, &session.ReadError{Path: registry, Err: err}
	}

	var result []session.Session
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := fs.ReadFile(root, strings.TrimPrefix(path.Join(registry, entry.Name()), "/"))
		if readErr != nil {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil || fields == nil {
			continue
		}
		id, idOK := stringField(fields["sessionId"])
		pidValue, pidOK := numberField(fields["pid"], false)
		if !idOK || id == "" || !pidOK || pidValue == 0 || pidValue > uint64(int(^uint(0)>>1)) {
			continue
		}
		pid := int(pidValue)
		if !live(root, pid, fields) {
			continue
		}
		s := session.Session{ID: id, Status: session.StatusUnknown}
		if status, ok := stringField(fields["status"]); ok {
			switch status {
			case "busy":
				s.Status = session.StatusWorking
			case "idle":
				s.Status = session.StatusIdle
			}
		}
		s.Title, _ = stringField(fields["name"])
		s.CWD, _ = stringField(fields["cwd"])
		if s.CWD == "" {
			s.CWD, _ = proc.Cwd(root, pid)
		}
		if id != "." && id != ".." && !strings.Contains(id, "/") {
			s.LastActive, s.HasLastActive = latest(root, projects, id)
		}
		result = append(result, s)
	}
	return result, nil
}

func stringField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

// numberField accepts JSON numbers whose mathematical value is an integer.
// It keeps decimal arithmetic exact, including exponent and fraction forms.
func numberField(raw json.RawMessage, signed bool) (uint64, bool) {
	if len(raw) == 0 || raw[0] == '"' || raw[0] == 'n' || raw[0] == 't' || raw[0] == 'f' || raw[0] == '[' || raw[0] == '{' {
		return 0, false
	}
	s := string(raw)
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	exponentText := ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponentText = s[i+1:]
		s = s[:i]
	}
	fraction := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		fraction = len(s) - i - 1
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return 0, true
	}
	exponent := int64(0)
	if exponentText != "" {
		var err error
		exponent, err = strconv.ParseInt(exponentText, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	if exponent < -int64(len(s)) {
		return 0, false
	}
	shift := exponent - int64(fraction)
	if shift < 0 {
		if -shift > int64(len(s)) || strings.TrimRight(s[len(s)+int(shift):], "0") != "" {
			return 0, false
		}
		s = s[:len(s)+int(shift)]
	} else {
		if shift > 20 || len(s)+int(shift) > 20 {
			return 0, false
		}
		s += strings.Repeat("0", int(shift))
	}
	value, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	if negative {
		if !signed || value > uint64(1)<<63 {
			return 0, false
		}
		return value, true
	}
	if signed && value > uint64(1<<63-1) {
		return 0, false
	}
	return value, true
}

func live(root fs.FS, pid int, fields map[string]json.RawMessage) bool {
	if start, ok := stringField(fields["procStart"]); ok && start != "" {
		value, err := strconv.ParseUint(start, 10, 64)
		if err == nil {
			actual, readErr := proc.StartTicks(root, pid)
			return readErr == nil && actual <= value
		}
	}
	if started, ok := numberField(fields["startedAt"], true); ok {
		text := strconv.FormatUint(started, 10)
		if bytes.HasPrefix(fields["startedAt"], []byte("-")) {
			text = "-" + text
		}
		millis, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return false
		}
		actual, err := proc.Start(root, pid)
		return err == nil && !actual.After(time.UnixMilli(millis))
	}
	_, err := proc.StartTicks(root, pid)
	return err == nil
}

func latest(root fs.FS, projects, id string) (time.Time, bool) {
	dirs, err := fs.ReadDir(root, strings.TrimPrefix(projects, "/"))
	if err != nil {
		return time.Time{}, false
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	name := id + ".jsonl"
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		folder := path.Join(projects, dir.Name())
		entries, err := fs.ReadDir(root, strings.TrimPrefix(folder, "/"))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.Name() != name {
				continue
			}
			data, err := fs.ReadFile(root, strings.TrimPrefix(path.Join(folder, name), "/"))
			if err != nil {
				break
			}
			return latestInLog(data)
		}
	}
	return time.Time{}, false
}

func latestInLog(data []byte) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, line := range session.Lines(data) {
		if !json.Valid(line) {
			continue
		}
		line = bytes.TrimLeft(line, " \t\r\n")
		if line[0] != '{' {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(line, &fields) != nil {
			continue
		}
		value, ok := stringField(fields["timestamp"])
		if !ok {
			continue
		}
		stamp, err := time.Parse(time.RFC3339Nano, value)
		if err == nil && (!found || stamp.After(latest)) {
			latest, found = stamp, true
		}
	}
	return latest, found
}
