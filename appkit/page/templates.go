package page

import "html/template"

// Templates returns a fresh set of the embedded banner, footer, and preload templates.
func Templates() *template.Template {
	markup, err := assetsFS.ReadFile("assets/banner.html")
	if err != nil {
		panic(err)
	}
	return template.Must(template.New("appkit").Funcs(template.FuncMap{"preloadURL": PreloadURL}).Parse(string(markup)))
}
