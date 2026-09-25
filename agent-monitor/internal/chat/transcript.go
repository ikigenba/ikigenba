package chat

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

// Transcript follows one agent's log and accumulates its decoded usage.
type Transcript struct {
	path       string
	recorded   Recorded
	newDecoder func() Decoder
	decoder    Decoder
	log        session.Log
	usage      Usage
}

// NewTranscript creates a reader for one agent's transcript.
func NewTranscript(path string, recorded Recorded, newDecoder func() Decoder) *Transcript {
	t := &Transcript{path: path, recorded: recorded, newDecoder: newDecoder}
	if path != "" {
		t.decoder = newDecoder()
	}
	return t
}

// Path returns the transcript path supplied at construction.
func (t *Transcript) Path() string { return t.path }

// Recorded reports which usage fields the harness records.
func (t *Transcript) Recorded() Recorded { return t.recorded }

// Usage returns the running usage since construction or the last reset.
func (t *Transcript) Usage() Usage { return t.usage }

// Read decodes the complete records gained in one log pass.
func (t *Transcript) Read(fsys fs.FS) (entries []Entry, reset bool, err error) {
	if t.path == "" {
		return nil, false, nil
	}
	lines, reset, err := t.log.Read(fsys, strings.TrimPrefix(t.path, "/"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return nil, false, &session.ReadError{Path: t.path, Err: err}
	}
	if reset {
		t.decoder = t.newDecoder()
		t.usage = Usage{}
	}
	for _, line := range lines {
		if !record(line) {
			continue
		}
		decoded, usage := t.decoder.Decode(fsys, line)
		for _, entry := range decoded {
			if keepEntry(entry) {
				entries = append(entries, entry)
			}
		}
		t.usage.In += max(0, usage.In)
		t.usage.CacheWrite += max(0, usage.CacheWrite)
		t.usage.CacheRead += max(0, usage.CacheRead)
		t.usage.Out += max(0, usage.Out)
		t.usage.Reasoning += max(0, usage.Reasoning)
		t.usage.Calls += max(0, usage.Calls)
	}
	return entries, reset, nil
}

func record(line []byte) bool {
	if !json.Valid(line) {
		return false
	}
	for _, b := range line {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b == '{'
		}
	}
	return false
}

func keepEntry(entry Entry) bool {
	switch entry.Kind {
	case KindUser, KindAssistant, KindReasoning, KindAgent:
		return entry.Text != ""
	case KindTool, KindResultOK, KindResultError:
		return true
	default:
		return false
	}
}
