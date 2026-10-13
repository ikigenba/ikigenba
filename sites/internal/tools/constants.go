package tools

// Copy constants supply tool refusals and their format operands.
const (
	MissingSite     string = "no site named '%s'"
	InvalidName     string = "invalid name '%s'"
	InvalidRef      string = "invalid ref '%s'"
	NameTaken       string = "a site named '%s' already exists"
	NoRepository    string = "no repository '%s'"
	RepoUnavailable string = "repository '%s' is unavailable"
	NoCommit        string = "no commit for '%s'"
	TimedOut        string = "git took longer than %d seconds"
	TooLarge        string = "site exceeds %d bytes"
	BadVisibility   string = "visibility must be public or private"
	EmptyUpdate     string = "update needs at least one of visibility, listed, ref"
	NameOrClear     string = "apex takes name or clear, not both"
	ApexNotPublic   string = "apex site must be public"
	GitFailed       string = "git failed"
)

// ListDescription describes the paged catalog listing.
const ListDescription string = "The sites you own, by name, one page at a time.\n\nPass limit for the most entries to return, and the previous answer's next as page to continue, keeping the same limit. When next is absent the listing is complete. Each entry carries id, name, slug, url, visibility, listed, and commit when published. Use show for a site's repository and ref."
