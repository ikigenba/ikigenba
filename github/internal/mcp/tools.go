package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"appkit"

	gh "github/internal/gh"

	"eventplane/consumer"
)

const toolPrefix = ""

func tool(verb string) string { return toolPrefix + verb }

func toolDescriptors() []map[string]any {
	return []map[string]any{
		desc(tool("repos_list"), "List repositories in the configured GitHub organization.", obj(map[string]any{})),
		desc(tool("repo_get"), "Fetch one repository by name.", obj(map[string]any{"repo": descTyp("string", "repository name")}, "repo")),
		desc(tool("pr_list"), "List pull requests in a repository.", obj(map[string]any{"repo": descTyp("string", "repository name"), "state": descTyp("string", "optional state filter")}, "repo")),
		desc(tool("pr_get"), "Fetch one pull request with changed files.", obj(map[string]any{"repo": descTyp("string", "repository name"), "number": descTyp("integer", "pull request number")}, "repo", "number")),
		desc(tool("pr_comment"), "Create a pull request comment.", obj(map[string]any{"repo": descTyp("string", "repository name"), "number": descTyp("integer", "pull request number"), "body": descTyp("string", "comment body")}, "repo", "number", "body")),
		desc(tool("pr_review"), "Create a pull request review.", obj(map[string]any{"repo": descTyp("string", "repository name"), "number": descTyp("integer", "pull request number"), "event": descTyp("string", "review event"), "body": descTyp("string", "optional review body")}, "repo", "number", "event")),
		desc(tool("pr_merge"), "Merge a pull request.", obj(map[string]any{"repo": descTyp("string", "repository name"), "number": descTyp("integer", "pull request number"), "method": descTyp("string", "optional merge method")}, "repo", "number")),
		desc(tool("issue_list"), "List issues in a repository.", obj(map[string]any{"repo": descTyp("string", "repository name"), "state": descTyp("string", "optional state filter")}, "repo")),
		desc(tool("issue_get"), "Fetch one issue.", obj(map[string]any{"repo": descTyp("string", "repository name"), "number": descTyp("integer", "issue number")}, "repo", "number")),
		desc(tool("issue_create"), "Create an issue.", obj(map[string]any{"repo": descTyp("string", "repository name"), "title": descTyp("string", "issue title"), "body": descTyp("string", "optional issue body")}, "repo", "title")),
		desc(tool("issue_comment"), "Create an issue comment.", obj(map[string]any{"repo": descTyp("string", "repository name"), "number": descTyp("integer", "issue number"), "body": descTyp("string", "comment body")}, "repo", "number", "body")),
		desc(tool("issue_update"), "Update issue state, labels, or assignees.", obj(map[string]any{"repo": descTyp("string", "repository name"), "number": descTyp("integer", "issue number"), "state": descTyp("string", "optional issue state"), "labels": arrayTyp("string", "optional full label set"), "assignees": arrayTyp("string", "optional full assignee set")}, "repo", "number")),
		desc(tool("file_get"), "Fetch repository file content.", obj(map[string]any{"repo": descTyp("string", "repository name"), "path": descTyp("string", "file path"), "ref": descTyp("string", "optional git ref")}, "repo", "path")),
		desc(tool("file_put"), "Create or update repository file content.", obj(map[string]any{"repo": descTyp("string", "repository name"), "path": descTyp("string", "file path"), "message": descTyp("string", "commit message"), "content": descTyp("string", "UTF-8 file content"), "sha": descTyp("string", "optional current blob SHA")}, "repo", "path", "message", "content")),
		desc(tool("health"), "Return the service health envelope after proving GitHub App authentication.", obj(map[string]any{})),
		desc(tool("reflection"), "Report github's event graph edges. github publishes no events and subscribes to none.", obj(map[string]any{})),
	}
}

func desc(name, description string, schema map[string]any) map[string]any {
	return map[string]any{"name": name, "description": description, "inputSchema": schema}
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func descTyp(t, description string) map[string]any {
	return map[string]any{"type": t, "description": description}
}

func arrayTyp(itemType, description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": itemType}, "description": description}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (h *Handler) handleToolCall(ctx context.Context, w http.ResponseWriter, req jsonRPCRequest, id Identity) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeJSONRPCError(w, req.ID, -32602, "invalid params")
		return
	}
	res, err := h.dispatchTool(ctx, p.Name, p.Arguments, id)
	if err != nil {
		writeJSONRPCResult(w, req.ID, toolResultErr(err.Error()))
		return
	}
	writeJSONRPCResult(w, req.ID, res)
}

func (h *Handler) dispatchTool(ctx context.Context, name string, raw json.RawMessage, id Identity) (map[string]any, error) {
	switch name {
	case tool("repos_list"):
		return h.toolReposList(ctx)
	case tool("repo_get"):
		return h.toolRepoGet(ctx, raw)
	case tool("pr_list"):
		return h.toolPRList(ctx, raw)
	case tool("pr_get"):
		return h.toolPRGet(ctx, raw)
	case tool("pr_comment"):
		return h.toolPRComment(ctx, raw, id)
	case tool("pr_review"):
		return h.toolPRReview(ctx, raw, id)
	case tool("pr_merge"):
		return h.toolPRMerge(ctx, raw, id)
	case tool("issue_list"):
		return h.toolIssueList(ctx, raw)
	case tool("issue_get"):
		return h.toolIssueGet(ctx, raw)
	case tool("issue_create"):
		return h.toolIssueCreate(ctx, raw, id)
	case tool("issue_comment"):
		return h.toolIssueComment(ctx, raw, id)
	case tool("issue_update"):
		return h.toolIssueUpdate(ctx, raw, id)
	case tool("file_get"):
		return h.toolFileGet(ctx, raw)
	case tool("file_put"):
		return h.toolFilePut(ctx, raw, id)
	case tool("health"):
		return h.toolHealth(ctx)
	case tool("reflection"):
		return h.toolReflection()
	default:
		return nil, errors.New("unknown tool: " + name)
	}
}

func (h *Handler) toolReposList(ctx context.Context) (map[string]any, error) {
	repos, err := h.client.ReposList(ctx)
	return clientResult(repos, err)
}

func (h *Handler) toolRepoGet(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Repo string `json:"repo"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("repo", a.Repo); err != nil {
		return toolResultErr(err.Error()), nil
	}
	repo, err := h.client.RepoGet(ctx, a.Repo)
	return clientResult(repo, err)
}

func (h *Handler) toolPRList(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Repo  string `json:"repo"`
		State string `json:"state"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("repo", a.Repo); err != nil {
		return toolResultErr(err.Error()), nil
	}
	prs, err := h.client.PRList(ctx, a.Repo, a.State)
	return clientResult(prs, err)
}

func (h *Handler) toolPRGet(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a repoNumberArgs
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := a.validate("number"); err != nil {
		return toolResultErr(err.Error()), nil
	}
	pr, err := h.client.PRGet(ctx, a.Repo, a.Number)
	return clientResult(pr, err)
}

func (h *Handler) toolPRComment(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		Body   string `json:"body"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := validateRepoNumberBody(a.Repo, a.Number, "body", a.Body); err != nil {
		return toolResultErr(err.Error()), nil
	}
	h.logWrite(ctx, id, "pr_comment", a.Repo, a.Number, "")
	c, err := h.client.PRComment(ctx, a.Repo, a.Number, a.Body)
	return clientResult(c, err)
}

func (h *Handler) toolPRReview(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		Event  string `json:"event"`
		Body   string `json:"body"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := (repoNumberArgs{Repo: a.Repo, Number: a.Number}).validate("number"); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("event", a.Event); err != nil {
		return toolResultErr(err.Error()), nil
	}
	h.logWrite(ctx, id, "pr_review", a.Repo, a.Number, "")
	r, err := h.client.PRReview(ctx, a.Repo, a.Number, a.Event, a.Body)
	return clientResult(r, err)
}

func (h *Handler) toolPRMerge(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		Method string `json:"method"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := (repoNumberArgs{Repo: a.Repo, Number: a.Number}).validate("number"); err != nil {
		return toolResultErr(err.Error()), nil
	}
	h.logWrite(ctx, id, "pr_merge", a.Repo, a.Number, "")
	m, err := h.client.PRMerge(ctx, a.Repo, a.Number, a.Method)
	return clientResult(m, err)
}

func (h *Handler) toolIssueList(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Repo  string `json:"repo"`
		State string `json:"state"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("repo", a.Repo); err != nil {
		return toolResultErr(err.Error()), nil
	}
	issues, err := h.client.IssueList(ctx, a.Repo, a.State)
	return clientResult(issues, err)
}

func (h *Handler) toolIssueGet(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a repoNumberArgs
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := a.validate("number"); err != nil {
		return toolResultErr(err.Error()), nil
	}
	issue, err := h.client.IssueGet(ctx, a.Repo, a.Number)
	return clientResult(issue, err)
}

func (h *Handler) toolIssueCreate(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Repo  string `json:"repo"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("repo", a.Repo); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("title", a.Title); err != nil {
		return toolResultErr(err.Error()), nil
	}
	h.logWrite(ctx, id, "issue_create", a.Repo, 0, "")
	issue, err := h.client.IssueCreate(ctx, a.Repo, a.Title, a.Body)
	return clientResult(issue, err)
}

func (h *Handler) toolIssueComment(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		Body   string `json:"body"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := validateRepoNumberBody(a.Repo, a.Number, "body", a.Body); err != nil {
		return toolResultErr(err.Error()), nil
	}
	h.logWrite(ctx, id, "issue_comment", a.Repo, a.Number, "")
	c, err := h.client.IssueComment(ctx, a.Repo, a.Number, a.Body)
	return clientResult(c, err)
}

func (h *Handler) toolIssueUpdate(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Repo      string   `json:"repo"`
		Number    int      `json:"number"`
		State     string   `json:"state"`
		Labels    []string `json:"labels"`
		Assignees []string `json:"assignees"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := (repoNumberArgs{Repo: a.Repo, Number: a.Number}).validate("number"); err != nil {
		return toolResultErr(err.Error()), nil
	}
	h.logWrite(ctx, id, "issue_update", a.Repo, a.Number, "")
	issue, err := h.client.IssueUpdate(ctx, a.Repo, a.Number, gh.IssuePatch{
		State:     a.State,
		Labels:    a.Labels,
		Assignees: a.Assignees,
	})
	return clientResult(issue, err)
}

func (h *Handler) toolFileGet(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Repo string `json:"repo"`
		Path string `json:"path"`
		Ref  string `json:"ref"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("repo", a.Repo); err != nil {
		return toolResultErr(err.Error()), nil
	}
	if err := requireString("path", a.Path); err != nil {
		return toolResultErr(err.Error()), nil
	}
	f, err := h.client.FileGet(ctx, a.Repo, a.Path, a.Ref)
	return clientResult(f, err)
}

func (h *Handler) toolFilePut(ctx context.Context, raw json.RawMessage, id Identity) (map[string]any, error) {
	var a struct {
		Repo    string `json:"repo"`
		Path    string `json:"path"`
		Message string `json:"message"`
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return toolResultErr(err.Error()), nil
	}
	for name, value := range map[string]string{"repo": a.Repo, "path": a.Path, "message": a.Message} {
		if err := requireString(name, value); err != nil {
			return toolResultErr(err.Error()), nil
		}
	}
	h.logWrite(ctx, id, "file_put", a.Repo, 0, a.Path)
	f, err := h.client.FilePut(ctx, a.Repo, a.Path, gh.FilePut{
		Message: a.Message,
		Content: []byte(a.Content),
		SHA:     a.SHA,
	})
	return clientResult(f, err)
}

func (h *Handler) toolHealth(ctx context.Context) (map[string]any, error) {
	details := map[string]any{}
	if h.health != nil {
		d, err := h.health(ctx)
		if err != nil {
			env := appkit.Envelope(h.version, h.service, map[string]any{"error": err.Error()})
			env["status"] = "error"
			return toolResultJSON(env)
		}
		if d != nil {
			details = d
		}
	}
	return toolResultJSON(appkit.Envelope(h.version, h.service, details))
}

func (h *Handler) toolReflection() (map[string]any, error) {
	return toolResultJSON(map[string]any{
		"publishes":  []map[string]any{},
		"subscribes": renderSubscriptions(nil),
	})
}

func renderSubscriptions(provider func() []consumer.Subscription) []map[string]any {
	out := []map[string]any{}
	if provider == nil {
		return out
	}
	for _, s := range provider() {
		out = append(out, map[string]any{
			"source":      s.Source,
			"filter":      s.Filter,
			"description": s.Description,
		})
	}
	return out
}

func (h *Handler) logWrite(ctx context.Context, id Identity, verb, repo string, number int, path string) {
	args := []any{
		"owner_email", id.OwnerEmail,
		"client_id", id.ClientID,
		"verb", verb,
		"repo", repo,
	}
	if number > 0 {
		args = append(args, "number", number)
	}
	if path != "" {
		args = append(args, "path", path)
	}
	h.logger.InfoContext(ctx, "github write", args...)
}

type repoNumberArgs struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

func (a repoNumberArgs) validate(numberName string) error {
	if err := requireString("repo", a.Repo); err != nil {
		return err
	}
	if a.Number <= 0 {
		return fmt.Errorf("%s is required", numberName)
	}
	return nil
}

func validateRepoNumberBody(repo string, number int, bodyName, body string) error {
	if err := (repoNumberArgs{Repo: repo, Number: number}).validate("number"); err != nil {
		return err
	}
	return requireString(bodyName, body)
}

func requireString(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func clientResult(v any, err error) (map[string]any, error) {
	if err != nil {
		return toolResultErr(err.Error()), nil
	}
	return toolResultJSON(v)
}

func toolResultJSON(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return toolResultText(string(b)), nil
}
