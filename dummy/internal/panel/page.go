package panel

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/ikigenba/ikigenba/appkit"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

type pageData struct {
	ServiceName    string
	Banner         appkit.Banner
	BannerMarkup   string
	Subtitle       string
	Message        string
	FailureService bool
	Panel          bool
	Widgets        []widget.Widget
	Form           formData
}

func (h *handler) pageData(r *http.Request) pageData {
	user := appkit.User{Email: r.Header.Get("X-User-Email"), ProfileURL: ProfileURL(r.Host, r.Header.Get("X-Forwarded-Proto")), LogoutURL: LogoutURL(r.Host, r.Header.Get("X-Forwarded-Proto"))}
	return pageData{ServiceName: ServiceName, Banner: h.banner(user)}
}

func (h *handler) renderPage(w http.ResponseWriter, r *http.Request, status int, sub widget.Submission, errs widget.FieldErrors) {
	data := h.pageData(r)
	data.Panel = true
	data.Widgets = h.store.All()
	noun := " widgets"
	if len(data.Widgets) == 1 {
		noun = " widget"
	}
	data.Subtitle = strconv.Itoa(len(data.Widgets)) + noun + " · refreshes every 5 seconds"
	data.Form = newFormData(sub, errs)
	h.renderDocument(w, r, status, data)
}

func (h *handler) renderFailure(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := h.pageData(r)
	data.Message = message
	data.FailureService = status == http.StatusUnsupportedMediaType
	h.renderDocument(w, r, status, data)
}

func (h *handler) renderDocument(w http.ResponseWriter, r *http.Request, status int, data pageData) {
	var banner bytes.Buffer
	if err := h.bannerTemplates.ExecuteTemplate(&banner, "banner", data.Banner); err != nil {
		panic(err)
	}
	data.BannerMarkup = banner.String()
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
