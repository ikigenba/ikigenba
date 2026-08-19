package mcp

import (
	"appkit/server"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sites/internal/sites"
	"strings"
	"syscall"

	appkitmcp "appkit/mcp"

	sitefiles "sites/internal/files"
)

// toolFileList walks a site's current public/private directory and returns every
// regular file with its path (relative to the site root), size, and md5. An
// optional "path" scopes the walk to a subdirectory, confined to the site root; a missing
// scope dir yields an empty list rather than an error.
func (h *toolHandlers) toolFileList(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Site string `json:"site"`
		Path string `json:"path"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Site == "" {
		return errResultMsg(appkitmcp.ErrValidation, "missing required \"site\" argument"), nil
	}
	root, env := h.siteRoot(ctx, a.Site)
	if env != nil {
		return env, nil
	}

	scope := root
	if a.Path != "" {
		s, err := sitefiles.ConfinePath(root, a.Path)
		if err != nil {
			if errors.Is(err, sitefiles.ErrEscapes) {
				return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
			}
			return errResultMsg(appkitmcp.ErrInternal, "walk: "+err.Error()), nil
		}
		scope = s
	}

	listed, err := sitefiles.List(root, scope)
	if err != nil {
		if errors.Is(err, sitefiles.ErrEscapes) {
			return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
		}
		return errResultMsg(appkitmcp.ErrInternal, "walk: "+err.Error()), nil
	}
	files := make([]map[string]any, 0, len(listed))
	for _, f := range listed {
		files = append(files, map[string]any{"path": f.Path, "size": f.Size, "md5": f.Md5})
	}

	return appkitmcp.StructuredResult(map[string]any{"site": a.Site, "files": files})
}

// toolFileWrite writes content to a confined path in the site's current
// public/private directory, truncating by default or appending when append:true.
func (h *toolHandlers) toolFileWrite(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
	var a struct {
		Site     string `json:"site"`
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
		Append   bool   `json:"append"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Site == "" {
		return errResultMsg(appkitmcp.ErrValidation, "missing required \"site\" argument"), nil
	}
	root, env := h.siteRoot(ctx, a.Site)
	if env != nil {
		return env, nil
	}
	site, err := h.store.Get(ctx, a.Site)
	if err != nil {
		return errResult(err), nil
	}

	confined, path, err := confinedSitePath(root, a.FilePath)
	if err != nil {
		if errors.Is(err, sitefiles.ErrEscapes) {
			return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
		}
		return errResultMsg(appkitmcp.ErrInternal, "write: "+err.Error()), nil
	}
	if env := interactiveWritePathError("write", root, confined); env != nil {
		return env, nil
	}
	data := []byte(a.Content)
	if a.Append {
		existing, err := os.ReadFile(confined)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errResultMsg(appkitmcp.ErrInternal, "write: "+err.Error()), nil
		}
		data = append(existing, data...)
	}
	ctx = sites.WithVersionActor(ctx, id.ClientID)
	commit, err := h.version.Commit(ctx, a.Site, "write "+path, []sites.FileChange{{Path: repositoryPath(site.Path, path), Data: data}})
	if err != nil {
		return versionErrorResult(err), nil
	}
	if err := h.store.SetRepoSha(ctx, a.Site, commit.Sha); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "record repo sha: "+err.Error()), nil
	}
	if err := sitefiles.Write(root, path, string(data), false); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "write: "+err.Error()), nil
	}

	return appkitmcp.StructuredResult(map[string]any{"written": a.FilePath, "site": a.Site, "appended": a.Append})
}

func (h *toolHandlers) toolFileRead(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Site     string `json:"site"`
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Site == "" {
		return errResultMsg(appkitmcp.ErrValidation, "missing required \"site\" argument"), nil
	}
	root, env := h.siteRoot(ctx, a.Site)
	if env != nil {
		return env, nil
	}
	content, err := sitefiles.Read(root, a.FilePath, a.Offset, a.Limit)
	if err != nil {
		if errors.Is(err, sitefiles.ErrEscapes) {
			return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
		}
		return errResultMsg(appkitmcp.ErrInternal, "read: "+err.Error()), nil
	}
	return appkitmcp.TextResult(content), nil
}

func (h *toolHandlers) toolFileEdit(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
	var a struct {
		Site       string `json:"site"`
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Site == "" {
		return errResultMsg(appkitmcp.ErrValidation, "missing required \"site\" argument"), nil
	}
	root, env := h.siteRoot(ctx, a.Site)
	if env != nil {
		return env, nil
	}
	site, err := h.store.Get(ctx, a.Site)
	if err != nil {
		return errResult(err), nil
	}
	confined, path, err := confinedSitePath(root, a.FilePath)
	if err != nil {
		if errors.Is(err, sitefiles.ErrEscapes) {
			return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
		}
		return errResultMsg(appkitmcp.ErrInternal, "edit: "+err.Error()), nil
	}
	if env := interactiveWritePathError("edit", root, confined); env != nil {
		return env, nil
	}
	if a.OldString == "" {
		return errResultMsg(appkitmcp.ErrInternal, "edit: old string is required"), nil
	}
	content, err := os.ReadFile(confined)
	if err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "edit: "+err.Error()), nil
	}
	replaced := strings.Count(string(content), a.OldString)
	if replaced == 0 {
		return errResultMsg(appkitmcp.ErrInternal, "edit: old string not found"), nil
	}
	count := 1
	if a.ReplaceAll {
		count = -1
	} else {
		replaced = 1
	}
	data := []byte(strings.Replace(string(content), a.OldString, a.NewString, count))
	ctx = sites.WithVersionActor(ctx, id.ClientID)
	commit, err := h.version.Commit(ctx, a.Site, "edit "+path, []sites.FileChange{{Path: repositoryPath(site.Path, path), Data: data}})
	if err != nil {
		return versionErrorResult(err), nil
	}
	if err := h.store.SetRepoSha(ctx, a.Site, commit.Sha); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "record repo sha: "+err.Error()), nil
	}
	if err := os.WriteFile(confined, data, 0o644); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "edit: "+err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]any{"edited": a.FilePath, "site": a.Site, "replaced": replaced})
}

var errWrongTypedWritePath = errors.New("wrong-typed write path")

func interactiveWritePathError(operation, root, target string) map[string]any {
	err := validateInteractiveWritePath(root, target)
	if err == nil {
		return nil
	}
	if errors.Is(err, errWrongTypedWritePath) {
		return errResultMsg(appkitmcp.ErrValidation, operation+": "+err.Error())
	}
	return errResultMsg(appkitmcp.ErrInternal, operation+": "+err.Error())
}

func validateInteractiveWritePath(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	for path := target; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		switch {
		case err == nil && path == target && info.IsDir():
			return fmt.Errorf("%w: target is a directory", errWrongTypedWritePath)
		case err == nil && path != target && !info.IsDir():
			return fmt.Errorf("%w: %s is not a directory", errWrongTypedWritePath, path)
		case err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR):
			return err
		}
		if path == root {
			return nil
		}
	}
}

func confinedSitePath(root, raw string) (confined, relative string, resultErr error) {
	confined, err := sitefiles.ConfinePath(root, raw)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, confined)
	if err != nil {
		return "", "", err
	}
	return confined, filepath.ToSlash(rel), nil
}

func repositoryPath(root, rel string) string {
	if root == "" {
		return rel
	}
	return root + "/" + rel
}

func versionErrorResult(err error) map[string]any {
	if errors.Is(err, sites.ErrVersionUnavailable) {
		return errResultMsg(appkitmcp.ErrSourceUnavailable, "version commit: "+err.Error())
	}
	return errResultMsg(appkitmcp.ErrInternal, "version commit: "+err.Error())
}

func (h *toolHandlers) toolFileGlob(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Site    string `json:"site"`
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Site == "" {
		return errResultMsg(appkitmcp.ErrValidation, "missing required \"site\" argument"), nil
	}
	root, env := h.siteRoot(ctx, a.Site)
	if env != nil {
		return env, nil
	}
	matches, err := sitefiles.Glob(root, a.Pattern, a.Path)
	if err != nil {
		if errors.Is(err, sitefiles.ErrEscapes) {
			return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
		}
		return errResultMsg(appkitmcp.ErrInternal, "glob: "+err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]any{"site": a.Site, "matches": matches})
}

func (h *toolHandlers) toolFileGrep(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Site    string `json:"site"`
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Site == "" {
		return errResultMsg(appkitmcp.ErrValidation, "missing required \"site\" argument"), nil
	}
	root, env := h.siteRoot(ctx, a.Site)
	if env != nil {
		return env, nil
	}
	matches, err := sitefiles.Grep(root, a.Pattern, a.Path, a.Glob)
	if err != nil {
		if errors.Is(err, sitefiles.ErrEscapes) {
			return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
		}
		return errResultMsg(appkitmcp.ErrInternal, "grep: "+err.Error()), nil
	}
	out := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		out = append(out, map[string]any{"path": m.Path, "line": m.Line, "text": m.Text})
	}
	return appkitmcp.StructuredResult(map[string]any{"site": a.Site, "matches": out})
}

func (h *toolHandlers) siteRoot(ctx context.Context, slug string) (root string, result map[string]any) {
	site, err := h.store.Get(ctx, slug)
	if err != nil {
		return "", errResult(err)
	}
	return h.layout.SiteDir(site.Visibility, slug), nil
}
