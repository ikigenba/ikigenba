package toolkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
)

type bashInput struct {
	Command string `json:"command" jsonschema:"required,minLength=1,description=Shell command to run"`
	Timeout *int   `json:"timeout,omitempty" jsonschema:"minimum=1,maximum=600000,description=Timeout in milliseconds"`
}

// Bash returns a tool that runs shell commands starting in root.
func Bash(root string) (agentkit.Tool, error) {
	root, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf("bash: %w", err)
	}

	return agentkit.NewTool[bashInput]("Bash", "Run a shell command starting in the root directory; commands are not confined to the root", capOutput(func(ctx context.Context, input bashInput) (string, error) {
		return runBash(ctx, bashPath, root, input)
	}))
}

func runBash(ctx context.Context, bashPath, root string, input bashInput) (string, error) {
	effectiveTimeout := 120000
	if input.Timeout != nil {
		effectiveTimeout = *input.Timeout
	}

	// #nosec G204 -- executing the caller-supplied shell command is the tool's purpose.
	cmd := exec.Command(bashPath, "-c", input.Command)
	cmd.Dir = root
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("bash: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timer := time.NewTimer(time.Duration(effectiveTimeout) * time.Millisecond)
	defer timer.Stop()

	select {
	case err := <-done:
		if err == nil {
			return output.String(), nil
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Sprintf("%s\n[exit code %d]", output.String(), exitErr.ExitCode()), nil
		}
		return "", fmt.Errorf("bash: %w", err)
	case <-ctx.Done():
		killProcessGroup(cmd.Process.Pid)
		<-done
		return "", fmt.Errorf("bash: %w: %s", ctx.Err(), output.String())
	case <-timer.C:
		killProcessGroup(cmd.Process.Pid)
		<-done
		return "", fmt.Errorf("command timed out after %d ms: %s", effectiveTimeout, output.String())
	}
}

func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
