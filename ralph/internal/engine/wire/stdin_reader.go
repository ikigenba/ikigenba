package wire

import (
	"bufio"
	"io"
)

// MaxLineBytes is the per-line cap for NDJSON wire I/O (R-SJGQ-N1DV:
// ralph-loops' scanner buffer is 16 MiB).
const MaxLineBytes = 16 * 1024 * 1024

// StdinReader reads NDJSON `user` events from a stream.
//
// R-V0HE-KAIK: ralph-loops closes stdin when the iteration ends or is
// cancelled. ikigai-cli treats EOF on stdin as a signal that no further
// user input will arrive — Next returns io.EOF — but the iteration does
// not terminate on that signal; the caller (the driver) keeps running
// until it emits a `result` event. Subsequent calls to Next after EOF
// continue to return io.EOF rather than panicking or blocking.
type StdinReader struct {
	scanner *bufio.Scanner
	eof     bool
	err     error
	lastRaw []byte // copy of the most-recently scanned raw line
}

// NewStdinReader wraps r so Next returns one parsed user event per call.
func NewStdinReader(r io.Reader) *StdinReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), MaxLineBytes)
	return &StdinReader{scanner: sc}
}

// Next returns the next stdin user event, or io.EOF if stdin has been
// closed. After EOF, further calls keep returning io.EOF; the driver
// continues running until it emits the iteration's `result` event.
func (s *StdinReader) Next() (UserEvent, error) {
	if s.eof {
		return UserEvent{}, io.EOF
	}
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			s.err = err
			return UserEvent{}, err
		}
		s.eof = true
		return UserEvent{}, io.EOF
	}
	line := s.scanner.Bytes()
	s.lastRaw = append(s.lastRaw[:0], line...) // copy: scanner reuses its buffer
	return ParseStdinUserEvent(line)
}

// LastRaw returns a copy of the raw bytes from the most recent successful
// call to Next. Returns nil before the first successful scan.
func (s *StdinReader) LastRaw() []byte { return s.lastRaw }

// EOF reports whether stdin has been closed. Once true it stays true.
func (s *StdinReader) EOF() bool { return s.eof }
