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

func (s *Server) tokenRows(userID string, drawTime time.Time, position tokenPage) ([]tokenRowData, tokenPageWindow, error) {
	window, err := s.tokenWindow(userID, store.TokenPersonal, position, "tokens")
	tokens := window.tokens
	if err != nil {
		return nil, tokenPageWindow{}, err
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
	return rows, window, nil
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
	Clients                                           []mcpClientData
	ClientsPage, ClientsPrev, ClientsNext, TokensPage string
}
type mcpClientData struct {
	ID, Name, Approved, ApprovedText, LastUsed, LastUsedTitle string
	LastUsedElapsed                                           elapsedData
	Expires, ExpiresText                                      string
	Expired                                                   bool
}

func (s *Server) profileClients(userID string, drawTime time.Time, position tokenPage) (mcpClientsData, bool, error) {
	window, err := s.tokenWindow(userID, store.TokenClient, position, "clients")
	tokens := window.tokens
	if err != nil {
		return mcpClientsData{}, false, err
	}
	data := mcpClientsData{Clients: make([]mcpClientData, 0, len(tokens)), ClientsPrev: window.prev, ClientsNext: window.next}
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
	return data, window.underflows, nil
}

type tokenPage struct {
	position            *store.TokenPosition
	before, last, empty bool
}

type tokenPageWindow struct {
	tokens     []store.Token
	prev, next string
	underflows bool
}

func tokenPagePosition(value, tool string) (tokenPage, error) {
	if value == "" {
		return tokenPage{empty: true}, nil
	}
	var used, last, created int64
	var id string
	err := (paging.Token{Tool: tool}).Decode(value, &used, &last, &created, &id)
	before := false
	if err != nil {
		err = (paging.Token{Tool: tool + "-before"}).Decode(value, &used, &last, &created, &id)
		before = true
	}
	if err != nil {
		if value == "last" {
			return tokenPage{last: true}, nil
		}
		return tokenPage{}, err
	}
	position := &store.TokenPosition{CreatedAt: time.Unix(0, created), ID: id}
	if used != 0 {
		at := time.Unix(0, last)
		position.LastUsedAt = &at
	}
	return tokenPage{position: position, before: before}, nil
}

func tokenPageValue(token store.Token, tool string) string {
	var used, at int64
	if token.LastUsedAt != nil {
		used, at = 1, token.LastUsedAt.UnixNano()
	}
	return (paging.Token{Tool: tool}).Encode(used, at, token.CreatedAt.UnixNano(), token.ID)
}

func (s *Server) tokenWindow(userID string, kind store.TokenKind, position tokenPage, tool string) (tokenPageWindow, error) {
	var tokens []store.Token
	var err error
	reverse := position.before || position.last
	if reverse {
		tokens, err = s.st.ListTokensBefore(userID, kind, position.position, paging.DefaultLimit+1)
	} else {
		tokens, err = s.st.ListTokens(userID, kind, position.position, paging.DefaultLimit+1)
	}
	if err != nil {
		return tokenPageWindow{}, err
	}
	more := len(tokens) > paging.DefaultLimit
	if more {
		tokens = tokens[:paging.DefaultLimit]
	}
	window := tokenPageWindow{tokens: tokens, underflows: reverse && !more}
	if reverse {
		for i, j := 0, len(tokens)-1; i < j; i, j = i+1, j-1 {
			tokens[i], tokens[j] = tokens[j], tokens[i]
		}
	}
	if len(tokens) > 0 {
		if reverse && more || !reverse && !position.empty {
			window.prev = tokenPageValue(tokens[0], tool+"-before")
		}
		if position.before || !reverse && more {
			window.next = tokenPageValue(tokens[len(tokens)-1], tool)
		}
	}
	return window, nil
}
