package server

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth"
)

type authPageData struct {
	Banner  page.Banner
	SignIn  *signInPageData
	Profile *profilePageData
	Create  *tokenCreateData
	Created *tokenCreatedData
}

type signInPageData struct {
	Apex, Refused, Return, Destination, Host, Workspace, Email string
}

type profilePageData struct {
	Apex, Email, Workspace string
	Rows                   []tokenRowData
	Create                 tokenCreateData
	Clients                mcpClientsData
}

// html/template replaces a NUL in a dynamic string with U+FFFD. The read
// contracts preserve submitted bytes, so emit NUL from static template text
// between independently context-escaped segments. No segment is trusted HTML.
const pageValueTemplate string = `{{define "value"}}{{range $i, $part := splitNUL .}}{{if $i}}` + "\x00" + `{{end}}{{$part}}{{end}}{{end}}`

func splitNUL(value string) []string { return strings.Split(value, "\x00") }

func makeAuthTemplates() (*template.Template, error) {
	set, err := page.Templates().Funcs(template.FuncMap{"splitNUL": splitNUL}).Parse(pageValueTemplate)
	if err != nil {
		return nil, err
	}
	return set.ParseFS(auth.Assets(), "*.html")
}

var authTemplates = template.Must(makeAuthTemplates())

func writeAuthPage(w http.ResponseWriter, status int, data authPageData) {
	w.Header().Set("Content-Type", signInHTMLContentType)
	w.WriteHeader(status)
	// The templates and their selection are static; external values enter only
	// through typed data and html/template's contextual escaping.
	_ = authTemplates.ExecuteTemplate(w, "page", data)
}

func signInValues(host, workspace, returnURL, refused, email string) signInPageData {
	data := signInPageData{Apex: apexName(host), Refused: refused, Host: host, Workspace: workspace, Email: email}
	if refused == "" && returnURL != "" {
		data.Return = percentEncode(returnURL)
		if inSpace(returnURL, host) {
			display, _ := returnAuthority(returnURL)
			data.Destination = strings.TrimSuffix(display, ":")
		}
	}
	return data
}

func writeSignInPage(w http.ResponseWriter, host, workspace, returnURL string) {
	data := signInValues(host, workspace, returnURL, "", "")
	writeAuthPage(w, http.StatusOK, authPageData{SignIn: &data})
}

func writeCancelledPage(w http.ResponseWriter, host, workspace string) {
	data := signInValues(host, workspace, "", "cancelled", "")
	writeAuthPage(w, http.StatusOK, authPageData{SignIn: &data})
}

func writeNonMemberPage(w http.ResponseWriter, host, workspace, email string) {
	data := signInValues(host, workspace, "", "not_member", email)
	writeAuthPage(w, http.StatusForbidden, authPageData{SignIn: &data})
}

func (s *Server) pageBanner(email string) page.Banner {
	banner := s.cfg.Banner(page.User{Email: email, ProfileURL: "/", LogoutURL: "/logout"})
	banner.Trail = []page.Level{}
	return banner
}
