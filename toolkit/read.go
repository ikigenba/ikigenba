package toolkit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/agentkit"
)

type readInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Path of the file to read"`
	Offset   *int   `json:"offset,omitempty" jsonschema:"minimum=1,description=One-based line number to start reading"`
	Limit    *int   `json:"limit,omitempty" jsonschema:"minimum=1,description=Maximum number of lines to return"`
}

// Read returns a tool that reads numbered lines from files beneath root.
func Read(root string) (agentkit.Tool, error) {
	root, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	return agentkit.NewTool[readInput]("Read", "Read a file with line numbers", capOutput(func(_ context.Context, input readInput) (string, error) {
		contents, err := readTextFile(root, input.FilePath)
		if err != nil {
			return "", err
		}
		if len(contents) == 0 {
			// This response deliberately preserves the supplied path verbatim as
			// part of the tool's specified data format; it is not a diagnostic.
			return fmt.Sprintf("%s is an empty file", input.FilePath), nil
		}

		offset := 1
		if input.Offset != nil {
			offset = *input.Offset
		}
		limit := 2000
		if input.Limit != nil {
			limit = *input.Limit
		}

		return renderLines(contents, input.FilePath, offset, limit)
	}))
}

func readTextFile(root, filePath string) ([]byte, error) {
	path, err := resolveSearchPath(root, "file_path", filePath)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file_path %q does not exist", filePath)
		}
		return nil, fmt.Errorf("file_path %q: %w", filePath, err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("file_path %q is a directory", filePath)
	}

	// #nosec G304 -- resolveSearchPath confines path to the validated tool root.
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("file_path %q: %w", filePath, err)
	}
	if bytes.IndexByte(contents, 0) >= 0 || !utf8.Valid(contents) {
		return nil, fmt.Errorf("file_path %q does not contain valid text", filePath)
	}
	return contents, nil
}

func renderLines(contents []byte, filePath string, offset, limit int) (string, error) {
	allLines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	totalLines := len(allLines)
	if offset > totalLines {
		return "", fmt.Errorf("file_path %q: offset %d exceeds file's %d lines", filePath, offset, totalLines)
	}

	last := min(offset+limit-1, totalLines)
	lines := make([]string, 0, last-offset+2)
	for lineNumber := offset; lineNumber <= last; lineNumber++ {
		line := allLines[lineNumber-1]
		if len(line) > 2000 {
			line = line[:2000] + " [line truncated]"
		}
		lines = append(lines, fmt.Sprintf("%6d\t%s", lineNumber, line))
	}
	if last < totalLines {
		lines = append(lines, fmt.Sprintf("[showing lines %d-%d of %d]", offset, last, totalLines))
	}

	return strings.Join(lines, "\n"), nil
}
