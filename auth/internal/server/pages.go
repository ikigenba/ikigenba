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
	Apex, Sentence, Link, Word, Footer string
	Alert                              *pageAlertData
}

type pageAlertData struct {
	Kind, Role, Title, Message string
}

type profilePageData struct {
	Apex, Email, Workspace string
	Rows                   []tokenRowData
	Clients                mcpClientsData
	Create                 tokenCreateData
}

const iconStart = `<svg class="ico" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`

const authPageTemplates = `{{define "page"}}<!DOCTYPE html><html><head><title>auth</title>{{template "preload"}}<link rel="stylesheet" href="/_appkit/theme.css"><link rel="icon" type="image/svg+xml" href="/_appkit/favicon.svg"><script src="/_appkit/feedback.js" defer></script><meta name="viewport" content="width=device-width, initial-scale=1"></head><body>{{if .SignIn}}{{template "signIn" .SignIn}}{{else}}{{template "chrome" .}}{{end}}</body></html>{{end}}
{{define "signIn"}}<main class="auth-page"><section class="card"><span class="mark">ikigenba</span> <h1>Sign in to {{template "value" .Apex}}</h1> {{if .Alert}}{{template "alert" .Alert}}{{else}}<p>{{template "value" .Sentence}}</p>{{end}} <a class="button secondary large google" href="{{.Link}}">{{.Word}}</a>{{if .Footer}} <footer>{{template "value" .Footer}}</footer>{{end}}</section></main>{{end}}
{{define "alert"}}<div class="alert" data-kind="{{.Kind}}" role="{{.Role}}"><strong>{{template "value" .Title}}</strong> <p>{{template "value" .Message}}</p></div>{{end}}
{{define "chrome"}}{{template "banner" .Banner}}<main>{{if .Profile}}{{template "profile" .Profile}}{{else if .Create}}{{template "tokenCreate" .Create}}{{else if .Created}}{{template "tokenCreated" .Created}}{{end}}</main>{{template "footer" .Banner}}{{end}}
{{define "profile"}}<h1>Your account</h1><p>You're signed in to {{template "value" .Apex}}.</p><section class="card"><header><h2>Account</h2></header><dl class="kv"><dt>Email</dt><dd>{{template "value" .Email}}</dd><dt>Workspace</dt><dd>{{template "value" .Workspace}}</dd><dt>Signed in via</dt><dd>Google</dd></dl></section>{{template "tokenList" .Rows}}{{template "tokenCreate" .Create}}{{template "mcp-clients" .Clients}}{{end}}
`

// html/template replaces a NUL in a dynamic string with U+FFFD. The read
// contracts preserve submitted bytes, so emit NUL from static template text
// between independently context-escaped segments. No segment is trusted HTML.
const pageValueTemplate = `{{define "value"}}{{range $i, $part := splitNUL .}}{{if $i}}` + "\x00" + `{{end}}{{$part}}{{end}}{{end}}`

func makeAuthTemplates() (*template.Template, error) {
	return page.Templates().ParseFS(auth.Assets(), "*.html")
}

var authTemplates = template.Must(makeAuthTemplates())

var pageTemplates = template.Must(template.Must(authTemplates.Clone()).Funcs(template.FuncMap{
	"splitNUL": func(value string) []string { return strings.Split(value, "\x00") },
}).Parse(authPageTemplates + tokenPageTemplates + pageValueTemplate))

func writeAuthPage(w http.ResponseWriter, status int, data authPageData) {
	w.Header().Set("Content-Type", signInHTMLContentType)
	w.WriteHeader(status)
	// The templates and their selection are static; external values enter only
	// through typed data and html/template's contextual escaping.
	_ = pageTemplates.ExecuteTemplate(w, "page", data)
}

func writeSignInPage(w http.ResponseWriter, host, workspace, returnURL string) {
	link := "/login/google"
	if returnURL != "" {
		link += "?return=" + percentEncode(returnURL)
	}
	sentence := "Access is limited to Google accounts in the " + workspace + " workspace."
	footer := "You're signing in at " + host + ". One sign-in covers every service in this space."
	if inSpace(returnURL, host) {
		display, _ := returnAuthority(returnURL)
		display = strings.TrimSuffix(display, ":")
		footer = sentence
		sentence = "Sign in to continue to " + display + "."
	}
	writeAuthPage(w, http.StatusOK, authPageData{SignIn: &signInPageData{
		Apex: apexName(host), Sentence: sentence, Link: link, Word: "Continue with Google", Footer: footer,
	}})
}

func writeCancelledPage(w http.ResponseWriter, host string) {
	writeAuthPage(w, http.StatusOK, authPageData{SignIn: &signInPageData{
		Apex: apexName(host), Link: "/login/google", Word: "Continue with Google",
		Alert: &pageAlertData{Kind: "warn", Role: "status", Title: "Sign-in cancelled", Message: "Google didn't grant access, so you weren't signed in. You can try again."},
	}})
}

func writeNonMemberPage(w http.ResponseWriter, host, workspace, email string) {
	message := email + " isn't a verified account in the " + workspace + " workspace. Sign in with your @" + workspace + " account instead."
	writeAuthPage(w, http.StatusForbidden, authPageData{SignIn: &signInPageData{
		Apex: apexName(host), Link: "/login/google", Word: "Try another account", Footer: "Think you should have access? Ask your " + workspace + " workspace admin to add you.",
		Alert: &pageAlertData{Kind: "err", Role: "alert", Title: "Workspace membership required", Message: message},
	}})
}

func (s *Server) pageBanner(email string) page.Banner {
	return s.cfg.Banner(page.User{Email: email, ProfileURL: "/", LogoutURL: "/logout"})
}
