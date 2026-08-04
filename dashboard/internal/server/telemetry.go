package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"appkit/telemetry"
)

// recordEdge builds the deliberately small, allowlisted forensic record for an
// auth decision. The chassis recorder is asynchronous and nil-receiver-safe.
func (a *app) recordEdge(r *http.Request, correlationID, decision string, status int, reason, ownerEmail, clientID string) {
	if a.telemetryRecorder == nil {
		return
	}
	method := r.Header.Get("X-Original-Method")
	if method == "" {
		method = r.Method
	}
	originalURI := r.Header.Get("X-Original-URI")
	actor := (*telemetry.Actor)(nil)
	if ownerEmail != "" || clientID != "" {
		actor = &telemetry.Actor{OwnerEmail: ownerEmail, ClientID: clientID}
	}
	a.telemetryRecorder.Record(telemetry.Record{
		CorrelationID: correlationID,
		Service:       "dashboard",
		Kind:          telemetry.KindEdge,
		Actor:         actor,
		Op:            method + " " + originalURI,
		Outcome: &telemetry.Outcome{
			Status: strconv.Itoa(status),
			Error:  reason,
		},
		Detail: map[string]any{
			"decision": decision,
			"service":  addressedService(originalURI),
		},
	})
}

func addressedService(originalURI string) string {
	parsed, err := url.Parse(originalURI)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "srv" {
		return parts[1]
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
