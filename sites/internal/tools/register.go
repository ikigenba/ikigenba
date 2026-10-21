package tools

import "github.com/ikigenba/ikigenba/appkit/mcp"

// Register registers the seven catalog tools in their advertised order.
func Register(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, mcp.Tool[ListArgs, SiteList]{Name: "list", Description: ListDescription, Effect: mcp.Read, Handler: cfg.list})
	mcp.AddTool(srv, mcp.Tool[ShowArgs, Site]{Name: "show", Description: ShowDescription, Effect: mcp.Read, Handler: cfg.show})
	mcp.AddTool(srv, mcp.Tool[CreateArgs, Site]{Name: "create", Description: CreateDescription, Effect: mcp.Additive, Handler: cfg.create})
	mcp.AddTool(srv, mcp.Tool[PublishArgs, Site]{Name: "publish", Description: PublishDescription, Effect: mcp.Additive, Handler: cfg.publish})
	mcp.AddTool(srv, mcp.Tool[UpdateArgs, Site]{Name: "update", Description: UpdateDescription, Effect: mcp.Additive, Handler: cfg.update})
	mcp.AddTool(srv, mcp.Tool[DeleteArgs, Deleted]{Name: "delete", Description: "Delete one of your sites; its repository is untouched.\n\nPass name. The site stops answering at once, its name is free for anyone to take, and if it was the apex site the apex is cleared. The repository and its history stay in repos. The result is the id of the deleted site.", Effect: mcp.Destructive, Handler: cfg.delete})
	mcp.AddRawTool(srv, mcp.RawTool[ApexArgs]{Name: "apex", Description: "Show, set or clear the site the space's apex domain redirects to.\n\nWith no arguments, the result is the apex site, or null when there is none. Pass name to make one of your public sites the apex, or clear true to clear it, whoever set it; not both. The result is the apex after the call.", Effect: mcp.Additive, Handler: cfg.apex})
}
