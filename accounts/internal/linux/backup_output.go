package linux

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

type backupCredentials [3]string

func backupCommandCredentials(env []string) backupCredentials {
	var credentials backupCredentials
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		switch key {
		case "AWS_ACCESS_KEY_ID":
			credentials[0] = value
		case "AWS_SECRET_ACCESS_KEY":
			credentials[1] = value
		case "AWS_SESSION_TOKEN":
			credentials[2] = value
		}
	}
	return credentials
}

type backupCaptureKey struct{}

type backupCaptureBounds struct {
	known, stdoutCut, stderrCut bool
}

type backupDiagnosticKey struct{}

// An external error can include a credential other than the one the command
// received. Group the pass's credentials by first byte for diagnostic matching.
type backupDiagnostics [256][]string

func withBackupDiagnosticRedactor(ctx context.Context, backup *protocol.Backup) context.Context {
	if backup == nil {
		return ctx
	}
	redact := new(backupDiagnostics)
	add := func(c protocol.Credentials) {
		for _, value := range []string{c.AccessKeyID, c.SecretAccessKey, c.SessionToken} {
			if value != "" {
				redact[value[0]] = append(redact[value[0]], value)
			}
		}
	}
	add(backup.Host)
	for _, account := range backup.Accounts {
		add(account.Credentials)
	}
	for _, group := range redact {
		slices.SortFunc(group, func(a, b string) int { return len(b) - len(a) })
	}
	return context.WithValue(ctx, backupDiagnosticKey{}, redact)
}

func backupDiagnosticRedactor(ctx context.Context) *backupDiagnostics {
	redact, _ := ctx.Value(backupDiagnosticKey{}).(*backupDiagnostics)
	return redact
}

func redactBackupDiagnostic(redact *backupDiagnostics, err error) error {
	if redact != nil && err != nil {
		message := err.Error()
		if cleaned := redact.replace([]byte(message), len(message), backupCredentials{}); cleaned != nil {
			return errors.New(string(cleaned))
		}
	}
	return err
}

func (d *backupDiagnostics) capture(output []byte, credentials backupCredentials, known, cut bool) []byte {
	// An injected result of exactly MaxCapture bytes has no cut information.
	if !known && credentials != (backupCredentials{}) && len(output) > MaxCapture {
		output = output[:MaxCapture]
		cut = true
	}
	cutStart := len(output)
	if cut {
		for _, value := range credentials {
			for n := min(len(value)-1, len(output)); n > len(output)-cutStart; n-- {
				if bytes.HasSuffix(output, []byte(value[:n])) {
					cutStart = len(output) - n
					break
				}
			}
		}
	}
	if cleaned := d.replace(output, cutStart, credentials); cleaned != nil {
		return cleaned
	}
	return output
}

// replace scans the original bytes from left to right. It returns nil when
// no edit is needed, and allocates a single bounded copy at the first match.
func (d *backupDiagnostics) replace(output []byte, cutStart int, credentials backupCredentials) []byte {
	var cleaned []byte
	for i := 0; i < len(output); {
		length := 0
		if i >= cutStart {
			length = len(output) - i
		} else {
			length = d.match(output[i:], credentials)
		}
		if length != 0 {
			if cleaned == nil {
				cleaned = make([]byte, 0, len(machine.Redacted)*len(output))
				cleaned = append(cleaned, output[:i]...)
			}
			cleaned = append(cleaned, machine.Redacted...)
			i += length
			continue
		}
		if cleaned != nil {
			cleaned = append(cleaned, output[i])
		}
		i++
	}
	return cleaned
}

func (d *backupDiagnostics) match(output []byte, credentials backupCredentials) int {
	longest := 0
	for _, value := range credentials {
		if len(value) > longest && bytes.HasPrefix(output, []byte(value)) {
			longest = len(value)
		}
	}
	if longest != 0 {
		return longest
	}
	for _, value := range d[output[0]] {
		if bytes.HasPrefix(output, []byte(value)) {
			return len(value)
		}
	}
	return 0
}

type backupJSONArray struct {
	*io.PipeWriter
	done chan struct{}
}

func newBackupJSONArray(fields []string, emit func(map[string]string)) *backupJSONArray {
	return newBackupJSONArrayWithStrings(fields, "", nil, emit)
}

// newBackupJSONArrayWithStrings projects selected strings without buffering a
// listing or an object. item receives selected array strings before emit.
func newBackupJSONArrayWithStrings(fields []string, arrayField string, item func(string), emit func(map[string]string)) *backupJSONArray {
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = reader.Close() }()
		keyLimit := len(arrayField)
		for _, field := range fields {
			keyLimit = max(keyLimit, len(field))
		}
		projection := backupJSONProjection{
			in: bufio.NewReader(reader), fields: fields, arrayField: arrayField, item: item, emit: emit, keyLimit: 6 * keyLimit,
		}
		projection.read()
		_, _ = io.Copy(io.Discard, reader)
	}()
	return &backupJSONArray{PipeWriter: writer, done: done}
}

func (w *backupJSONArray) finish() {
	_ = w.Close()
	<-w.done
}

type backupJSONProjection struct {
	in         *bufio.Reader
	fields     []string
	arrayField string
	item       func(string)
	emit       func(map[string]string)
	scratch    []byte
	keyLimit   int
}

func (p *backupJSONProjection) next() byte {
	for {
		b, err := p.in.ReadByte()
		if err != nil {
			return 0
		}
		switch b {
		case ' ', '\t', '\r', '\n':
		default:
			return b
		}
	}
}

func (p *backupJSONProjection) read() {
	if p.next() != '[' {
		return
	}
	for first := p.next(); first != 0 && first != ']'; first = p.next() {
		if first == '{' {
			p.object()
		} else {
			p.skipValue(first)
		}
		if p.next() != ',' {
			return
		}
	}
}

func (p *backupJSONProjection) object() {
	values := make(map[string]string, len(p.fields))
	tooLong := false
	for opening := p.next(); opening == '"'; opening = p.next() {
		key, _, ok := p.readString(p.keyLimit)
		if !ok || p.next() != ':' {
			p.emit(nil)
			return
		}
		value := p.next()
		switch {
		case tooLong:
			p.skipValue(value)
		case value == '[' && p.item != nil && key == p.arrayField:
			tooLong = p.stringArray()
		case value == '"' && p.selected(key):
			text, long, complete := p.readProjectedString(backupFieldBytes(key), key)
			if !complete {
				p.emit(nil)
				return
			}
			tooLong = long
			if !long {
				values[key] = text
			}
		default:
			p.skipValue(value)
		}
		switch p.next() {
		case ',':
			continue
		case '}':
			if tooLong {
				values = nil
			}
			p.emit(values)
		default:
			p.emit(nil)
		}
		return
	}
	p.emit(nil)
}

func (p *backupJSONProjection) selected(key string) bool {
	for _, field := range p.fields {
		if key == field {
			return true
		}
	}
	return false
}

// Only the snapshot ID needs the full string allowance. Timestamp, transaction,
// and path fields have small fixed storage in addition to that payload.
func backupFieldBytes(field string) int {
	const jsonEscapeWidth = 6
	switch field {
	case "status":
		return jsonEscapeWidth * len("ok")
	case "database":
		return jsonEscapeWidth * unix.PathMax
	default:
		return MaxJournalLine
	}
}

func (p *backupJSONProjection) stringArray() bool {
	tooLong := false
	for first := p.next(); first != ']' && first != 0; first = p.next() {
		if first == '"' {
			value, long, ok := p.readString(6 * unix.PathMax)
			tooLong = tooLong || long
			if ok && !long {
				p.item(value)
			}
		} else {
			p.skipValue(first)
		}
		if p.next() != ',' {
			break
		}
	}
	return tooLong
}

// readString consumes the whole token even when it exceeds its field's storage
// limit. Only a token exceeding MaxJournalLine disqualifies the whole row.
func (p *backupJSONProjection) readString(limit int) (value string, tooLong, complete bool) {
	return p.readProjectedString(limit, "")
}

func (p *backupJSONProjection) readProjectedString(limit int, field string) (value string, tooLong, complete bool) {
	p.scratch = p.scratch[:0]
	escaped := false
	size := 0
	keep := limit >= 0
	if keep {
		if p.scratch == nil {
			p.scratch = make([]byte, 0, MaxJournalLine+2)
		}
		p.scratch = append(p.scratch, '"')
	}
	for {
		b, err := p.in.ReadByte()
		if err != nil {
			return "", tooLong, false
		}
		if b == '"' && !escaped {
			if keep && size <= limit {
				p.scratch = append(p.scratch, '"')
				value = projectBackupString(field, p.scratch)
			}
			return value, tooLong, true
		}
		if keep && !tooLong {
			size++
			tooLong = size > MaxJournalLine
			if size <= limit {
				p.scratch = append(p.scratch, b)
			}
		}
		escaped = b == '\\' && !escaped
	}
}

// A transaction number or timestamp can have arbitrarily many insignificant
// digits. Decode those ASCII fields in the token buffer and copy only the
// compact value, leaving room for a full-size snapshot ID in the same row.
func projectBackupString(field string, quoted []byte) string {
	switch field {
	case "time", "timestamp":
		raw := backupJSONASCII(quoted[1 : len(quoted)-1])
		if decimal := bytes.IndexAny(raw, ".,"); decimal >= 0 {
			end := decimal + 1
			for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
				end++
			}
			keep := decimal + 1 + 9
			if end > keep {
				raw = raw[:keep+copy(raw[keep:], raw[end:])]
			}
		}
		if len(raw) <= len(time.RFC3339Nano) {
			return string(raw)
		}
	case "local_txid", "max_txid":
		raw := backupJSONASCII(quoted[1 : len(quoted)-1])
		if len(raw) == 0 {
			return ""
		}
		trimmed := bytes.TrimLeft(raw, "0")
		if len(trimmed) == 0 {
			return "0"
		}
		if len(trimmed) <= 16 {
			return string(trimmed)
		}
	default:
		var value string
		if utf8.Valid(quoted) {
			_ = json.Unmarshal(quoted, &value)
		}
		return value
	}
	return ""
}

// The numeric and timestamp grammars contain only ASCII. JSON can represent
// those bytes literally or with a Unicode escape, which cannot expand here.
func backupJSONASCII(raw []byte) []byte {
	out := raw[:0]
	for len(raw) != 0 {
		b := raw[0]
		raw = raw[1:]
		if b == '\\' {
			if len(raw) < 5 || raw[0] != 'u' || raw[1] != '0' || raw[2] != '0' {
				return nil
			}
			value, err := strconv.ParseUint(string(raw[3:5]), 16, 7)
			if err != nil {
				return nil
			}
			b = byte(value)
			raw = raw[5:]
		}
		if b < ' ' || b > '~' {
			return nil
		}
		out = append(out, b)
	}
	return out
}

func (p *backupJSONProjection) skipValue(first byte) {
	switch first {
	case '"':
		p.readString(-1)
	case '{', '[':
		depth := 1
		for depth > 0 {
			switch p.next() {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			case '"':
				p.readString(-1)
			case 0:
				return
			}
		}
	case 0, ',', '}', ']':
		// Commas and closing brackets are left to the enclosing grammar.
		if first != 0 {
			_ = p.in.UnreadByte()
		}
	default:
		for {
			b, err := p.in.ReadByte()
			if err != nil {
				return
			}
			switch b {
			case ',', '}', ']':
				_ = p.in.UnreadByte()
				return
			case ' ', '\t', '\r', '\n':
				return
			}
		}
	}
}
