package server

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

type tokenTimeData struct {
	Datetime, Text string
}

type tokenRowData struct {
	ID, Name string
	Created  tokenTimeData
	LastUsed *tokenLastUsedData
	Expires  *tokenTimeData
	Enabled  bool
}

type tokenCreateData struct {
	Name, Expiry                     string
	Rejected, NameError, ExpiryError bool
}

type tokenCreatedData struct {
	Name, Secret string
}

type tokenLastUsedData struct {
	Datetime, Title string
	Elapsed         elapsedData
}

type elapsedData struct {
	Unit  string
	Count int64
}

func (s *Server) tokenRows(userID string, drawTime time.Time, after *store.TokenPosition) ([]tokenRowData, string, error) {
	tokens, next, err := s.tokenWindow(userID, store.TokenPersonal, after, "tokens")
	if err != nil {
		return nil, "", err
	}
	rows := make([]tokenRowData, 0, len(tokens))
	for _, token := range tokens {
		row := tokenRowData{ID: token.ID, Name: token.Name, Created: tokenTime(token.CreatedAt), Enabled: token.Enabled}
		if token.LastUsedAt != nil {
			row.LastUsed = &tokenLastUsedData{Datetime: tokenDatetime(*token.LastUsedAt), Title: tokenMinute(*token.LastUsedAt), Elapsed: tokenElapsedData(drawTime.Sub(*token.LastUsedAt))}
		}
		if token.ExpiresAt != nil {
			expires := tokenTime(*token.ExpiresAt)
			row.Expires = &expires
		}
		rows = append(rows, row)
	}
	return rows, next, nil
}

func tokenDatetime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
func tokenMinute(t time.Time) string   { return t.UTC().Format("2006-01-02 15:04 UTC") }
func tokenTime(t time.Time) tokenTimeData {
	return tokenTimeData{Datetime: tokenDatetime(t), Text: tokenMinute(t)}
}

func tokenElapsedData(d time.Duration) elapsedData {
	switch {
	case d < time.Minute:
		return elapsedData{Unit: "now"}
	case d < time.Hour:
		return elapsedData{Unit: "minute", Count: int64(d / time.Minute)}
	case d < 24*time.Hour:
		return elapsedData{Unit: "hour", Count: int64(d / time.Hour)}
	default:
		return elapsedData{Unit: "day", Count: int64(d / (24 * time.Hour))}
	}
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
type mcpClientsData struct {
	Clients                              []mcpClientData
	ClientsPage, ClientsNext, TokensPage string
}
type mcpClientData struct {
	ID, Name, Approved, ApprovedText, LastUsed, LastUsedTitle string
	LastUsedElapsed                                           elapsedData
	Expires, ExpiresText                                      string
	Expired                                                   bool
}

func (s *Server) profileClients(userID string, drawTime time.Time, after *store.TokenPosition) (mcpClientsData, error) {
	tokens, next, err := s.tokenWindow(userID, store.TokenClient, after, "clients")
	if err != nil {
		return mcpClientsData{}, err
	}
	data := mcpClientsData{Clients: make([]mcpClientData, 0, len(tokens)), ClientsNext: next}
	for _, token := range tokens {
		row := mcpClientData{ID: token.ID, Name: token.Name, Approved: tokenDatetime(token.CreatedAt), ApprovedText: tokenMinute(token.CreatedAt)}
		if token.LastUsedAt != nil {
			row.LastUsed = tokenDatetime(*token.LastUsedAt)
			row.LastUsedTitle = tokenMinute(*token.LastUsedAt)
			row.LastUsedElapsed = tokenElapsedData(drawTime.Sub(*token.LastUsedAt))
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

func tokenPagePosition(value, tool string) (*store.TokenPosition, error) {
	if value == "" {
		return nil, nil
	}
	var used, last, created int64
	var id string
	if err := (paging.Token{Tool: tool}).Decode(value, &used, &last, &created, &id); err != nil {
		return nil, err
	}
	position := &store.TokenPosition{CreatedAt: time.Unix(0, created), ID: id}
	if used != 0 {
		at := time.Unix(0, last)
		position.LastUsedAt = &at
	}
	return position, nil
}

func (s *Server) tokenWindow(userID string, kind store.TokenKind, after *store.TokenPosition, tool string) ([]store.Token, string, error) {
	tokens, err := s.st.ListTokens(userID, kind, after, paging.DefaultLimit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(tokens) > paging.DefaultLimit {
		tokens = tokens[:paging.DefaultLimit]
		last := tokens[len(tokens)-1]
		var used, at int64
		if last.LastUsedAt != nil {
			used, at = 1, last.LastUsedAt.UnixNano()
		}
		next = (paging.Token{Tool: tool}).Encode(used, at, last.CreatedAt.UnixNano(), last.ID)
	}
	return tokens, next, nil
}
