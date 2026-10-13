// Package drive implements the connector's Drive REST boundary.
package drive

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// REST limits, event names and endpoint identifiers.
const (
	DefaultBaseURL = "https://www.googleapis.com"
	PushChunk      = 8 << 20
	ExportCap      = 10_000_000
	Called         = "drive.called"
	EndpointList   = "files.list"
	EndpointGet    = "files.get"
	EndpointMedia  = "files.get.media"
	EndpointExport = "files.export"
	EndpointDrives = "drives.list"
	EndpointCreate = "files.create"
	EndpointUpdate = "files.update"
	EndpointChunk  = "upload.chunk"
)

// Config supplies the HTTP transport, base address and event writer.
type Config struct {
	HTTP      *http.Client
	BaseURL   string
	Telemetry *telemetry.Writer
}

// Kind identifies a connector file category.
type Kind string

// File categories reduced from Drive MIME types.
const (
	KindFile         Kind = "file"
	KindFolder       Kind = "folder"
	KindShortcut     Kind = "shortcut"
	KindDocument     Kind = "document"
	KindSpreadsheet  Kind = "spreadsheet"
	KindPresentation Kind = "presentation"
	KindDrawing      Kind = "drawing"
	KindOther        Kind = "other"
)

// File is the connector metadata object.
type File struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     Kind      `json:"kind"`
	MimeType string    `json:"mime_type"`
	Bytes    *int64    `json:"bytes,omitempty"`
	Modified time.Time `json:"modified"`
	Folder   string    `json:"folder,omitempty"`
	Drive    string    `json:"drive,omitempty"`
	Link     string    `json:"link"`
	Target   *Target   `json:"target,omitempty"`
}

// Target identifies a shortcut destination.
type Target struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
}

// Drive is a shared-drive metadata object.
type Drive struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Page holds one ordered page and its continuation token.
type Page struct {
	Files []File
	Next  string
}

// Query selects a search scope and continuation.
type Query struct {
	Text, Folder, Drive, Page string
	Size                      int
}

// Upload describes a create or replacement upload.
type Upload struct {
	File, Name, Folder, ContentType string
	Bytes                           int64
}

// Failure classes expose provider outcomes without credentials.
var (
	ErrNotFound  = errors.New("drive: not found")
	ErrForbidden = errors.New("drive: forbidden")
	ErrTooLarge  = errors.New("drive: too large")
	ErrProvider  = errors.New("drive: provider failure")
)

// KindOf reduces a Drive MIME type to its connector category.
func KindOf(m string) Kind {
	switch m {
	case "application/vnd.google-apps.folder":
		return KindFolder
	case "application/vnd.google-apps.shortcut":
		return KindShortcut
	case "application/vnd.google-apps.document":
		return KindDocument
	case "application/vnd.google-apps.spreadsheet":
		return KindSpreadsheet
	case "application/vnd.google-apps.presentation":
		return KindPresentation
	case "application/vnd.google-apps.drawing":
		return KindDrawing
	}
	if strings.HasPrefix(m, "application/vnd.google-apps.") {
		return KindOther
	}
	return KindFile
}

// Formats returns a fresh ordered list of supported export MIME types.
func Formats(k Kind) []string {
	switch k {
	case KindDocument:
		return []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.oasis.opendocument.text", "application/rtf", "application/pdf", "text/plain", "text/html", "application/zip", "application/epub+zip", "text/markdown"}
	case KindSpreadsheet:
		return []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.oasis.opendocument.spreadsheet", "application/pdf", "application/zip", "text/csv", "text/tab-separated-values"}
	case KindPresentation:
		return []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/vnd.oasis.opendocument.presentation", "application/pdf", "text/plain"}
	case KindDrawing:
		return []string{"application/pdf", "image/jpeg", "image/png", "image/svg+xml"}
	}
	return []string{}
}

// DefaultFormat returns the default export MIME type for a category.
func DefaultFormat(k Kind) string {
	if k == KindDrawing {
		return "image/png"
	}
	f := Formats(k)
	if len(f) > 0 {
		return f[0]
	}
	return ""
}
