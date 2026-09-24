package session

import (
	"errors"
	"io"
	"io/fs"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/proc"
)

// Log incrementally reads complete lines from one file.
// Its zero value is ready for a first pass.
type Log struct {
	offset   int64
	fragment []byte
	id       proc.FileID
	hasID    bool
	passed   bool
}

// Read returns the complete lines gained in one pass over name.
func (l *Log) Read(fsys fs.FS, name string) (lines [][]byte, reset bool, err error) {
	file, err := fsys.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	reader, ok := file.(io.ReaderAt)
	if !ok {
		return nil, false, errors.New("log file does not support ReadAt")
	}

	size := info.Size()
	id, hasID := proc.FileIDOf(info)
	reset = l.passed && (size < l.offset || (hasID && l.hasID && id != l.id))
	start := l.offset
	if reset {
		start = 0
	}

	// Assemble this pass in fresh storage so previously returned lines remain
	// untouched when later passes replace or extend the log.
	data := make([]byte, 0, len(l.fragment))
	if !reset {
		data = append(data, l.fragment...)
	}
	const chunkSize int64 = 32 * 1024
	position := start
	for position < size {
		length := min(chunkSize, size-position)
		chunk := make([]byte, int(length))
		n, readErr := reader.ReadAt(chunk, position)
		if readErr != nil && readErr != io.EOF {
			return nil, false, readErr
		}
		if n < 0 || n > len(chunk) {
			return nil, false, errors.New("log file returned invalid ReadAt count")
		}
		data = append(data, chunk[:n]...)
		position += int64(n)
		if readErr == io.EOF {
			break
		}
		if n != len(chunk) {
			return nil, false, io.ErrNoProgress
		}
	}

	lines = Lines(data)
	lastNewline := len(data) - 1
	for lastNewline >= 0 && data[lastNewline] != '\n' {
		lastNewline--
	}
	var fragment []byte
	if lastNewline+1 < len(data) {
		fragment = append(fragment, data[lastNewline+1:]...)
	}
	l.offset = position
	l.fragment = fragment
	l.id = id
	l.hasID = hasID
	l.passed = true
	return lines, reset, nil
}
