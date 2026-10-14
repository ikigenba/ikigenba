// Package mnparcels collects Minnesota's public parcel records.
package mnparcels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Source constants define the endpoint, paging and retry policy.
const (
	Name           = "mn_parcels"
	DefaultBaseURL = "https://enterprise.gisdata.mn.gov/aghost/rest/services/us_mn_state_mngeo/plan_parcels_open/FeatureServer/1/query"
	PageSize       = 2000
	Retries        = 3
	Backoff        = time.Second
	RequestTimeout = 180 * time.Second
	OwnerRole      = "owner"
	TaxpayerRole   = "taxpayer"
)

// Config supplies the source scope and runtime hooks.
type Config struct {
	HTTP      *http.Client
	BaseURL   string
	Telemetry *telemetry.Writer
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration)
	UserAgent string
	Counties  []string
	Timeout   time.Duration
}

// Scraper walks configured counties in order.
type Scraper struct{ cfg Config }

// New constructs a scraper with the supplied hooks.
func New(cfg Config) *Scraper {
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = RequestTimeout
	}
	if cfg.Sleep == nil {
		cfg.Sleep = pause
	}
	cfg.Counties = slices.Clone(cfg.Counties)
	return &Scraper{cfg}
}
func pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Name identifies the parcel scraper.
func (*Scraper) Name() string { return Name }

// Where quotes a county name for the service's SQL expression.
func Where(county string) string { return "co_name='" + strings.ReplaceAll(county, "'", "''") + "'" }

// Scrape walks every county until its first short page.
func (s *Scraper) Scrape(ctx context.Context, batch func([]source.Row) error) error {
	for _, county := range s.cfg.Counties {
		countQuery := url.Values{"where": {Where(county)}, "returnCountOnly": {"true"}, "f": {"json"}}
		if _, err := s.request(ctx, countQuery, true); err != nil {
			return err
		}
		for offset := 0; ; offset += PageSize {
			query := url.Values{"where": {Where(county)}, "outFields": {"*"}, "returnGeometry": {"false"}, "returnCentroid": {"true"}, "outSR": {"4326"}, "orderByFields": {"objectid"}, "resultOffset": {strconv.Itoa(offset)}, "resultRecordCount": {strconv.Itoa(PageSize)}, "f": {"json"}}
			features, err := s.request(ctx, query, false)
			if err != nil {
				return err
			}
			if len(features) > 0 {
				collected := s.cfg.Now()
				rows := make([]source.Row, len(features))
				for i, feature := range features {
					rows[i], err = RowOf(feature, collected)
					if err != nil {
						return errors.New("unreadable source feature")
					}
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if batch(rows) != nil {
					return errors.New("source batch refused")
				}
			}
			if len(features) < PageSize {
				break
			}
		}
	}
	return ctx.Err()
}

func (s *Scraper) request(ctx context.Context, query url.Values, count bool) ([]json.RawMessage, error) {
	for attempt := 0; attempt <= Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, s.cfg.BaseURL+"?"+query.Encode(), nil)
		if err != nil {
			cancel()
			return nil, errors.New("invalid source request")
		}
		req.Header.Set("User-Agent", s.cfg.UserAgent)
		client := *s.cfg.HTTP
		client.Jar = nil
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		var start time.Time
		if s.cfg.Telemetry != nil {
			start = s.cfg.Telemetry.Now()
		}
		resp, requestErr := client.Do(req)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if s.cfg.Telemetry != nil {
			duration := s.cfg.Telemetry.Now().Sub(start) / time.Microsecond
			if duration < 0 {
				duration = 0
			}
			s.cfg.Telemetry.Emit(ctx, source.Called, telemetry.Attrs{"scraper": Name, "status": int64(status), "duration_us": int64(duration)})
		}
		retry := requestErr != nil || attemptCtx.Err() != nil || status == 429 || status >= 500 && status <= 599
		var features []json.RawMessage
		if requestErr == nil && status == http.StatusOK {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil || attemptCtx.Err() != nil {
				_ = resp.Body.Close()
				cancel()
				return nil, errors.New("unreadable source body")
			}
			answer, valid := object(body)
			if !valid {
				retry = false
			} else if _, exists := answer["error"]; exists {
				retry = true
			} else {
				retry = false
				if count {
					var n json.Number
					raw := answer["count"]
					if len(raw) > 0 && raw[0] != '"' && json.Unmarshal(raw, &n) == nil {
						r, ok := new(big.Rat).SetString(string(n))
						valid = ok && r.Sign() >= 0 && r.IsInt()
					} else {
						valid = false
					}
				} else {
					raw := answer["features"]
					valid = len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &features) == nil && len(features) <= PageSize
					if valid {
						for _, f := range features {
							if _, err := RowOf(f, time.Time{}); err != nil {
								valid = false
								break
							}
						}
					}
				}
				if valid {
					_ = resp.Body.Close()
					cancel()
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					return features, nil
				}
			}
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !retry || attempt == Retries {
			return nil, fmt.Errorf("source request failed (status %d)", status)
		}
		s.cfg.Sleep(ctx, Backoff<<attempt)
	}
	return nil, errors.New("source retries exhausted")
}

func object(raw []byte) (map[string]json.RawMessage, bool) {
	var value map[string]json.RawMessage
	err := json.Unmarshal(raw, &value)
	return value, err == nil && value != nil
}
func stringValue(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}
func text(raw json.RawMessage) string {
	if len(raw) > 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') {
		return string(raw)
	}
	return stringValue(raw)
}
func joined(a map[string]json.RawMessage, fields ...string) []string {
	result := []string{}
	for _, key := range fields {
		if value := strings.TrimSpace(stringValue(a[key])); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// RowOf maps a feature to its common envelope, preserving attributes exactly.
func RowOf(feature json.RawMessage, collected time.Time) (source.Row, error) {
	f, ok := object(feature)
	if !ok {
		return source.Row{}, errors.New("invalid source feature")
	}
	a, ok := object(f["attributes"])
	if !ok {
		return source.Row{}, errors.New("invalid source attributes")
	}
	id := stringValue(a["state_pin"])
	if strings.TrimSpace(id) == "" {
		countyID := stringValue(a["county_pin"])
		if strings.TrimSpace(countyID) == "" {
			return source.Row{}, errors.New("source feature lacks parcel id")
		}
		id = stringValue(a["co_code"]) + "-" + countyID
	}
	owner := strings.Join(joined(a, "owner_name", "owner_more"), " ")
	taxpayer := strings.TrimSpace(stringValue(a["tax_name"]))
	names := source.Names(owner, taxpayer)
	name := ""
	if len(names) > 0 {
		name = names[0]
	}
	street := []string{}
	for _, key := range []string{"anumberpre", "anumber", "anumbersuf", "st_pre_dir", "st_pre_typ", "st_name", "st_pos_typ", "st_pos_dir"} {
		if value := strings.TrimSpace(text(a[key])); value != "" {
			street = append(street, value)
		}
	}
	city := stringValue(a["ctu_name"])
	if strings.TrimSpace(city) == "" {
		city = stringValue(a["postcomm"])
	}
	people := []source.Person{}
	if owner != "" {
		people = append(people, source.Person{Name: owner, Role: OwnerRole})
	}
	lowerOwner, lowerTaxpayer := strings.ToLower(owner), strings.ToLower(taxpayer)
	if taxpayer != "" && lowerTaxpayer != lowerOwner {
		people = append(people, source.Person{Name: taxpayer, Role: TaxpayerRole})
	}
	facts := map[string]any{}
	for _, key := range []string{"acres_deed", "acres_poly", "co_code", "county_pin", "emv_bldg", "emv_land", "emv_total", "exported", "homestead", "legal", "ownership", "plat_name", "sale_date", "sale_value", "state_pin", "tax_exempt", "tax_name", "tax_year", "total_tax", "year_built"} {
		field := key
		if key == "legal" {
			field = "abb_legal"
		}
		if key == "exported" {
			field = "exp_date"
		}
		if value, exists := a[field]; exists {
			facts[key] = value
		} else {
			facts[key] = nil
		}
	}
	facts["owner_name"] = nil
	if owner != "" {
		facts["owner_name"] = owner
	}
	facts["owner_address"] = joined(a, "own_add_l1", "own_add_l2", "own_add_l3", "own_add_l4")
	facts["tax_address"] = joined(a, "tax_add_l1", "tax_add_l2", "tax_add_l3", "tax_add_l4")
	facts["use_class"] = joined(a, "useclass1", "useclass2", "useclass3", "useclass4")
	factJSON, _ := json.Marshal(facts)
	raw := f["attributes"]
	row := source.Row{Scraper: Name, SourceID: id, Collected: collected.UTC().Truncate(time.Second), Digest: source.Digest(raw), Name: name, Names: names, Address: source.NewAddress(strings.Join(street, " "), city, "MN", stringValue(a["zip"]), stringValue(a["co_name"])), People: people, Facts: factJSON, Raw: raw}
	if centroid, ok := object(f["centroid"]); ok {
		x, y := centroid["x"], centroid["y"]
		numeric := func(raw json.RawMessage) bool {
			return len(raw) > 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9')
		}
		if numeric(x) && numeric(y) {
			lon, _ := strconv.ParseFloat(string(x), 64)
			lat, _ := strconv.ParseFloat(string(y), 64)
			row.Lon, row.Lat = &lon, &lat
		}
	}
	return row, nil
}
