package appkit

import "html/template"

// Templates returns a fresh set of the embedded banner, launcher, and footer templates.
func Templates() *template.Template {
	markup, err := assetsFS.ReadFile("assets/banner.html")
	if err != nil {
		panic(err)
	}
	return template.Must(template.New("appkit").Parse(string(markup)))
}
