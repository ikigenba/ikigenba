package panel

import (
	"bytes"
	"context"
	"net/http"
	"slices"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

type pageData struct {
	Banner  page.Banner
	Panel   bool
	Message string
	Count   int
	Table   []widget.Widget
	Form    FormView
	Page    string
	Prev    string
	Next    string
}

func (h *handler) pageData(r *http.Request) pageData {
	caller, _ := identity.FromContext(r.Context())
	user := page.User{Email: caller.Email, ProfileURL: ProfileURL(r.Host, r.Header.Get("X-Forwarded-Proto")), LogoutURL: LogoutURL(r.Host, r.Header.Get("X-Forwarded-Proto"))}
	return pageData{Banner: h.banner(user)}
}

func (h *handler) renderPage(w http.ResponseWriter, r *http.Request, status int, sub widget.Submission, errs widget.FieldErrors) {
	pageValue := ""
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		pageValue = r.URL.Query().Get("page")
	}
	cursor, err := pageCursor(pageValue)
	if err != nil {
		h.renderFailure(w, r, http.StatusNotFound, NotFoundMessage)
		return
	}
	window, err := h.window(r.Context(), cursor)
	if err != nil {
		plainFailure(w, r, http.StatusServiceUnavailable, widget.Unreachable+"\n")
		return
	}
	if window.underflow {
		w.Header().Set("Location", "/widgets")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	data := h.pageData(r)
	data.Panel = true
	data.Table = window.widgets
	data.Count = len(data.Table)
	data.Page = pageValue
	data.Prev = window.prev
	data.Next = window.next
	data.Form = newFormData(sub, errs)
	h.renderDocument(w, r, status, data)
}

func (h *handler) renderFailure(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := h.pageData(r)
	data.Message = message
	h.renderDocument(w, r, status, data)
}

func (h *handler) renderDocument(w http.ResponseWriter, r *http.Request, status int, data pageData) {
	h.renderTemplate(w, r, status, "page", data)
}

func (h *handler) renderTemplate(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var body bytes.Buffer
	if err := h.templates.ExecuteTemplate(&body, name, data); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

type cursor struct {
	key    *int64
	before bool
	last   bool
}

func pageCursor(value string) (cursor, error) {
	if value == "" {
		return cursor{}, nil
	}
	var seq int64
	if err := (paging.Token{Tool: "list_widgets"}).Decode(value, &seq); err == nil {
		return cursor{key: &seq}, nil
	}
	err := (paging.Token{Tool: "list_widgets-before"}).Decode(value, &seq)
	if err == nil {
		return cursor{key: &seq, before: true}, nil
	}
	if value == "last" {
		return cursor{last: true}, nil
	}
	return cursor{}, err
}

type widgetWindow struct {
	widgets    []widget.Widget
	prev, next string
	underflow  bool
}

func (h *handler) window(ctx context.Context, c cursor) (widgetWindow, error) {
	var widgets []widget.Widget
	var err error
	if c.before || c.last {
		widgets, err = h.store.ListBefore(ctx, c.key, paging.DefaultLimit+1)
	} else {
		widgets, err = h.store.List(ctx, c.key, paging.DefaultLimit+1)
	}
	if err != nil {
		return widgetWindow{}, err
	}
	result := widgetWindow{}
	more := len(widgets) > paging.DefaultLimit
	if c.before || c.last {
		result.underflow = !more
	}
	if more {
		widgets = widgets[:paging.DefaultLimit]
	}
	if c.before || c.last {
		slices.Reverse(widgets)
	}
	result.widgets = widgets
	if len(widgets) > 0 {
		if !c.last && (c.before || more) {
			result.next = (paging.Token{Tool: "list_widgets"}).Encode(widgets[len(widgets)-1].Seq)
		}
		if (c.key != nil || c.last) && !result.underflow {
			result.prev = (paging.Token{Tool: "list_widgets-before"}).Encode(widgets[0].Seq)
		}
	}
	return result, nil
}
