package server

import (
	"io/fs"
	"mime"
	"net/http"
)

func init() {
	if err := mime.AddExtensionType(".ico", "image/x-icon"); err != nil {
		panic("register .ico content type: " + err.Error())
	}
}

// staticHandler serves the embedded assets under /static/. Directory listings
// are disabled: a request for a directory 404s rather than exposing the asset
// inventory (see noDirFS).
func (a *app) staticHandler() http.Handler {
	return http.StripPrefix("/static/", http.FileServerFS(noDirFS{a.static}))
}

// handleRootFavicon serves the apex favicon through the same embedded static
// handler as /static/favicon.ico, without redirecting or requiring a session.
func (a *app) handleRootFavicon() http.Handler {
	static := a.staticHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := new(http.Request)
		*request = *r
		requestURL := *r.URL
		request.URL = &requestURL
		request.URL.Path = "/static/favicon.ico"
		request.URL.RawPath = ""
		static.ServeHTTP(w, request)
	})
}

// noDirFS wraps an fs.FS and hides directories: opening one returns
// fs.ErrNotExist, so http.FileServerFS returns 404 instead of an autoindex
// listing. Files are served unchanged.
type noDirFS struct{ fsys fs.FS }

func (f noDirFS) Open(name string) (fs.File, error) {
	file, err := f.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.IsDir() {
		file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}
