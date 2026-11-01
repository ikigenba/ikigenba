package figures_test

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sessions/internal/figures"
)

func instant(s string) time.Time {
	v, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		panic(e)
	}
	return v
}
func rate(s string) figures.Rate {
	v, ok := figures.ParseRate(s)
	if !ok {
		panic(s)
	}
	return v
}
func money(s string) figures.Money {
	v, ok := figures.ParseMoney(s)
	if !ok {
		panic(s)
	}
	return v
}
func eq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func signature[T any](T) {}

func TestPublicTypes(t *testing.T) {
	// R-CC56-ROA8 R-CDD3-5G0X R-CEKZ-J7RM R-CFSV-WZIB
	var tool figures.Tool = "claude"
	eq(t, tool, figures.ToolClaude)
	eq(t, string(figures.ToolCodex), "codex")
	eq(t, string(figures.ToolGrok), "grok")
	var whose figures.Whose = "person"
	eq(t, whose, figures.WhosePerson)
	eq(t, string(figures.WhoseAgent), "agent")
	eq(t, string(figures.WhoseTool), "tool")
	var kind figures.Kind = "plan"
	eq(t, kind, figures.KindPlan)
	for i, k := range []figures.Kind{figures.KindPlan, figures.KindBuild, figures.KindPublish, figures.KindOperate, figures.KindSell, figures.KindCustomer, figures.KindUntagged} {
		eq(t, string(k), []string{"plan", "build", "publish", "operate", "sell", "customer", "untagged"}[i])
	}
	var period figures.Period = "week"
	eq(t, period, figures.PeriodWeek)
	eq(t, string(figures.PeriodMonth), "month")
	signature[func(string) (figures.Tool, bool)](figures.ParseTool)
	signature[func(string) (figures.Whose, bool)](figures.ParseWhose)
	signature[func() []figures.Kind](figures.Kinds)
	signature[func(string) (figures.Kind, bool)](figures.ParseKind)
	signature[func(string) (figures.Period, bool)](figures.ParsePeriod)
	signature[func(figures.Period, time.Time) time.Time](figures.Period.Start)
	signature[func(figures.Period, time.Time) time.Time](figures.Period.Next)
	// R-CH0S-AR90 R-CKOH-G2H3 R-CLWD-TU7S R-COC6-LDP6
	tokens := figures.Tokens(struct{ Input, Cached, Output, Reasoning int64 }{1, 2, 3, 4})
	eq(t, tokens.Total(), int64(10))
	eq(t, tokens.Add(figures.Tokens{}), tokens)
	rule := figures.Rule(struct {
		Host, Directory, Command string
		Kind                     figures.Kind
	}{"host", "/work", "/go", kind})
	eq(t, rule.Host, "host")
	price := figures.Price(struct {
		Model                            string
		Effective                        time.Time
		Input, Cached, Output, Reasoning figures.Rate
	}{"model", time.Time{}, rate("1"), rate("2"), rate("3"), rate("4")})
	eq(t, price.Reasoning.String(), "4")
	turn := figures.Turn(struct {
		Moment time.Time
		Whose  figures.Whose
		Model  string
		Tokens figures.Tokens
	}{time.Time{}, whose, "model", tokens})
	eq(t, turn.Tokens, tokens)
	// R-1S43-ZTG4 R-1TC0-DL6T R-CQRZ-CX6K R-CRZV-QOX9
	key := figures.SessionKey(struct {
		Tool figures.Tool
		ID   string
	}{tool, "session"})
	eq(t, key.ID, "session")
	session := figures.Session(struct {
		Key                      figures.SessionKey
		Parent                   figures.SessionKey
		Host, Directory, Command string
		Headless                 bool
		Turns                    []figures.Turn
	}{key, figures.SessionKey{}, "host", "/work", "/go", true, []figures.Turn{turn}})
	eq(t, session.Headless, true)
	f := figures.Figures(struct {
		Attention, Wall, Agent time.Duration
		Tokens                 figures.Tokens
		Dollars                figures.Money
		Unpriced               int64
	}{time.Minute, time.Hour, time.Second, tokens, money("1"), 2})
	eq(t, f.Add(figures.Figures{}), f)
	summary := figures.Summary(struct {
		Kind      figures.Kind
		Figures   figures.Figures
		Subagents int
	}{kind, f, 3})
	eq(t, summary.Subagents, 3)
	// R-CI8O-OIZP R-CJGL-2AQE
	for _, v := range []any{rate("1"), money("1")} {
		typ := reflect.TypeOf(v)
		eq(t, typ.Kind(), reflect.Struct)
		for i := range typ.NumField() {
			if typ.Field(i).IsExported() {
				t.Fatal("opaque value exports a field")
			}
		}
	}
	signature[func(string) (figures.Rate, bool)](figures.ParseRate)
	signature[func(figures.Rate) string](figures.Rate.String)
	signature[func(string) (figures.Money, bool)](figures.ParseMoney)
	signature[func(figures.Money, figures.Money) figures.Money](figures.Money.Add)
	signature[func(figures.Money) string](figures.Money.String)
	signature[func(figures.Money) string](figures.Money.Dollars)
	// R-CT7S-4GNY R-CUFO-I8EN R-1UJW-RCXI R-CWVH-9RW1
	signature[func([]figures.Rule, string, string, string) figures.Kind](figures.Match)
	signature[func([]figures.Price, string, time.Time) (figures.Price, bool)](figures.PriceAt)
	signature[func([]figures.Turn, []figures.Price) (figures.Money, int64)](figures.Cost)
	signature[func(figures.Session, time.Duration) time.Duration](figures.Attention)
	signature[func(figures.Session) time.Duration](figures.AgentTime)
	signature[func(figures.Session) time.Duration](figures.Wall)
	signature[func(figures.SessionKey, map[figures.SessionKey]figures.SessionKey) figures.SessionKey](figures.Root)
	signature[func(figures.Session, []figures.Session, []figures.Rule, []figures.Price, time.Duration) figures.Summary](figures.Summarize)
	signature[func(time.Duration) int64](figures.Seconds)
	signature[func(string) (time.Time, bool)](figures.ParseDate)
}

func TestEnumerations(t *testing.T) {
	// R-CY3D-NJMQ R-CZBA-1BDF R-D1R2-SUUT R-D2YZ-6MLI
	parsers := []struct {
		parse          func(string) (string, bool)
		valid, invalid []string
	}{
		{func(s string) (string, bool) { v, o := figures.ParseTool(s); return string(v), o }, []string{"claude", "codex", "grok"}, []string{"Claude", "claude ", "gemini", "", "\x00claude"}},
		{func(s string) (string, bool) { v, o := figures.ParseWhose(s); return string(v), o }, []string{"person", "agent", "tool"}, []string{"Person", "user", "assistant", "", "agent "}},
		{func(s string) (string, bool) { v, o := figures.ParseKind(s); return string(v), o }, []string{"plan", "build", "publish", "operate", "sell", "customer", "untagged"}, []string{"Plan", "sales", "", "plan "}},
		{func(s string) (string, bool) { v, o := figures.ParsePeriod(s); return string(v), o }, []string{"week", "month"}, []string{"Week", "day", "year", "", "month "}},
	}
	for _, p := range parsers {
		for _, s := range p.valid {
			v, ok := p.parse(s)
			eq(t, ok, true)
			eq(t, v, s)
		}
		for _, s := range p.invalid {
			v, ok := p.parse(s)
			eq(t, ok, false)
			eq(t, v, "")
		}
	}
	// R-D0J6-F344
	want := []figures.Kind{figures.KindPlan, figures.KindBuild, figures.KindPublish, figures.KindOperate, figures.KindSell, figures.KindCustomer, figures.KindUntagged}
	eq(t, slices.Equal(figures.Kinds(), want), true)
	got := figures.Kinds()
	got[0] = figures.KindUntagged
	eq(t, slices.Equal(figures.Kinds(), want), true)
}

func TestPeriods(t *testing.T) {
	// R-D46V-KEC7 R-D5ER-Y62W R-D7UK-PPKA
	for _, tc := range []struct {
		p               figures.Period
		in, start, next string
	}{
		{figures.PeriodWeek, "2026-10-13T14:02:11.250Z", "2026-10-12T00:00:00Z", "2026-10-19T00:00:00Z"},
		{figures.PeriodWeek, "2026-10-12T00:00:00Z", "2026-10-12T00:00:00Z", "2026-10-19T00:00:00Z"},
		{figures.PeriodWeek, "2026-10-18T23:59:59.999Z", "2026-10-12T00:00:00Z", "2026-10-19T00:00:00Z"},
		{figures.PeriodWeek, "2026-10-19T00:00:00Z", "2026-10-19T00:00:00Z", "2026-10-26T00:00:00Z"},
		{figures.PeriodWeek, "2026-10-19T01:00:00+02:00", "2026-10-12T00:00:00Z", "2026-10-19T00:00:00Z"},
		{figures.PeriodWeek, "2026-01-01T09:00:00Z", "2025-12-29T00:00:00Z", "2026-01-05T00:00:00Z"},
		{figures.PeriodWeek, "2026-12-28T00:00:00Z", "2026-12-28T00:00:00Z", "2027-01-04T00:00:00Z"},
		{figures.PeriodMonth, "2026-10-13T14:02:11Z", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"},
		{figures.PeriodMonth, "2026-10-31T23:59:59.999Z", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"},
		{figures.PeriodMonth, "2026-03-01T00:30:00+01:00", "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z"},
		{figures.PeriodMonth, "2026-12-03T09:00:00Z", "2026-12-01T00:00:00Z", "2027-01-01T00:00:00Z"},
	} {
		for i, got := range []time.Time{tc.p.Start(instant(tc.in)), tc.p.Next(instant(tc.in))} {
			eq(t, got, instant([]string{tc.start, tc.next}[i]))
			eq(t, got.Location(), time.UTC)
		}
	}
	for _, p := range []figures.Period{"", "day", "Week"} {
		eq(t, p.Start(time.Time{}), time.Time{})
		eq(t, p.Next(time.Time{}), time.Time{})
	}
	// R-D92H-3HAZ
	for _, s := range []string{"2026-10-13", "2028-02-29", "0000-01-01", "9999-12-31"} {
		v, ok := figures.ParseDate(s)
		eq(t, ok, true)
		eq(t, v, instant(s+"T00:00:00Z"))
		eq(t, v.Location(), time.UTC)
	}
	for _, s := range []string{"2026-02-29", "2026-13-01", "2026-1-05", "2026-10-13T00:00:00Z", "", "2026-00-10", "2026-01-00", "2026-01-32", "２０２６-01-01", "+026-01-01", "2026/01/01"} {
		v, ok := figures.ParseDate(s)
		eq(t, ok, false)
		eq(t, v, time.Time{})
	}
	// R-DAAD-H91O
	for _, tc := range []struct {
		d time.Duration
		n int64
	}{{time.Minute + 59*time.Second + 999*time.Millisecond, 119}, {20 * time.Minute, 1200}, {999 * time.Millisecond, 0}, {0, 0}, {time.Duration(math.MaxInt64), math.MaxInt64 / int64(time.Second)}} {
		eq(t, figures.Seconds(tc.d), tc.n)
	}
}

func TestTokensAndFiguresAddition(t *testing.T) {
	// R-KKD0-3R3P
	eq(t, (figures.Tokens{Input: 1, Cached: 2, Output: 3, Reasoning: 4}).Total(), int64(10))
	eq(t, (figures.Tokens{Input: 1, Output: 5}).Add(figures.Tokens{Input: 2, Reasoning: 7}), figures.Tokens{Input: 3, Output: 5, Reasoning: 7})
	bound := int64(math.MaxInt64)
	eq(t, (figures.Tokens{Input: bound - 1, Cached: bound - 1, Output: bound - 1, Reasoning: bound - 1}).Add(figures.Tokens{Input: 5, Cached: 5, Output: 5, Reasoning: 5}), figures.Tokens{Input: bound, Cached: bound, Output: bound, Reasoning: bound})
	for _, x := range []figures.Tokens{{Input: bound, Output: 1}, {Input: bound / 2, Cached: bound / 2, Reasoning: 2}, {}} {
		want := bound
		if x == (figures.Tokens{}) {
			want = 0
		}
		eq(t, x.Total(), want)
	}
	// R-BUZJ-NNEJ
	a := figures.Figures{Attention: 20 * time.Minute, Wall: time.Hour, Agent: time.Minute, Tokens: figures.Tokens{Input: 4, Cached: 2}, Dollars: money("0.005"), Unpriced: 10}
	b := figures.Figures{Attention: 5 * time.Minute, Wall: time.Minute, Agent: time.Second, Tokens: figures.Tokens{Output: 3, Reasoning: 1}, Dollars: money("0.005"), Unpriced: 3}
	eq(t, a.Add(b), figures.Figures{Attention: 25 * time.Minute, Wall: 61 * time.Minute, Agent: 61 * time.Second, Tokens: figures.Tokens{Input: 4, Cached: 2, Output: 3, Reasoning: 1}, Dollars: money("0.01"), Unpriced: 13})
	f := figures.Figures{Attention: time.Duration(bound), Wall: time.Duration(bound), Agent: time.Duration(bound), Tokens: figures.Tokens{Input: bound}, Dollars: money("99999999999999999999999999.999999999999"), Unpriced: bound - 1}
	eq(t, f.Add(figures.Figures{Attention: time.Millisecond, Wall: time.Millisecond, Agent: time.Millisecond, Tokens: figures.Tokens{Input: 1}, Dollars: money("1"), Unpriced: 5}), figures.Figures{Attention: time.Duration(bound), Wall: time.Duration(bound), Agent: time.Duration(bound), Tokens: f.Tokens, Dollars: f.Dollars, Unpriced: bound})
}

func TestRatesAndMoney(t *testing.T) {
	// R-DCQ6-8SJ2 R-DDY2-MK9R R-DF5Z-0C0G
	eq(t, figures.Rate{}, rate("0"))
	eq(t, rate("0.5"), rate("0.500"))
	eq(t, rate("0.5") == rate("0.500001"), false)
	for _, tc := range []struct{ in, out string }{{"4", "4"}, {"0.20", "0.2"}, {"0.075", "0.075"}, {"007.500000", "7.5"}, {"0.000000", "0"}, {"1000000", "1000000"}, {"1000000.000000", "1000000"}, {"0.000001", "0.000001"}} {
		v, ok := figures.ParseRate(tc.in)
		eq(t, ok, true)
		eq(t, v.String(), tc.out)
		eq(t, rate(v.String()), v)
	}
	for _, s := range []string{"", "-1", "+1", "1.", ".5", "0.0000001", "1e3", " 1", "1,000", "1000000.000001", "١", "1\x00", "1.2.3", "99999999999999999999999999999999999999"} {
		v, ok := figures.ParseRate(s)
		eq(t, ok, false)
		eq(t, v, figures.Rate{})
	}
	// R-DITO-5N8J R-DHLR-RVHU
	eq(t, figures.Money{}, money("0"))
	eq(t, money("0.5"), money("0.500"))
	eq(t, money("0.5") == money("0.500000000001"), false)
	bound := "99999999999999999999999999.999999999999"
	for _, tc := range []struct{ in, out string }{{"0", "0"}, {"0.0640", "0.064"}, {"007", "7"}, {"0.000000000001", "0.000000000001"}, {bound, bound}, {"0000000000000000000000000001.000000000000", "1"}} {
		v, ok := figures.ParseMoney(tc.in)
		eq(t, ok, true)
		eq(t, v.String(), tc.out)
		eq(t, money(v.String()), v)
	}
	for _, s := range []string{"", "-0.01", "+0", "1.", ".5", "0.0000000000001", "1e3", "100000000000000000000000000", "1.2.3", " 0", "١", "1\x00"} {
		v, ok := figures.ParseMoney(s)
		eq(t, ok, false)
		eq(t, v, figures.Money{})
	}
	// R-DGDV-E3R5
	eq(t, money("0.000000000001").Add(money("0.999999999999")), money("1"))
	eq(t, money(bound).Add(money("0.000000000001")), money(bound))
	eq(t, money("50000000000000000000000000").Add(money("50000000000000000000000000")), money(bound))
	// R-DK1K-JEZ8
	for _, tc := range []struct{ in, out string }{{"0.125", "0.13"}, {"0.025", "0.03"}, {"0.005", "0.01"}, {"0.004999999999", "0.00"}, {"2.675", "2.68"}, {"1234.5", "1234.50"}, {"0", "0.00"}, {bound, "100000000000000000000000000.00"}, {"9.995", "10.00"}} {
		eq(t, money(tc.in).Dollars(), tc.out)
	}
}

func TestMatch(t *testing.T) {
	// R-DL9G-X6PX
	r := figures.Rule{Host: "ld01", Directory: "/mnt/projects/biz", Kind: figures.KindPlan}
	for _, tc := range []struct {
		host, dir, cmd string
		want           figures.Kind
	}{{"ld01", r.Directory, "", figures.KindPlan}, {"ld01", r.Directory + "/docs", "/deliver", figures.KindPlan}, {"ld01", "/mnt/projects/bizarre", "", figures.KindUntagged}, {"ld01", "/mnt/projects", "", figures.KindUntagged}, {"ld02", r.Directory, "", figures.KindUntagged}, {"LD01", r.Directory, "", figures.KindUntagged}} {
		eq(t, figures.Match([]figures.Rule{r}, tc.host, tc.dir, tc.cmd), tc.want)
	}
	r.Directory = "/"
	r.Kind = figures.KindOperate
	for _, d := range []string{"/home/x", "/"} {
		eq(t, figures.Match([]figures.Rule{r}, "ld01", d, ""), figures.KindOperate)
	}
	eq(t, figures.Match([]figures.Rule{r}, "ld01", "relative", ""), figures.KindUntagged)
	r.Directory = "/mnt/projects/biz"
	r.Command = "/deliver"
	for _, c := range []string{"", "/review", "/Deliver"} {
		eq(t, figures.Match([]figures.Rule{r}, "ld01", r.Directory, c), figures.KindUntagged)
	}
	eq(t, figures.Match([]figures.Rule{r}, "ld01", r.Directory, "/deliver"), figures.KindOperate)
	eq(t, figures.Match(nil, "ld01", "/", ""), figures.KindUntagged)
	// R-DMHD-AYGM
	rules := []figures.Rule{{Host: "ld01", Directory: "/mnt/projects/biz", Kind: figures.KindPlan}, {Host: "ld01", Directory: "/mnt/projects/ikigenba", Kind: figures.KindBuild}, {Host: "ld01", Directory: "/mnt/projects/ikigenba", Command: "/deliver", Kind: figures.KindPublish}, {Host: "ld01", Directory: "/mnt/projects", Command: "/deliver", Kind: figures.KindSell}, {Host: "ld01", Directory: "/", Kind: figures.KindOperate}}
	for range len(rules) {
		for _, tc := range []struct {
			dir, cmd string
			want     figures.Kind
		}{{"/mnt/projects/ikigenba/wip-sessions", "/deliver", figures.KindPublish}, {"/mnt/projects/ikigenba/wip-sessions", "/review", figures.KindBuild}, {"/mnt/projects/biz", "/deliver", figures.KindPlan}, {"/mnt/projects/bizarre", "/deliver", figures.KindSell}, {"/mnt/projects/bizarre", "", figures.KindOperate}} {
			eq(t, figures.Match(rules, "ld01", tc.dir, tc.cmd), tc.want)
		}
		rules = append(rules[1:], rules[0])
	}
	same := []figures.Rule{{Host: "h", Directory: "/α", Kind: figures.KindPlan}, {Host: "h", Directory: "/α", Kind: figures.KindBuild}}
	eq(t, figures.Match(same, "h", "/α/x", ""), figures.KindPlan)
	slices.Reverse(same)
	eq(t, figures.Match(same, "h", "/α/x", ""), figures.KindBuild)
}

func TestPriceAt(t *testing.T) {
	// R-DNP9-OQ7B
	p := func(model, date, value string) figures.Price {
		return figures.Price{Model: model, Effective: instant(date), Input: rate(value)}
	}
	prices := []figures.Price{p("m", "2026-03-11T00:00:00Z", "3"), p("n", "2026-03-12T00:00:00Z", "4"), p("m", "2026-01-01T00:00:00Z", "1"), p("m", "2026-03-10T23:00:00Z", "2"), p("m", "2026-03-10T01:00:00Z", "5")}
	for _, tc := range []struct {
		model, moment string
		index         int
	}{{"m", "2026-03-09T23:30:00-05:00", 3}, {"m", "2026-03-10T23:59:59.999Z", 3}, {"m", "2026-03-09T23:59:59.999Z", 2}, {"m", "2026-03-12T00:00:00Z", 0}, {"m", "2025-12-31T23:59:59.999Z", -1}, {"M", "2026-03-12T00:00:00Z", -1}} {
		v, ok := figures.PriceAt(prices, tc.model, instant(tc.moment))
		eq(t, ok, tc.index >= 0)
		if ok {
			eq(t, v, prices[tc.index])
		} else {
			eq(t, v, figures.Price{})
		}
	}
	zonePrice := p("z", "2026-03-11T00:30:00+02:00", "6")
	v, ok := figures.PriceAt([]figures.Price{zonePrice}, "z", instant("2026-03-10T00:00:00Z"))
	eq(t, ok, true)
	eq(t, v, zonePrice)
	v, ok = figures.PriceAt(nil, "m", time.Time{})
	eq(t, v, figures.Price{})
	eq(t, ok, false)
}

func standardPrices() []figures.Price {
	return []figures.Price{{Model: "priced", Effective: instant("2026-01-01T00:00:00Z"), Input: rate("4"), Cached: rate("0.2"), Output: rate("20"), Reasoning: rate("20")}}
}
func TestCost(t *testing.T) {
	// R-BXFC-F6VX R-KO0P-92BS
	turn := figures.Turn{Moment: instant("2026-10-13T00:00:00Z"), Model: "priced", Tokens: figures.Tokens{Input: 1000, Cached: 50000, Output: 2000, Reasoning: 500}}
	c, u := figures.Cost([]figures.Turn{turn}, standardPrices())
	eq(t, c, money("0.064"))
	eq(t, u, int64(0))
	// Distinct rates and counts make each token channel independently visible.
	distinct := []figures.Price{{Model: "channels", Effective: turn.Moment, Input: rate("1"), Cached: rate("2"), Output: rate("3"), Reasoning: rate("4")}}
	channels := figures.Turn{Moment: turn.Moment, Model: "channels", Tokens: figures.Tokens{Input: 2, Cached: 3, Output: 5, Reasoning: 7}}
	c, u = figures.Cost([]figures.Turn{channels}, distinct)
	eq(t, c, money("0.000051"))
	eq(t, u, int64(0))
	for _, counts := range []figures.Tokens{{Input: 1}, {Cached: 1}, {Output: 1}, {Reasoning: 1}} {
		channels.Tokens = counts
		micro := rate("0.000001")
		fine := []figures.Price{{Model: "channels", Effective: turn.Moment, Input: micro, Cached: micro, Output: micro, Reasoning: micro}}
		c, u = figures.Cost([]figures.Turn{channels}, fine)
		eq(t, c, money("0.000000000001"))
		eq(t, u, int64(0))
	}
	turns := []figures.Turn{turn, {Moment: turn.Moment, Model: "mystery", Tokens: figures.Tokens{Input: 300, Output: 200}}, {Moment: instant("2025-12-31T23:59:59.999Z"), Model: "priced", Tokens: figures.Tokens{Output: 40}}}
	c, u = figures.Cost(turns, standardPrices())
	eq(t, c, money("0.064"))
	eq(t, u, int64(540))
	turn.Tokens = figures.Tokens{Input: 1250}
	c, u = figures.Cost([]figures.Turn{turn, turn}, standardPrices())
	eq(t, c, money("0.01"))
	eq(t, c.Dollars(), "0.01")
	eq(t, u, int64(0))
	turn.Tokens = figures.Tokens{Input: math.MaxInt64}
	prices := standardPrices()
	prices[0].Input = rate("1000000")
	c, u = figures.Cost([]figures.Turn{turn}, prices)
	eq(t, c, money("9223372036854775807"))
	eq(t, u, int64(0))
	turn.Model = "mystery"
	c, u = figures.Cost([]figures.Turn{turn, turn}, prices)
	eq(t, c, figures.Money{})
	eq(t, u, int64(math.MaxInt64))
	c, u = figures.Cost(nil, prices)
	eq(t, c, figures.Money{})
	eq(t, u, int64(0))
}

func session(parts ...string) figures.Session {
	s := figures.Session{}
	for i := 0; i+1 < len(parts); i += 2 {
		s.Turns = append(s.Turns, figures.Turn{Moment: instant("2026-10-13T" + parts[i] + ":00Z"), Whose: figures.Whose(parts[i+1])})
	}
	return s
}
func canonical() figures.Session {
	return session("09:00", "person", "09:01", "agent", "09:02", "tool", "09:04", "agent", "09:31", "person", "09:32", "agent", "09:37", "person")
}
func TestAttention(t *testing.T) {
	// R-KHX7-C7MB
	for _, tc := range []struct {
		s          figures.Session
		away, want time.Duration
	}{{canonical(), 15 * time.Minute, 20 * time.Minute}, {canonical(), 30 * time.Minute, 32 * time.Minute}, {session("10:00", "person", "10:01", "agent", "10:05", "person", "10:08", "person"), 15 * time.Minute, 11 * time.Minute}, {session("09:04", "agent", "09:20", "tool", "09:31", "person"), 15 * time.Minute, 15 * time.Minute}, {session("10:00", "person", "10:05", "person"), 15 * time.Minute, 5 * time.Minute}, {canonical(), 0, 0}, {canonical(), -time.Minute, 0}} {
		eq(t, figures.Attention(tc.s, tc.away), tc.want)
	}
	// R-DW8K-D4E6
	eq(t, figures.Attention(session("10:00", "person", "10:05", "agent", "10:05", "person", "10:06", "agent"), 15*time.Minute), 5*time.Minute)
	// R-DXGG-QW4V
	s := canonical()
	s.Headless = true
	eq(t, figures.Attention(s, 15*time.Minute), time.Duration(0))
	eq(t, figures.Attention(figures.Session{}, 15*time.Minute), time.Duration(0))
}
func TestAgentTime(t *testing.T) {
	// R-KJ53-PZD0
	for _, tc := range []struct {
		s    figures.Session
		want time.Duration
	}{{canonical(), 5 * time.Minute}, {session("08:00", "tool", "08:10", "agent", "08:20", "person", "08:21", "agent"), 11 * time.Minute}, {session("08:00", "person", "08:40", "agent"), 40 * time.Minute}, {figures.Session{}, 0}, {session("09:00", "person", "09:01", "agent", "09:06", "tool"), time.Minute}, {session("09:00", "tool", "09:30", "person", "09:40", "tool"), 0}} {
		eq(t, figures.AgentTime(tc.s), tc.want)
		tc.s.Headless = true
		eq(t, figures.AgentTime(tc.s), tc.want)
	}
	// R-DZW9-IFM9
	eq(t, figures.AgentTime(session("10:00", "person", "10:05", "agent", "10:05", "person", "10:06", "agent")), time.Minute)
}
func TestWall(t *testing.T) {
	// R-E145-W7CY
	eq(t, figures.Wall(canonical()), 37*time.Minute)
	eq(t, figures.Wall(figures.Session{}), time.Duration(0))
	eq(t, figures.Wall(session("09:00", "agent")), time.Duration(0))
	eq(t, figures.Wall(figures.Session{Turns: []figures.Turn{{Moment: instant("2026-10-13T14:02:11.250Z")}, {Moment: instant("2026-10-13T14:02:12Z")}}}), 750*time.Millisecond)
}

func TestRoot(t *testing.T) {
	// R-1VRT-54O7
	a := figures.SessionKey{Tool: figures.ToolClaude, ID: "a"}
	b := figures.SessionKey{Tool: figures.ToolClaude, ID: "b"}
	c := figures.SessionKey{Tool: figures.ToolCodex, ID: "c"}
	otherB := figures.SessionKey{Tool: figures.ToolCodex, ID: "b"}
	parents := map[figures.SessionKey]figures.SessionKey{c: b, b: a, a: {}}
	for _, k := range []figures.SessionKey{a, b, c} {
		eq(t, figures.Root(k, parents), a)
	}
	eq(t, figures.Root(c, map[figures.SessionKey]figures.SessionKey{c: b, b: otherB}), b)
	eq(t, figures.Root(a, map[figures.SessionKey]figures.SessionKey{a: b, b: a}), b)
	z := figures.SessionKey{Tool: figures.ToolGrok, ID: "z"}
	eq(t, figures.Root(z, nil), z)
	eq(t, figures.Root(a, map[figures.SessionKey]figures.SessionKey{a: a}), a)
}

func summaryInputs() (figures.Session, []figures.Session, []figures.Rule, []figures.Price) {
	root := canonical()
	root.Host = "ld01"
	root.Directory = "/mnt/projects/ikigenba/wip"
	root.Command = "/deliver"
	root.Key = figures.SessionKey{Tool: figures.ToolClaude, ID: "root"}
	for i := range root.Turns {
		if root.Turns[i].Whose == figures.WhoseAgent {
			root.Turns[i].Model = "priced"
			root.Turns[i].Tokens.Input = 1250
		}
	}
	sub := session("09:05", "person", "09:20", "agent", "09:55", "agent", "09:50", "person", "09:52", "agent")
	sub.Host = "ld01"
	sub.Directory = "/mnt/projects/biz"
	sub.Parent = root.Key
	for i := range sub.Turns {
		if sub.Turns[i].Whose == figures.WhoseAgent {
			sub.Turns[i].Model = "priced"
			sub.Turns[i].Tokens.Input = 1250
		}
		if sub.Turns[i].Moment.Equal(instant("2026-10-13T09:52:00Z")) {
			sub.Turns[i].Model = "mystery"
			sub.Turns[i].Tokens = figures.Tokens{Output: 700}
		}
	}
	return root, []figures.Session{sub}, []figures.Rule{{Host: "ld01", Directory: "/mnt/projects/biz", Kind: figures.KindPlan}, {Host: "ld01", Directory: "/mnt/projects/ikigenba", Kind: figures.KindBuild}}, standardPrices()
}
func TestSummarize(t *testing.T) {
	// R-E4RV-1IL1 R-E5ZR-FABQ R-E8FK-6TT4
	root, subs, rules, prices := summaryInputs()
	got := figures.Summarize(root, subs, rules, prices, 15*time.Minute)
	eq(t, got.Kind, figures.KindBuild)
	eq(t, got.Figures, figures.Figures{Attention: 20 * time.Minute, Wall: 37 * time.Minute, Agent: 25 * time.Minute, Tokens: figures.Tokens{Input: 6250, Output: 700}, Dollars: money("0.025"), Unpriced: 700})
	eq(t, got.Figures.Tokens.Total(), int64(6950))
	eq(t, got.Figures.Dollars.Dollars(), "0.03")
	// R-E9NG-KLJT
	eq(t, got.Subagents, 1)
	second := figures.Session{Parent: subs[0].Key}
	eq(t, figures.Summarize(root, append(subs, second), rules, prices, 15*time.Minute).Subagents, 2)
	alone := figures.Summarize(root, nil, rules, prices, 15*time.Minute)
	c, u := figures.Cost(root.Turns, prices)
	eq(t, alone, figures.Summary{Kind: figures.KindBuild, Figures: figures.Figures{Attention: 20 * time.Minute, Wall: 37 * time.Minute, Agent: 5 * time.Minute, Tokens: figures.Tokens{Input: 3750}, Dollars: c, Unpriced: u}, Subagents: 0})
}

func TestOrderAndInputPreservation(t *testing.T) {
	// R-DSKV-7T63
	root, subs, rules, prices := summaryInputs()
	want := figures.Summarize(root, subs, rules, prices, 15*time.Minute)
	att, agent, wall := figures.Attention(root, 15*time.Minute), figures.AgentTime(root), figures.Wall(root)
	cost, unpriced := figures.Cost(root.Turns, prices)
	// Exercise every cyclic ordering and its reverse, including equal-instant ties.
	for range len(root.Turns) {
		root.Turns = append(root.Turns[1:], root.Turns[0])
		slices.Reverse(subs[0].Turns)
		slices.Reverse(prices)
		eq(t, figures.Attention(root, 15*time.Minute), att)
		eq(t, figures.AgentTime(root), agent)
		eq(t, figures.Wall(root), wall)
		c, u := figures.Cost(root.Turns, prices)
		eq(t, c, cost)
		eq(t, u, unpriced)
		eq(t, figures.Summarize(root, subs, rules, prices, 15*time.Minute), want)
	}
	tied := session("10:00", "person", "10:05", "agent", "10:05", "person", "10:06", "agent")
	for range len(tied.Turns) {
		slices.Reverse(tied.Turns)
		eq(t, figures.Attention(tied, 15*time.Minute), 5*time.Minute)
		eq(t, figures.AgentTime(tied), time.Minute)
		tied.Turns = append(tied.Turns[1:], tied.Turns[0])
	}
	second := session("08:00", "tool", "08:02", "agent")
	two := []figures.Session{subs[0], second}
	twoWant := figures.Summarize(root, two, rules, prices, 15*time.Minute)
	slices.Reverse(two)
	eq(t, figures.Summarize(root, two, rules, prices, 15*time.Minute), twoWant)
	// R-KQGI-0LT6
	root, subs, rules, prices = summaryInputs()
	slices.Reverse(root.Turns)
	rules[0], rules[1] = rules[1], rules[0]
	rootCopy := root
	rootCopy.Turns = slices.Clone(root.Turns)
	subsCopy := slices.Clone(subs)
	for i := range subsCopy {
		subsCopy[i].Turns = slices.Clone(subs[i].Turns)
	}
	rulesCopy := slices.Clone(rules)
	pricesCopy := slices.Clone(prices)
	parents := map[figures.SessionKey]figures.SessionKey{root.Key: {}, subs[0].Key: root.Key}
	parentCopy := map[figures.SessionKey]figures.SessionKey{}
	for k, v := range parents {
		parentCopy[k] = v
	}
	figures.Attention(root, 15*time.Minute)
	figures.AgentTime(root)
	figures.Wall(root)
	figures.Cost(root.Turns, prices)
	figures.Match(rules, root.Host, root.Directory, root.Command)
	figures.PriceAt(prices, "priced", root.Turns[0].Moment)
	figures.Root(root.Key, parents)
	figures.Summarize(root, subs, rules, prices, 15*time.Minute)
	for _, pair := range [][2]any{{root, rootCopy}, {subs, subsCopy}, {rules, rulesCopy}, {prices, pricesCopy}, {parents, parentCopy}} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Fatalf("input changed: got %#v, want %#v", pair[0], pair[1])
		}
	}
}

func TestSummarizeAllTokenChannels(t *testing.T) {
	// R-E8FK-6TT4
	moment := instant("2026-10-13T12:00:00Z")
	build := func(counts figures.Tokens) figures.Session {
		return figures.Session{Turns: []figures.Turn{{Moment: moment, Whose: figures.WhoseAgent, Model: "channels", Tokens: counts}}}
	}
	root := build(figures.Tokens{Input: 2, Cached: 3, Output: 5, Reasoning: 7})
	subs := []figures.Session{build(figures.Tokens{Input: 11, Cached: 13, Output: 17, Reasoning: 19}), build(figures.Tokens{Input: 23, Cached: 29, Output: 31, Reasoning: 37})}
	prices := []figures.Price{{Model: "channels", Effective: moment, Input: rate("1"), Cached: rate("2"), Output: rate("3"), Reasoning: rate("4")}}
	got := figures.Summarize(root, subs, nil, prices, 15*time.Minute)
	eq(t, got.Figures, figures.Figures{Tokens: figures.Tokens{Input: 36, Cached: 45, Output: 53, Reasoning: 63}, Dollars: money("0.000537")})
}
