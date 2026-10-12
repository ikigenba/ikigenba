// Package tools exposes Gmail through typed MCP tools.
package tools

// NoArgs describes the typed tool contract.
type NoArgs struct {
}

// SearchArgs describes the typed tool contract.
type SearchArgs struct {
	Query string `json:"query" mcp:"required"`
	Label string `json:"label"`
	Page  string `json:"page"`
}

// ThreadArgs describes the typed tool contract.
type ThreadArgs struct {
	Thread string `json:"thread" mcp:"required"`
	Page   string `json:"page"`
}

// ShowArgs describes the typed tool contract.
type ShowArgs struct {
	Message string `json:"message" mcp:"required"`
}

// PullArgs describes the typed tool contract.
type PullArgs struct {
	Message    string  `json:"message" mcp:"required"`
	Path       string  `json:"path" mcp:"required"`
	Attachment string  `json:"attachment"`
	Expires    *string `json:"expires"`
}

// ComposeArgs describes the typed tool contract.
type ComposeArgs struct {
	To      []string `json:"to"`
	Cc      []string `json:"cc"`
	Bcc     []string `json:"bcc"`
	Subject *string  `json:"subject"`
	Body    string   `json:"body"`
	ReplyTo string   `json:"reply_to"`
}

// PushArgs describes the typed tool contract.
type PushArgs struct {
	Path  string `json:"path" mcp:"required"`
	Draft string `json:"draft" mcp:"required"`
}

// DraftArgs describes the typed tool contract.
type DraftArgs struct {
	Draft string `json:"draft" mcp:"required"`
}

// TransferArgs describes the typed tool contract.
type TransferArgs struct {
	Transfer string `json:"transfer" mcp:"required"`
}

// Summary describes the typed tool contract.
type Summary struct {
	ID          string   `json:"id"`
	Thread      string   `json:"thread"`
	Date        string   `json:"date"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Cc          string   `json:"cc"`
	Subject     string   `json:"subject"`
	Snippet     string   `json:"snippet"`
	Labels      []string `json:"labels"`
	Unread      bool     `json:"unread"`
	Attachments int64    `json:"attachments"`
	Link        string   `json:"link"`
}

// Part describes the typed tool contract.
type Part struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Bytes    int64  `json:"bytes"`
}

// Message describes the typed tool contract.
type Message struct {
	ID          string   `json:"id"`
	Thread      string   `json:"thread"`
	Date        string   `json:"date"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Cc          string   `json:"cc"`
	Subject     string   `json:"subject"`
	Snippet     string   `json:"snippet"`
	Labels      []string `json:"labels"`
	Unread      bool     `json:"unread"`
	Link        string   `json:"link"`
	MessageID   string   `json:"message_id"`
	InReplyTo   string   `json:"in_reply_to"`
	Body        string   `json:"body"`
	Truncated   bool     `json:"truncated"`
	Attachments []Part   `json:"attachments"`
}

// Label describes the typed tool contract.
type Label struct {
	Name   string `json:"name"`
	System bool   `json:"system"`
}

// MessageList describes the typed tool contract.
type MessageList struct {
	Messages []Summary `json:"messages"`
	Next     *string   `json:"next"`
}

// LabelList describes the typed tool contract.
type LabelList struct {
	Labels []Label `json:"labels"`
}

// Attachment describes the typed tool contract.
type Attachment struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Bytes       int64  `json:"bytes"`
}

// Draft describes the typed tool contract.
type Draft struct {
	ID          string       `json:"id"`
	Draft       *string      `json:"draft"`
	Message     string       `json:"message"`
	Thread      string       `json:"thread"`
	Status      string       `json:"status"`
	To          []string     `json:"to"`
	Cc          []string     `json:"cc"`
	Bcc         []string     `json:"bcc"`
	Subject     string       `json:"subject"`
	ReplyTo     *string      `json:"reply_to"`
	Attachments []Attachment `json:"attachments"`
	Link        *string      `json:"link"`
	Created     string       `json:"created"`
	Modified    string       `json:"modified"`
	Sent        *string      `json:"sent"`
	Body        *string      `json:"body"`
}

// DraftList describes the typed tool contract.
type DraftList struct {
	Drafts []Draft `json:"drafts"`
}

// Record describes the typed tool contract.
type Record struct {
	ID          string  `json:"id"`
	Direction   string  `json:"direction"`
	Status      string  `json:"status"`
	Reason      *string `json:"reason"`
	Message     string  `json:"message"`
	Attachment  *string `json:"attachment"`
	Draft       *string `json:"draft"`
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	ContentType string  `json:"content_type"`
	Bytes       int64   `json:"bytes"`
	Started     string  `json:"started"`
	Finished    *string `json:"finished"`
	URL         *string `json:"url"`
	Link        *string `json:"link"`
}

// TransferList describes the typed tool contract.
type TransferList struct {
	Transfers []Record `json:"transfers"`
}
