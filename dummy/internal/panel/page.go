package panel

import (
	"bytes"
	"net/http"

	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

type pageData struct {
	ServiceName string
	Email       string
	SignOutText string
	SignOutURL  string
	Message     string
	Panel       bool
	Widgets     []widget.Widget
	Form        formData
}

func chromeData(r *http.Request) pageData {
	return pageData{ServiceName: ServiceName, Email: r.Header.Get("X-User-Email"), SignOutText: SignOutText, SignOutURL: SignOutURL(r.Host, r.Header.Get("X-Forwarded-Proto"))}
}

func (h *handler) renderPage(w http.ResponseWriter, r *http.Request, status int, sub widget.Submission, errs widget.FieldErrors) {
	data := chromeData(r)
	data.Panel = true
	data.Widgets = h.store.All()
	data.Form = newFormData(sub, errs)
	h.renderDocument(w, r, status, data)
}

func (h *handler) renderFailure(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := chromeData(r)
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
