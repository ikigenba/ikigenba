package repos

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Git is the single subprocess seam for every git operation owned by repos.
type Git interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
	RunIn(ctx context.Context, dir string, env []string, stdin io.Reader, stdout io.Writer, args ...string) error
}

// CommandGit runs the real git executable with a deliberately small environment.
type CommandGit struct {
	binary string
	home   string
}

func NewCommandGit(binary, home string) *CommandGit {
	if binary == "" {
		binary = "git"
	}
	return &CommandGit{binary: binary, home: home}
}

func (g *CommandGit) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	if err := g.RunIn(ctx, dir, nil, nil, &stdout, args...); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (g *CommandGit) RunIn(ctx context.Context, dir string, extraEnv []string, stdin io.Reader, stdout io.Writer, args ...string) error {
	command := exec.CommandContext(ctx, g.binary, args...)
	command.Dir = dir
	command.Stdin = stdin
	command.Stdout = stdout
	var stderr bytes.Buffer
	command.Stderr = &stderr
	command.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + g.home,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
	}, extraEnv...)
	if err := command.Run(); err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return nil
}
