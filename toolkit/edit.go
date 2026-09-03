package toolkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

type editInput struct {
	FilePath   string `json:"file_path" jsonschema:"required,description=Path of the file to edit"`
	OldString  string `json:"old_string" jsonschema:"required,minLength=1,description=Exact text to replace"`
	NewString  string `json:"new_string" jsonschema:"required,description=Replacement text"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace every occurrence instead of requiring a unique match"`
}

// Edit returns a tool that replaces exact text in an existing file beneath root.
func Edit(root string) (agentkit.Tool, error) {
	return agentkit.NewTool[editInput]("Edit", "Replace exact text in an existing file", func(_ context.Context, input editInput) (string, error) {
		if err := validateEdit(input); err != nil {
			return "", err
		}
		path, mode, contents, err := editableFile(root, input.FilePath)
		if err != nil {
			return "", err
		}
		replacedContents, replaced, err := editContents(contents, input)
		if err != nil {
			return "", err
		}
		// Path confinement is deliberately added by a later build phase.
		if err := os.WriteFile(path, replacedContents, mode); err != nil {
			return "", fmt.Errorf("file_path %q: %w", input.FilePath, err)
		}

		// This response deliberately preserves the supplied path verbatim as part
		// of the tool's specified data format; it is not a diagnostic.
		return fmt.Sprintf("replaced %d occurrence(s) of old_string in %s", replaced, input.FilePath), nil
	})
}

func validateEdit(input editInput) error {
	if input.OldString == input.NewString {
		return fmt.Errorf("old_string and new_string are identical")
	}
	return nil
}

func editableFile(root, filePath string) (string, os.FileMode, []byte, error) {
	// Path confinement is deliberately added by a later build phase.
	path := filepath.Clean(filepath.Join(root, filePath))
	stat, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return "", 0, nil, fmt.Errorf("file_path %q does not exist", filePath)
	case err != nil:
		return "", 0, nil, fmt.Errorf("file_path %q: %w", filePath, err)
	case stat.IsDir():
		return "", 0, nil, fmt.Errorf("file_path %q is a directory", filePath)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return "", 0, nil, fmt.Errorf("file_path %q: %w", filePath, err)
	}
	return path, stat.Mode(), contents, nil
}

func editContents(contents []byte, input editInput) ([]byte, int, error) {
	text := string(contents)
	count := strings.Count(text, input.OldString)
	if count == 0 {
		return nil, 0, fmt.Errorf("old_string not found in file_path %q", input.FilePath)
	}
	if count > 1 && !input.ReplaceAll {
		return nil, 0, fmt.Errorf("old_string occurs %d times in file_path %q; set replace_all or narrow old_string", count, input.FilePath)
	}
	if input.ReplaceAll {
		return []byte(strings.ReplaceAll(text, input.OldString, input.NewString)), count, nil
	}
	return []byte(strings.Replace(text, input.OldString, input.NewString, 1)), 1, nil
}
