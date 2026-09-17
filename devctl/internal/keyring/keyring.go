package keyring

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// NoValueError reports that neither the environment nor the keyring held a
// value for a name.
type NoValueError struct {
	Name string
}

func (err *NoValueError) Error() string {
	return fmt.Sprintf("no value for '%s' in the keyring or the environment", err.Name)
}

// Lookup returns the environment value for name, falling back to the system
// keyring when the environment does not provide one.
func Lookup(ctx context.Context, deps seam.Deps, name string) (string, error) {
	deps = deps.Defaults()

	if value := trimNewlines(deps.Getenv(name)); value != "" {
		return value, nil
	}

	result, err := deps.Exec(ctx, seam.Cmd{
		Path: "secret-tool",
		Args: []string{"lookup", "name", name},
		Dir:  deps.Dir,
	})
	if err != nil {
		return "", fmt.Errorf("secret-tool: %w", err)
	}
	value := trimNewlines(string(result.Stdout))
	if result.ExitCode != 0 || value == "" {
		return "", &NoValueError{Name: name}
	}
	return value, nil
}

func trimNewlines(value string) string {
	return strings.TrimRight(value, "\r\n")
}
