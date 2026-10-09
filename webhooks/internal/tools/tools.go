// Package tools registers webhooks' MCP tools.
package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks/internal/ingress"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/trail"
	"github.com/ikigenba/ikigenba/webhooks/internal/urls"
)

// Config supplies the records and the two event sinks.
type Config struct {
	Store     *store.Store
	Telemetry *telemetry.Writer
	Events    *events.Emitter
}

// Scheme is a webhook's scheme as a tool argument.
type Scheme string

// Enum lists the two schemes.
func (Scheme) Enum() []string { return []string{store.Bearer, store.GitHubHMAC} }

// ListArgs has no arguments.
type ListArgs struct{}

// SlugArgs names a webhook.
type SlugArgs struct {
	Slug string `json:"slug" mcp:"required" description:"The webhook's slug."`
}

// CreateArgs supplies a new webhook.
type CreateArgs struct {
	Slug   string  `json:"slug" mcp:"required" description:"The new webhook's slug: a lowercase letter, then lowercase letters and digits with single underscores between them, 1 to 64 characters, not already a webhook's slug in the space."`
	Scheme *Scheme `json:"scheme" description:"How a sender proves it holds the secret: bearer (the default), the secret in the X-Webhook-Secret header; or github-hmac, the body signed with HMAC-SHA256 in X-Hub-Signature-256."`
}

// DeliveryArgs names a delivery.
type DeliveryArgs struct {
	ID string `json:"id" mcp:"required" description:"The delivery's id, the delivery attribute of its received event."`
}

// Webhook describes a single webhook.
type Webhook struct {
	ID           string  `json:"id"`
	Slug         string  `json:"slug"`
	Scheme       string  `json:"scheme"`
	URL          string  `json:"url"`
	Owner        string  `json:"owner"`
	Created      string  `json:"created"`
	LastReceived *string `json:"last_received"`
}

// Minted describes a webhook with the secret just minted for it.
type Minted struct {
	ID           string  `json:"id"`
	Slug         string  `json:"slug"`
	Scheme       string  `json:"scheme"`
	URL          string  `json:"url"`
	Owner        string  `json:"owner"`
	Created      string  `json:"created"`
	LastReceived *string `json:"last_received"`
	Secret       string  `json:"secret"`
}

// ListedWebhook omits the created time.
type ListedWebhook struct {
	ID           string  `json:"id"`
	Slug         string  `json:"slug"`
	Scheme       string  `json:"scheme"`
	URL          string  `json:"url"`
	Owner        string  `json:"owner"`
	LastReceived *string `json:"last_received"`
}

// WebhookList holds webhooks in slug order.
type WebhookList struct {
	Webhooks []ListedWebhook `json:"webhooks"`
}

// Deleted confirms removal.
type Deleted struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// Header is one kept header of a delivery.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Delivery describes one accepted delivery, its body included.
type Delivery struct {
	ID          string   `json:"id"`
	Hook        string   `json:"hook"`
	Received    string   `json:"received"`
	ContentType string   `json:"content_type"`
	Headers     []Header `json:"headers"`
	Body        *string  `json:"body"`
	BodyBase64  *string  `json:"body_base64"`
}

// Entry names a tool and its description.
type Entry struct{ Name, Description string }

// Catalog lists the six tools in the order they are registered.
func Catalog() []Entry {
	return []Entry{
		{"create", createDescription},
		{"list", listDescription},
		{"show", showDescription},
		{"rotate", rotateDescription},
		{"delete", deleteDescription},
		{"delivery", deliveryDescription},
	}
}

// Register registers the six public tools.
func Register(srv *mcp.Server, cfg Config) {
	h := handlers{cfg}
	mcp.AddTool(srv, mcp.Tool[CreateArgs, Minted]{Name: "create", Description: createDescription, Effect: mcp.Additive, Handler: h.create})
	mcp.AddTool(srv, mcp.Tool[ListArgs, WebhookList]{Name: "list", Description: listDescription, Effect: mcp.Read, Handler: h.list})
	mcp.AddTool(srv, mcp.Tool[SlugArgs, Webhook]{Name: "show", Description: showDescription, Effect: mcp.Read, Handler: h.show})
	mcp.AddTool(srv, mcp.Tool[SlugArgs, Minted]{Name: "rotate", Description: rotateDescription, Effect: mcp.Destructive, Handler: h.rotate})
	mcp.AddTool(srv, mcp.Tool[SlugArgs, Deleted]{Name: "delete", Description: deleteDescription, Effect: mcp.Destructive, Handler: h.delete})
	mcp.AddTool(srv, mcp.Tool[DeliveryArgs, Delivery]{Name: "delivery", Description: deliveryDescription, Effect: mcp.Read, Handler: h.delivery})
}

type handlers struct{ cfg Config }

// NoWebhook describes a webhook the caller cannot find or does not own.
func NoWebhook(slug string) string { return fmt.Sprintf("no webhook named '%s'", slug) }

// InvalidSlug describes a refused slug.
func InvalidSlug(slug string) string { return fmt.Sprintf("invalid slug '%s'", slug) }

// SlugTaken describes an occupied slug.
func SlugTaken(slug string) string { return fmt.Sprintf("a webhook named '%s' already exists", slug) }

// NoDelivery describes a delivery the caller cannot find or may not read.
func NoDelivery(id string) string { return fmt.Sprintf("no delivery '%s'", id) }

func answerError(err error, slug string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return errors.New(NoWebhook(slug))
	case errors.Is(err, store.ErrSlugTaken):
		return errors.New(SlugTaken(slug))
	case errors.Is(err, store.ErrInvalid):
		return errors.New(InvalidSlug(slug))
	default:
		return errors.New(store.Unreachable)
	}
}

func text(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

func object(ctx context.Context, x store.Webhook) Webhook {
	base, _ := urls.FromContext(ctx)
	w := Webhook{ID: x.ID, Slug: x.Slug, Scheme: x.Scheme, URL: urls.Hook(base, x.Slug), Owner: x.OwnerEmail, Created: text(x.Created)}
	if !x.LastReceived.IsZero() {
		v := text(x.LastReceived)
		w.LastReceived = &v
	}
	return w
}

func minted(ctx context.Context, x store.Webhook, secret string) Minted {
	w := object(ctx, x)
	return Minted{ID: w.ID, Slug: w.Slug, Scheme: w.Scheme, URL: w.URL, Owner: w.Owner, Created: w.Created, LastReceived: w.LastReceived, Secret: secret}
}

func (h handlers) list(ctx context.Context, _ identity.Caller, _ ListArgs) (WebhookList, error) {
	xs, err := h.cfg.Store.List(ctx)
	if err != nil {
		return WebhookList{}, answerError(err, "")
	}
	result := WebhookList{Webhooks: make([]ListedWebhook, 0, len(xs))}
	for _, x := range xs {
		v := object(ctx, x)
		result.Webhooks = append(result.Webhooks, ListedWebhook{ID: v.ID, Slug: v.Slug, Scheme: v.Scheme, URL: v.URL, Owner: v.Owner, LastReceived: v.LastReceived})
	}
	return result, nil
}

func (h handlers) show(ctx context.Context, _ identity.Caller, a SlugArgs) (Webhook, error) {
	if !store.ValidSlug(a.Slug) {
		return Webhook{}, errors.New(NoWebhook(a.Slug))
	}
	x, err := h.cfg.Store.Get(ctx, a.Slug)
	if err != nil {
		return Webhook{}, answerError(err, a.Slug)
	}
	return object(ctx, x), nil
}

func (h handlers) create(ctx context.Context, c identity.Caller, a CreateArgs) (Minted, error) {
	if !store.ValidSlug(a.Slug) {
		return Minted{}, errors.New(InvalidSlug(a.Slug))
	}
	scheme := store.Bearer
	if a.Scheme != nil {
		scheme = string(*a.Scheme)
	}
	x, secret, err := h.cfg.Store.Create(ctx, store.Draft{Slug: a.Slug, Scheme: scheme, OwnerID: c.UserID, OwnerEmail: c.Email})
	if err != nil {
		return Minted{}, answerError(err, a.Slug)
	}
	trail.Lifecycle(ctx, h.cfg.Telemetry, h.cfg.Events, trail.Created, x)
	return minted(ctx, x, secret), nil
}

func (h handlers) owned(ctx context.Context, c identity.Caller, slug string) (store.Webhook, error) {
	if !store.ValidSlug(slug) {
		return store.Webhook{}, store.ErrNotFound
	}
	x, err := h.cfg.Store.Get(ctx, slug)
	if err != nil {
		return store.Webhook{}, err
	}
	if x.OwnerID != c.UserID {
		return store.Webhook{}, store.ErrNotFound
	}
	return x, nil
}

func (h handlers) rotate(ctx context.Context, c identity.Caller, a SlugArgs) (Minted, error) {
	x, err := h.owned(ctx, c, a.Slug)
	if err != nil {
		return Minted{}, answerError(err, a.Slug)
	}
	x, secret, err := h.cfg.Store.Rotate(ctx, x.ID)
	if err != nil {
		return Minted{}, answerError(err, a.Slug)
	}
	trail.Lifecycle(ctx, h.cfg.Telemetry, h.cfg.Events, trail.Rotated, x)
	return minted(ctx, x, secret), nil
}

func (h handlers) delete(ctx context.Context, c identity.Caller, a SlugArgs) (Deleted, error) {
	x, err := h.owned(ctx, c, a.Slug)
	if err != nil {
		return Deleted{}, answerError(err, a.Slug)
	}
	x, err = h.cfg.Store.Delete(ctx, x.ID)
	if err != nil {
		return Deleted{}, answerError(err, a.Slug)
	}
	trail.Lifecycle(ctx, h.cfg.Telemetry, h.cfg.Events, trail.Deleted, x)
	return Deleted{Deleted: true, ID: x.ID}, nil
}

func (h handlers) delivery(ctx context.Context, c identity.Caller, a DeliveryArgs) (Delivery, error) {
	if !store.ValidDeliveryID(a.ID) {
		return Delivery{}, errors.New(NoDelivery(a.ID))
	}
	d, x, err := h.cfg.Store.Delivery(ctx, a.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && x.OwnerID != c.UserID) {
		return Delivery{}, errors.New(NoDelivery(a.ID))
	}
	if err != nil {
		return Delivery{}, errors.New(store.Unreachable)
	}
	result := Delivery{ID: d.ID, Hook: x.Slug, Received: text(d.Received), ContentType: d.ContentType, Headers: make([]Header, 0, 3)}
	for _, kept := range []Header{{"Content-Type", d.ContentType}, {ingress.GitHubEventHeader, d.GitHubEvent}, {ingress.GitHubDeliveryHeader, d.GitHubDelivery}} {
		if kept.Value != "" {
			result.Headers = append(result.Headers, kept)
		}
	}
	if utf8.Valid(d.Body) {
		v := string(d.Body)
		result.Body = &v
	} else {
		v := base64.StdEncoding.EncodeToString(d.Body)
		result.BodyBase64 = &v
	}
	return result, nil
}

// FirstLine returns a description's first line, the line the tools page shows.
func FirstLine(description string) string {
	line, _, _ := strings.Cut(description, "\n")
	return line
}

const createDescription = "Create a webhook that turns each authenticated delivery from outside into an event on the suite's event bus.\n\nslug is 1 to 64 characters: a lowercase letter, then lowercase letters and digits, with single underscores between them; it must not already be a webhook's slug in the space, whoever owns that webhook. scheme is how a sender proves it holds the secret, fixed for the webhook's life: bearer, the default, sends the secret itself in the X-Webhook-Secret header (n8n's Header Auth does this); github-hmac signs the raw body with HMAC-SHA256 under the secret and sends X-Hub-Signature-256: sha256=<hex>, as GitHub does, and its secret is kept in plaintext so it can be checked. The result is what show returns plus secret, minted here as whs_ and 52 characters; it is shown this once and never again, so hand it and url to the sender now. A sender POSTs to url; an accepted delivery is answered 202, and a wrong or missing secret or signature, like an unknown slug, is answered 404. Each accepted delivery emits webhook.<slug>.received with attrs hook (the webhook's id), delivery (the delivery's id), type (X-GitHub-Event for github-hmac, otherwise empty), content_type and bytes; fetch its body with delivery. Creating it emits webhook.<slug>.created. The webhook is yours: only you can rotate or delete it, or read its deliveries."

const listDescription = "Every webhook in the space, by slug.\n\nTakes no arguments. Every user's webhooks are listed, not only yours. Each has its id, slug, scheme (bearer or github-hmac), url (the address a sender posts to), owner (the email of the user who created it), and last_received (when it last accepted a delivery; absent until its first). Times are UTC. No secret is ever shown. Use show for one webhook's created time."

const showDescription = "One webhook, with its scheme, its address and when it last received a delivery.\n\nPass slug, the webhook's slug; any user's webhook can be shown. The result has its id, slug, scheme (bearer or github-hmac), url (the address a sender posts to), owner (the email of the user who created it), created, and last_received (when it last accepted a delivery; absent until its first). Times are UTC. The secret is never shown again after create or rotate."

const rotateDescription = "Replace the secret of a webhook you own.\n\nPass slug. A new secret is minted and the old one stops working at once; the url, the scheme and the id stay the same. The result is what show returns plus secret, the new one, shown this once and never again: hand it to the sender now. Rotating emits webhook.<slug>.rotated. Another user's webhook is refused as one that does not exist."

const deleteDescription = "Delete a webhook you own, and every delivery it holds.\n\nPass slug. Its url answers 404 from then on, its deliveries can no longer be fetched, and deleting it emits webhook.<slug>.deleted. Its slug is free for anyone to take; a webhook created with the slug later is a new webhook with its own id and secret. Another user's webhook is refused as one that does not exist. The result is deleted, true, and the id of the deleted webhook."

const deliveryDescription = "One delivery to a webhook you own, its body included.\n\nPass id, the delivery attribute of a webhook.<slug>.received event. The result has its id, hook (the webhook's slug), received (UTC), content_type (as the sender sent it; empty when it sent none), headers (the ones kept and present: Content-Type, X-GitHub-Event, X-GitHub-Delivery, each a name and a value), and the body exactly as sent: body as text when it is valid UTF-8, otherwise body_base64. Deliveries are kept for a retention window, two days unless the space says otherwise, and go with their webhook when it is deleted. A delivery to another user's webhook is refused as one that does not exist."
