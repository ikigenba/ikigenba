package panel

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// FormView is the data consumed by the form asset.
type FormView struct {
	Submission widget.Submission
	Errors     widget.FieldErrors
	Statuses   []widget.Status
	Selected   widget.Status
}

func newFormData(sub widget.Submission, errs widget.FieldErrors) FormView {
	d, _ := widget.ParseSubmission(sub)
	return FormView{
		Submission: sub, Errors: errs, Statuses: widget.Statuses(),
		Selected: d.Status,
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
	d, errs := widget.ParseSubmission(sub)
	var created widget.Widget
	var err error
	if errs.Any() {
		rules, checkErr := h.store.Check(r.Context(), d)
		err = checkErr
		if errs.Name == "" {
			errs.Name = rules.Name
		}
		if errs.Count == "" {
			errs.Count = rules.Count
		}
		if errs.Status == "" {
			errs.Status = rules.Status
		}
	} else {
		created, errs, err = h.store.Create(r.Context(), d)
	}
	if err != nil {
		plainFailure(w, r, http.StatusServiceUnavailable, widget.Unreachable+"\n")
		return
	}
	if errs.Any() {
		h.renderPage(w, r, http.StatusUnprocessableEntity, sub, errs)
		return
	}
	h.writer.Emit(r.Context(), "widget.created", telemetry.Attrs{"widget": created.ID})
	w.Header().Set("Location", "/widgets")
	w.WriteHeader(http.StatusSeeOther)
}
