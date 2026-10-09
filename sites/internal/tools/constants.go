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
