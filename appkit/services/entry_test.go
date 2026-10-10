package services_test

import (
	"html/template"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/services"
)

func TestEntryFields(t *testing.T) {
	// R-SGLC-TV3B
	var icon template.HTML = "icon"
	entry := services.Entry{
		Name: "name", URL: "url", Description: "description", Socket: "socket",
		Enabled: true, MCP: false, Icon: icon, HasIcon: true, Group: "group",
	}
	// Assignment to this unnamed struct checks the exact field types and order.
	var shape struct {
		Name, URL, Description, Socket string
		Enabled, MCP                   bool
		Icon                           template.HTML
		HasIcon                        bool
		Group                          string
	} = entry
	if shape.Name != "name" || shape.URL != "url" || shape.Description != "description" || shape.Socket != "socket" || !shape.Enabled || shape.MCP || shape.Icon != icon || !shape.HasIcon || shape.Group != "group" {
		t.Fatalf("entry fields: %#v", entry)
	}
}
