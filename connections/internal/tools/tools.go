// Package tools exposes connection metadata and person-facing connect links.
package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
)

// Connection status values and shared tool guidance.
const (
	StatusConnected       = "connected"
	StatusBroken          = "broken"
	StatusNotConnected    = "not_connected"
	CredentialGuide       = "No tool carries a credential: connect answers the URL a person opens to connect an account, and connectors get their tokens on the host."
	ConnectInstructions   = "Give this URL to the person to connect their account."
	listDescription       = "List your connections to outside providers."
	connectDescription    = "Get the link to connect an outside account."
	disconnectDescription = "Disconnect your account at an outside provider."
)

// Config supplies the provider table, connection store and broker.
type Config struct {
	Base      string
	Providers []providers.Provider
	Store     *store.Store
	Broker    *broker.Broker
}

// Entry describes one tool for the catalog.
type Entry struct{ Name, Description string }

// ListArgs takes no tool arguments.
type ListArgs struct{}

// ProviderArgs names the provider a tool acts on.
type ProviderArgs struct {
	Provider string `json:"provider" mcp:"required"`
}

// Listed contains public connection metadata for one provider.
type Listed struct {
	Provider    string   `json:"provider"`
	DisplayName string   `json:"display_name"`
	Status      string   `json:"status"`
	Account     *string  `json:"account"`
	Scopes      []string `json:"scopes"`
	Connected   *string  `json:"connected"`
}

// ConnectionList holds provider rows in table order.
type ConnectionList struct {
	Connections []Listed `json:"connections"`
}

// Link gives the connect address and person-facing instructions.
type Link struct {
	URL          string `json:"url"`
	Instructions string `json:"instructions"`
}

// Deleted identifies the disconnected connection.
type Deleted struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// UnknownProvider explains an unrecognized provider name.
func UnknownProvider(provider string) string { return fmt.Sprintf("Unknown provider: %s", provider) }

// NoConnection explains that the caller has no grant for a provider.
func NoConnection(provider string) string {
	return fmt.Sprintf("No connection for provider: %s", provider)
}

// Catalog returns the tool descriptions in registration order.
func Catalog() []Entry {
	return []Entry{{"list", listDescription + "\n\n" + CredentialGuide}, {"connect", connectDescription + "\n\n" + CredentialGuide}, {"disconnect", disconnectDescription + "\n\n" + CredentialGuide}}
}

// FirstLine returns the text preceding the first newline.
func FirstLine(description string) string { line, _, _ := strings.Cut(description, "\n"); return line }

// StatusOf reports whether a stored grant needs reconnecting.
func StatusOf(p providers.Provider, c store.Connection) string {
	if c.Status == store.StatusBroken {
		return StatusBroken
	}
	for _, s := range p.Scopes {
		if !slices.Contains(c.Scopes, p.GrantedName(s)) {
			return StatusBroken
		}
	}
	return StatusConnected
}

func known(cfg Config, name string) bool {
	for _, p := range cfg.Providers {
		if p.Name == name {
			return true
		}
	}
	return false
}

// Register adds the three caller-scoped tools to the server.
func Register(srv *mcp.Server, cfg Config) {
	catalog := Catalog()
	mcp.AddTool(srv, mcp.Tool[ListArgs, ConnectionList]{Name: catalog[0].Name, Description: catalog[0].Description, Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, _ ListArgs) (ConnectionList, error) {
		cons, err := cfg.Store.List(ctx, c.UserID)
		if err != nil {
			return ConnectionList{}, errors.New(store.Unreachable)
		}
		byProvider := make(map[string]store.Connection, len(cons))
		for _, con := range cons {
			byProvider[con.Provider] = con
		}
		out := ConnectionList{Connections: make([]Listed, 0, len(cfg.Providers))}
		for _, p := range cfg.Providers {
			row := Listed{Provider: p.Name, DisplayName: p.DisplayName, Status: StatusNotConnected, Scopes: []string{}}
			if con, ok := byProvider[p.Name]; ok {
				account := con.AccountEmail
				connected := con.Created.UTC().Format(time.RFC3339)
				row.Status = StatusOf(p, con)
				row.Account = &account
				row.Connected = &connected
				row.Scopes = append([]string{}, con.Scopes...)
			}
			out.Connections = append(out.Connections, row)
		}
		return out, nil
	}})
	mcp.AddTool(srv, mcp.Tool[ProviderArgs, Link]{Name: catalog[1].Name, Description: catalog[1].Description, Effect: mcp.Read, Handler: func(_ context.Context, _ identity.Caller, in ProviderArgs) (Link, error) {
		if !known(cfg, in.Provider) {
			return Link{}, errors.New(UnknownProvider(in.Provider))
		}
		return Link{URL: cfg.Base + "/" + in.Provider + "/connect", Instructions: ConnectInstructions}, nil
	}})
	mcp.AddTool(srv, mcp.Tool[ProviderArgs, Deleted]{Name: catalog[2].Name, Description: catalog[2].Description, Effect: mcp.Destructive, Handler: func(ctx context.Context, _ identity.Caller, in ProviderArgs) (Deleted, error) {
		if !known(cfg, in.Provider) {
			return Deleted{}, errors.New(UnknownProvider(in.Provider))
		}
		con, err := cfg.Broker.Disconnect(ctx, in.Provider)
		if errors.Is(err, store.ErrNotFound) {
			return Deleted{}, errors.New(NoConnection(in.Provider))
		}
		if err != nil {
			return Deleted{}, errors.New(store.Unreachable)
		}
		return Deleted{Deleted: true, ID: con.ID}, nil
	}})
}
