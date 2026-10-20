// Package ppp streams the SBA's public Paycheck Protection Program files.
package ppp

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Source constants define the folder and request policy.
const (
	Name           = "ppp"
	DefaultBaseURL = "https://data.sba.gov/sites/default/files/distribution/SBA-OCA-2022-07-001/"
	BatchSize      = 1000
	Retries        = 3
	Backoff        = time.Second
	HeaderTimeout  = 60 * time.Second
	BodyTimeout    = 20 * time.Minute
)

// Config supplies the runtime hooks and source scope.
type Config struct {
	HTTP          *http.Client
	BaseURL       string
	Telemetry     *telemetry.Writer
	Now           func() time.Time
	Sleep         func(ctx context.Context, d time.Duration)
	UserAgent     string
	Files         []string
	States        []string
	HeaderTimeout time.Duration
	BodyTimeout   time.Duration
}

// Scraper streams one file at a time.
type Scraper struct{ cfg Config }

// New constructs a scraper with the supplied hooks.
func New(cfg Config) *Scraper {
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.HeaderTimeout == 0 {
		cfg.HeaderTimeout = HeaderTimeout
	}
	if cfg.BodyTimeout == 0 {
		cfg.BodyTimeout = BodyTimeout
	}
	if cfg.Sleep == nil {
		cfg.Sleep = pause
	}
	cfg.Files = slices.Clone(cfg.Files)
	cfg.States = slices.Clone(cfg.States)
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

// Name identifies the PPP scraper.
func (*Scraper) Name() string { return Name }

// Columns returns an independent copy of the source header.
func Columns() []string {
	return []string{"LoanNumber", "DateApproved", "SBAOfficeCode", "ProcessingMethod", "BorrowerName", "BorrowerAddress", "BorrowerCity", "BorrowerState", "BorrowerZip", "LoanStatusDate", "LoanStatus", "Term", "SBAGuarantyPercentage", "InitialApprovalAmount", "CurrentApprovalAmount", "UndisbursedAmount", "FranchiseName", "ServicingLenderLocationID", "ServicingLenderName", "ServicingLenderAddress", "ServicingLenderCity", "ServicingLenderState", "ServicingLenderZip", "RuralUrbanIndicator", "HubzoneIndicator", "LMIIndicator", "BusinessAgeDescription", "ProjectCity", "ProjectCountyName", "ProjectState", "ProjectZip", "CD", "JobsReported", "NAICSCode", "Race", "Ethnicity", "UTILITIES_PROCEED", "PAYROLL_PROCEED", "MORTGAGE_INTEREST_PROCEED", "RENT_PROCEED", "REFINANCE_EIDL_PROCEED", "HEALTH_CARE_PROCEED", "DEBT_INTEREST_PROCEED", "BusinessType", "OriginatingLenderLocationID", "OriginatingLender", "OriginatingLenderCity", "OriginatingLenderState", "Gender", "Veteran", "NonProfit", "ForgivenessAmount", "ForgivenessDate"}
}
func decode(record []string) []string {
	for i, v := range record {
		var b strings.Builder
		for j := 0; j < len(v); j++ {
			b.WriteRune(rune(v[j]))
		}
		record[i] = b.String()
	}
	return record
}

// Scrape walks the configured files in their configured order.
func (s *Scraper) Scrape(ctx context.Context, batch func([]source.Row) error) error {
	for _, file := range s.cfg.Files {
		resp, cancel, err := s.request(ctx, file)
		if err != nil {
			return err
		}
		err = s.read(resp.Request.Context(), resp.Body, batch)
		_ = resp.Body.Close()
		cancel()
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (s *Scraper) request(ctx context.Context, file string) (*http.Response, context.CancelFunc, error) {
	for attempt := 0; attempt <= Retries; attempt++ {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		attemptCtx, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(s.cfg.HeaderTimeout, cancel)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, s.cfg.BaseURL+file, nil)
		if err != nil {
			timer.Stop()
			cancel()
			return nil, nil, errors.New("invalid source request")
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
		timer.Stop()
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
		if err == nil && attemptCtx.Err() == nil && status == http.StatusOK {
			resp.Request = req
			bodyTimer := time.AfterFunc(s.cfg.BodyTimeout, cancel)
			return resp, func() { bodyTimer.Stop(); cancel() }, nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		retry := err != nil || attemptCtx.Err() != nil || status == 429 || status >= 500 && status <= 599
		cancel()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if !retry || attempt == Retries {
			return nil, nil, fmt.Errorf("source request failed (status %d)", status)
		}
		s.cfg.Sleep(ctx, Backoff<<attempt)
	}
	return nil, nil, errors.New("source retries exhausted")
}

type endingReader struct {
	io.Reader
	last byte
}

func (r *endingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.last = p[n-1]
	}
	return n, err
}
func (s *Scraper) read(ctx context.Context, body io.Reader, batch func([]source.Row) error) error {
	ending := &endingReader{Reader: body}
	reader := csv.NewReader(ending)
	header, err := reader.Read()
	if err != nil || !slices.Equal(decode(header), Columns()) {
		return errors.New("unreadable source header")
	}
	records := make([][]string, 0, BatchSize)
	flush := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		scraped := s.cfg.Now()
		rows := make([]source.Row, len(records))
		for i, record := range records {
			var err error
			rows[i], err = RowOf(record, scraped)
			if err != nil {
				return errors.New("unreadable source record")
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if batch(rows) != nil {
			return errors.New("source batch refused")
		}
		records = records[:0]
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("unreadable source body")
		}
		record = decode(record)
		state := record[7]
		if state == "" {
			state = record[29]
		}
		if !slices.Contains(s.cfg.States, strings.ToUpper(state)) {
			continue
		}
		if record[0] == "" {
			return errors.New("source record lacks loan number")
		}
		records = append(records, record)
		if len(records) == BatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if ending.last != '\n' {
		return errors.New("source body lacks final line end")
	}
	if len(records) > 0 {
		return flush()
	}
	return ctx.Err()
}

// RowOf maps decoded CSV fields to their envelope and ordered raw JSON.
func RowOf(record []string, scraped time.Time) (source.Row, error) {
	columns := Columns()
	if len(record) != len(columns) || record[0] == "" {
		return source.Row{}, errors.New("invalid source record")
	}
	values := make(map[string]string, len(columns))
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, column := range columns {
		values[column] = record[i]
		if i > 0 {
			raw.WriteByte(',')
		}
		key, _ := json.Marshal(column)
		value, _ := json.Marshal(record[i])
		raw.Write(key)
		raw.WriteByte(':')
		raw.Write(value)
	}
	raw.WriteByte('}')
	fallback := func(a, b string) string {
		if values[a] != "" {
			return values[a]
		}
		return values[b]
	}
	mapping := map[string]string{"amount": "CurrentApprovalAmount", "business_age": "BusinessAgeDescription", "business_type": "BusinessType", "cd": "CD", "date_approved": "DateApproved", "debt_interest_proceed": "DEBT_INTEREST_PROCEED", "forgiven": "ForgivenessAmount", "forgiveness_date": "ForgivenessDate", "franchise": "FranchiseName", "health_care_proceed": "HEALTH_CARE_PROCEED", "hubzone": "HubzoneIndicator", "initial_amount": "InitialApprovalAmount", "jobs_reported": "JobsReported", "lender": "OriginatingLender", "lender_city": "OriginatingLenderCity", "lender_state": "OriginatingLenderState", "lmi": "LMIIndicator", "loan_number": "LoanNumber", "loan_status": "LoanStatus", "loan_status_date": "LoanStatusDate", "mortgage_interest_proceed": "MORTGAGE_INTEREST_PROCEED", "naics": "NAICSCode", "nonprofit": "NonProfit", "payroll_proceed": "PAYROLL_PROCEED", "processing_method": "ProcessingMethod", "refinance_eidl_proceed": "REFINANCE_EIDL_PROCEED", "rent_proceed": "RENT_PROCEED", "rural_urban": "RuralUrbanIndicator", "sba_office": "SBAOfficeCode", "servicing_lender": "ServicingLenderName", "term": "Term", "undisbursed": "UndisbursedAmount", "utilities_proceed": "UTILITIES_PROCEED"}
	facts := make(map[string]string, len(mapping))
	for key, column := range mapping {
		facts[key] = values[column]
	}
	factJSON, _ := json.Marshal(facts)
	names := source.Names(values["BorrowerName"])
	name := ""
	if len(names) > 0 {
		name = names[0]
	}
	return source.Row{Scraper: Name, SourceID: record[0], Scraped: scraped.UTC().Truncate(time.Second), Digest: source.Digest(raw.Bytes()), Name: name, Names: names, Address: source.NewAddress(values["BorrowerAddress"], fallback("BorrowerCity", "ProjectCity"), fallback("BorrowerState", "ProjectState"), fallback("BorrowerZip", "ProjectZip"), values["ProjectCountyName"]), People: []source.Person{}, Facts: factJSON, Raw: raw.Bytes()}, nil
}
