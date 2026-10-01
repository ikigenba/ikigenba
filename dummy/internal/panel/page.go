package panel

import (
	"bytes"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

type pageData struct {
	Banner  page.Banner
	Panel   bool
	Message string
	Count   int
	Table   []widget.Widget
	Form    FormView
}

func (h *handler) pageData(r *http.Request) pageData {
	caller, _ := identity.FromContext(r.Context())
	user := page.User{Email: caller.Email, ProfileURL: ProfileURL(r.Host, r.Header.Get("X-Forwarded-Proto")), LogoutURL: LogoutURL(r.Host, r.Header.Get("X-Forwarded-Proto"))}
	return pageData{Banner: h.banner(user)}
}

func (h *handler) renderPage(w http.ResponseWriter, r *http.Request, status int, sub widget.Submission, errs widget.FieldErrors) {
	data := h.pageData(r)
	data.Panel = true
	data.Table = h.store.All()
	data.Count = len(data.Table)
	data.Form = newFormData(sub, errs)
	h.renderDocument(w, r, status, data)
}

func (h *handler) renderFailure(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := h.pageData(r)
	data.Message = message
	h.renderDocument(w, r, status, data)
}

func (h *handler) renderDocument(w http.ResponseWriter, r *http.Request, status int, data pageData) {
	var body bytes.Buffer
	if err := h.templates.ExecuteTemplate(&body, "page", data); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
