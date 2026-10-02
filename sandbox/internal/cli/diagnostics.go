package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func printedName(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 32 || c == 127 {
			fmt.Fprintf(&b, "\\x%02x", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
func (r *invocation) diagnostic(message string) int {
	if _, err := fmt.Fprintf(r.stderr, "sandbox: %s\n", message); err != nil {
		return 1
	}
	return 2
}
func (r *invocation) runnerError(action string, err error) int {
	if _, writeErr := fmt.Fprintf(r.stderr, "sandbox: %s: %s\n", action, printedName(err.Error())); writeErr != nil {
		return 1
	}
	return 1
}
func (r *invocation) externalFailure(action string, result seam.Result, detail string) int {
	if _, err := fmt.Fprintf(r.stderr, "sandbox: %s: exit status %d\n", action, result.ExitCode); err != nil {
		return 1
	}
	if len(result.Output) > 0 {
		if _, err := fmt.Fprintln(r.stderr); err != nil {
			return 1
		}
		output := strings.TrimSuffix(string(result.Output), "\n")
		for _, line := range strings.Split(output, "\n") {
			if _, err := fmt.Fprintf(r.stderr, "> %s\n", line); err != nil {
				return 1
			}
		}
	}
	if detail != "" {
		if _, err := fmt.Fprintf(r.stderr, "\n%s\n", strings.TrimSuffix(detail, "\n")); err != nil {
			return 1
		}
	}
	return 1
}
func (r *invocation) fileError(path string, err error) int {
	var pe *fs.PathError
	var le *os.LinkError
	if errors.As(err, &pe) {
		err = pe.Err
	} else if errors.As(err, &le) {
		err = le.Err
	}
	if _, writeErr := fmt.Fprintf(r.stderr, "sandbox: %s: %s\n", path, err.Error()); writeErr != nil {
		return 1
	}
	return 1
}
