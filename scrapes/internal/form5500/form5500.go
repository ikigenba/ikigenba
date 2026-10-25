// Package form5500 reads the Department of Labor's zipped annual filings.
package form5500

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Source constants identify the forms and request policy.
const (
	Name              = "form5500"
	DefaultBaseURL    = "https://askebsa.dol.gov/FOIA%20Files/"
	BatchSize         = 1000
	Retries           = 3
	Backoff           = time.Second
	HeaderTimeout     = 60 * time.Second
	FileTimeout       = 30 * time.Minute
	SponsorSignerRole = "sponsor signer"
	AdminSignerRole   = "administrator signer"
)

// Form identifies a filing's schema.
type Form string

// Full and Short identify the two supported forms.
const (
	Full  Form = "5500"
	Short Form = "5500-SF"
)

// Config supplies runtime hooks and filing scope.
type Config struct {
	HTTP          *http.Client
	BaseURL       string
	Telemetry     *telemetry.Writer
	Now           func() time.Time
	Sleep         func(ctx context.Context, d time.Duration)
	UserAgent     string
	Years         []string
	States        []string
	HeaderTimeout time.Duration
	FileTimeout   time.Duration
}

// Scraper walks one archive at a time.
type Scraper struct{ cfg Config }

// New constructs a scraper using the supplied hooks.
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
	if cfg.FileTimeout == 0 {
		cfg.FileTimeout = FileTimeout
	}
	if cfg.Sleep == nil {
		cfg.Sleep = func(ctx context.Context, d time.Duration) {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
			case <-t.C:
			}
		}
	}
	cfg.Years = slices.Clone(cfg.Years)
	cfg.States = slices.Clone(cfg.States)
	return &Scraper{cfg}
}

// Name identifies the scraper.
func (*Scraper) Name() string { return Name }

var fullColumns = strings.Fields(`ACK_ID FORM_PLAN_YEAR_BEGIN_DATE FORM_TAX_PRD TYPE_PLAN_ENTITY_CD INITIAL_FILING_IND AMENDED_IND FINAL_FILING_IND SHORT_PLAN_YR_IND COLLECTIVE_BARGAIN_IND PLAN_NAME SPONS_DFE_PN PLAN_EFF_DATE SPONSOR_DFE_NAME SPONS_DFE_DBA_NAME SPONS_DFE_CARE_OF_NAME SPONS_DFE_MAIL_US_ADDRESS1 SPONS_DFE_MAIL_US_ADDRESS2 SPONS_DFE_MAIL_US_CITY SPONS_DFE_MAIL_US_STATE SPONS_DFE_MAIL_US_ZIP SPONS_DFE_LOC_US_ADDRESS1 SPONS_DFE_LOC_US_ADDRESS2 SPONS_DFE_LOC_US_CITY SPONS_DFE_LOC_US_STATE SPONS_DFE_LOC_US_ZIP SPONS_DFE_MAIL_FOREIGN_CNTRY SPONS_DFE_EIN SPONS_DFE_PHONE_NUM BUSINESS_CODE ADMIN_NAME ADMIN_EIN LAST_RPT_SPONS_NAME LAST_RPT_SPONS_EIN SPONS_SIGNED_NAME SPONS_SIGNED_DATE ADMIN_SIGNED_NAME ADMIN_SIGNED_DATE TOT_PARTCP_BOY_CNT TOT_ACT_PARTCP_BOY_CNT TOT_ACTIVE_PARTCP_CNT TYPE_PENSION_BNFT_CODE TYPE_WELFARE_BNFT_CODE DATE_RECEIVED FILING_STATUS PREPARER_FIRM_NAME`)
var shortColumns = strings.Fields(`ACK_ID SF_PLAN_YEAR_BEGIN_DATE SF_TAX_PRD SF_PLAN_ENTITY_CD SF_INITIAL_FILING_IND SF_AMENDED_IND SF_FINAL_FILING_IND SF_SHORT_PLAN_YR_IND SF_PLAN_NAME SF_PLAN_NUM SF_PLAN_EFF_DATE SF_SPONSOR_NAME SF_SPONSOR_DFE_DBA_NAME SF_SPONS_CARE_OF_NAME SF_SPONS_US_ADDRESS1 SF_SPONS_US_ADDRESS2 SF_SPONS_US_CITY SF_SPONS_US_STATE SF_SPONS_US_ZIP SF_SPONS_LOC_US_ADDRESS1 SF_SPONS_LOC_US_ADDRESS2 SF_SPONS_LOC_US_CITY SF_SPONS_LOC_US_STATE SF_SPONS_LOC_US_ZIP SF_SPONS_FOREIGN_CNTRY SF_SPONS_EIN SF_SPONS_PHONE_NUM SF_BUSINESS_CODE SF_ADMIN_NAME SF_ADMIN_EIN SF_LAST_RPT_SPONS_NAME SF_LAST_RPT_SPONS_EIN SF_SPONS_SIGNED_NAME SF_SPONS_SIGNED_DATE SF_ADMIN_SIGNED_NAME SF_ADMIN_SIGNED_DATE SF_TOT_PARTCP_BOY_CNT SF_TOT_ACT_PARTCP_BOY_CNT SF_TOT_ACT_PARTCP_EOY_CNT SF_TYPE_PENSION_BNFT_CODE SF_TYPE_WELFARE_BNFT_CODE SF_EMPLR_CONTRIB_INCOME_AMT SF_PARTICIP_CONTRIB_INCOME_AMT SF_NET_ASSETS_EOY_AMT DATE_RECEIVED FILING_STATUS SF_PREPARER_FIRM_NAME`)

// Columns returns independent required column names for a form.
func Columns(f Form) []string {
	switch f {
	case Full:
		return slices.Clone(fullColumns)
	case Short:
		return slices.Clone(shortColumns)
	default:
		return []string{}
	}
}
func index(f Form, h []string) (map[string]int, error) {
	if f != Full && f != Short {
		return nil, errors.New("invalid source form")
	}
	m := make(map[string]int, len(h))
	for i, c := range h {
		if _, ok := m[c]; ok {
			return nil, errors.New("duplicate source column")
		}
		m[c] = i
	}
	for _, c := range Columns(f) {
		if _, ok := m[c]; !ok {
			return nil, errors.New("missing source column")
		}
	}
	return m, nil
}
func join(a, b string) string {
	return strings.TrimSpace(strings.TrimSpace(a) + " " + strings.TrimSpace(b))
}

// RowOf maps a named record into its envelope, facts and ordered raw encoding.
func RowOf(f Form, header, record []string, scraped time.Time) (source.Row, error) {
	idx, err := index(f, header)
	if err != nil || len(record) != len(header) || len(record) == 0 {
		return source.Row{}, errors.New("invalid source record")
	}
	v := func(c string) string {
		if i, ok := idx[c]; ok {
			return record[i]
		}
		return ""
	}
	if v("ACK_ID") == "" {
		return source.Row{}, errors.New("source record lacks identifier")
	}
	cols := Columns(f)
	if f == Short {
		cols = append(cols[:8], append([]string{"COLLECTIVELY_BARGAINED"}, cols[8:]...)...)
	}
	keys := strings.Fields(`ack_id plan_year_begin plan_year_end entity initial_filing amended final_filing short_plan_year collectively_bargained plan_name plan_number plan_effective sponsor_name dba care_of mail_street mail_city mail_state mail_zip loc_street loc_city loc_state loc_zip foreign_country ein phone business_code admin_name admin_ein last_sponsor_name last_sponsor_ein sponsor_signed_name sponsor_signed_date admin_signed_name admin_signed_date participants_boy active_participants_boy active_participants_eoy pension_codes welfare_codes employer_contributions participant_contributions net_assets_eoy received filing_status preparer_firm`)
	facts := map[string]string{"form": string(f)}
	ci := 0
	for _, key := range keys {
		switch key {
		case "mail_street", "loc_street":
			facts[key] = join(v(cols[ci]), v(cols[ci+1]))
			ci += 2
		case "employer_contributions", "participant_contributions", "net_assets_eoy":
			if f == Full {
				facts[key] = ""
			} else {
				facts[key] = v(cols[ci])
				ci++
			}
		default:
			facts[key] = v(cols[ci])
			ci++
		}
	}
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, c := range header {
		if i > 0 {
			raw.WriteByte(',')
		}
		k, _ := json.Marshal(c)
		val, _ := json.Marshal(record[i])
		raw.Write(k)
		raw.WriteByte(':')
		raw.Write(val)
	}
	raw.WriteByte('}')
	names := source.Names(facts["sponsor_name"], facts["dba"])
	name := ""
	if len(names) > 0 {
		name = names[0]
	}
	prefix := "mail_"
	if facts["loc_state"] != "" {
		prefix = "loc_"
	}
	people := []source.Person{}
	ss := strings.TrimSpace(facts["sponsor_signed_name"])
	as := strings.TrimSpace(facts["admin_signed_name"])
	if ss != "" {
		people = append(people, source.Person{Name: ss, Role: SponsorSignerRole})
	}
	sponsorLower, adminLower := strings.ToLower(ss), strings.ToLower(as)
	if as != "" && sponsorLower != adminLower {
		people = append(people, source.Person{Name: as, Role: AdminSignerRole})
	}
	factJSON, _ := json.Marshal(facts)
	return source.Row{Scraper: Name, SourceID: v("ACK_ID"), Scraped: scraped.UTC().Truncate(time.Second), Digest: source.Digest(raw.Bytes()), Name: name, Names: names, Address: source.NewAddress(facts[prefix+"street"], facts[prefix+"city"], facts[prefix+"state"], facts[prefix+"zip"], ""), People: people, Facts: factJSON, Raw: raw.Bytes()}, nil
}

// Scrape downloads and reads each configured year's full and short forms.
func (s *Scraper) Scrape(ctx context.Context, batch func([]source.Row) error) error {
	spool := source.SpoolFrom(ctx)
	if spool == nil {
		return errors.New("source spool unavailable")
	}
	for _, y := range s.cfg.Years {
		for _, f := range []Form{Full, Short} {
			suffix := ""
			if f == Short {
				suffix = "SF_"
			}
			resp, fc, cancel, err := s.request(ctx, y+"/Latest/F_5500_"+suffix+y+"_Latest.zip")
			if err != nil {
				return err
			}
			err = s.file(fc, resp.Body, spool, f, batch)
			_ = resp.Body.Close()
			cancel()
			if err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
func (s *Scraper) request(ctx context.Context, path string) (*http.Response, context.Context, context.CancelFunc, error) {
	for attempt := 0; attempt <= Retries; attempt++ {
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		ac, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(s.cfg.HeaderTimeout, cancel)
		req, err := http.NewRequestWithContext(ac, http.MethodGet, s.cfg.BaseURL+path, nil)
		if err != nil {
			timer.Stop()
			cancel()
			return nil, nil, nil, errors.New("invalid source request")
		}
		req.Header.Set("User-Agent", s.cfg.UserAgent)
		var start time.Time
		if s.cfg.Telemetry != nil {
			start = s.cfg.Telemetry.Now()
		}
		client := *s.cfg.HTTP
		client.Jar = nil
		resp, err := client.Do(req)
		timer.Stop()
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		success := err == nil && ac.Err() == nil && status == http.StatusOK
		var ft *time.Timer
		if success {
			ft = time.AfterFunc(s.cfg.FileTimeout, cancel)
		}
		if s.cfg.Telemetry != nil {
			d := s.cfg.Telemetry.Now().Sub(start) / time.Microsecond
			if d < 0 {
				d = 0
			}
			s.cfg.Telemetry.Emit(ctx, source.Called, telemetry.Attrs{"scraper": Name, "status": int64(status), "duration_us": int64(d)})
		}
		if success {
			return resp, ac, func() { ft.Stop(); cancel() }, nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		retry := err != nil || ac.Err() != nil || status == 429 || status >= 500 && status <= 599
		cancel()
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		if !retry || attempt == Retries {
			return nil, nil, nil, errors.New("source request failed")
		}
		s.cfg.Sleep(ctx, Backoff<<attempt)
	}
	return nil, nil, nil, errors.New("source request failed")
}
func (s *Scraper) file(ctx context.Context, body io.Reader, spool source.Spool, f Form, batch func([]source.Row) error) error {
	file, err := spool()
	if err != nil {
		return errors.New("source spool unavailable")
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	n, err := io.Copy(file, body)
	if err != nil || ctx.Err() != nil {
		return errors.New("unreadable source body")
	}
	z, err := zip.NewReader(file, n)
	if err != nil || len(z.File) != 1 || !strings.HasSuffix(strings.ToLower(z.File[0].Name), ".csv") {
		return errors.New("unreadable source archive")
	}
	entry, err := z.File[0].Open()
	if err != nil {
		return errors.New("unreadable source entry")
	}
	defer func() { _ = entry.Close() }()
	reader := csv.NewReader(entry)
	h, err := reader.Read()
	if err != nil {
		return errors.New("unreadable source header")
	}
	idx, err := index(f, h)
	if err != nil {
		return err
	}
	mail, loc := "SPONS_DFE_MAIL_US_STATE", "SPONS_DFE_LOC_US_STATE"
	if f == Short {
		mail, loc = "SF_SPONS_US_STATE", "SF_SPONS_LOC_US_STATE"
	}
	records := make([][]string, 0, BatchSize)
	flush := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stamp := s.cfg.Now()
		rows := make([]source.Row, len(records))
		for i, r := range records {
			var e error
			rows[i], e = RowOf(f, h, r, stamp)
			if e != nil {
				return e
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if batch(rows) != nil {
			return errors.New("source batch refused")
		}
		records = records[:0]
		return ctx.Err()
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return errors.New("unreadable source record")
		}
		if !slices.Contains(s.cfg.States, strings.ToUpper(r[idx[mail]])) && !slices.Contains(s.cfg.States, strings.ToUpper(r[idx[loc]])) {
			continue
		}
		if r[idx["ACK_ID"]] == "" {
			return errors.New("source record lacks identifier")
		}
		records = append(records, r)
		if len(records) == BatchSize {
			if e := flush(); e != nil {
				return e
			}
		}
	}
	if len(records) > 0 {
		return flush()
	}
	return ctx.Err()
}
