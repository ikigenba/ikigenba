package toolkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/agentkit"
)

type writeInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Path of the file to write"`
	Content  string `json:"content" jsonschema:"required,description=Content to write to the file"`
}

// Write returns a tool that replaces a file's contents beneath root.
func Write(root string) (agentkit.Tool, error) {
	root, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	return agentkit.NewTool[writeInput]("Write", "Write the entire contents of a file", capOutput(func(_ context.Context, input writeInput) (string, error) {
		path, err := resolveSearchPath(root, "file_path", input.FilePath)
		if err != nil {
			return "", err
		}
		mode := os.FileMode(0o644)

		stat, err := os.Stat(path)
		switch {
		case err == nil:
			if stat.IsDir() {
				return "", fmt.Errorf("file_path %q is a directory", input.FilePath)
			}
			mode = stat.Mode()
		case os.IsNotExist(err):
			// The file and any missing parent directories are created below.
		default:
			return "", fmt.Errorf("file_path %q: %w", input.FilePath, err)
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return "", fmt.Errorf("file_path %q: create parent directories: %w", input.FilePath, err)
		}
		if err := os.WriteFile(path, []byte(input.Content), mode); err != nil {
			return "", fmt.Errorf("file_path %q: %w", input.FilePath, err)
		}

		// This response deliberately preserves the supplied path verbatim as part
		// of the tool's specified data format; it is not a diagnostic.
		return fmt.Sprintf("wrote %d bytes to %s", len(input.Content), input.FilePath), nil
	}))
}
