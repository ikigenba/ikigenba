// Package tools registers the repository MCP tools.
package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

// Config supplies the shared repository catalog, limits and event writer.
type Config struct {
	Store     *store.Store
	Limits    *limits.Limits
	Telemetry *telemetry.Writer
}

type emptyArgs struct{}

type repoArgs struct {
	Repo string `json:"repo" mcp:"required" description:"The repository's id (rep_ and 16 hexadecimal digits) or its name."`
}

type createArgs struct {
	Name string `json:"name" mcp:"required" description:"The new repository's name: 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, not already one of your repositories."`
}

type renameArgs struct {
	Repo string `json:"repo" mcp:"required" description:"The repository's id (rep_ and 16 hexadecimal digits) or its name."`
	Name string `json:"name" mcp:"required" description:"The repository's new name, under the rules of create."`
}

type repositoryObject struct {
	ID            string  `json:"id" mcp:"required"`
	Name          string  `json:"name" mcp:"required"`
	DefaultBranch string  `json:"default_branch" mcp:"required"`
	Head          *string `json:"head"`
	SizeBytes     int64   `json:"size_bytes" mcp:"required"`
	Available     bool    `json:"available" mcp:"required"`
	Created       string  `json:"created" mcp:"required"`
	CloneURL      string  `json:"clone_url" mcp:"required"`
	Credentials   string  `json:"credentials" mcp:"required"`
}

type listingEntry struct {
	ID        string  `json:"id" mcp:"required"`
	Name      string  `json:"name" mcp:"required"`
	SizeBytes int64   `json:"size_bytes" mcp:"required"`
	Head      *string `json:"head"`
	Available bool    `json:"available" mcp:"required"`
}

type listOutput struct {
	Repos []listingEntry `json:"repos" mcp:"required"`
}

type deleteOutput struct {
	ID   string `json:"id" mcp:"required"`
	Name string `json:"name" mcp:"required"`
}

type usageOutput struct {
	Slots  int64 `json:"slots" mcp:"required"`
	Active int64 `json:"active" mcp:"required"`
	Queued int64 `json:"queued" mcp:"required"`
}

type statusEntry struct {
	ID         string `json:"id" mcp:"required"`
	Name       string `json:"name" mcp:"required"`
	SizeBytes  int64  `json:"size_bytes" mcp:"required"`
	LimitBytes int64  `json:"limit_bytes" mcp:"required"`
	Available  bool   `json:"available" mcp:"required"`
	Busy       bool   `json:"busy" mcp:"required"`
}

type statusOutput struct {
	Read  usageOutput   `json:"read" mcp:"required"`
	Write usageOutput   `json:"write" mcp:"required"`
	Repos []statusEntry `json:"repos" mcp:"required"`
}

// Unreachable is the refusal for an unreachable store.
const Unreachable string = "cannot reach the repositories; try again later"

// InvalidName is the naming rule refusal.
const InvalidName string = "must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit"

// NameTaken formats a taken-name refusal.
const NameTaken string = "'%s' is already one of your repositories"

// NoRepository formats a missing-repository refusal.
const NoRepository string = "no repository '%s'"

// Busy formats a busy-repository refusal.
const Busy string = "repository '%s' is busy; try again once its git operations finish"

var errUnreachable = errors.New(Unreachable)

func missingLine(ref string) string { return "repo: " + fmt.Sprintf(NoRepository, ref) }
func takenLine(name string) string  { return "name: " + fmt.Sprintf(NameTaken, name) }
func ruleRefusal(lines ...string) error {
	return errors.New("invalid arguments:\n" + strings.Join(lines, "\n"))
}

func optionalHead(head string) *string {
	if head == "" {
		return nil
	}
	return &head
}

func objectFromValues(ctx context.Context, r store.Repo, size int64, head string) repositoryObject {
	base, _ := clone.FromContext(ctx)
	return repositoryObject{
		ID: r.ID, Name: r.Name, DefaultBranch: store.DefaultBranch, Head: optionalHead(head),
		SizeBytes: size, Available: r.Available, Created: r.Created.UTC().Format("2006-01-02T15:04:05Z"),
		CloneURL: clone.URL(base, r.Name), Credentials: clone.Guidance(base).Text(),
	}
}
