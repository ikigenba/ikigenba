package tools

// Copy constants supply tool refusals and their format operands.
const (
	MissingSite     string = "no site named '%s'"
	InvalidName     string = "invalid name '%s'"
	InvalidPath     string = "invalid path '%s'"
	NoDirectory     string = "no directory '%s' at '%s'"
	InvalidRef      string = "invalid ref '%s'"
	NameTaken       string = "a site named '%s' already exists"
	NoRepository    string = "no repository '%s'"
	RepoUnavailable string = "repository '%s' is unavailable"
	NoCommit        string = "no commit for '%s'"
	TimedOut        string = "git took longer than %d seconds"
	TooLarge        string = "site exceeds %d bytes"
	BadVisibility   string = "visibility must be public or private"
	EmptyUpdate     string = "update needs at least one of visibility, listed, ref, path"
	NameOrClear     string = "apex takes name or clear, not both"
	ApexNotPublic   string = "apex site must be public"
	GitFailed       string = "git failed"
)

// ListDescription describes the paged catalog listing.
const ListDescription string = "The sites you own, by name, one page at a time.\n\nPass limit for the most entries to return, and the previous answer's next as page to continue, keeping the same limit. When next is absent the listing is complete. Each entry carries id, name, slug, url, visibility, listed, and commit when published. Use show for a site's repository and ref."

// ShowDescription describes the show tool.
const ShowDescription string = "One of your sites, with its URL, its repository, and what is published.\n\nPass name, the site's name. The result has its id, name, slug, url, repo (the id of the repos repository it is served from), ref (the ref it tracks), visibility, listed, commit (the sha it is published at), created, and published (when it was last published); commit and published are absent before the first publish. The path member names the directory served and is absent for the whole tree."

// CreateDescription describes the create tool.
const CreateDescription string = "Create a site from one of your repositories and return it; publish it to make it live.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, is none of about, mcp, api, or tools, and must not already name a site in the space: names are shared by every user, because a listed site answers at its name. repo is the id of one of your repositories in repos. ref is the branch, tag, or commit the site tracks, main unless given. visibility is public, served to anyone, or private, served only to users signed in to the space; public unless given. listed is true unless given: a listed site answers at its name and is on the landing page; an unlisted one answers at its name followed by '-' and 8 random hexadecimal digits, and is on the landing page only for you. The site serves nothing until you publish it. The result is what show returns. path is the directory of the repository to serve, a forward-slash relative path such as site/dist; the whole tree is the default. The directory is checked when you publish."

// PublishDescription describes the publish tool.
const PublishDescription string = "Publish one of your sites at a commit of its repository: the ref it tracks, or a ref or commit you name.\n\nPass name, and ref to publish a branch, tag, or commit sha other than the one the site tracks; a ref given here is used for this publish only and does not change the site's ref. The site serves the files of that commit exactly as git holds them, from the moment publish returns, and what it served before until then. Push to the repository, then publish again, to change what a site serves. The result is what show returns, with the new commit. The configured path, or a one-off path supplied here, serves only that directory as the site's root. The commit must contain a directory there."

// UpdateDescription describes the update tool.
const UpdateDescription string = "Change one of your sites' visibility, whether it is listed, the ref it tracks, or the directory it serves.\n\nPass name and at least one of visibility, listed, ref, and path, under the rules of create. Changing ref does not publish: call publish to serve the new ref. Changing listed never changes the slug, so a site keeps answering at the address it was created with. The apex site must stay public. The result is what show returns. You can also change path; an empty path resets it to the whole tree. Changing ref or path does not publish."
