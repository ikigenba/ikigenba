package runs

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"golang.org/x/sys/unix"
)

const usageLineLimit = 16777216

func addUsage(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}
func readUsage(path string) store.Usage {
	zero := store.Usage{}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return zero
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return zero
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	var line []byte
	oversize := false
	u := zero
	consume := func() {
		if !oversize {
			var r agentkit.LogRecord
			if json.Unmarshal(line, &r) == nil {
				switch r.Type {
				case agentkit.RecordToolResult:
					u.ToolCalls = addUsage(u.ToolCalls, 1)
				case agentkit.RecordUsage:
					if r.Usage != nil && r.Cost != nil && r.Usage.InputTokens >= 0 && r.Usage.CachedTokens >= 0 && r.Usage.OutputTokens >= 0 && r.Usage.ReasoningTokens >= 0 && *r.Cost >= 0 {
						u.Calls = addUsage(u.Calls, 1)
						u.InputTokens = addUsage(u.InputTokens, r.Usage.InputTokens)
						u.CachedTokens = addUsage(u.CachedTokens, r.Usage.CachedTokens)
						u.OutputTokens = addUsage(u.OutputTokens, r.Usage.OutputTokens)
						u.ReasoningTokens = addUsage(u.ReasoningTokens, r.Usage.ReasoningTokens)
						u.CostNanos = addUsage(u.CostNanos, int64(*r.Cost))
					}
				}
			}
		}
		line = line[:0]
		oversize = false
	}
	for {
		part, err := reader.ReadSlice('\n')
		body := part
		if len(part) > 0 && part[len(part)-1] == '\n' {
			body = part[:len(part)-1]
		}
		if !oversize {
			if len(line)+len(body) > usageLineLimit {
				oversize = true
				line = nil
			} else {
				line = append(line, body...)
			}
		}
		if err == nil {
			consume()
			continue
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(part) > 0 || len(line) > 0 || oversize {
				consume()
			}
			return u
		}
		return zero
	}
}
