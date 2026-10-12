package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

// Config describes the typed tool contract.
type Config struct {
	Transfers *transfers.Engine
	Drafts    *drafts.Service
	Gmail     *gmail.Client
	Store     *store.Store
}

func refuse(word, url string) error {
	sentences := map[string]string{"not_connected": RefuseNotConnected, "reconnect": RefuseReconnect, "not_found": RefuseNotFound, "too_large": RefuseTooLarge, "provider": RefuseProvider, "path": RefusePath, "expires": RefuseExpires, "unavailable": RefuseUnavailable, "stopping": RefuseStopping, "queue_full": RefuseQueueFull, "not_open": RefuseNotOpen, "busy": RefuseBusy, "to": RefuseTo, "address": RefuseAddress}
	s, ok := sentences[word]
	if !ok {
		word = "unavailable"
		s = RefuseUnavailable
		url = ""
	}
	v := word + ": " + s
	if url != "" {
		v += " " + url
	}
	return errors.New(v)
}
func serviceError(err error) error {
	if err == nil {
		return nil
	}
	var r *transfers.Refusal
	if errors.As(err, &r) {
		return refuse(r.Word, r.URL)
	}
	return refuse("unavailable", "")
}
func gmailError(cfg Config, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gmail.ErrNotFound) {
		return refuse("not_found", "")
	}
	if errors.Is(err, gmail.ErrUnauthorized) {
		return refuse("reconnect", cfg.Transfers.ConnectURL())
	}
	return refuse("provider", "")
}
func add[I, O any](srv *mcp.Server, name, description string, e mcp.Effect, fn func(context.Context, identity.Caller, I) (O, error)) {
	mcp.AddTool(srv, mcp.Tool[I, O]{Name: name, Description: description + "\n\n" + MailGuide, Effect: e, Handler: fn})
}
func messagePage(pg gmail.Page) MessageList {
	a := make([]Summary, 0, len(pg.Messages))
	for _, s := range pg.Messages {
		a = append(a, SummaryOf(s))
	}
	return MessageList{a, text(pg.Next)}
}

// Register supplies the public tool contract.
func Register(srv *mcp.Server, cfg Config) {
	add(srv, "search", DescribeSearch, mcp.Read, func(ctx context.Context, c identity.Caller, a SearchArgs) (MessageList, error) {
		tok, err := cfg.Transfers.Token(ctx, c)
		if err != nil {
			return MessageList{}, serviceError(err)
		}
		pg, err := cfg.Gmail.Search(ctx, tok, gmail.Query{Text: a.Query, Label: a.Label, Page: a.Page})
		return messagePage(pg), gmailError(cfg, err)
	})
	add(srv, "thread", DescribeThread, mcp.Read, func(ctx context.Context, c identity.Caller, a ThreadArgs) (MessageList, error) {
		tok, err := cfg.Transfers.Token(ctx, c)
		if err != nil {
			return MessageList{}, serviceError(err)
		}
		if a.Thread == "" {
			return MessageList{}, refuse("not_found", "")
		}
		pg, err := cfg.Gmail.Thread(ctx, tok, a.Thread, a.Page)
		return messagePage(pg), gmailError(cfg, err)
	})
	add(srv, "show", DescribeShow, mcp.Read, func(ctx context.Context, c identity.Caller, a ShowArgs) (Message, error) {
		tok, err := cfg.Transfers.Token(ctx, c)
		if err != nil {
			return Message{}, serviceError(err)
		}
		if a.Message == "" {
			return Message{}, refuse("not_found", "")
		}
		v, err := cfg.Gmail.Show(ctx, tok, a.Message)
		return MessageOf(v), gmailError(cfg, err)
	})
	add(srv, "labels", DescribeLabels, mcp.Read, func(ctx context.Context, c identity.Caller, _ NoArgs) (LabelList, error) {
		tok, err := cfg.Transfers.Token(ctx, c)
		if err != nil {
			return LabelList{}, serviceError(err)
		}
		ls, err := cfg.Gmail.Labels(ctx, tok)
		v := make([]Label, 0, len(ls))
		for _, l := range ls {
			v = append(v, LabelOf(l))
		}
		return LabelList{v}, gmailError(cfg, err)
	})
	add(srv, "pull", DescribePull, mcp.Destructive, func(ctx context.Context, c identity.Caller, a PullArgs) (Record, error) {
		v, err := cfg.Transfers.Pull(ctx, c, transfers.PullRequest{Message: a.Message, Path: a.Path, Attachment: a.Attachment, Expires: a.Expires})
		return RecordOf(v), serviceError(err)
	})
	add(srv, "compose", DescribeCompose, mcp.Additive, func(ctx context.Context, c identity.Caller, a ComposeArgs) (Draft, error) {
		v, err := cfg.Drafts.Compose(ctx, c, drafts.ComposeRequest{To: a.To, Cc: a.Cc, Bcc: a.Bcc, Subject: a.Subject, Body: a.Body, ReplyTo: a.ReplyTo})
		return DraftOf(v, false), serviceError(err)
	})
	add(srv, "push", DescribePush, mcp.Destructive, func(ctx context.Context, c identity.Caller, a PushArgs) (Record, error) {
		v, err := cfg.Transfers.Push(ctx, c, transfers.PushRequest{Path: a.Path, Draft: a.Draft})
		return RecordOf(v), serviceError(err)
	})
	add(srv, "send", DescribeSend, mcp.Destructive, func(ctx context.Context, c identity.Caller, a DraftArgs) (Draft, error) {
		v, err := cfg.Drafts.Send(ctx, c, a.Draft)
		return DraftOf(v, false), serviceError(err)
	})
	add(srv, "discard", DescribeDiscard, mcp.Destructive, func(ctx context.Context, c identity.Caller, a DraftArgs) (Draft, error) {
		v, err := cfg.Drafts.Discard(ctx, c, a.Draft)
		return DraftOf(v, false), serviceError(err)
	})
	add(srv, "drafts", DescribeDrafts, mcp.Read, func(ctx context.Context, c identity.Caller, _ NoArgs) (DraftList, error) {
		ls, err := cfg.Store.ListDrafts(ctx, c.UserID)
		v := make([]Draft, 0, len(ls))
		for _, d := range ls {
			v = append(v, DraftOf(d, false))
		}
		return DraftList{v}, serviceError(err)
	})
	add(srv, "draft", DescribeDraft, mcp.Read, func(ctx context.Context, c identity.Caller, a DraftArgs) (Draft, error) {
		v, err := cfg.Drafts.Get(ctx, c, a.Draft)
		return DraftOf(v, true), serviceError(err)
	})
	add(srv, "transfers", DescribeTransfers, mcp.Read, func(ctx context.Context, c identity.Caller, _ NoArgs) (TransferList, error) {
		ls, err := cfg.Store.ListTransfers(ctx, c.UserID)
		v := make([]Record, 0, len(ls))
		for _, t := range ls {
			v = append(v, RecordOf(t))
		}
		return TransferList{v}, serviceError(err)
	})
	add(srv, "transfer", DescribeTransfer, mcp.Read, func(ctx context.Context, c identity.Caller, a TransferArgs) (Record, error) {
		if !store.ValidTransferID(a.Transfer) {
			return Record{}, refuse("not_found", "")
		}
		v, err := cfg.Store.GetTransfer(ctx, c.UserID, a.Transfer)
		if errors.Is(err, store.ErrNotFound) {
			return Record{}, refuse("not_found", "")
		}
		return RecordOf(v), serviceError(err)
	})
}
