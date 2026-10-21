package enrichers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/ikigenba/ikigenba/candidates/internal/routes"
)

// KindDriveRoute and SourceGoogleRoutes identify requested drives.
const (
	KindDriveRoute     = "drive_route"
	SourceGoogleRoutes = "google_routes"
)

// RouteFound records a drive and the outcome of its requests.
type RouteFound struct {
	Minutes   *int64   `json:"minutes,omitempty"`
	Miles     *float64 `json:"miles,omitempty"`
	Address   string   `json:"address"`
	Precision string   `json:"precision"`
	Status    string   `json:"status"`
	Why       string   `json:"why"`
	Error     string   `json:"request_error"`
	CostNanos int64    `json:"cost_nanos"`
}

// Failed reports whether a request failed.
func (f RouteFound) Failed() bool { return f.Error != "" }

// Tally records request counts and cost.
type Tally struct{ Sent, Failed, CostNanos int64 }

// Failer recognizes failed recorded outcomes.
type Failer interface {
	Failed(found json.RawMessage) bool
}

// Router obtains a drive for one candidate subject.
type Router interface {
	Enricher
	Ready() bool
	Route(ctx context.Context, s Subject) (Result, Tally)
	Failed(found json.RawMessage) bool
}

// DriveRoute requests drives through the routes client.
type DriveRoute struct{ Routes *routes.Client }

// Kind returns the route kind word.
func (DriveRoute) Kind() string { return KindDriveRoute }

// Fetched reports that routes use an outside service.
func (DriveRoute) Fetched() bool { return true }

// Inputs fingerprints the address and origin without normalization.
func (DriveRoute) Inputs(s Subject) string {
	if s.Street == "" || s.Origin == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s.Street + "\t" + s.City + "\t" + s.State + "\t" + s.Zip + "\t" + s.Origin))
	return hex.EncodeToString(sum[:])
}

// Destination constructs the destination address.
func Destination(s Subject) string {
	out := s.Street
	if s.City != "" {
		out += ", " + s.City
	}
	if s.State != "" {
		out += ", " + s.State
	}
	if s.Zip != "" {
		out += " " + s.Zip
	}
	return out
}

// Ready reports whether requests can be made.
func (d DriveRoute) Ready() bool { return d.Routes != nil && d.Routes.HTTP != nil }

// Route records exactly one drive outcome.
func (d DriveRoute) Route(ctx context.Context, s Subject) (Result, Tally) {
	o := d.Routes.Drive(ctx, s.Origin, Destination(s))
	r := result(SourceGoogleRoutes, RouteFound{o.Minutes, o.Miles, o.Address, o.Precision, o.Status, o.Why, o.Error, o.CostNanos})
	r.Cost = float64(o.CostNanos) / 1e9
	return r, Tally{o.Sent, o.Failed, o.CostNanos}
}

// Valid checks the strict route result shape.
func (DriveRoute) Valid(b json.RawMessage) bool {
	var f RouteFound
	return strictObject(b, &f) && nonnegative(f.Minutes) && (f.Miles == nil || *f.Miles >= 0) && f.CostNanos >= 0 && (f.Precision == "" || f.Precision == routes.PrecisionBuilding || f.Precision == routes.PrecisionStreet || f.Precision == routes.PrecisionTown)
}

// Failed reports a decoded request failure.
func (DriveRoute) Failed(b json.RawMessage) bool {
	var f RouteFound
	return json.Unmarshal(b, &f) == nil && f.Failed()
}
