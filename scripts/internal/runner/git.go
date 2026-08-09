package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"scripts/internal/script"
)

const credentialHelper = `!f(){ echo username=run; echo "password=$SUITE_GIT_TOKEN"; };f`

// materializeGit clones the script repository, detaches at the recorded pin,
// and configures credentials for git commands authored by the running script.
func (r *Runner) materializeGit(ctx context.Context, dir, sha string, sc script.Script) (string, error) {
	token, cloneURL, err := r.Plane.RunToken(ctx, sc.NameKey, r.ttl+5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("mint run token: %w", err)
	}
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "SUITE_GIT_TOKEN="+token)
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create runs dir: %w", err)
	}
	if err := gitStep(ctx, env, "clone", "-c", "credential.helper="+credentialHelper, "clone", "--quiet", cloneURL, dir); err != nil {
		return "", err
	}
	steps := []struct {
		name string
		args []string
	}{
		{"checkout", []string{"-C", dir, "checkout", "--quiet", "--detach", sha}},
		{"configure credential helper", []string{"-C", dir, "config", "credential.helper", credentialHelper}},
		{"configure user name", []string{"-C", dir, "config", "user.name", sc.Name}},
		{"configure user email", []string{"-C", dir, "config", "user.email", sc.OwnerEmail}},
	}
	for _, step := range steps {
		if err := gitStep(ctx, env, step.name, step.args...); err != nil {
			return "", err
		}
	}
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("open git exclude: %w", err)
	}
	_, writeErr := f.WriteString("\nsuite.py\nconfig.json\nstdout.log\nstderr.log\n")
	closeErr := f.Close()
	if writeErr != nil {
		return "", fmt.Errorf("write git exclude: %w", writeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close git exclude: %w", closeErr)
	}
	return token, nil
}

func gitStep(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("git %s: %s", name, message)
	}
	return nil
}
