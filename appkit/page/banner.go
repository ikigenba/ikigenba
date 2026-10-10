// Package page provides the shared Ikigenba page chrome and browser assets.
package page

import (
	"html/template"
	"os"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/version"
)

// User describes the signed-in person and the app's account links.
type User struct {
	Email, ProfileURL, LogoutURL string
}

// Banner is the data rendered by the banner and footer templates.
type Banner struct {
	Service                                       string
	Icon                                          template.HTML
	Release, Commit, Email, ProfileURL, LogoutURL string
	Home                                          string
	Tools                                         bool
	Trail                                         []Level
}

// Level describes one step below the app's landing page.
type Level struct {
	Name, URL string
}

// Kit holds the app identity and the services path captured at construction.
type Kit struct {
	service, path string
	id            version.Identity
}

// New captures the host services path for the named app.
func New(service string, id version.Identity) *Kit {
	return &Kit{service: service, id: id, path: os.Getenv(services.Variable)}
}

// Banner reads the current services file and combines it with the user's data.
func (k *Kit) Banner(u User) Banner {
	banner := Banner{
		Service: k.service, Release: k.id.Release, Commit: k.id.Commit, Email: u.Email,
		ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL,
	}
	entries, err := services.Read(k.path)
	if err != nil {
		return banner
	}
	if home, ok := entries.Find("home"); ok && home.Enabled {
		banner.Home = home.URL
	}
	if service, ok := entries.Find(k.service); ok {
		banner.Tools = service.MCP
	}
	if service, ok := entries.Find(k.service); ok && service.HasIcon {
		banner.Icon = service.Icon
	}
	return banner
}
