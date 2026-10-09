package panel

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/page"
)

// AboutData supplies the service description and page chrome.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// Tool supplies the name and description of a registered MCP tool.
type Tool struct {
	Name, Description string
}

// ToolsData supplies the registered tools and page chrome.
type ToolsData struct {
	Banner page.Banner
	Tools  []Tool
}

func (h *handler) renderInfo(w http.ResponseWriter, r *http.Request) {
	banner := h.pageData(r).Banner
	name := strings.TrimPrefix(r.URL.Path, "/")
	banner.Trail = []page.Level{{Name: name, URL: r.URL.Path}}
	if name == "about" {
		h.renderTemplate(w, r, http.StatusOK, name, AboutData{Banner: banner, Description: Description})
		return
	}
	registered := h.srv.Tools()
	list := make([]Tool, len(registered))
	for i, entry := range registered {
		list[i] = Tool{Name: entry.Name, Description: entry.Description}
	}
	h.renderTemplate(w, r, http.StatusOK, name, ToolsData{Banner: banner, Tools: list})
}
