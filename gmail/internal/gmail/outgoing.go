package gmail

import (
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/mail"
	"strings"
)

const messageBoundary = "=_ikigenba_gmail"

// Outgoing streams one deterministic MIME message.
type Outgoing struct {
	segments []io.Reader
	parts    []*encodedPart
	size     int64
	index    int
	closed   bool
}

// Build prepares headers and text without opening attachment streams.
func Build(c Composition) *Outgoing {
	o := &Outgoing{}
	var headers strings.Builder
	headers.WriteString("MIME-Version: 1.0\r\nFrom: " + addressString(c.From) + "\r\n")
	for _, group := range []struct {
		name   string
		values []mail.Address
	}{{"To", c.To}, {"Cc", c.Cc}, {"Bcc", c.Bcc}} {
		if len(group.values) == 0 {
			continue
		}
		addresses := make([]string, len(group.values))
		for i, a := range group.values {
			addresses[i] = addressString(a)
		}
		headers.WriteString(group.name + ": " + strings.Join(addresses, ", ") + "\r\n")
	}
	if subject := cleanCompositionHeader(c.Subject); subject != "" {
		headers.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	}
	if reply := cleanCompositionHeader(c.InReplyTo); reply != "" {
		headers.WriteString("In-Reply-To: " + reply + "\r\n")
	}
	if references := cleanCompositionHeader(c.References); references != "" {
		headers.WriteString("References: " + references + "\r\n")
	}
	text := encodeText(c.Text)
	if len(c.Attachments) == 0 {
		headers.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		o.add(headers.String())
		o.add(text)
		return o
	}
	headers.WriteString("Content-Type: " + mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": messageBoundary}) + "\r\n\r\n")
	o.add(headers.String())
	o.add("--" + messageBoundary + "\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	o.add(text)
	for _, part := range c.Attachments {
		contentType, params, err := mime.ParseMediaType(cleanHeader(part.ContentType))
		if err != nil {
			contentType = "application/octet-stream"
			params = nil
		}
		o.add("\r\n--" + messageBoundary + "\r\nContent-Disposition: " + mime.FormatMediaType("attachment", map[string]string{"filename": cleanHeader(part.Name)}) + "\r\nContent-Transfer-Encoding: base64\r\nContent-Type: " + mime.FormatMediaType(contentType, params) + "\r\n\r\n")
		p := &encodedPart{part: part, remaining: part.Bytes}
		o.parts = append(o.parts, p)
		o.segments = append(o.segments, p)
		o.size += encodedSize(part.Bytes)
	}
	o.add("\r\n--" + messageBoundary + "--\r\n")
	return o
}

func cleanHeader(s string) string            { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
func cleanCompositionHeader(s string) string { return strings.Trim(cleanHeader(s), " \t") }
func addressString(a mail.Address) string    { a = cleanAddress(a); return a.String() }
func cleanAddress(a mail.Address) mail.Address {
	return mail.Address{Name: cleanCompositionHeader(a.Name), Address: cleanCompositionHeader(a.Address)}
}
func encodedSize(n int64) int64 {
	if n <= 0 {
		return 0
	}
	encoded := ((n + 2) / 3) * 4
	return encoded + ((encoded+75)/76)*2
}
func encodeText(text string) string {
	var normalized strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' && (i == 0 || text[i-1] != '\r') {
			normalized.WriteByte('\r')
		}
		normalized.WriteByte(text[i])
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(normalized.String()))
	var body strings.Builder
	for len(encoded) > 0 {
		n := min(76, len(encoded))
		body.WriteString(encoded[:n])
		body.WriteString("\r\n")
		encoded = encoded[n:]
	}
	return body.String()
}
func (o *Outgoing) add(s string) {
	o.segments = append(o.segments, strings.NewReader(s))
	o.size += int64(len(s))
}

// Size answers the exact encoded length from declared part lengths.
func (o *Outgoing) Size() int64 { return o.size }

// Read advances only after yielding all bytes of the preceding segment.
func (o *Outgoing) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if o.closed {
		return 0, io.EOF
	}
	for o.index < len(o.segments) {
		n, err := o.segments[o.index].Read(p)
		if errors.Is(err, io.EOF) {
			o.index++
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
	return 0, io.EOF
}

// Close closes each opened attachment exactly once.
func (o *Outgoing) Close() error {
	o.closed = true
	var errs []error
	for _, p := range o.parts {
		if err := p.close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type streamFailure struct{ cause error }

func (e streamFailure) Error() string { return "gmail: attachment stream failed" }
func (e streamFailure) Unwrap() error { return e.cause }

type encodedPart struct {
	part                     Part
	remaining                int64
	stream                   io.ReadCloser
	opened, finished, closed bool
	pending                  []byte
	failure                  error
}

func (p *encodedPart) close() error {
	if p.stream == nil || p.closed {
		return nil
	}
	p.closed = true
	if err := p.stream.Close(); err != nil {
		return streamFailure{err}
	}
	return nil
}
func (p *encodedPart) fail(err error) {
	p.failure = streamFailure{err}
	p.finished = true
	_ = p.close()
}
func (p *encodedPart) Read(dst []byte) (int, error) {
	if len(p.pending) > 0 {
		n := copy(dst, p.pending)
		p.pending = p.pending[n:]
		return n, nil
	}
	if p.failure != nil {
		return 0, p.failure
	}
	if p.finished {
		return 0, io.EOF
	}
	if !p.opened {
		p.opened = true
		if p.part.Open == nil {
			p.fail(ErrPartSize)
			return 0, p.failure
		}
		var err error
		p.stream, err = p.part.Open()
		if err != nil {
			p.fail(err)
			return 0, p.failure
		}
		if p.stream == nil {
			p.fail(ErrPartSize)
			return 0, p.failure
		}
	}
	if p.remaining < 0 {
		p.fail(ErrPartSize)
		return 0, p.failure
	}
	if p.remaining == 0 {
		var extra [1]byte
		n, err := p.stream.Read(extra[:])
		switch {
		case n > 0:
			p.fail(ErrPartSize)
		case errors.Is(err, io.EOF):
			p.finished = true
			if closeErr := p.close(); closeErr != nil {
				p.failure = closeErr
			}
		case err != nil:
			p.fail(err)
		default:
			return 0, nil
		}
		if p.failure != nil {
			return 0, p.failure
		}
		return 0, io.EOF
	}
	var raw [57]byte
	need := int(min(p.remaining, int64(len(raw))))
	n, err := readPart(p.stream, raw[:need])
	p.remaining -= int64(n)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			p.fail(ErrPartSize)
		} else {
			p.fail(err)
		}
		return 0, p.failure
	}
	encoded := make([]byte, base64.StdEncoding.EncodedLen(n)+2)
	base64.StdEncoding.Encode(encoded, raw[:n])
	copy(encoded[len(encoded)-2:], "\r\n")
	p.pending = encoded
	count := copy(dst, p.pending)
	p.pending = p.pending[count:]
	return count, nil
}

// readPart preserves failures returned together with the final requested byte.
func readPart(r io.Reader, dst []byte) (int, error) {
	count, empty := 0, 0
	for count < len(dst) {
		n, err := r.Read(dst[count:])
		count += n
		if err != nil {
			if errors.Is(err, io.EOF) && count == len(dst) {
				return count, nil
			}
			return count, err
		}
		if n == 0 {
			empty++
			if empty >= 100 {
				return count, io.ErrNoProgress
			}
		} else {
			empty = 0
		}
	}
	return count, nil
}
