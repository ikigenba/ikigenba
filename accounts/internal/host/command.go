package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Command runs one configured executable for each host operation.
type Command struct{ fields []string }

// NewCommand retains a private copy of the executable and leading arguments.
func NewCommand(fields []string) *Command { return &Command{fields: append([]string(nil), fields...)} }

func (c *Command) run(ctx context.Context, args ...string) ([]byte, error) {
	if ctx.Err() != nil || len(c.fields) == 0 {
		return nil, &FailedError{-1}
	}
	program, err := exec.LookPath(c.fields[0])
	if err != nil {
		return nil, &FailedError{-1}
	}
	cmd := &exec.Cmd{Path: program, Args: append(append([]string(nil), c.fields...), args...)}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return nil, &FailedError{-1}
	}
	defer func() { _ = outRead.Close(); _ = outWrite.Close() }()
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		return nil, &FailedError{-1}
	}
	defer func() { _ = errRead.Close(); _ = errWrite.Close() }()
	var stdout, stderr bytes.Buffer
	// File outputs keep Wait's direct-process result independent of descendants.
	cmd.Stdout, cmd.Stderr = outWrite, errWrite
	if err = cmd.Start(); err != nil {
		return nil, &FailedError{-1}
	}
	_ = outWrite.Close()
	_ = errWrite.Close()
	outDone, errDone := make(chan struct{}), make(chan struct{})
	go func() { _, _ = io.Copy(&stdout, outRead); close(outDone) }()
	go func() { _, _ = io.Copy(&stderr, errRead); close(errDone) }()
	killed := make(chan bool, 1)
	stop := context.AfterFunc(ctx, func() { killed <- cmd.Process.Kill() == nil })
	err = cmd.Wait()
	interrupted := false
	if !stop() {
		interrupted = <-killed
	}
	if interrupted {
		_ = outRead.Close()
		_ = errRead.Close()
		<-outDone
		<-errDone
		return nil, &FailedError{-1}
	}
	// A successful direct exit remains successful if cancellation occurs while
	// draining inherited output. Bound the drain when a descendant keeps it open.
	drained := make(chan struct{})
	go func() { <-outDone; <-errDone; close(drained) }()
	timer := time.NewTimer(time.Second)
	select {
	case <-drained:
	case <-timer.C:
		_ = outRead.Close()
		_ = errRead.Close()
		<-drained
	}
	timer.Stop()
	if err == nil {
		return stdout.Bytes(), nil
	}
	status := -1
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		status = exit.ExitCode()
	}
	if status == 2 || status == 4 {
		line, _, _ := strings.Cut(stderr.String(), "\n")
		line = strings.TrimPrefix(line, DiagnosticPrefix)
		if line != "" {
			return nil, &RefusedError{status, line}
		}
	}
	return nil, &FailedError{status}
}

func (c *Command) change(ctx context.Context, args ...string) error {
	_, err := c.run(ctx, args...)
	return err
}

// GroupAdd creates a group on the command host.
func (c *Command) GroupAdd(ctx context.Context, slug string) error {
	return c.change(ctx, "group", "add", slug)
}

// GroupRemove removes an empty group on the command host.
func (c *Command) GroupRemove(ctx context.Context, slug string) error {
	return c.change(ctx, "group", "remove", slug)
}

// UserAdd creates a user with a group and key on the command host.
func (c *Command) UserAdd(ctx context.Context, name, group, key string) error {
	return c.change(ctx, "user", "add", name, group, key)
}

// UserKeyAdd adds a key on the command host.
func (c *Command) UserKeyAdd(ctx context.Context, name, key string) error {
	return c.change(ctx, "user", "key", "add", name, key)
}

// UserKeyRemove removes a key on the command host.
func (c *Command) UserKeyRemove(ctx context.Context, name, key string) error {
	return c.change(ctx, "user", "key", "remove", name, key)
}

// UserJoin adds a membership on the command host.
func (c *Command) UserJoin(ctx context.Context, name, group string) error {
	return c.change(ctx, "user", "join", name, group)
}

// UserLeave removes a membership on the command host.
func (c *Command) UserLeave(ctx context.Context, name, group string) error {
	return c.change(ctx, "user", "leave", name, group)
}

// UserLock locks a user on the command host.
func (c *Command) UserLock(ctx context.Context, name string) error {
	return c.change(ctx, "user", "lock", name)
}

// UserUnlock unlocks a user on the command host.
func (c *Command) UserUnlock(ctx context.Context, name string) error {
	return c.change(ctx, "user", "unlock", name)
}

// UserDelete deletes a locked user on the command host.
func (c *Command) UserDelete(ctx context.Context, name string) error {
	return c.change(ctx, "user", "delete", name)
}

func object(data []byte) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return nil, &FailedError{0}
	}
	return obj, nil
}
func array(data json.RawMessage) ([]json.RawMessage, bool) {
	if len(data) == 0 {
		return nil, true
	}
	var out []json.RawMessage
	err := json.Unmarshal(data, &out)
	return out, err == nil
}
func text(data json.RawMessage) (string, bool) {
	var out string
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return "", false
	}
	err := json.Unmarshal(data, &out)
	return out, err == nil
}
func stringsArray(data json.RawMessage) ([]string, bool) {
	items, ok := array(data)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, valid := text(item)
		if !valid {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// GroupList reads and normalizes the command host's group document.
func (c *Command) GroupList(ctx context.Context) ([]Group, error) {
	data, err := c.run(ctx, "group", "list")
	if err != nil {
		return nil, err
	}
	obj, err := object(data)
	if err != nil {
		return nil, err
	}
	items, ok := array(obj["groups"])
	if !ok {
		return nil, &FailedError{0}
	}
	out := make([]Group, 0, len(items))
	for _, item := range items {
		entry, e := object(item)
		if e != nil {
			return nil, e
		}
		slug, valid := text(entry["slug"])
		var count *int
		if e = json.Unmarshal(entry["members"], &count); !valid || slug == "" || e != nil || count == nil || *count < 0 {
			return nil, &FailedError{0}
		}
		out = append(out, Group{slug, *count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// GroupMembers reads and normalizes the command host's membership document.
func (c *Command) GroupMembers(ctx context.Context, slug string) (Members, error) {
	data, err := c.run(ctx, "group", "members", slug)
	if err != nil {
		return Members{}, err
	}
	obj, err := object(data)
	if err != nil {
		return Members{}, err
	}
	group, valid := text(obj["group"])
	members, ok := stringsArray(obj["members"])
	if !valid || group != slug || !ok {
		return Members{}, &FailedError{0}
	}
	sort.Strings(members)
	return Members{group, members}, nil
}

// UserList reads and normalizes the command host's user document.
func (c *Command) UserList(ctx context.Context) ([]User, error) {
	data, err := c.run(ctx, "user", "list")
	if err != nil {
		return nil, err
	}
	obj, err := object(data)
	if err != nil {
		return nil, err
	}
	items, ok := array(obj["users"])
	if !ok {
		return nil, &FailedError{0}
	}
	out := make([]User, 0, len(items))
	for _, item := range items {
		entry, e := object(item)
		if e != nil {
			return nil, e
		}
		name, valid := text(entry["name"])
		state, stateOK := text(entry["state"])
		groups, groupsOK := stringsArray(entry["groups"])
		keys, keysOK := stringsArray(entry["keys"])
		if !valid || name == "" || !stateOK || (state != StateActive && state != StateLocked) || !groupsOK || !keysOK {
			return nil, &FailedError{0}
		}
		var last time.Time
		if raw := entry["last_sign_in"]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
			stamp, stampOK := text(raw)
			if !stampOK {
				return nil, &FailedError{0}
			}
			last, e = time.Parse(time.RFC3339, stamp)
			if e != nil {
				return nil, &FailedError{0}
			}
			last = last.UTC()
		}
		sort.Strings(groups)
		out = append(out, User{name, groups, keys, state, last})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
