package appkit

import (
	"encoding/json"
	"html/template"
	"os"
	"unicode/utf8"
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

// Banner is the data rendered by the banner and launcher templates.
type Banner struct {
	Service, Version, Email, ProfileURL, LogoutURL string
	Services                                       []Service
}

// Kit holds the app identity and the services path captured at construction.
type Kit struct {
	service string
	version string
	path    string
}

// New captures the host services path for the named app.
func New(service, version string) *Kit {
	return &Kit{service: service, version: version, path: os.Getenv("IKIGENBA_SERVICES")}
}

// Banner reads the current services file and combines it with the user's data.
func (k *Kit) Banner(u User) Banner {
	return Banner{
		Service: k.service, Version: k.version, Email: u.Email, ProfileURL: u.ProfileURL,
		LogoutURL: u.LogoutURL, Services: k.services(),
	}
}

func (k *Kit) services() []Service {
	if k.path == "" {
		return nil
	}
	data, err := os.ReadFile(k.path)
	if err != nil || !utf8.Valid(data) {
		return nil
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(data, &document) != nil || document == nil {
		return nil
	}
	var entries []json.RawMessage
	raw := document["services"]
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var services []Service
	for _, entry := range entries {
		service, ok := decodeService(entry)
		if !ok {
			continue
		}
		service.Current = service.Name == k.service
		services = append(services, service)
	}
	return services
}

func decodeService(raw json.RawMessage) (Service, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return Service{}, false
	}
	name, nameOK := serviceString(fields["name"])
	url, urlOK := serviceString(fields["url"])
	var icon *template.HTML
	iconErr := json.Unmarshal(fields["icon"], &icon)
	var enabled *bool
	err := json.Unmarshal(fields["enabled"], &enabled)
	if !nameOK || name == "" || !urlOK || iconErr != nil || icon == nil || err != nil || enabled == nil {
		return Service{}, false
	}
	return Service{Name: name, URL: url, Icon: *icon, Enabled: *enabled}, true
}

func serviceString(raw json.RawMessage) (string, bool) {
	var value string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}
