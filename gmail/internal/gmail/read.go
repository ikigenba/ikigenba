package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

type wirePart struct {
	PartID   string                         `json:"partId"`
	Filename string                         `json:"filename"`
	MimeType string                         `json:"mimeType"`
	Headers  []struct{ Name, Value string } `json:"headers"`
	Body     struct {
		Size         int64   `json:"size"`
		Data         *string `json:"data"`
		AttachmentID string  `json:"attachmentId"`
	} `json:"body"`
	Parts []wirePart `json:"parts"`
}
type wireMessage struct {
	ID      *string  `json:"id"`
	Thread  string   `json:"threadId"`
	Labels  []string `json:"labelIds"`
	Snippet string   `json:"snippet"`
	Date    *string  `json:"internalDate"`
	Size    int64    `json:"sizeEstimate"`
	Payload wirePart `json:"payload"`
}

func walk(p *wirePart, fn func(*wirePart)) {
	fn(p)
	for i := range p.Parts {
		walk(&p.Parts[i], fn)
	}
}

func (p *wirePart) header(name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func date(s *string) (time.Time, error) {
	if s == nil {
		return time.UnixMilli(0).UTC(), nil
	}
	n, err := strconv.ParseInt(*s, 10, 64)
	if err != nil {
		return time.Time{}, ErrProvider
	}
	return time.UnixMilli(n).UTC(), nil
}

func decodeURL(s string) ([]byte, error) {
	if strings.ContainsAny(s, "\r\n") {
		return nil, ErrProvider
	}
	encoding := base64.RawURLEncoding
	if strings.Contains(s, "=") {
		encoding = base64.URLEncoding
	}
	b, err := encoding.Strict().DecodeString(s)
	if err != nil {
		return nil, ErrProvider
	}
	return b, nil
}

func validMessage(m wireMessage) error {
	if m.ID == nil {
		return ErrProvider
	}
	if _, err := date(m.Date); err != nil {
		return err
	}
	var err error
	walk(&m.Payload, func(p *wirePart) {
		if p.Body.Data != nil {
			if _, e := decodeURL(*p.Body.Data); e != nil {
				err = e
			}
		}
	})
	return err
}

// validateArrays also rejects null date and data fields that pointer decoding
// would otherwise treat as absent.
func validateArrays(b []byte) error {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return ErrProvider
	}
	var inspect func(any) bool
	inspect = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, w := range x {
				if k == "internalDate" || k == "data" || k == "raw" {
					if _, ok := w.(string); !ok {
						return false
					}
				}
				if k == "messages" || k == "labels" || k == "parts" || k == "headers" || k == "labelIds" {
					if _, ok := w.([]any); !ok {
						return false
					}
				}
				if !inspect(w) {
					return false
				}
			}
		case []any:
			for _, w := range x {
				if !inspect(w) {
					return false
				}
			}
		}
		return true
	}
	if !inspect(v) {
		return ErrProvider
	}
	return nil
}

func parseMessage(b []byte) (wireMessage, error) {
	var m wireMessage
	if object(b, &m) != nil || validateArrays(b) != nil || validMessage(m) != nil {
		return wireMessage{}, ErrProvider
	}
	return m, nil
}

func summary(m wireMessage, labels []Label) Summary {
	d, _ := date(m.Date)
	s := Summary{ID: *m.ID, Thread: m.Thread, Date: d, From: m.Payload.header("From"), To: m.Payload.header("To"), Cc: m.Payload.header("Cc"), Subject: m.Payload.header("Subject"), Snippet: html.UnescapeString(m.Snippet), Labels: make([]string, 0, len(m.Labels)), Link: urls.MessageLink(*m.ID)}
	for _, id := range m.Labels {
		name := id
		for _, l := range labels {
			if l.ID == id {
				name = l.Name
				break
			}
		}
		s.Labels = append(s.Labels, name)
		if id == "UNREAD" {
			s.Unread = true
		}
	}
	walk(&m.Payload, func(p *wirePart) {
		if p.Filename != "" {
			s.Attachments++
		}
	})
	return s
}

// Search calls the provider and reduces its answer to connector types.
func (c *Client) Search(ctx context.Context, token string, q Query) (Page, error) {
	if q.Limit < 1 || q.Limit > paging.MaxLimit {
		return Page{}, ErrProvider
	}
	labels, err := c.Labels(ctx, token)
	if err != nil {
		return Page{}, err
	}
	params := url.Values{"maxResults": {strconv.Itoa(q.Limit)}, "includeSpamTrash": {"false"}}
	if q.Text != "" {
		params.Set("q", q.Text)
	}
	if q.Page != "" {
		params.Set("pageToken", q.Page)
	}
	if q.Label != "" {
		found := false
		for _, l := range labels {
			if strings.EqualFold(l.Name, q.Label) {
				params.Set("labelIds", l.ID)
				found = true
				break
			}
		}
		if !found {
			return Page{}, ErrNotFound
		}
	}
	b, err := c.request(ctx, token, http.MethodGet, api+"messages", EndpointList, params, nil, q.Page != "", -1)
	if err != nil {
		return Page{}, err
	}
	var list struct {
		Messages []struct {
			ID *string `json:"id"`
		} `json:"messages"`
		Next string `json:"nextPageToken"`
	}
	if object(b, &list) != nil || validateArrays(b) != nil {
		return Page{}, ErrProvider
	}
	for _, m := range list.Messages {
		if m.ID == nil {
			return Page{}, ErrProvider
		}
	}
	p := Page{Messages: make([]Summary, 0, len(list.Messages)), Next: list.Next}
	for _, item := range list.Messages {
		b, e := c.request(ctx, token, http.MethodGet, api+"messages/"+segment(*item.ID), EndpointGet, url.Values{"format": {"full"}, "fields": {SummaryFields}}, nil, true, -1)
		// Only a 404, rather than the category ErrNotFound (which includes 400), is skipped.
		if e != nil {
			return Page{}, e
		}
		if b == nil {
			continue
		}
		m, e := parseMessage(b)
		if e != nil {
			return Page{}, e
		}
		p.Messages = append(p.Messages, summary(m, labels))
	}
	return p, nil
}

// Thread calls the provider and reduces its answer to connector types.
func (c *Client) Thread(ctx context.Context, token, thread, page string) (Page, error) {
	if thread == "" {
		return Page{}, ErrNotFound
	}
	offset := 0
	if page != "" {
		n, err := strconv.Atoi(page)
		if err != nil || n <= 0 || strconv.Itoa(n) != page {
			return Page{}, ErrBadPage
		}
		offset = n
	}
	labels, err := c.Labels(ctx, token)
	if err != nil {
		return Page{}, err
	}
	ms, err := c.threadMessages(ctx, token, thread, ThreadFields)
	if err != nil {
		return Page{}, err
	}
	all := make([]Summary, 0, len(ms))
	for _, m := range ms {
		all = append(all, summary(m, labels))
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Date.Before(all[j].Date) })
	p := Page{Messages: make([]Summary, 0)}
	if offset >= len(all) {
		return p, nil
	}
	end := min(offset+PageSize, len(all))
	p.Messages = append(p.Messages, all[offset:end]...)
	if end < len(all) {
		p.Next = strconv.Itoa(end)
	}
	return p, nil
}

func (c *Client) threadMessages(ctx context.Context, token, thread, fields string) ([]wireMessage, error) {
	b, err := c.request(ctx, token, http.MethodGet, api+"threads/"+segment(thread), EndpointThread, url.Values{"format": {"full"}, "fields": {fields}}, nil, false, -1)
	if err != nil {
		return nil, err
	}
	var response struct {
		Messages []wireMessage `json:"messages"`
	}
	if object(b, &response) != nil || validateArrays(b) != nil {
		return nil, ErrProvider
	}
	for _, m := range response.Messages {
		if validMessage(m) != nil {
			return nil, ErrProvider
		}
	}
	return response.Messages, nil
}

// Show calls the provider and reduces its answer to connector types.
func (c *Client) Show(ctx context.Context, token, message string) (Message, error) {
	if message == "" {
		return Message{}, ErrNotFound
	}
	labels, err := c.Labels(ctx, token)
	if err != nil {
		return Message{}, err
	}
	w, err := c.getMessage(ctx, token, message, "full")
	if err != nil {
		return Message{}, err
	}
	s := summary(w, labels)
	m := Message{ID: s.ID, Thread: s.Thread, Date: s.Date, From: s.From, To: s.To, Cc: s.Cc, Subject: s.Subject, Snippet: s.Snippet, Labels: s.Labels, Unread: s.Unread, Attachments: make([]Attachment, 0), Link: s.Link, MessageID: w.Payload.header("Message-ID"), InReplyTo: w.Payload.header("In-Reply-To"), ReplyTo: w.Payload.header("Reply-To"), References: w.Payload.header("References")}
	var plain, markup *wirePart
	walk(&w.Payload, func(p *wirePart) {
		if p.Filename != "" {
			m.Attachments = append(m.Attachments, Attachment{p.PartID, p.Filename, p.MimeType, p.Body.Size})
			return
		}
		if p.Body.Data == nil {
			return
		}
		if plain == nil && strings.EqualFold(p.MimeType, "text/plain") {
			plain = p
		}
		if markup == nil && strings.EqualFold(p.MimeType, "text/html") {
			markup = p
		}
	})
	source := plain
	if source == nil {
		source = markup
	}
	if source != nil {
		b, _ := decodeURL(*source.Body.Data)
		m.Body = strings.ReplaceAll(strings.ToValidUTF8(string(b), "�"), "\r\n", "\n")
		if plain == nil {
			m.Body = ReduceHTML(m.Body)
		}
	}
	if len(m.Body) > BodyCap {
		end := BodyCap
		for !utf8.RuneStart(m.Body[end]) {
			end--
		}
		m.Body = m.Body[:end]
		m.Truncated = true
	}
	return m, nil
}

func (c *Client) getMessage(ctx context.Context, token, message, format string) (wireMessage, error) {
	b, err := c.request(ctx, token, http.MethodGet, api+"messages/"+segment(message), EndpointGet, url.Values{"format": {format}}, nil, false, -1)
	if err != nil {
		return wireMessage{}, err
	}
	return parseMessage(b)
}

func wireStat(m wireMessage, size bool) Stat {
	d, _ := date(m.Date)
	s := Stat{ID: *m.ID, Thread: m.Thread, Date: d}
	if size {
		s.SizeEstimate = m.Size
	}
	for _, id := range m.Labels {
		if id == "SENT" {
			s.Sent = true
		}
	}
	return s
}

// Stat calls the provider and reduces its answer to connector types.
func (c *Client) Stat(ctx context.Context, token, message string) (Stat, error) {
	if message == "" {
		return Stat{}, ErrNotFound
	}
	m, err := c.getMessage(ctx, token, message, "minimal")
	if err != nil {
		return Stat{}, err
	}
	return wireStat(m, true), nil
}

// ThreadStats calls the provider and reduces its answer to connector types.
func (c *Client) ThreadStats(ctx context.Context, token, thread string) ([]Stat, error) {
	if thread == "" {
		return nil, ErrNotFound
	}
	ms, err := c.threadMessages(ctx, token, thread, "messages(id,threadId,labelIds,internalDate)")
	if err != nil {
		return nil, err
	}
	stats := make([]Stat, 0, len(ms))
	for _, m := range ms {
		stats = append(stats, wireStat(m, false))
	}
	return stats, nil
}

func content(data string, limit int64) (Content, error) {
	decoded, err := decodeURL(data)
	if err != nil {
		return Content{}, err
	}
	if int64(len(decoded)) > limit {
		return Content{}, ErrTooLarge
	}
	enc := base64.RawURLEncoding
	if strings.Contains(data, "=") {
		enc = base64.URLEncoding
	}
	return Content{int64(len(decoded)), base64.NewDecoder(enc, strings.NewReader(data))}, nil
}

// Attachment calls the provider and reduces its answer to connector types.
func (c *Client) Attachment(ctx context.Context, token, message, part string, limit int64) (Content, error) {
	if message == "" || part == "" {
		return Content{}, ErrNotFound
	}
	m, err := c.getMessage(ctx, token, message, "full")
	if err != nil {
		return Content{}, err
	}
	var found *wirePart
	walk(&m.Payload, func(p *wirePart) {
		if found == nil && p.PartID == part && p.Filename != "" {
			found = p
		}
	})
	if found == nil {
		return Content{}, ErrNotFound
	}
	if found.Body.Size > limit {
		return Content{}, ErrTooLarge
	}
	if found.Body.AttachmentID == "" {
		if found.Body.Data != nil {
			return content(*found.Body.Data, limit)
		}
		if found.Body.Size == 0 {
			return content("", limit)
		}
		return Content{}, ErrProvider
	}
	b, err := c.request(ctx, token, http.MethodGet, api+"messages/"+segment(message)+"/attachments/"+segment(found.Body.AttachmentID), EndpointAttachment, nil, nil, false, encodedBound(limit))
	if err != nil {
		return Content{}, err
	}
	var response struct {
		Data *string `json:"data"`
	}
	if object(b, &response) != nil || response.Data == nil {
		return Content{}, ErrProvider
	}
	return content(*response.Data, limit)
}

func encodedBound(limit int64) int64 { return 4*((limit+2)/3) + 65536 }

// Raw calls the provider and reduces its answer to connector types.
func (c *Client) Raw(ctx context.Context, token, message string, limit int64) (Content, error) {
	if message == "" {
		return Content{}, ErrNotFound
	}
	b, err := c.request(ctx, token, http.MethodGet, api+"messages/"+segment(message), EndpointGet, url.Values{"format": {"raw"}}, nil, false, encodedBound(limit))
	if err != nil {
		return Content{}, err
	}
	var response struct {
		Raw *string `json:"raw"`
	}
	if object(b, &response) != nil || response.Raw == nil {
		return Content{}, ErrProvider
	}
	return content(*response.Raw, limit)
}

var _ io.Reader = (*Outgoing)(nil)
