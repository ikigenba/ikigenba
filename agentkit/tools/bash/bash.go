// Package bash implements the Bash tool exposed to the model.
package bash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"agentkit/wire"
)

// R-YNXM-CVXI: the tool's exposed name and input JSON schema match
// Claude Code's built-in Bash tool for the supported MVP subset
// (just `command`). Background execution, custom timeouts, sandbox
// flags, and descriptions are intentionally absent — ikigai-cli's
// Bash runs a fixed-policy foreground subprocess only.
const Name = "Bash"

// InputSchema is the JSON Schema advertised to the model for the
// Bash tool. Shape matches Claude Code's Bash schema for the
// supported subset.
var InputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "The command to execute"
    }
  },
  "required": ["command"]
}`)

// BashSidecar is the tool_use_result sidecar emitted alongside every
// Bash tool_result user event. R-DPI6-73NQ.
type BashSidecar struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Interrupted bool   `json:"interrupted"`
}

// RunResult carries the model-facing tool_result block plus the
// separately captured stdout, stderr, and interruption flag.
//
// R-EBGD-2Z08: stdout and stderr are captured as separate streams so
// the wire-level sidecar (R-DPI6-73NQ) can preserve the distinction
// for downstream consumers that render stderr differently from stdout.
// Block.Content combines them; what the model sees is unchanged.
type RunResult struct {
	Block       wire.ToolResultBlock
	Stdout      string
	Stderr      string
	Interrupted bool
}

// mutexWriter serialises concurrent writes from the stdout and stderr
// drain goroutines so the combined buffer is consistent.
type mutexWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *mutexWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// Run executes cmd via `bash -c` and returns a RunResult whose Block
// is correlated to toolUseID.
//
// R-IR21-6UNB: the Bash tool runs the command in a `bash -c`
// subprocess and returns combined stdout+stderr. The command is not
// parsed or sanitized; whatever the model supplies is what runs.
// R-LBQE-9F03: the exit code is appended to the content as a final
// `[exit: N]` line; non-zero exit is not an is_error — it is data.
// R-LXOL-5ACL: combined output is truncated to maxOutputBytes; when
// truncation occurs a visible `[truncated: ...]` notice is appended
// between the (truncated) output and the `[exit: N]` line.
// R-KM4I-88FI: the subprocess inherits the session cwd (the working
// directory ikigai-cli was launched in). exec.Command leaves Cmd.Dir
// empty by default, which causes the child to inherit the parent's
// cwd — that is precisely what this requirement specifies.
// R-JBSB-OY94: Bash runs in the foreground only — Run blocks until
// the subprocess exits, so the model only sees the result after
// completion. There is no background/async path.
// R-JWIM-71UX: each invocation is bounded by bashTimeout (120s by
// default). Setpgid puts the child in its own process group so a
// timeout can SIGKILL the whole group (-pgid), tearing down any
// grandchildren bash forked. On timeout Run returns an is_error
// tool_result indicating the timeout.
// R-EBGD-2Z08: stdout and stderr are captured as separate streams
// internally via io.MultiWriter; the combined buffer is rebuilt from
// both streams and is what the model receives.
func Run(ctx context.Context, root, toolUseID, cmd string) (RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "bash", "-c", cmd)
	if root != "" {
		c.Dir = root
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}

	// R-EBGD-2Z08: capture stdout and stderr into separate buffers
	// while also writing both to a shared combined buffer whose byte
	// order reflects true interleaving order via mutexWriter.
	var combined mutexWriter
	var stdoutBuf, stderrBuf bytes.Buffer
	c.Stdout = io.MultiWriter(&combined, &stdoutBuf)
	c.Stderr = io.MultiWriter(&combined, &stderrBuf)

	runErr := c.Run()
	interrupted := errors.Is(ctx.Err(), context.DeadlineExceeded)

	stdout := capStream(stdoutBuf.String())
	stderr := capStream(stderrBuf.String())

	if interrupted {
		block, err := wire.NewToolResultBlock(toolUseID, true,
			fmt.Sprintf("Bash: command timed out after %s", bashTimeout))
		return RunResult{Block: block, Stdout: stdout, Stderr: stderr, Interrupted: true}, err
	}
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); !ok {
			block, err := wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Bash: %v", runErr))
			return RunResult{Block: block, Stdout: stdout, Stderr: stderr}, err
		}
	}

	out := combined.buf.Bytes()
	truncated := false
	if len(out) > maxOutputBytes {
		out = out[:maxOutputBytes]
		truncated = true
	}
	var body string
	if truncated {
		body = fmt.Sprintf("%s\n[truncated: output exceeded %d bytes]\n[exit: %d]",
			string(out), maxOutputBytes, c.ProcessState.ExitCode())
	} else {
		body = fmt.Sprintf("%s\n[exit: %d]", string(out), c.ProcessState.ExitCode())
	}
	block, err := wire.NewToolResultBlock(toolUseID, false, body)
	return RunResult{Block: block, Stdout: stdout, Stderr: stderr}, err
}

// capStream caps a captured stream at maxOutputBytes to satisfy the
// per-stream limit described in R-DPI6-73NQ.
func capStream(s string) string {
	if len(s) > maxOutputBytes {
		return s[:maxOutputBytes]
	}
	return s
}

const maxOutputBytes = 30000

var bashTimeout = 120 * time.Second
