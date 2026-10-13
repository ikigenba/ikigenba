// Package tools exposes the caller's Drive and transfers over MCP.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/transfers"
)

// Config supplies the dependencies used by the handler.
type Config struct {
	Transfers *transfers.Engine
	Drive     *drive.Client
	Store     *store.Store
}

// Entry names a tool and its one-line description.
type Entry struct{ Name, Description string }

// TransferGuide explains the shared transfer workflow to tool callers.
const TransferGuide = "No tool carries bytes or a credential; pull and push record a transfer that moves them on the host, transfer shows its outcome, and the artifact's url is where a shell fetches the result."

// Refusal sentences explain the stable machine-readable refusal words.
const (
	RefuseNotConnected  = "Connect your Google account to continue."
	RefuseReconnect     = "Reconnect your Google account to continue."
	RefuseNotAFile      = "This Drive item is not a file."
	RefuseNotExportable = "This Google file cannot be exported."
	RefuseFormat        = "Choose an export format supported by this file."
	RefuseExpires       = "Choose a valid future expiry time."
	RefuseTooLarge      = "This file exceeds the transfer size limit."
	RefuseNotFound      = "The requested item was not found."
	RefuseProvider      = "Google Drive could not complete the request."
	RefusePath          = "The artifact path is invalid."
	RefuseUnavailable   = "A required service is unavailable; try again later."
	RefuseStopping      = "The service is stopping; try again later."
	RefuseQueueFull     = "The transfer queue is full; try again later."
)

const (
	descriptionDrives    = "List the shared drives your account can see."
	descriptionList      = "List one page of a folder's children."
	descriptionSearch    = "Search for files by name and content."
	descriptionShow      = "Show one Drive file and its metadata."
	descriptionPull      = "Pull a Drive file into artifacts."
	descriptionPush      = "Push an artifact into Google Drive."
	descriptionTransfers = "List your transfers, newest first."
	descriptionTransfer  = "Show one of your transfers."
)

// Catalog returns the tool summaries in registration order.
func Catalog() []Entry {
	return []Entry{{"drives", descriptionDrives}, {"list", descriptionList}, {"search", descriptionSearch}, {"show", descriptionShow}, {"pull", descriptionPull}, {"push", descriptionPush}, {"transfers", descriptionTransfers}, {"transfer", descriptionTransfer}}
}

// NoArgs is the empty tool argument object.
type NoArgs struct{}

// ListArgs selects a folder and an opaque page token.
type ListArgs struct {
	Folder *string `json:"folder"`
	Page   *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit  *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// SearchArgs selects the search text, scope and page.
type SearchArgs struct {
	Query  string  `json:"query" mcp:"required"`
	Folder *string `json:"folder"`
	Drive  *string `json:"drive"`
	Page   *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit  *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// TransfersArgs selects a transfer continuation and window.
type TransfersArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// ShowArgs names the Drive file to inspect.
type ShowArgs struct {
	File string `json:"file" mcp:"required"`
}

// PullArgs names a Drive file and its artifact destination.
type PullArgs struct {
	File    string  `json:"file" mcp:"required"`
	Path    string  `json:"path" mcp:"required"`
	Format  *string `json:"format"`
	Expires *string `json:"expires"`
}

// PushArgs names the artifact source and Drive destination.
type PushArgs struct {
	Path   string  `json:"path" mcp:"required"`
	Folder *string `json:"folder"`
	Name   *string `json:"name"`
	File   *string `json:"file"`
}

// TransferArgs names the transfer to inspect.
type TransferArgs struct {
	Transfer string `json:"transfer" mcp:"required"`
}

// DriveList is a shared-drive tool result.
type DriveList struct{ Drives []drive.Drive }

// MarshalJSON encodes an empty collection as an array.
func (d DriveList) MarshalJSON() ([]byte, error) {
	if d.Drives == nil {
		d.Drives = []drive.Drive{}
	}
	return json.Marshal(struct {
		Drives []drive.Drive `json:"drives"`
	}{d.Drives})
}

// FileList is a file-page tool result.
type FileList struct {
	Files []drive.File
	Next  *string
}

// MarshalJSON encodes an empty collection as an array.
func (f FileList) MarshalJSON() ([]byte, error) {
	if f.Files == nil {
		f.Files = []drive.File{}
	}
	return json.Marshal(struct {
		Files []drive.File `json:"files"`
		Next  *string      `json:"next,omitempty"`
	}{f.Files, f.Next})
}

// Record is the public transfer result, without ownership or credentials.
type Record struct {
	ID          string  `json:"id"`
	Direction   string  `json:"direction"`
	Status      string  `json:"status"`
	Reason      *string `json:"reason"`
	File        string  `json:"file"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Path        string  `json:"path"`
	ContentType string  `json:"content_type"`
	Format      *string `json:"format"`
	Bytes       int64   `json:"bytes"`
	Started     string  `json:"started"`
	Finished    *string `json:"finished"`
	URL         *string `json:"url"`
	Link        *string `json:"link"`
}

// TransferList is a list of public transfer records.
type TransferList struct {
	Transfers []Record `json:"transfers"`
	Next      *string  `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func val(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RecordOf projects a stored transfer into its public tool result.
func RecordOf(t store.Transfer) Record {
	r := Record{t.ID, t.Direction, t.Status, ptr(t.Reason), t.File, t.Name, t.Kind, t.Path, t.ContentType, ptr(t.Format), t.Bytes, t.Started.UTC().Format(time.RFC3339), nil, ptr(t.URL), ptr(t.Link)}
	if !t.Finished.IsZero() {
		r.Finished = ptr(t.Finished.UTC().Format(time.RFC3339))
	}
	return r
}
func refusal(word, url string) error {
	sentences := map[string]string{"not_connected": RefuseNotConnected, "reconnect": RefuseReconnect, "not_a_file": RefuseNotAFile, "not_exportable": RefuseNotExportable, "format": RefuseFormat, "expires": RefuseExpires, "too_large": RefuseTooLarge, "not_found": RefuseNotFound, "provider": RefuseProvider, "path": RefusePath, "unavailable": RefuseUnavailable, "stopping": RefuseStopping, "queue_full": RefuseQueueFull}
	s := word + ": " + sentences[word]
	if url != "" {
		s += " " + url
	}
	return errors.New(s)
}
func engineError(err error) error {
	var r *transfers.Refusal
	if errors.As(err, &r) {
		return refusal(r.Word, r.URL)
	}
	return refusal("unavailable", "")
}
func driveError(err error) error {
	if errors.Is(err, drive.ErrNotFound) {
		return refusal("not_found", "")
	}
	return refusal("provider", "")
}
func raw(v any) (mcp.Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.Result{}, err
	}
	text, _ := json.Marshal(string(b))
	var r mcp.Result
	err = json.Unmarshal(append(append(append([]byte(`{"content":[{"type":"text","text":`), text...), []byte(`}],"structuredContent":`)...), append(b, '}')...), &r)
	return r, err
}

// Register adds the eight caller-scoped tools to the MCP server.
func Register(srv *mcp.Server, cfg Config) {
	entries := Catalog()
	desc := func(i int) string { return entries[i].Description + "\n\n" + TransferGuide }
	mcp.AddRawTool(srv, mcp.RawTool[NoArgs]{Name: "drives", Description: desc(0), Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, _ NoArgs) (mcp.Result, error) {
		token, e := cfg.Transfers.Token(ctx, c)
		if e != nil {
			return mcp.Result{}, engineError(e)
		}
		ds, e := cfg.Drive.Drives(ctx, token)
		if e != nil {
			return mcp.Result{}, driveError(e)
		}
		return raw(DriveList{ds})
	}})
	mcp.AddRawTool(srv, mcp.RawTool[ListArgs]{Name: "list", Description: desc(1), Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, a ListArgs) (mcp.Result, error) {
		token, e := cfg.Transfers.Token(ctx, c)
		if e != nil {
			return mcp.Result{}, engineError(e)
		}
		size, e := paging.Window(a.Limit)
		if e != nil {
			return mcp.Result{}, e
		}
		pg, e := cfg.Drive.List(ctx, token, val(a.Folder), val(a.Page), size)
		if e != nil {
			return mcp.Result{}, driveError(e)
		}
		return raw(FileList{pg.Files, ptr(pg.Next)})
	}})
	mcp.AddRawTool(srv, mcp.RawTool[SearchArgs]{Name: "search", Description: desc(2), Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, a SearchArgs) (mcp.Result, error) {
		token, e := cfg.Transfers.Token(ctx, c)
		if e != nil {
			return mcp.Result{}, engineError(e)
		}
		size, e := paging.Window(a.Limit)
		if e != nil {
			return mcp.Result{}, e
		}
		pg, e := cfg.Drive.Search(ctx, token, drive.Query{Size: size, Text: a.Query, Folder: val(a.Folder), Drive: val(a.Drive), Page: val(a.Page)})
		if e != nil {
			return mcp.Result{}, driveError(e)
		}
		return raw(FileList{pg.Files, ptr(pg.Next)})
	}})
	mcp.AddRawTool(srv, mcp.RawTool[ShowArgs]{Name: "show", Description: desc(3), Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, a ShowArgs) (mcp.Result, error) {
		token, e := cfg.Transfers.Token(ctx, c)
		if e != nil {
			return mcp.Result{}, engineError(e)
		}
		f, e := cfg.Drive.Show(ctx, token, a.File)
		if e != nil {
			return mcp.Result{}, driveError(e)
		}
		return raw(f)
	}})
	mcp.AddTool(srv, mcp.Tool[PullArgs, Record]{Name: "pull", Description: desc(4), Effect: mcp.Destructive, Handler: func(ctx context.Context, c identity.Caller, a PullArgs) (Record, error) {
		t, e := cfg.Transfers.Pull(ctx, c, transfers.PullRequest{File: a.File, Path: a.Path, Format: val(a.Format), Expires: a.Expires})
		if e != nil {
			return Record{}, engineError(e)
		}
		return RecordOf(t), nil
	}})
	mcp.AddTool(srv, mcp.Tool[PushArgs, Record]{Name: "push", Description: desc(5), Effect: mcp.Destructive, Handler: func(ctx context.Context, c identity.Caller, a PushArgs) (Record, error) {
		t, e := cfg.Transfers.Push(ctx, c, transfers.PushRequest{Path: a.Path, Folder: val(a.Folder), Name: val(a.Name), File: val(a.File)})
		if e != nil {
			return Record{}, engineError(e)
		}
		return RecordOf(t), nil
	}})
	mcp.AddTool(srv, mcp.Tool[TransfersArgs, TransferList]{Name: "transfers", Description: desc(6), Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, a TransfersArgs) (TransferList, error) {
		size, e := paging.Window(a.Limit)
		if e != nil {
			return TransferList{}, e
		}
		var started time.Time
		var stamp, id string
		if val(a.Page) != "" {
			if e := (paging.Token{Tool: "transfers"}).Decode(*a.Page, &stamp, &id); e != nil {
				return TransferList{}, paging.ErrToken
			}
			started, e = time.Parse(time.RFC3339, stamp)
			if e != nil || started.UTC().Format(time.RFC3339) != stamp || !store.ValidID(id) {
				return TransferList{}, paging.ErrToken
			}
		}
		ts, e := cfg.Store.ListAfter(ctx, c.UserID, started, id, size+1)
		if e != nil {
			return TransferList{}, refusal("unavailable", "")
		}
		var next *string
		if len(ts) > size {
			ts = ts[:size]
			last := ts[len(ts)-1]
			next = ptr((paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID))
		}
		rs := make([]Record, 0, len(ts))
		for _, t := range ts {
			rs = append(rs, RecordOf(t))
		}
		return TransferList{rs, next}, nil
	}})
	mcp.AddTool(srv, mcp.Tool[TransferArgs, Record]{Name: "transfer", Description: desc(7), Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, a TransferArgs) (Record, error) {
		if !store.ValidID(a.Transfer) {
			return Record{}, refusal("not_found", "")
		}
		t, e := cfg.Store.Get(ctx, c.UserID, a.Transfer)
		if errors.Is(e, store.ErrNotFound) {
			return Record{}, refusal("not_found", "")
		}
		if e != nil {
			return Record{}, refusal("unavailable", "")
		}
		return RecordOf(t), nil
	}})
}
