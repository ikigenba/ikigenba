package bash_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ralph/internal/engine/tools/bash"
)

// R-JBSB-OY94: Bash runs in the foreground only. Run must block until
// the subprocess exits before returning the tool_result, so the model
// only sees the result after completion. Verified by running a command
// that sleeps measurably and asserting (a) the post-sleep output is
// present in the returned content, and (b) elapsed wall time is at
// least the sleep duration.
func TestR_JBSB_OY94_BashForegroundOnly(t *testing.T) {
	const useID = "toolu_bash_fg"
	const sleepFor = 200 * time.Millisecond
	start := time.Now()
	res, err := bash.Run(context.Background(), "", useID, "sleep 0.2; echo done")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Block.IsError {
		t.Fatalf("IsError = true; want false")
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content, "done") {
		t.Fatalf("content missing post-sleep output; got %q", content)
	}
	if elapsed < sleepFor {
		t.Fatalf("Run returned in %v, less than sleep %v — did not block", elapsed, sleepFor)
	}
}

// R-JWIM-71UX: Bash enforces a per-invocation timeout. On timeout
// the process group is killed (so children spawned by the bash -c
// command are also torn down) and the returned tool_result is an
// error indicating the timeout. We shrink the timeout via a test
// hook so the suite doesn't wait 120s.
func TestR_JWIM_71UX_BashTimeout(t *testing.T) {
	restore := bash.SetTimeoutForTest(150 * time.Millisecond)
	defer restore()
	const useID = "toolu_bash_timeout"
	start := time.Now()
	res, err := bash.Run(context.Background(), "", useID, "sleep 5; echo should-not-appear")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Block.IsError {
		t.Fatalf("IsError = false; want true on timeout")
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(strings.ToLower(content), "timed out") {
		t.Fatalf("content missing timeout marker; got %q", content)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("Run waited %v; expected fast timeout", elapsed)
	}
}

// R-IR21-6UNB: Bash runs `bash -c <cmd>` and returns combined
// stdout+stderr as the tool_result content.
func TestR_IR21_6UNB_BashCombinedOutput(t *testing.T) {
	const useID = "toolu_bash_1"
	res, err := bash.Run(context.Background(), "", useID, "echo hello-out; echo hello-err 1>&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Block.Type != "tool_result" {
		t.Fatalf("Type = %q, want tool_result", res.Block.Type)
	}
	if res.Block.ToolUseID != useID {
		t.Fatalf("ToolUseID = %q, want %q", res.Block.ToolUseID, useID)
	}
	if res.Block.IsError {
		t.Fatalf("IsError = true; want false for successful command")
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content, "hello-out") || !strings.Contains(content, "hello-err") {
		t.Fatalf("content missing combined stdout+stderr; got %q", content)
	}
}

// R-LBQE-9F03: Bash surfaces the subprocess exit code in the
// tool_result body and treats non-zero exit as data, not is_error.
func TestR_LBQE_9F03_BashExitCodeInBody(t *testing.T) {
	const useID = "toolu_bash_exit"
	res, err := bash.Run(context.Background(), "", useID, "echo before-exit; exit 3")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Block.IsError {
		t.Fatalf("IsError = true; non-zero exit must not set is_error")
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content, "before-exit") {
		t.Fatalf("content missing stdout; got %q", content)
	}
	if !strings.Contains(content, "[exit: 3]") {
		t.Fatalf("content missing exit code marker; got %q", content)
	}

	res2, err := bash.Run(context.Background(), "", useID, "true")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var content2 string
	if err := json.Unmarshal(res2.Block.Content, &content2); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content2, "[exit: 0]") {
		t.Fatalf("content missing exit 0 marker; got %q", content2)
	}
}

// R-KM4I-88FI: Bash runs in the session cwd — the working directory
// ikigai-cli was launched in. The subprocess must inherit the parent
// process cwd; we verify by chdir-ing the test process to a tempdir
// and asserting `pwd` reports that same path.
func TestR_KM4I_88FI_BashRunsInSessionCwd(t *testing.T) {
	t.Chdir(t.TempDir())
	const useID = "toolu_bash_cwd"
	res, err := bash.Run(context.Background(), "", useID, "pwd")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Block.IsError {
		t.Fatalf("IsError = true; want false")
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// macOS tempdirs resolve through /private; compare via realpath.
	real, err := filepath.EvalSymlinks(wd)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if !strings.Contains(content, wd) && !strings.Contains(content, real) {
		t.Fatalf("pwd output %q does not contain session cwd %q (real %q)", content, wd, real)
	}
}

// R-LXOL-5ACL: Bash truncates combined output exceeding 30000 bytes
// and appends a visible truncation notice (not silent).
func TestR_LXOL_5ACL_BashOutputTruncated(t *testing.T) {
	const useID = "toolu_bash_trunc"
	// Emit ~40000 bytes via head from /dev/zero piped through tr.
	res, err := bash.Run(context.Background(), "", useID, "head -c 40000 /dev/zero | tr '\\0' x")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Block.IsError {
		t.Fatalf("IsError = true; want false")
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content, "[truncated:") {
		t.Fatalf("content missing truncation notice; got len=%d", len(content))
	}
	if !strings.Contains(content, "[exit: 0]") {
		t.Fatalf("content missing exit marker after truncation; got %q", content[len(content)-200:])
	}
	// Body shape: 30000 bytes of output + "\n[truncated: output exceeded 30000 bytes]\n[exit: 0]".
	// The output portion must be capped; total should be modest beyond 30000.
	if len(content) > 30000+200 {
		t.Fatalf("content not bounded; len=%d", len(content))
	}

	// Sanity: short output gets no truncation notice.
	res2, err := bash.Run(context.Background(), "", useID, "echo short")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var content2 string
	if err := json.Unmarshal(res2.Block.Content, &content2); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if strings.Contains(content2, "[truncated:") {
		t.Fatalf("short output should not carry truncation notice; got %q", content2)
	}
}

// R-EBGD-2Z08: Bash captures stdout and stderr as separate streams
// internally while the model-facing content remains their combination.
// Verified by running a command that emits distinct strings to each
// stream and asserting (a) the combined content contains both, (b)
// RunResult.Stdout contains only the stdout string, (c)
// RunResult.Stderr contains only the stderr string, and (d) neither
// stream bleeds into the other.
func TestR_EBGD_2Z08_BashSeparateStreamCapture(t *testing.T) {
	const useID = "toolu_bash_split"
	res, err := bash.Run(context.Background(), "", useID, "echo only-stdout; echo only-stderr 1>&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Block.IsError {
		t.Fatalf("Block.IsError = true; want false")
	}

	// Combined model-facing content must contain both strings.
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content, "only-stdout") {
		t.Errorf("combined content missing stdout string; got %q", content)
	}
	if !strings.Contains(content, "only-stderr") {
		t.Errorf("combined content missing stderr string; got %q", content)
	}

	// Stdout stream must contain only the stdout string.
	if !strings.Contains(res.Stdout, "only-stdout") {
		t.Errorf("Stdout missing stdout string; got %q", res.Stdout)
	}
	if strings.Contains(res.Stdout, "only-stderr") {
		t.Errorf("Stdout must not contain stderr string; got %q", res.Stdout)
	}

	// Stderr stream must contain only the stderr string.
	if !strings.Contains(res.Stderr, "only-stderr") {
		t.Errorf("Stderr missing stderr string; got %q", res.Stderr)
	}
	if strings.Contains(res.Stderr, "only-stdout") {
		t.Errorf("Stderr must not contain stdout string; got %q", res.Stderr)
	}

	// Interrupted must be false for a successful command.
	if res.Interrupted {
		t.Errorf("Interrupted = true; want false for successful command")
	}
}

// When a non-empty root is supplied, bash runs with cwd == root so the
// sandbox folder is its working directory. Compared via EvalSymlinks to
// tolerate /tmp symlinks.
func TestBashRunsInSandboxRoot(t *testing.T) {
	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	res, err := bash.Run(context.Background(), root, "toolu_root_cwd", "pwd")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Block.IsError {
		t.Fatalf("IsError = true; want false")
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content, realRoot) && !strings.Contains(content, root) {
		t.Fatalf("pwd %q does not report sandbox root %q (real %q)", content, root, realRoot)
	}
}

// An already-cancelled context causes Run to return promptly with an
// error result rather than executing the command to completion. We do
// not rely on wall-clock; the command would otherwise sleep for 5s.
func TestBashHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	res, err := bash.Run(ctx, "", "toolu_cancel", "sleep 5; echo should-not-appear")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Block.IsError {
		t.Fatalf("IsError = false; want true for a cancelled context")
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("Run waited %v; expected prompt return on cancelled ctx", elapsed)
	}
	var content string
	if err := json.Unmarshal(res.Block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content, "should-not-appear") {
		// Good: the command did not run to completion.
	} else {
		t.Fatalf("command appears to have run to completion: %q", content)
	}
}
