package panel

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

type formData struct {
	Submission     widget.Submission
	Errors         widget.FieldErrors
	Statuses       []widget.Status
	SelectedStatus string
}

func newFormData(sub widget.Submission, errs widget.FieldErrors) formData {
	return formData{
		Submission: sub, Errors: errs, Statuses: widget.Statuses(),
		SelectedStatus: strings.TrimSpace(sub.Status),
	}
}

func (h *handler) serveForm(w http.ResponseWriter, r *http.Request) {
	mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/x-www-form-urlencoded") {
		h.renderFailure(w, r, http.StatusUnsupportedMediaType, UnsupportedMediaTypeMessage)
		return
	}

	// Keep successfully received and decoded fields even if the transport or
	// encoding is incomplete; field validation still determines the answer.
	body, _ := io.ReadAll(r.Body)
	values, _ := url.ParseQuery(string(body))
	sub := widget.Submission{
		Name: values.Get("name"), Count: values.Get("count"), Status: values.Get("status"),
	}
	_, errs := h.store.Create(sub)
	if errs.Any() {
		h.renderPage(w, r, http.StatusUnprocessableEntity, sub, errs)
		return
	}
	w.Header().Set("Location", "/widgets")
	w.WriteHeader(http.StatusSeeOther)
}
