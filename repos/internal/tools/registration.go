package tools

import "github.com/ikigenba/ikigenba/appkit/mcp"

const listDescription = "The repositories you own, by name.\n\nTakes no arguments. Each repository has its id, its name, size_bytes, its size on disk, head, the sha its main branch points at (absent before the first push), and available, false when repos found it damaged at startup and will not serve it. Use show for one repository's clone URL."

const showDescription = "One of your repositories, with its clone URL and how to give git your token.\n\nPass repo, the repository's id or its name. The result has its id, name, default_branch (always main), head (absent before the first push), size_bytes, available, created, clone_url, and credentials. The clone URL never holds a credential: credentials tells how to give git your personal access token without putting it in the URL or on a command line. Do not use credential.helper store, which writes the token to disk."

const statusDescription = "How busy repos is, and how close each of your repositories is to its size limit.\n\nTakes no arguments. read covers clones and fetches, write covers pushes and maintenance: slots is how many run at once, active how many are running, and queued how many wait for a slot or their repository's lock. repos lists each of your repositories with size_bytes, limit_bytes, the size at which pushes to it are refused, available, and busy, true while a git operation or maintenance runs on it; delete is refused while busy."

const createDescription = "Create an empty repository and return it, with its clone URL and how to give git your token.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, and must not already be one of your repositories. The repository starts with no commits; its default branch is main. Push to clone_url to fill it. The result is what show returns: credentials tells how to give git your personal access token without putting it in the URL or on a command line. Do not use credential.helper store, which writes the token to disk."

const renameDescription = "Give one of your repositories a new name; its id does not change.\n\nPass repo, its id or current name, and name, the new name, under the rules of create. The clone URL follows the name, so a clone made under the old name must have its remote's URL updated before it can fetch or push again. The result is what show returns."

const deleteDescription = "Delete one of your repositories and everything in it.\n\nPass repo, its id or name. The repository and its history are gone for good; this cannot be undone. Refused while a git operation or maintenance runs on it; check busy with status and try again once it is false. The result is the id and name of the deleted repository."

// Register adds the six repository tools in their published order.
func Register(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, mcp.Tool[emptyArgs, listOutput]{Name: "list", Description: listDescription, Effect: mcp.Read, Handler: cfg.list})
	mcp.AddTool(srv, mcp.Tool[repoArgs, repositoryObject]{Name: "show", Description: showDescription, Effect: mcp.Read, Handler: cfg.show})
	mcp.AddTool(srv, mcp.Tool[emptyArgs, statusOutput]{Name: "status", Description: statusDescription, Effect: mcp.Read, Handler: cfg.status})
	mcp.AddTool(srv, mcp.Tool[createArgs, repositoryObject]{Name: "create", Description: createDescription, Effect: mcp.Additive, Handler: cfg.create})
	mcp.AddTool(srv, mcp.Tool[renameArgs, repositoryObject]{Name: "rename", Description: renameDescription, Effect: mcp.Destructive, Handler: cfg.rename})
	mcp.AddTool(srv, mcp.Tool[repoArgs, deleteOutput]{Name: "delete", Description: deleteDescription, Effect: mcp.Destructive, Handler: cfg.delete})
}
