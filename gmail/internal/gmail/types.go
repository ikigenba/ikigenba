// Package gmail reduces Gmail's REST API to the connector's types.
package gmail

import (
	"errors"
	"io"
	"net/http"
	"net/mail"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Config supplies the transport, provider root and event writer.
type Config struct {
	HTTP      *http.Client
	BaseURL   string
	Telemetry *telemetry.Writer
}

// Provider limits, field selections and endpoint names.
const (
	DefaultBaseURL     = "https://gmail.googleapis.com"
	PageSize           = 25
	BodyCap            = 65536
	UploadCap          = 36700160
	PushChunk          = 8 << 20
	Called             = "gmail.called"
	EndpointLabels     = "labels.list"
	EndpointList       = "messages.list"
	EndpointGet        = "messages.get"
	EndpointAttachment = "messages.attachments.get"
	EndpointThread     = "threads.get"
	EndpointCreate     = "drafts.create"
	EndpointDraft      = "drafts.get"
	EndpointUpdate     = "drafts.update"
	EndpointChunk      = "upload.chunk"
	EndpointSend       = "drafts.send"
	EndpointDelete     = "drafts.delete"
	SummaryFields      = "id,threadId,labelIds,snippet,internalDate,payload(headers,filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename)))))))))"
	ThreadFields       = "messages(" + SummaryFields + ")"
)

// Distinct error categories for provider and message failures.
var (
	ErrNotFound     = errors.New("gmail: not found")
	ErrUnauthorized = errors.New("gmail: unauthorized")
	ErrTooLarge     = errors.New("gmail: too large")
	ErrBadPage      = errors.New("gmail: bad page")
	ErrPartSize     = errors.New("gmail: part size mismatch")
	ErrProvider     = errors.New("gmail: provider failure")
)

// Label names one mailbox label.
type Label struct {
	ID, Name string
	System   bool
}

// Summary holds the readable metadata of a message.
type Summary struct {
	ID, Thread                     string
	Date                           time.Time
	From, To, Cc, Subject, Snippet string
	Labels                         []string
	Unread                         bool
	Attachments                    int
	Link                           string
}

// Page is a page of message summaries.
type Page struct {
	Messages []Summary
	Next     string
}

// Message includes readable text and attachment metadata.
type Message struct {
	ID, Thread                                            string
	Date                                                  time.Time
	From, To, Cc, Subject, Snippet                        string
	Labels                                                []string
	Unread                                                bool
	Attachments                                           []Attachment
	Link, MessageID, InReplyTo, ReplyTo, References, Body string
	Truncated                                             bool
}

// Attachment identifies a named MIME part.
type Attachment struct {
	ID, Name, MimeType string
	Bytes              int64
}

// Query selects mail and carries an opaque page token.
type Query struct{ Text, Label, Page string }

// Stat holds message metadata used for transfers and reconciliation.
type Stat struct {
	ID, Thread   string
	Date         time.Time
	SizeEstimate int64
	Sent         bool
}

// Draft holds the current provider identifiers of a draft.
type Draft struct{ ID, Message, Thread string }

// Content is a bounded decoded byte stream.
type Content struct {
	Bytes  int64
	Reader io.Reader
}

// Composition supplies the headers, text and attachments of outgoing mail.
type Composition struct {
	From                                 mail.Address
	To, Cc, Bcc                          []mail.Address
	Subject, Text, InReplyTo, References string
	Attachments                          []Part
}

// Part opens one attachment stream when its content is reached.
type Part struct {
	Name, ContentType string
	Bytes             int64
	Open              func() (io.ReadCloser, error)
}
