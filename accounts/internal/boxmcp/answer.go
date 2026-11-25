// Package boxmcp relays stdio MCP tool requests through the accounts daemon's socket.
package boxmcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Socket and answer limits are shared with the accounts daemon.
const (
	SocketPath    = "/run/accountsd.sock"
	EnvSocket     = "ACCOUNTSD_SOCKET"
	ServerName    = "accounts"
	MaxAnswerLine = protocol.MaxBoxAnswer + 1024
	AnswerTimeout = 30 * time.Second

	NotAnswering = "the host's accounts daemon is not answering"

	refusalPrefix = `{"refusal":`
)

// Answer carries either the server's raw response or the daemon's refusal.
type Answer struct {
	Message json.RawMessage `json:"message,omitempty"`
	Refusal string          `json:"refusal,omitempty"`
}

// ErrAnswer means an answer cannot be represented by the socket protocol.
var ErrAnswer = errors.New("invalid accounts daemon answer")

// WriteAnswer writes one socket answer, preserving the message's original bytes.
func WriteAnswer(w io.Writer, a Answer) error {
	switch {
	case !hasMessage(a.Message) && a.Refusal != "":
		if !fitsJSONString(a.Refusal, MaxAnswerLine-len(refusalPrefix)-1) {
			return ErrAnswer
		}
		e := json.NewEncoder(refusalWriter{w})
		e.SetEscapeHTML(false)
		return e.Encode(a.Refusal)
	case hasMessage(a.Message) && a.Refusal == "":
		const prefix = `{"message":`
		if len(a.Message) > protocol.MaxBoxAnswer || len(prefix)+len(a.Message)+1 > MaxAnswerLine ||
			bytes.ContainsAny(a.Message, "\r\n") || !isObject(a.Message) {
			return ErrAnswer
		}
		return writeParts(w, []byte(prefix), a.Message, []byte("}\n"))
	default:
		return ErrAnswer
	}
}

// fitsJSONString checks the wire size before the encoder allocates its output.
// The refusal uses encoding/json with HTML escaping disabled.
func fitsJSONString(s string, limit int) bool {
	remaining := limit - 2
	if len(s) > remaining {
		return false
	}
	for len(s) != 0 && remaining >= 0 {
		r, size := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && size == 1, r == '\u2028', r == '\u2029':
			remaining -= 6
		case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
			remaining -= 2
		case r < ' ':
			remaining -= 6
		default:
			remaining -= size
		}
		s = s[size:]
	}
	return remaining >= 0
}

// refusalWriter wraps the encoder's one write without copying the encoded string.
type refusalWriter struct{ io.Writer }

func (w refusalWriter) Write(encoded []byte) (int, error) {
	if err := writeParts(w.Writer, []byte(refusalPrefix), encoded[:len(encoded)-1], []byte("}\n")); err != nil {
		return 0, err
	}
	return len(encoded), nil
}

func hasMessage(message json.RawMessage) bool {
	return len(message) != 0 && !bytes.Equal(message, []byte("null"))
}

func isObject(data []byte) bool {
	return len(data) >= 2 && data[0] == '{' && data[len(data)-1] == '}' && json.Valid(data)
}

func writeParts(w io.Writer, parts ...[]byte) error {
	for _, part := range parts {
		n, err := w.Write(part)
		if err != nil {
			return err
		}
		if n != len(part) {
			return io.ErrShortWrite
		}
	}
	return nil
}
