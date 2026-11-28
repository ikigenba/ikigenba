package machine

import (
	"bytes"
	"io"
)

// UserToken and AccountToken mark placeholders in a home template.
const (
	UserToken    = "<user>"
	AccountToken = "<account>"
)

// FillHome substitutes the user's name and account slug while reading r.
func FillHome(r io.Reader, user, account string) io.Reader {
	return &homeReader{source: r, user: user, account: account}
}

type homeReader struct {
	source        io.Reader
	user, account string
	replacement   string
	buffer        [4096]byte
	start, end    int
	err           error
}

func (r *homeReader) Read(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		if r.replacement != "" {
			n := copy(p[written:], r.replacement)
			r.replacement = r.replacement[n:]
			written += n
			continue
		}
		unread := r.buffer[r.start:r.end]
		if len(unread) == 0 {
			if r.err != nil {
				return written, r.err
			}
			if written > 0 || !r.readMore() {
				return written, nil
			}
			continue
		}
		if marker := bytes.IndexByte(unread, '<'); marker != 0 {
			if marker < 0 {
				marker = len(unread)
			}
			n := copy(p[written:], unread[:marker])
			r.start += n
			written += n
			continue
		}
		token, replacement := r.token(unread)
		if token != "" && bytes.HasPrefix(unread, []byte(token)) {
			r.start += len(token)
			r.replacement = replacement
			continue
		}
		if r.err == nil && (len(unread) == 1 || bytes.HasPrefix([]byte(token), unread)) {
			if written > 0 || !r.readMore() {
				return written, nil
			}
			continue
		}
		p[written] = '<'
		written++
		r.start++
	}
	return written, nil
}

func (r *homeReader) readMore() bool {
	// Only an incomplete token can remain when more input is required.
	r.end = copy(r.buffer[:], r.buffer[r.start:r.end])
	r.start = 0
	n, err := r.source.Read(r.buffer[r.end:])
	r.end += n
	r.err = err
	return n > 0 || err != nil
}

func (r *homeReader) token(unread []byte) (token, replacement string) {
	if len(unread) > 1 {
		switch unread[1] {
		case 'u':
			return UserToken, r.user
		case 'a':
			return AccountToken, r.account
		}
	}
	return "", ""
}
