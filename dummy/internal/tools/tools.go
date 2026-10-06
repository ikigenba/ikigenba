// Package tools registers the widget tools on an appkit MCP server.
package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

type widgetInput struct {
	Name   string        `json:"name" mcp:"required" description:"The widget's name: 1 to 40 characters after trimming, unique."`
	Count  int           `json:"count" mcp:"required" description:"How many: a whole number, zero or more."`
	Status widget.Status `json:"status" mcp:"required" description:"The widget's status."`
}

type widgetObject struct {
	ID     string        `json:"id" mcp:"required" description:"The widget's id, which names it in dummy's telemetry trail."`
	Name   string        `json:"name" mcp:"required" description:"The widget's name: 1 to 40 characters after trimming, unique."`
	Count  int           `json:"count" mcp:"required" description:"How many: a whole number, zero or more."`
	Status widget.Status `json:"status" mcp:"required" description:"The widget's status."`
}

type widgetList struct {
	Widgets []widgetObject `json:"widgets" mcp:"required" description:"Every widget, oldest first."`
}

func object(w widget.Widget) widgetObject {
	return widgetObject{ID: w.ID, Name: w.Name, Count: w.Count, Status: w.Status}
}

// Register adds the read-only listing and additive creation tools over s.
func Register(srv *mcp.Server, s *widget.Store, writer *telemetry.Writer) {
	mcp.AddTool(srv, mcp.Tool[struct{}, widgetList]{
		Name: "list_widgets", Description: "List the widgets, oldest first.", Effect: mcp.Read,
		Handler: func(ctx context.Context, _ identity.Caller, _ struct{}) (widgetList, error) {
			widgets, err := s.All(ctx)
			if err != nil {
				return widgetList{}, errors.New(widget.Unreachable)
			}
			out := widgetList{Widgets: make([]widgetObject, len(widgets))}
			for i, w := range widgets {
				out.Widgets[i] = object(w)
			}
			return out, nil
		},
	})
	mcp.AddTool(srv, mcp.Tool[widgetInput, widgetObject]{
		Name:        "create_widget",
		Description: "Create a widget and return it.\n\nThe name is trimmed of surrounding white space and must then be 1 to 40 characters and not already taken (letter case counts). The count is a whole number, zero or more. Every rule the arguments break is reported in one error, and nothing is created unless all of them hold.",
		Effect:      mcp.Additive,
		Handler: func(ctx context.Context, _ identity.Caller, in widgetInput) (widgetObject, error) {
			w, errs, err := s.Create(ctx, widget.Draft{Name: in.Name, Count: in.Count, Status: in.Status})
			if err != nil {
				return widgetObject{}, errors.New(widget.Unreachable)
			}
			if errs.Any() {
				lines := []string{"invalid arguments:"}
				if errs.Name != "" {
					lines = append(lines, "name: "+errs.Name)
				}
				if errs.Count != "" {
					lines = append(lines, "count: "+errs.Count)
				}
				return widgetObject{}, errors.New(strings.Join(lines, "\n"))
			}
			writer.Emit(ctx, "widget.created", telemetry.Attrs{"widget": w.ID})
			return object(w), nil
		},
	})
}
