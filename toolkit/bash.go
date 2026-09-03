package toolkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/ikigenba/ikigenba/agentkit"
)

type bashInput struct {
	Command string `json:"command" jsonschema:"required,minLength=1,description=Shell command to run"`
	Timeout *int   `json:"timeout,omitempty" jsonschema:"minimum=1,maximum=600000,description=Timeout in milliseconds"`
}

// Bash returns a tool that runs shell commands starting in root.
func Bash(root string) (agentkit.Tool, error) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf("bash: %w", err)
	}

	return agentkit.NewTool[bashInput]("Bash", "Run a shell command starting in the root directory; commands are not confined to the root", func(ctx context.Context, input bashInput) (string, error) {
		return runBash(ctx, bashPath, root, input)
	})
}

func runBash(ctx context.Context, bashPath, root string, input bashInput) (string, error) {
	// #nosec G204 -- executing the caller-supplied shell command is the tool's purpose.
	cmd := exec.CommandContext(ctx, bashPath, "-c", input.Command)
	cmd.Dir = root
	cmd.Env = os.Environ()

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	if err == nil {
		return output.String(), nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Sprintf("%s\n[exit code %d]", output.String(), exitErr.ExitCode()), nil
	}
	return "", fmt.Errorf("bash: %w", err)
}
