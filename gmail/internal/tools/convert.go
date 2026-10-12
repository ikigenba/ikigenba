package tools

import (
	"time"

	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
)

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func optionalTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := stamp(t)
	return &s
}
func list(s []string) []string { return append([]string{}, s...) }

// SummaryOf supplies the public tool contract.
func SummaryOf(s gmail.Summary) Summary {
	return Summary{s.ID, s.Thread, stamp(s.Date), s.From, s.To, s.Cc, s.Subject, s.Snippet, list(s.Labels), s.Unread, int64(s.Attachments), s.Link}
}

// MessageOf supplies the public tool contract.
func MessageOf(m gmail.Message) Message {
	a := make([]Part, 0, len(m.Attachments))
	for _, p := range m.Attachments {
		a = append(a, Part{p.ID, p.Name, p.MimeType, p.Bytes})
	}
	return Message{m.ID, m.Thread, stamp(m.Date), m.From, m.To, m.Cc, m.Subject, m.Snippet, list(m.Labels), m.Unread, m.Link, m.MessageID, m.InReplyTo, m.Body, m.Truncated, a}
}

// LabelOf supplies the public tool contract.
func LabelOf(l gmail.Label) Label { return Label{l.Name, l.System} }

// DraftOf supplies the public tool contract.
func DraftOf(d store.Draft, body bool) Draft {
	a := make([]Attachment, 0, len(d.Attachments))
	for _, p := range d.Attachments {
		a = append(a, Attachment{p.Path, p.Name, p.ContentType, p.Bytes})
	}
	var b *string
	if body {
		v := d.Body
		b = &v
	}
	return Draft{d.ID, text(d.GmailDraft), d.Message, d.Thread, d.Status, list(d.To), list(d.Cc), list(d.Bcc), d.Subject, text(d.ReplyTo), a, text(d.Link), stamp(d.Created), stamp(d.Modified), optionalTime(d.Sent), b}
}

// RecordOf supplies the public tool contract.
func RecordOf(t store.Transfer) Record {
	return Record{t.ID, t.Direction, t.Status, text(t.Reason), t.Message, text(t.Attachment), text(t.Draft), t.Name, t.Path, t.ContentType, t.Bytes, stamp(t.Started), optionalTime(t.Finished), text(t.URL), text(t.Link)}
}
