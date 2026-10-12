// Package fmcsa walks the public motor carrier census over Socrata.
package fmcsa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Source constants define the dataset and request policy.
const (
	Name           = "fmcsa"
	DefaultBaseURL = "https://data.transportation.gov/resource/az4n-8mr2.json"
	PageSize       = 10000
	Retries        = 3
	Backoff        = time.Second
	RequestTimeout = 60 * time.Second
	Role           = "company officer"
)

// Config supplies runtime hooks and source scope.
type Config struct {
	HTTP          *http.Client
	BaseURL       string
	Telemetry     *telemetry.Writer
	Now           func() time.Time
	Sleep         func(context.Context, time.Duration)
	UserAgent     string
	States        []string
	MinPowerUnits int64
	Status        string
	Timeout       time.Duration
}

// Scraper walks a source and hands its pages to a batch consumer.
type Scraper struct{ cfg Config }

// New constructs a scraper with the configured runtime hooks.
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
	cfg.States = append([]string(nil), cfg.States...)
	return &Scraper{cfg: cfg}
}
func pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Name identifies the FMCSA scraper.
func (*Scraper) Name() string { return Name }

// Where builds the Socrata scope clause.
func Where(state, status string, n int64) string {
	return "phy_state='" + state + "' AND status_code='" + status + "' AND power_units::number >= " + strconv.FormatInt(n, 10)
}

// Scrape walks each state until its source pages end.
func (s *Scraper) Scrape(ctx context.Context, batch func([]source.Row) error) error {
	for _, state := range s.cfg.States {
		where := Where(state, s.cfg.Status, s.cfg.MinPowerUnits)
		body, err := s.request(ctx, url.Values{"$select": {"count(*)"}, "$where": {where}})
		if err != nil {
			return err
		}
		var count []map[string]json.RawMessage
		if err = json.Unmarshal(body, &count); err != nil || len(count) != 1 {
			return errors.New("unreadable source count")
		}
		var n string
		if json.Unmarshal(count[0]["count"], &n) != nil || !digits(n) {
			return errors.New("unreadable source count")
		}
		for offset := int64(0); ; offset += PageSize {
			body, err = s.request(ctx, url.Values{"$where": {where}, "$order": {"dot_number"}, "$limit": {strconv.Itoa(PageSize)}, "$offset": {strconv.FormatInt(offset, 10)}})
			if err != nil {
				return err
			}
			var records []json.RawMessage
			if err = json.Unmarshal(body, &records); err != nil || records == nil || len(records) > PageSize {
				return errors.New("unreadable source page")
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if len(records) > 0 {
				collected := s.cfg.Now()
				rows := make([]source.Row, len(records))
				for i, raw := range records {
					rows[i], err = RowOf(raw, collected)
					if err != nil {
						return err
					}
				}
				if err = ctx.Err(); err != nil {
					return err
				}
				if err = batch(rows); err != nil {
					return err
				}
			}
			if len(records) < PageSize {
				break
			}
		}
	}
	return ctx.Err()
}
func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func (s *Scraper) request(ctx context.Context, query url.Values) ([]byte, error) {
	for attempt := 0; attempt <= Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, s.cfg.BaseURL+"?"+query.Encode(), nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("User-Agent", s.cfg.UserAgent)
		var start time.Time
		if s.cfg.Telemetry != nil {
			start = s.cfg.Telemetry.Now()
		}
		client := *s.cfg.HTTP
		client.Jar = nil
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := client.Do(req)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if s.cfg.Telemetry != nil {
			elapsed := s.cfg.Telemetry.Now().Sub(start) / time.Microsecond
			if elapsed < 0 {
				elapsed = 0
			}
			s.cfg.Telemetry.Emit(ctx, source.Called, telemetry.Attrs{"scraper": Name, "status": int64(status), "duration_us": int64(elapsed)})
		}
		retry := err != nil || status == 429 || status >= 500 && status <= 599
		if retry {
			if resp != nil {
				_ = resp.Body.Close()
			}
			cancel()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == Retries {
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("source status %d", status)
			}
			s.cfg.Sleep(ctx, Backoff<<attempt)
			continue
		}
		if status != 200 {
			_ = resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("source status %d", status)
		}
		body, readErr := io.ReadAll(resp.Body)
		if readErr == nil {
			readErr = attemptCtx.Err()
		}
		_ = resp.Body.Close()
		cancel()
		if readErr != nil {
			return nil, readErr
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return body, nil
	}
	return nil, errors.New("source retries exhausted")
}

// RowOf maps an original FMCSA record into a row.
func RowOf(raw json.RawMessage, collected time.Time) (source.Row, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return source.Row{}, errors.New("record is not an object")
	}
	str := func(key string) string { var v string; _ = json.Unmarshal(fields[key], &v); return v }
	dot := str("dot_number")
	if dot == "" {
		return source.Row{}, errors.New("record lacks dot_number")
	}
	names := source.Names(str("legal_name"), str("dba_name"))
	name := ""
	if len(names) > 0 {
		name = names[0]
	}
	people := make([]source.Person, 0, 2)
	for _, key := range []string{"company_officer_1", "company_officer_2"} {
		if p := strings.TrimSpace(str(key)); p != "" {
			people = append(people, source.Person{Name: p, Role: Role})
		}
	}
	facts := make(map[string]any)
	keys := map[string]string{"added": "add_date", "class": "classdef", "dot_number": "dot_number", "email": "email_address", "hazmat": "hm_ind", "mcs150_date": "mcs150_date", "mileage": "mcs150_mileage", "mileage_year": "mcs150_mileage_year", "operation": "carrier_operation", "org_type": "business_org_desc", "owned_tractors": "owntract", "owned_trailers": "owntrail", "owned_trucks": "owntruck", "phone": "phone", "power_units": "power_units", "safety_rating": "safety_rating", "status": "status_code", "total_cdl": "total_cdl", "total_drivers": "total_drivers", "truck_units": "truck_units"}
	for key, input := range keys {
		var v *string
		if b, ok := fields[input]; ok && json.Unmarshal(b, &v) == nil && v != nil {
			facts[key] = *v
		} else {
			facts[key] = nil
		}
	}
	cargo := make([]string, 0)
	for key := range fields {
		if strings.HasPrefix(key, "crgo_") && (str(key) == "X" || str(key) == "Y") {
			cargo = append(cargo, strings.TrimPrefix(key, "crgo_"))
		}
	}
	sort.Strings(cargo)
	facts["cargo"] = cargo
	factJSON, err := json.Marshal(facts)
	if err != nil {
		return source.Row{}, err
	}
	return source.Row{Scraper: Name, SourceID: dot, Collected: collected.UTC().Truncate(time.Second), Digest: source.Digest(raw), Name: name, Names: names, Address: source.NewAddress(str("phy_street"), str("phy_city"), str("phy_state"), str("phy_zip"), str("phy_cnty")), People: people, Facts: factJSON, Raw: append(json.RawMessage(nil), raw...)}, nil
}
