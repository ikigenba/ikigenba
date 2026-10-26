// Package oshaita reads OSHA Injury Tracking Application establishment summaries.
package oshaita

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Source constants identify the endpoint and request policy.
const (
	Name           = "osha_ita"
	DefaultBaseURL = "https://www.osha.gov/sites/default/"
	UserAgent      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
	BatchSize      = 1000
	Retries        = 3
	Backoff        = time.Second
	HeaderTimeout  = 60 * time.Second
	FileTimeout    = 30 * time.Minute
)

// Config supplies runtime hooks and scope.
type Config struct {
	HTTP          *http.Client
	BaseURL       string
	Telemetry     *telemetry.Writer
	Now           func() time.Time
	Sleep         func(ctx context.Context, d time.Duration)
	Files         []string
	States        []string
	HeaderTimeout time.Duration
	FileTimeout   time.Duration
}

// Scraper reads one configured file at a time.
type Scraper struct{ cfg Config }

// New constructs a scraper with independent scope slices.
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

// Name identifies the scraper.
func (*Scraper) Name() string { return Name }

// Columns returns a fresh required-column list.
func Columns() []string {
	return []string{"id", "company_name", "establishment_name", "street_address", "city", "state", "zip_code", "establishment_id", "year_filing_for", "annual_average_employees", "total_hours_worked"}
}

// Decode preserves UTF-8 and repairs individual Windows-1252 bytes.
func Decode(field string) string {
	const cp = "€\u0081‚ƒ„…†‡ˆ‰Š‹Œ\u008dŽ\u008f\u0090‘’“”•–—˜™š›œ\u009džŸ"
	table := []rune(cp)
	var b strings.Builder
	for len(field) > 0 {
		r, n := utf8.DecodeRuneInString(field)
		if r == utf8.RuneError && n == 1 {
			r = rune(field[0])
			if r >= 0x80 && r <= 0x9f {
				r = table[r-0x80]
			}
			b.WriteRune(r)
		} else {
			b.WriteString(field[:n])
		}
		field = field[n:]
	}
	return b.String()
}

// ParseSubmitted recognizes the three published submission timestamp formats.
func ParseSubmitted(s string) (time.Time, bool) {
	for _, layout := range []string{"1/2/2006 15:04:05", "02Jan06:15:04:05", "1/2/2006"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
func index(header []string) (map[string]int, error) {
	idx := make(map[string]int, len(header))
	for i, c := range header {
		c = strings.ToLower(c)
		if _, ok := idx[c]; ok {
			return nil, errors.New("duplicate source column")
		}
		idx[c] = i
	}
	for _, c := range Columns() {
		if _, ok := idx[c]; !ok {
			return nil, errors.New("missing source column")
		}
	}
	return idx, nil
}
func value(idx map[string]int, record []string, column string) string {
	if i, ok := idx[column]; ok {
		return record[i]
	}
	return ""
}
func submitted(idx map[string]int, record []string) string {
	if _, ok := idx["date_submitted"]; ok {
		return value(idx, record, "date_submitted")
	}
	return value(idx, record, "created_timestamp")
}

// RowOf maps a named record into a normalized envelope and ordered raw object.
func RowOf(header, record []string, scraped time.Time) (source.Row, error) {
	idx, err := index(header)
	if err != nil || len(record) != len(header) || len(record) == 0 {
		return source.Row{}, errors.New("invalid source record")
	}
	v := func(c string) string { return value(idx, record, c) }
	id := strings.TrimSuffix(v("id"), ".00")
	if id == "" {
		return source.Row{}, errors.New("source record lacks identifier")
	}
	facts := make(map[string]string, 16)
	for _, c := range strings.Fields("company_name establishment_name ein id establishment_id year_filing_for naics_code naics_year industry_description establishment_type size annual_average_employees total_hours_worked no_injuries_illnesses date_submitted change_reason") {
		facts[c] = v(c)
	}
	facts["id"] = id
	facts["establishment_id"] = strings.TrimSuffix(v("establishment_id"), ".00")
	facts["date_submitted"] = submitted(idx, record)
	factJSON, _ := json.Marshal(facts)
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
	names := source.Names(v("company_name"), v("establishment_name"))
	name := ""
	if len(names) > 0 {
		name = names[0]
	}
	return source.Row{Scraper: Name, SourceID: id, Scraped: scraped.UTC().Truncate(time.Second), Digest: source.Digest(raw.Bytes()), Name: name, Names: names, Address: source.NewAddress(v("street_address"), v("city"), v("state"), v("zip_code"), ""), People: []source.Person{}, Facts: factJSON, Raw: raw.Bytes()}, nil
}
func escaped(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// Scrape downloads each file, keeping only its latest in-scope amendments.
func (s *Scraper) Scrape(ctx context.Context, batch func([]source.Row) error) error {
	spool := source.SpoolFrom(ctx)
	if spool == nil {
		return errors.New("source spool unavailable")
	}
	for _, path := range s.cfg.Files {
		resp, fc, cancel, err := s.request(ctx, path)
		if err != nil {
			return err
		}
		err = s.file(fc, resp.Body, spool, path, batch)
		_ = resp.Body.Close()
		cancel()
		if err != nil {
			return err
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
		req, err := http.NewRequestWithContext(ac, http.MethodGet, s.cfg.BaseURL+escaped(path), nil)
		if err != nil {
			timer.Stop()
			cancel()
			return nil, nil, nil, errors.New("invalid source request")
		}
		req.Header.Set("User-Agent", UserAgent)
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
func (s *Scraper) file(ctx context.Context, body io.Reader, spool source.Spool, path string, batch func([]source.Row) error) error {
	file, err := spool()
	if err != nil {
		return errors.New("source spool unavailable")
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	n, err := io.Copy(file, body)
	if err != nil || ctx.Err() != nil {
		return errors.New("unreadable source body")
	}
	var csvBody io.Reader = io.NewSectionReader(file, 0, n)
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		z, e := zip.NewReader(file, n)
		if e != nil {
			return errors.New("unreadable source archive")
		}
		var entry *zip.File
		for _, f := range z.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
				if entry != nil {
					return errors.New("unreadable source archive")
				}
				entry = f
			}
		}
		if entry == nil {
			return errors.New("unreadable source archive")
		}
		rc, e := entry.Open()
		if e != nil {
			return errors.New("unreadable source entry")
		}
		defer func() { _ = rc.Close() }()
		csvBody = rc
	}
	return s.read(ctx, csvBody, batch)
}
func (s *Scraper) read(ctx context.Context, body io.Reader, batch func([]source.Row) error) error {
	buffered := bufio.NewReader(body)
	if prefix, _ := buffered.Peek(3); bytes.Equal(prefix, []byte{0xef, 0xbb, 0xbf}) {
		_, _ = buffered.Discard(3)
	}
	reader := csv.NewReader(buffered)
	decode := func(r []string) []string {
		for i, v := range r {
			r[i] = Decode(v)
		}
		return r
	}
	header, e := reader.Read()
	if e != nil {
		return errors.New("unreadable source header")
	}
	header = decode(header)
	idx, e := index(header)
	if e != nil {
		return e
	}
	records := [][]string{}
	positions := map[string]int{}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("unreadable source record")
		}
		r = decode(r)
		if !slices.Contains(s.cfg.States, strings.ToUpper(value(idx, r, "state"))) {
			continue
		}
		id := strings.TrimSuffix(value(idx, r, "id"), ".00")
		if id == "" {
			return errors.New("source record lacks identifier")
		}
		if pos, ok := positions[id]; ok {
			old, oldOK := ParseSubmitted(submitted(idx, records[pos]))
			stamp, stampOK := ParseSubmitted(submitted(idx, r))
			if oldOK && stampOK && stamp.Before(old) {
				continue
			}
			records[pos] = nil
		}
		positions[id] = len(records)
		records = append(records, r)
	}
	rows := make([]source.Row, 0, BatchSize)
	var stamp time.Time
	for _, r := range records {
		if r == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(rows) == 0 {
			stamp = s.cfg.Now()
		}
		row, err := RowOf(header, r, stamp)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		if len(rows) == BatchSize {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if batch(rows) != nil {
				return errors.New("source batch refused")
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rows = make([]source.Row, 0, BatchSize)
		}
	}
	if len(rows) > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if batch(rows) != nil {
			return errors.New("source batch refused")
		}
	}
	return ctx.Err()
}
