// Package page provides the shared Ikigenba page chrome and browser assets.
package page

import (
	"html/template"
	"os"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// User describes the signed-in person and the app's account links.
type User struct {
	Email, ProfileURL, LogoutURL string
}

// Service is a launcher entry from the host's services file.
type Service struct {
	Name, URL        string
	Icon             template.HTML
	Enabled, Current bool
}

// Banner is the data rendered by the banner, launcher, and footer templates.
type Banner struct {
	Service                               string
	Icon                                  template.HTML
	Version, Email, ProfileURL, LogoutURL string
	Services                              []Service
	Home                                  string
	Tools                                 bool
	Trail                                 []Level
}

// Level describes one step below the app's landing page.
type Level struct {
	Name, URL string
}

// Kit holds the app identity and the services path captured at construction.
type Kit struct {
	service, version, path string
}

// New captures the host services path for the named app.
func New(service, version string) *Kit {
	return &Kit{service: service, version: version, path: os.Getenv(services.Variable)}
}

// Banner reads the current services file and combines it with the user's data.
func (k *Kit) Banner(u User) Banner {
	banner := Banner{
		Service: k.service, Version: k.version, Email: u.Email,
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
	for _, entry := range entries {
		if entry.HasIcon {
			banner.Services = append(banner.Services, Service{
				Name: entry.Name, URL: entry.URL, Icon: entry.Icon,
				Enabled: entry.Enabled, Current: entry.Name == k.service,
			})
		}
	}
	for _, service := range banner.Services {
		if service.Current {
			banner.Icon = service.Icon
			break
		}
	}
	return banner
}
