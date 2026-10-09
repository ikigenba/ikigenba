// Package tools offers sites' catalog through MCP.
package tools

import (
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

// Config supplies the shared catalog and runtime boundaries.
type Config struct {
	Store     *store.Store
	Cache     *cache.Cache
	Limits    *limits.Limits
	Telemetry *telemetry.Writer
}

// ListArgs requests the caller's sites.
type ListArgs struct{}

// ShowArgs names the caller's site.
type ShowArgs struct {
	Name string `json:"name" mcp:"required" description:"The site's name."`
}

// DeleteArgs names the site to remove.
type DeleteArgs struct {
	Name string `json:"name" mcp:"required" description:"The site's name."`
}

// CreateArgs supplies a new site's configuration.
type CreateArgs struct {
	Name       string  `json:"name" mcp:"required" description:"The new site's name: 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, not about, mcp, api, or tools, and not already a site's name in the space."`
	Repo       string  `json:"repo" mcp:"required" description:"The id of one of your repositories in repos (rep_ and 16 hexadecimal digits)."`
	Ref        *string `json:"ref" description:"The branch, tag, or commit the site tracks; main unless given."`
	Visibility *string `json:"visibility" description:"public or private; public unless given."`
	Listed     *bool   `json:"listed" description:"Whether the site is on the landing page and answers at its name; true unless given."`
}

// PublishArgs optionally selects a commit for one publish.
type PublishArgs struct {
	Name string  `json:"name" mcp:"required" description:"The site's name."`
	Ref  *string `json:"ref" description:"The branch, tag, or commit to publish, for this publish only; the site's own ref unless given."`
}

// UpdateArgs carries optional changes.
type UpdateArgs struct {
	Name       string  `json:"name" mcp:"required" description:"The site's name."`
	Visibility *string `json:"visibility" description:"public or private."`
	Listed     *bool   `json:"listed" description:"Whether the site is on the landing page."`
	Ref        *string `json:"ref" description:"The branch, tag, or commit the site tracks from now on."`
}

// ApexArgs reads, sets, or clears the apex.
type ApexArgs struct {
	Name  *string `json:"name" description:"The name of one of your public sites, to make the apex."`
	Clear *bool   `json:"clear" description:"true to clear the apex."`
}

// Site is the full public site object.
type Site struct {
	ID         string  `json:"id" mcp:"required"`
	Name       string  `json:"name" mcp:"required"`
	Slug       string  `json:"slug" mcp:"required"`
	URL        string  `json:"url" mcp:"required"`
	Repo       string  `json:"repo" mcp:"required"`
	Ref        string  `json:"ref" mcp:"required"`
	Visibility string  `json:"visibility" mcp:"required"`
	Listed     bool    `json:"listed" mcp:"required"`
	Commit     *string `json:"commit"`
	Created    string  `json:"created" mcp:"required"`
	Published  *string `json:"published"`
}

// ListedSite is a caller's listing entry.
type ListedSite struct {
	ID         string  `json:"id" mcp:"required"`
	Name       string  `json:"name" mcp:"required"`
	Slug       string  `json:"slug" mcp:"required"`
	URL        string  `json:"url" mcp:"required"`
	Visibility string  `json:"visibility" mcp:"required"`
	Listed     bool    `json:"listed" mcp:"required"`
	Commit     *string `json:"commit"`
}

// SiteList holds an ordered listing.
type SiteList struct {
	Sites []ListedSite `json:"sites" mcp:"required"`
}

// Deleted identifies the removed record.
type Deleted struct {
	Deleted bool   `json:"deleted" mcp:"required"`
	ID      string `json:"id" mcp:"required"`
}
