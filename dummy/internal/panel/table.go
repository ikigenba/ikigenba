package panel

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (h *handler) table(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		plainFailure(w, r, http.StatusMethodNotAllowed, MethodNotAllowedBody)
		return
	}

	var body bytes.Buffer
	if err := h.templates.ExecuteTemplate(&body, "table", h.store.All()); err != nil {
		panic(err)
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(body.Bytes()))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("ETag", etag)
	for _, field := range r.Header.Values("If-None-Match") {
		for entry := range strings.SplitSeq(field, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "*" || entry == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
