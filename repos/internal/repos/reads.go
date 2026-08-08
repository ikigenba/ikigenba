package repos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"registry"
)

type ReadEntry struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	BlobSHA    string `json:"blob_sha"`
	ContentURL string `json:"content_url,omitempty"`
}

type ListResult struct {
	Kind    string      `json:"kind"`
	Name    string      `json:"name"`
	Ref     string      `json:"ref"`
	Rev     string      `json:"rev"`
	Entries []ReadEntry `json:"entries"`
}

type ContentResult struct {
	Rev     string
	BlobSHA string
	Bytes   []byte
}

type ArchiveResult struct {
	Rev   string
	Bytes []byte
}

func (s *Service) ReadContent(ctx context.Context, kind, name, filePath, ref, pinnedRev string) (ContentResult, error) {
	repository, err := s.repositoryPath(kind, name)
	if err != nil {
		return ContentResult{}, err
	}
	filePath, err = validateTreePath(filePath, false)
	if err != nil {
		return ContentResult{}, err
	}
	ref = defaultRef(ref)
	rev, err := s.resolveRef(ctx, repository, ref)
	if err != nil {
		return ContentResult{}, err
	}
	if pinnedRev != "" && pinnedRev != rev {
		return ContentResult{}, fmt.Errorf("%w: ref %q resolved to %s, not pinned rev %s", ErrConflict, ref, rev, pinnedRev)
	}
	entry, err := s.treeEntry(ctx, repository, rev, ref, filePath)
	if err != nil {
		return ContentResult{}, err
	}
	if entry.Type != "file" {
		return ContentResult{}, fmt.Errorf("%w: no file at %q in %s", ErrNotFound, filePath, ref)
	}
	contents, err := s.custody.git.Run(ctx, repository, "cat-file", "blob", entry.BlobSHA)
	if err != nil {
		return ContentResult{}, fmt.Errorf("read blob %s: %w", entry.BlobSHA, err)
	}
	return ContentResult{Rev: rev, BlobSHA: entry.BlobSHA, Bytes: contents}, nil
}

func (s *Service) List(ctx context.Context, kind, name, ref, treePath string) (ListResult, error) {
	repository, err := s.repositoryPath(kind, name)
	if err != nil {
		return ListResult{}, err
	}
	ref = defaultRef(ref)
	result := ListResult{Kind: kind, Name: name, Ref: ref, Entries: []ReadEntry{}}
	rev, err := s.resolveRef(ctx, repository, ref)
	if err != nil {
		if errors.Is(err, ErrNotFound) && s.emptyRepository(ctx, repository) {
			return result, nil
		}
		return ListResult{}, err
	}
	result.Rev = rev
	treePath, err = validateTreePath(treePath, true)
	if err != nil {
		return ListResult{}, err
	}
	args := []string{"ls-tree", "-r", "-t", "-l", rev}
	if treePath != "" {
		args = append(args, "--", treePath)
	}
	output, err := s.custody.git.Run(ctx, repository, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list tree: %w", err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		entry, parseErr := parseTreeEntry(string(line))
		if parseErr != nil {
			return ListResult{}, parseErr
		}
		if treePath != "" && entry.Path == treePath {
			continue
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func (s *Service) Stat(ctx context.Context, kind, name, filePath, ref string) (ReadEntry, string, error) {
	repository, err := s.repositoryPath(kind, name)
	if err != nil {
		return ReadEntry{}, "", err
	}
	filePath, err = validateTreePath(filePath, false)
	if err != nil {
		return ReadEntry{}, "", err
	}
	ref = defaultRef(ref)
	rev, err := s.resolveRef(ctx, repository, ref)
	if err != nil {
		return ReadEntry{}, "", err
	}
	entry, err := s.treeEntry(ctx, repository, rev, ref, filePath)
	if err != nil {
		return ReadEntry{}, "", err
	}
	values := makeURLValues(kind, name, filePath, ref)
	entry.ContentURL = registry.BaseURL("repos") + "/content?" + values.Encode()
	return entry, rev, nil
}

func (s *Service) ReadArchive(ctx context.Context, kind, name, ref string) (ArchiveResult, error) {
	repository, err := s.repositoryPath(kind, name)
	if err != nil {
		return ArchiveResult{}, err
	}
	ref = defaultRef(ref)
	rev, err := s.resolveRef(ctx, repository, ref)
	if err != nil {
		return ArchiveResult{}, err
	}
	var archive bytes.Buffer
	if err := s.custody.git.RunIn(ctx, repository, nil, nil, &archive, "archive", "--format=tar", rev); err != nil {
		return ArchiveResult{}, fmt.Errorf("archive tree: %w", err)
	}
	return ArchiveResult{Rev: rev, Bytes: archive.Bytes()}, nil
}

func (s *Service) repositoryPath(kind, name string) (string, error) {
	if s == nil || s.custody == nil {
		return "", fmt.Errorf("repos: custody is not configured")
	}
	repository, err := s.custody.Path(kind, name)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(repository); errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("%w: repository %q/%q", ErrNotFound, kind, name)
	} else if statErr != nil {
		return "", statErr
	} else if !info.IsDir() {
		return "", fmt.Errorf("%w: repository %q/%q", ErrNotFound, kind, name)
	}
	return repository, nil
}

func (s *Service) resolveRef(ctx context.Context, repository, ref string) (string, error) {
	if strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
		return "", fmt.Errorf("%w: invalid ref %q", ErrValidation, ref)
	}
	output, err := s.custody.git.Run(ctx, repository, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: no ref %q", ErrNotFound, ref)
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *Service) emptyRepository(ctx context.Context, repository string) bool {
	output, err := s.custody.git.Run(ctx, repository, "for-each-ref", "--format=%(refname)")
	return err == nil && strings.TrimSpace(string(output)) == ""
}

func (s *Service) treeEntry(ctx context.Context, repository, rev, ref, filePath string) (ReadEntry, error) {
	output, err := s.custody.git.Run(ctx, repository, "ls-tree", "-l", rev, "--", filePath)
	if err != nil {
		return ReadEntry{}, fmt.Errorf("inspect tree: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		entry, parseErr := parseTreeEntry(line)
		if parseErr != nil {
			return ReadEntry{}, parseErr
		}
		if entry.Path == filePath {
			return entry, nil
		}
	}
	return ReadEntry{}, fmt.Errorf("%w: no entry at %q in %s", ErrNotFound, filePath, ref)
}

func parseTreeEntry(line string) (ReadEntry, error) {
	metadata, name, ok := strings.Cut(line, "\t")
	if !ok {
		return ReadEntry{}, fmt.Errorf("unexpected git ls-tree output %q", line)
	}
	fields := strings.Fields(metadata)
	if len(fields) != 4 {
		return ReadEntry{}, fmt.Errorf("unexpected git ls-tree metadata %q", metadata)
	}
	entry := ReadEntry{Path: name}
	switch fields[1] {
	case "blob":
		entry.Type = "file"
		entry.BlobSHA = fields[2]
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return ReadEntry{}, fmt.Errorf("parse blob size %q: %w", fields[3], err)
		}
		entry.Size = size
	case "tree":
		entry.Type = "dir"
	default:
		return ReadEntry{}, fmt.Errorf("unexpected git object type %q", fields[1])
	}
	return entry, nil
}

func validateTreePath(value string, allowEmpty bool) (string, error) {
	if value == "" && allowEmpty {
		return "", nil
	}
	if value == "" || strings.Contains(value, "\\") || strings.ContainsAny(value, "\x00\r\n") || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("%w: invalid tree path %q", ErrValidation, value)
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != value {
		return "", fmt.Errorf("%w: invalid tree path %q", ErrValidation, value)
	}
	return cleaned, nil
}

func defaultRef(ref string) string {
	if ref == "" {
		return "main"
	}
	return ref
}
