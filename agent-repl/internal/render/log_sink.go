package render

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"

	"github.com/ikigenba/ikigenba/agentkit"
)

// LogSink fans log bytes out to destinations and captures the latest summary.
type LogSink struct {
	mu      sync.Mutex
	dst     []io.Writer
	pending []byte
	usage   agentkit.Usage
	cost    agentkit.Cost
	has     bool
}

// NewLogSink returns a log sink writing to every destination in argument order.
func NewLogSink(dst ...io.Writer) *LogSink {
	return &LogSink{dst: append([]io.Writer(nil), dst...)}
}

// Write forwards p to every destination and inspects complete record lines.
func (s *LogSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	written := len(p)
	var writeErr error
	for _, dst := range s.dst {
		n, err := dst.Write(p)
		if writeErr == nil && (err != nil || n != len(p)) {
			written = n
			writeErr = err
			if writeErr == nil {
				writeErr = io.ErrShortWrite
			}
		}
	}

	s.pending = append(s.pending, p...)
	s.consumeLines()
	return written, writeErr
}

// Summary returns the usage and cost from the latest complete summary record.
func (s *LogSink) Summary() (agentkit.Usage, agentkit.Cost, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage, s.cost, s.has
}

func (s *LogSink) consumeLines() {
	for {
		newline := bytes.IndexByte(s.pending, '\n')
		if newline < 0 {
			return
		}
		line := s.pending[:newline]
		s.pending = s.pending[newline+1:]

		var record agentkit.LogRecord
		if err := json.Unmarshal(line, &record); err != nil || record.Type != agentkit.RecordSummary {
			continue
		}
		s.usage = agentkit.Usage{}
		s.cost = 0
		if record.Usage != nil {
			s.usage = *record.Usage
		}
		if record.Cost != nil {
			s.cost = *record.Cost
		}
		s.has = true
	}
}

var _ io.Writer = (*LogSink)(nil)
