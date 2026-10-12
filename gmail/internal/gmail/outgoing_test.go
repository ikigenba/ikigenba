package gmail

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"reflect"
	"strings"
	"testing"
)

type countedStream struct {
	io.Reader
	closes int
}

func (s *countedStream) Close() error { s.closes++; return nil }
func compositionFixture() Composition {
	return Composition{From: mail.Address{Name: "Séndér", Address: "sender@fixture.invalid"}, To: []mail.Address{{Name: "Receiver", Address: "receiver@fixture.invalid"}}, Cc: []mail.Address{{Address: "copy@fixture.invalid"}}, Bcc: []mail.Address{{Address: "blind@fixture.invalid"}}, Subject: "Résumé", Text: "alpha\r\nbeta\ngamma\rdelta", InReplyTo: "<reply@fixture.invalid>", References: "<prior@fixture.invalid>"}
}
func outgoingBytes(t *testing.T, c Composition) []byte {
	t.Helper()
	o := Build(c)
	b, err := io.ReadAll(o)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) != o.Size() {
		t.Fatalf("size=%d bytes=%d", o.Size(), len(b))
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}
	return b
}
func readMessage(t *testing.T, b []byte) *mail.Message {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func decodedBody(t *testing.T, r io.Reader, encoding string) string {
	t.Helper()
	if encoding != "base64" {
		t.Fatalf("encoding=%q", encoding)
	}
	b, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, r))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// R-GI5B-I7NI
func TestOutgoingAPI(t *testing.T) {
	c := Composition{mail.Address{Address: "sender@fixture.invalid"}, nil, nil, nil, "subject", "text", "", "", []Part{{"file", "application/octet-stream", 0, func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil }}}}
	var reader io.ReadCloser = Build(c)
	b, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty message")
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
}

// R-HXD3-KKSO
func TestOutgoingExactDeterministicSizeWithoutOpening(t *testing.T) {
	for _, length := range []int{0, 1, 2, 3, 56, 57, 58, 114, 115, 10000} {
		opened := 0
		c := compositionFixture()
		data := bytes.Repeat([]byte{0, 1, 2, 255}, length/4+1)[:length]
		c.Attachments = []Part{{Name: "sample.bin", ContentType: "application/octet-stream", Bytes: int64(length), Open: func() (io.ReadCloser, error) { opened++; return io.NopCloser(bytes.NewReader(data)), nil }}}
		a, b := Build(c), Build(c)
		size := a.Size()
		if size != b.Size() || opened != 0 {
			t.Fatal("construction opened data or changed size")
		}
		x, err := io.ReadAll(a)
		if err != nil {
			t.Fatal(err)
		}
		y, err := io.ReadAll(b)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(x)) != size || !bytes.Equal(x, y) || opened != 2 {
			t.Fatalf("incorrect size or nondeterministic bytes at length %d", length)
		}
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
		if err = b.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// R-A48Q-AWXJ
func TestOutgoingMessageHeaders(t *testing.T) {
	clean := func(s string) string {
		return strings.Trim(strings.NewReplacer("\r", " ", "\n", " ").Replace(s), " \t")
	}
	cases := []struct {
		name string
		edit func(*Composition)
	}{
		{"unchanged", func(*Composition) {}},
		{"injection", func(c *Composition) {
			c.From.Name += "\r\nX-Extra: value"
			c.From.Address = "send\r\ner@fixture.invalid"
			c.To[0].Name += "\n"
			c.To[0].Address = "receiv\rer@fixture.invalid"
			c.Cc[0].Address = "co\npy@fixture.invalid"
			c.Bcc[0].Address = "bli\rnd@fixture.invalid"
			c.Subject += "\r\nX-Extra: value"
			c.InReplyTo += "\r\nX-Extra: value"
			c.References = "<pri\nor@fixture.invalid>"
		}},
		{"whitespace-edges", func(c *Composition) {
			c.From = mail.Address{Name: " \t\rSéndér\n\t ", Address: "\r \tsender@fixture.invalid\n\t "}
			c.To = []mail.Address{{Name: "\t\rRéceiver\n ", Address: " \treceiver@fixture.invalid\r\n"}, {Name: " \t ", Address: "\tsecond@fixture.invalid "}}
			c.Cc = []mail.Address{{Name: " \tCópÿ\r\n ", Address: "\rcopy@fixture.invalid\n\t "}}
			c.Bcc = []mail.Address{{Name: "\r\tBlínd\n ", Address: " \tblind@fixture.invalid\r\n"}}
			c.Subject = " \t\rRésumé\n\t "
			c.InReplyTo = "\r \t<reply@fixture.invalid>\n\t "
			c.References = "\r<first@fixture.invalid>\n<second@fixture.invalid>\t"
		}},
		{"whitespace-only", func(c *Composition) {
			c.Subject = " \t\r\n \t"
			c.InReplyTo = " \t"
			c.References = "\r\n\t "
		}},
		{"empty", func(c *Composition) {
			*c = Composition{From: mail.Address{Address: "sender@fixture.invalid"}}
		}},
		{"subject-cannot-inject-recipient", func(c *Composition) {
			*c = Composition{From: mail.Address{Address: "sender@fixture.invalid"}, Subject: "a\r\nBcc: x@example.com"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := compositionFixture()
			tc.edit(&c)
			m := readMessage(t, outgoingBytes(t, c))
			if m.Header.Get("MIME-Version") != "1.0" {
				t.Fatal("missing MIME version")
			}
			groups := map[string][]mail.Address{"From": {c.From}, "To": c.To, "Cc": c.Cc, "Bcc": c.Bcc}
			for key, expected := range groups {
				if len(expected) == 0 {
					if _, present := m.Header[key]; present {
						t.Fatalf("unexpected empty header %s", key)
					}
					continue
				}
				got, err := m.Header.AddressList(key)
				if err != nil {
					t.Fatal(err)
				}
				want := make([]*mail.Address, len(expected))
				for i, a := range expected {
					a.Name = clean(a.Name)
					a.Address = clean(a.Address)
					want[i] = &a
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s got=%v want=%v", key, got, want)
				}
			}
			for key, value := range map[string]string{"Subject": c.Subject, "In-Reply-To": c.InReplyTo, "References": c.References} {
				want := clean(value)
				if want == "" {
					if _, present := m.Header[key]; present {
						t.Fatalf("unexpected empty header %s", key)
					}
					continue
				}
				got := m.Header.Get(key)
				if key == "Subject" {
					var err error
					got, err = new(mime.WordDecoder).DecodeHeader(got)
					if err != nil {
						t.Fatal(err)
					}
				}
				if got != want {
					t.Fatalf("%s got=%q want=%q", key, got, want)
				}
			}
			allowed := map[string]bool{"Mime-Version": true, "From": true, "To": true, "Cc": true, "Bcc": true, "Subject": true, "In-Reply-To": true, "References": true, "Content-Type": true, "Content-Transfer-Encoding": true}
			for key := range m.Header {
				if !allowed[key] {
					t.Fatalf("unexpected header %q", key)
				}
			}
		})
	}
}

// R-N5DF-E62M
func TestOutgoingPlainText(t *testing.T) {
	for _, text := range []string{"", "a\rb", "a\nb", "a\r\nb", "λ\n尾\r", strings.Repeat("long line", 100)} {
		c := compositionFixture()
		c.Text = text
		m := readMessage(t, outgoingBytes(t, c))
		media, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		if media != "text/plain" || params["charset"] != "utf-8" {
			t.Fatalf("type=%q %v", media, params)
		}
		if got := decodedBody(t, m.Body, m.Header.Get("Content-Transfer-Encoding")); got != strings.ReplaceAll(text, "\r\n", "\n") {
			t.Fatalf("text got=%q want=%q", got, text)
		}
	}
}

// R-8TN0-0NRZ
func TestOutgoingMultipart(t *testing.T) {
	c := compositionFixture()
	data := []string{"binary\x00\xffdata", "", strings.Repeat("payload", 50)}
	specs := []struct{ name, kind string }{{"résumé\r\nX-Extra: value.bin", "application/example; hint=\"quoted\""}, {"empty.bin", ""}, {"third.bin", "text/plain; charset=utf-8\r\nX-Extra: value"}}
	for i, s := range specs {
		content := data[i]
		c.Attachments = append(c.Attachments, Part{Name: s.name, ContentType: s.kind, Bytes: int64(len(content)), Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(content)), nil }})
	}
	m := readMessage(t, outgoingBytes(t, c))
	media, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || media != "multipart/mixed" {
		t.Fatalf("media=%s err=%v", media, err)
	}
	r := multipart.NewReader(m.Body, params["boundary"])
	first, err := r.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	media, params, err = mime.ParseMediaType(first.Header.Get("Content-Type"))
	if err != nil || media != "text/plain" || params["charset"] != "utf-8" {
		t.Fatalf("text media=%s params=%v err=%v", media, params, err)
	}
	if decodedBody(t, first, first.Header.Get("Content-Transfer-Encoding")) != strings.ReplaceAll(c.Text, "\r\n", "\n") {
		t.Fatal("text changed")
	}
	strip := strings.NewReplacer("\r", " ", "\n", " ")
	for i, a := range c.Attachments {
		part, nextErr := r.NextPart()
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		kind, p, parseErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		want, wp, werr := mime.ParseMediaType(strip.Replace(a.ContentType))
		if werr != nil {
			want = "application/octet-stream"
			wp = map[string]string{}
		}
		if kind != want || !reflect.DeepEqual(p, wp) {
			t.Fatalf("media got=%s %v want=%s %v", kind, p, want, wp)
		}
		disposition, dp, derr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if derr != nil || disposition != "attachment" || dp["filename"] != strip.Replace(a.Name) {
			t.Fatalf("disposition=%s %v err=%v", disposition, dp, derr)
		}
		if part.Header.Get("Content-Transfer-Encoding") != "base64" {
			t.Fatal("encoding")
		}
		payload, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if e != nil {
			t.Fatal(e)
		}
		if string(payload) != data[i] {
			t.Fatal("attachment changed")
		}
		for key := range part.Header {
			if key != "Content-Type" && key != "Content-Disposition" && key != "Content-Transfer-Encoding" {
				t.Fatalf("injected %q", key)
			}
		}
	}
	if _, err = r.NextPart(); !errors.Is(err, io.EOF) {
		t.Fatalf("extra part: %v", err)
	}
}

// R-I3GL-HFI5
func TestOutgoingBodyLineLengths(t *testing.T) {
	for _, attachment := range []bool{false, true} {
		c := compositionFixture()
		c.Text = strings.Repeat("x", 1000)
		if attachment {
			c.Attachments = []Part{{Name: "large.bin", Bytes: 1001, Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(strings.Repeat("y", 1001))), nil }}}
		}
		m := readMessage(t, outgoingBytes(t, c))
		media, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		check := func(r io.Reader) {
			body, e := io.ReadAll(r)
			if e != nil {
				t.Fatal(e)
			}
			for _, line := range bytes.Split(body, []byte("\r\n")) {
				if len(line) > 76 {
					t.Fatalf("line length=%d", len(line))
				}
			}
		}
		if media == "text/plain" {
			check(m.Body)
		} else {
			r := multipart.NewReader(m.Body, params["boundary"])
			for {
				p, e := r.NextPart()
				if errors.Is(e, io.EOF) {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				check(p)
			}
		}
	}
}

type finalFailureStream struct{ err error }

func (s finalFailureStream) Read(p []byte) (int, error) { return copy(p, "abc"), s.err }
func (s finalFailureStream) Close() error               { return nil }

type failingStream struct{ err error }

func (s failingStream) Read([]byte) (int, error) { return 0, s.err }
func (s failingStream) Close() error             { return nil }

// R-I4OH-V78U
func TestOutgoingLazyOpeningAndClosing(t *testing.T) {
	c := compositionFixture()
	data := []string{strings.Repeat("first", 20), "second"}
	var streams []*countedStream
	opens := make([]int, 2)
	yielded := 0
	baseline := c
	for _, s := range data {
		content := s
		baseline.Attachments = append(baseline.Attachments, Part{Name: "file", Bytes: int64(len(content)), Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(content)), nil }})
	}
	b := outgoingBytes(t, baseline)
	offsets := []int{bytes.Index(b, []byte(base64.StdEncoding.EncodeToString([]byte(data[0][:57])))), bytes.Index(b, []byte(base64.StdEncoding.EncodeToString([]byte(data[1]))))}
	for i, s := range data {
		content := s
		c.Attachments = append(c.Attachments, Part{Name: "file", Bytes: int64(len(content)), Open: func() (io.ReadCloser, error) {
			if yielded != offsets[i] {
				t.Fatalf("opened part %d at %d want %d", i, yielded, offsets[i])
			}
			opens[i]++
			stream := &countedStream{Reader: strings.NewReader(content)}
			streams = append(streams, stream)
			return stream, nil
		}})
	}
	o := Build(c)
	buf := make([]byte, 13)
	for {
		n, err := o.Read(buf)
		yielded += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	for i, s := range streams {
		if opens[i] != 1 || s.closes != 1 {
			t.Fatalf("part %d opens=%d closes=%d", i, opens[i], s.closes)
		}
	}
	opens = make([]int, 2)
	streams = nil
	yielded = 0
	o = Build(c)
	for opens[0] == 0 {
		n, err := o.Read(buf)
		yielded += n
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if opens[1] != 0 || streams[0].closes != 1 {
		t.Fatal("close lifecycle")
	}
}

// R-I4OH-V78U
// R-I8C7-0IGX (builder side)
func TestOutgoingStreamErrors(t *testing.T) {
	sentinel := errors.New("synthetic stream fault")
	cases := []struct {
		name   string
		length int64
		open   func() (io.ReadCloser, error)
		want   error
	}{{"short", 4, func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("abc")), nil }, ErrPartSize}, {"long", 2, func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("abc")), nil }, ErrPartSize}, {"empty-long", 0, func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("a")), nil }, ErrPartSize}, {"open", 3, func() (io.ReadCloser, error) { return nil, sentinel }, sentinel}, {"read", 3, func() (io.ReadCloser, error) { return failingStream{sentinel}, nil }, sentinel},
		{"final-read", 3, func() (io.ReadCloser, error) { return finalFailureStream{sentinel}, nil }, sentinel}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opens := 0
			c := compositionFixture()
			c.Attachments = []Part{{Name: "file", Bytes: tc.length, Open: func() (io.ReadCloser, error) { opens++; return tc.open() }}}
			o := Build(c)
			_, err := io.ReadAll(o)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			_, again := o.Read(make([]byte, 1))
			if !errors.Is(again, tc.want) || opens != 1 {
				t.Fatal("failure retried stream")
			}
			if err = o.Close(); err != nil {
				t.Fatal(err)
			}
			if errors.Is(tc.want, ErrPartSize) {
				for _, other := range []error{ErrNotFound, ErrUnauthorized, ErrTooLarge, ErrBadPage, ErrProvider} {
					if errors.Is(again, other) {
						t.Fatal("ambiguous classification")
					}
				}
			}
		})
	}
}

// R-2BVJ-C5DD (builder side)
func TestOutgoingErrorsHideComposition(t *testing.T) {
	c := compositionFixture()
	c.From.Name = "private sender name"
	c.To[0].Name = "private recipient name"
	c.Subject = "private subject content"
	c.Text = "private body content"
	c.InReplyTo = "private reply identifier"
	c.References = "private reference identifiers"
	secrets := []string{c.Subject, c.Text, c.InReplyTo, c.References, c.From.Name, c.From.Address, c.To[0].Name, c.To[0].Address, c.Cc[0].Address, c.Bcc[0].Address, "private filename.bin", "application/private-content"}
	cause := errors.New(strings.Join(secrets, ";"))
	for _, openFailure := range []bool{false, true} {
		c.Attachments = []Part{{Name: secrets[len(secrets)-2], ContentType: secrets[len(secrets)-1], Bytes: 5, Open: func() (io.ReadCloser, error) {
			if openFailure {
				return nil, cause
			}
			return failingStream{cause}, nil
		}}}
		o := Build(c)
		_, err := io.ReadAll(o)
		if !errors.Is(err, cause) {
			t.Fatalf("cause lost: %v", err)
		}
		for _, secret := range secrets {
			if len(secret) >= 8 && strings.Contains(err.Error(), secret) {
				t.Fatal("composition leaked")
			}
		}
		if err = o.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
