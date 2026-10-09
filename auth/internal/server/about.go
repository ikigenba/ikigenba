package server

import (
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/page"
)

// Description is auth's one-line description, shared by its page and manifest.
const Description string = "Sign-in and identity for the suite's services"

// AboutData supplies auth's about template.
type AboutData struct {
	Banner      page.Banner
	Description string
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writePlainError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	identity, signedIn, err := s.sessionIdentity(r)
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	if !signedIn {
		writeSignInPage(w, r.Host, s.cfg.WorkspaceDomain, returnQuery(r.URL.RawQuery))
		return
	}
	banner := s.pageBanner(identity.Email)
	banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
	w.Header().Set("Content-Type", signInHTMLContentType)
	w.WriteHeader(http.StatusOK)
	_ = authTemplates.ExecuteTemplate(w, "about", AboutData{banner, Description})
}
