package server

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/auth/internal/store"
)

type tokenTimeData struct {
	Datetime, Text, Title string
}

type tokenRowData struct {
	ID, Name          string
	Created           tokenTimeData
	LastUsed, Expires *tokenTimeData
	Enabled           bool
}

type tokenCreateData struct {
	Name, Expiry                     string
	Rejected, NameError, ExpiryError bool
}

type tokenCreatedData struct {
	Name, Secret string
}

const tokenPageTemplates = `{{define "tokenList"}}<section class="card flush"><header><h2>API tokens</h2><p>Personal access tokens let scripts and tools act as you. Send one as a bearer token.</p></header>{{if .}}<div class="table-scroll"><table><thead><tr><th>Name</th><th>Created</th><th>Last used</th><th>Expires</th><th>Status</th><th></th></tr></thead><tbody>{{range .}}<tr><td>{{template "value" .Name}}</td>{{template "tokenTime" .Created}}{{if .LastUsed}}{{template "tokenLastUsed" .LastUsed}}{{else}}{{template "tokenNever"}}{{end}}{{if .Expires}}{{template "tokenTime" .Expires}}{{else}}{{template "tokenNever"}}{{end}}<td>{{if .Enabled}}<span class="badge" data-kind="ok">Enabled</span>{{else}}<span class="badge">Disabled</span>{{end}}</td><td class="row-actions"><form class="inline" method="post" action="/tokens/{{.ID}}/{{if .Enabled}}disable{{else}}enable{{end}}"><button class="ghost small" type="submit">{{if .Enabled}}Disable{{else}}Enable{{end}}</button></form><form class="inline" method="post" action="/tokens/{{.ID}}/delete"><button class="ghost small" type="submit">Delete</button></form></td></tr>{{end}}</tbody></table></div>{{else}}<div class="empty"><h3>No tokens yet</h3><p>Create one below when a script or tool needs to act as you.</p></div>{{end}}</section>{{end}}
{{define "tokenTime"}}<td><time datetime="{{.Datetime}}">{{.Text}}</time></td>{{end}}
{{define "tokenLastUsed"}}<td><time datetime="{{.Datetime}}" title="{{.Title}}">{{.Text}}</time></td>{{end}}
{{define "tokenNever"}}<td class="muted">Never</td>{{end}}
{{define "tokenCreate"}}<section class="card"><header><h2>Create a token</h2></header><form method="post" action="/tokens"><label for="token-name">Name</label><input id="token-name" type="text" name="name" maxlength="64" placeholder="e.g. ci-deploy"{{if .Rejected}} value="{{template "value" .Name}}"{{end}}{{if .NameError}} aria-describedby="token-name-error"{{end}}>{{if .NameError}}<span id="token-name-error">the name must be 1 to 64 characters</span>{{else}}<span class="hint">Something that tells you where it's used. Up to 64 characters.</span>{{end}}<label for="token-expires">Expires</label><select id="token-expires" name="expires"{{if .ExpiryError}} aria-describedby="token-expires-error"{{end}}><option value="30d"{{if eq .Expiry "30d"}} selected{{end}}>In 30 days</option><option value="90d"{{if eq .Expiry "90d"}} selected{{end}}>In 90 days</option><option value="365d"{{if eq .Expiry "365d"}} selected{{end}}>In 365 days</option><option value="never"{{if eq .Expiry "never"}} selected{{end}}>Never</option></select>{{if .ExpiryError}}<span id="token-expires-error">choose one of the listed expiry options</span>{{end}}{{if .Rejected}}<div class="actions">{{end}}<button type="submit">{{template "plusIcon"}}Create token</button>{{if .Rejected}}<a class="button ghost" href="/">Cancel</a></div>{{end}}</form></section>{{end}}
{{define "tokenCreated"}}<section class="card"><header><h2>Token created</h2></header><div class="alert quiet" data-kind="warn"><strong>Copy it now</strong><p>This is the only time {{template "value" .Name}} is shown. Only its hash is stored.</p></div><div class="secret"><code>{{.Secret}}</code><button class="secondary" type="button">{{template "copyIcon"}}Copy</button></div><a href="/">Back to your account</a></section>{{end}}
{{define "plusIcon"}}` + iconStart + `<path d="M12 5l0 14"></path><path d="M5 12l14 0"></path></svg>{{end}}
{{define "copyIcon"}}` + iconStart + `<path d="M7 9.667a2.667 2.667 0 0 1 2.667 -2.667h8.666a2.667 2.667 0 0 1 2.667 2.667v8.666a2.667 2.667 0 0 1 -2.667 2.667h-8.666a2.667 2.667 0 0 1 -2.667 -2.667l0 -8.666"></path><path d="M4.012 16.737a2.005 2.005 0 0 1 -1.012 -1.737v-10c0 -1.1 .9 -2 2 -2h10c.75 0 1.158 .385 1.5 1"></path></svg>{{end}}`

func (s *Server) tokenRows(userID string, drawTime time.Time) ([]tokenRowData, error) {
	tokens, err := s.st.ListTokens(userID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(tokens, func(i, j int) bool { return tokenPrecedes(tokens[i], tokens[j]) })
	rows := make([]tokenRowData, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != store.TokenPersonal {
			continue
		}
		row := tokenRowData{ID: token.ID, Name: token.Name, Created: tokenTime(token.CreatedAt), Enabled: token.Enabled}
		if token.LastUsedAt != nil {
			row.LastUsed = &tokenTimeData{Datetime: tokenDatetime(*token.LastUsedAt), Title: tokenMinute(*token.LastUsedAt), Text: tokenElapsed(drawTime.Sub(*token.LastUsedAt))}
		}
		if token.ExpiresAt != nil {
			expires := tokenTime(*token.ExpiresAt)
			row.Expires = &expires
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func tokenDatetime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
func tokenMinute(t time.Time) string   { return t.UTC().Format("2006-01-02 15:04 UTC") }
func tokenTime(t time.Time) tokenTimeData {
	return tokenTimeData{Datetime: tokenDatetime(t), Text: tokenMinute(t)}
}

// ElapsedJustNow and the other elapsed constants supply relative-time copy.
const (
	ElapsedJustNow string = "just now"
	ElapsedMinute  string = "1 minute ago"
	ElapsedMinutes string = "%d minutes ago"
	ElapsedHour    string = "1 hour ago"
	ElapsedHours   string = "%d hours ago"
	ElapsedDay     string = "1 day ago"
	ElapsedDays    string = "%d days ago"
)

func tokenElapsed(d time.Duration) string {
	if d < time.Minute {
		return ElapsedJustNow
	}
	singular, plural, n := ElapsedMinute, ElapsedMinutes, int64(d/time.Minute)
	if d >= 24*time.Hour {
		singular, plural, n = ElapsedDay, ElapsedDays, int64(d/(24*time.Hour))
	} else if d >= time.Hour {
		singular, plural, n = ElapsedHour, ElapsedHours, int64(d/time.Hour)
	}
	if n == 1 {
		return singular
	}
	return fmt.Sprintf(plural, n)
}

func tokenCreateValues(name, expiry string, rejected bool) tokenCreateData {
	trimmed := strings.TrimSpace(name)
	nameError := rejected && (utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 64)
	_, validExpiry := tokenExpiry(expiry)
	expiryError := rejected && !validExpiry
	if !validExpiry {
		expiry = "90d"
	}
	return tokenCreateData{Name: name, Expiry: expiry, Rejected: rejected, NameError: nameError, ExpiryError: expiryError}
}

// mcpClientsData is the exact input of the human-authored mcp-clients template.
type mcpClientsData struct{ Clients []mcpClientData }
type mcpClientData struct {
	ID, Name, Approved, ApprovedText, LastUsed, LastUsedTitle, LastUsedText, Expires, ExpiresText string
	Expired                                                                                       bool
}

func (s *Server) profileClients(userID string, drawTime time.Time) (mcpClientsData, error) {
	tokens, err := s.st.ListTokens(userID)
	if err != nil {
		return mcpClientsData{}, err
	}
	sort.SliceStable(tokens, func(i, j int) bool { return tokenPrecedes(tokens[i], tokens[j]) })
	data := mcpClientsData{Clients: make([]mcpClientData, 0)}
	for _, token := range tokens {
		if token.Kind != store.TokenClient {
			continue
		}
		row := mcpClientData{ID: token.ID, Name: token.Name, Approved: tokenDatetime(token.CreatedAt), ApprovedText: tokenMinute(token.CreatedAt)}
		if token.LastUsedAt != nil {
			row.LastUsed = tokenDatetime(*token.LastUsedAt)
			row.LastUsedTitle = tokenMinute(*token.LastUsedAt)
			row.LastUsedText = tokenElapsed(drawTime.Sub(*token.LastUsedAt))
		}
		if token.ExpiresAt != nil {
			row.Expires = tokenDatetime(*token.ExpiresAt)
			row.ExpiresText = tokenMinute(*token.ExpiresAt)
			row.Expired = !token.ExpiresAt.After(drawTime)
		}
		data.Clients = append(data.Clients, row)
	}
	return data, nil
}

func tokenPrecedes(a, b store.Token) bool {
	if a.LastUsedAt == nil && b.LastUsedAt != nil {
		return false
	}
	if a.LastUsedAt != nil && b.LastUsedAt == nil {
		return true
	}
	if a.LastUsedAt != nil && !a.LastUsedAt.Equal(*b.LastUsedAt) {
		return a.LastUsedAt.After(*b.LastUsedAt)
	}
	return a.CreatedAt.After(b.CreatedAt)
}
