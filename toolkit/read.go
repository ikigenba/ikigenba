package toolkit

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

type readInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Path of the file to read"`
	Offset   *int   `json:"offset,omitempty" jsonschema:"minimum=1,description=One-based line number to start reading"`
	Limit    *int   `json:"limit,omitempty" jsonschema:"minimum=1,description=Maximum number of lines to return"`
}

// Read returns a tool that reads numbered lines from files beneath root.
func Read(root string) (agentkit.Tool, error) {
	return agentkit.NewTool[readInput]("Read", "Read a file with line numbers", func(_ context.Context, input readInput) (string, error) {
		// Path confinement is deliberately added by a later build phase.
		path := filepath.Clean(filepath.Join(root, input.FilePath))
		contents, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}

		offset := 1
		if input.Offset != nil {
			offset = *input.Offset
		}
		limit := 2000
		if input.Limit != nil {
			limit = *input.Limit
		}

		lines := make([]string, 0, limit)
		scanner := bufio.NewScanner(bytes.NewReader(contents))
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			if lineNumber < offset {
				continue
			}
			if len(lines) == limit {
				break
			}
			lines = append(lines, fmt.Sprintf("%6d\t%s", lineNumber, scanner.Text()))
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}

		return strings.Join(lines, "\n"), nil
	})
}
