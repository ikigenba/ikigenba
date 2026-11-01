// Package figures derives exact session figures from caller-provided records.
package figures

import "time"

// Tool identifies the agent command that owns a session.
type Tool string

// Supported values.
const (
	ToolClaude Tool = "claude"
	ToolCodex  Tool = "codex"
	ToolGrok   Tool = "grok"
)

// ParseTool accepts exactly the supported tool names.
func ParseTool(s string) (Tool, bool) {
	t := Tool(s)
	switch t {
	case ToolClaude, ToolCodex, ToolGrok:
		return t, true
	}
	return "", false
}

// Whose identifies the party responsible for a turn.
type Whose string

// Supported values.
const (
	WhosePerson Whose = "person"
	WhoseAgent  Whose = "agent"
	WhoseTool   Whose = "tool"
)

// ParseWhose accepts exactly the supported turn parties.
func ParseWhose(s string) (Whose, bool) {
	w := Whose(s)
	switch w {
	case WhosePerson, WhoseAgent, WhoseTool:
		return w, true
	}
	return "", false
}

// Kind identifies the work a session represents.
type Kind string

// Supported values.
const (
	KindPlan     Kind = "plan"
	KindBuild    Kind = "build"
	KindPublish  Kind = "publish"
	KindOperate  Kind = "operate"
	KindSell     Kind = "sell"
	KindCustomer Kind = "customer"
	KindUntagged Kind = "untagged"
)

// Kinds returns a fresh slice in display order.
func Kinds() []Kind {
	return []Kind{KindPlan, KindBuild, KindPublish, KindOperate, KindSell, KindCustomer, KindUntagged}
}

// ParseKind accepts exactly the supported kinds.
func ParseKind(s string) (Kind, bool) {
	for _, k := range Kinds() {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// Period identifies a UTC calendar bucket.
type Period string

// Supported values.
const (
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
)

// ParsePeriod accepts the supported bucket names.
func ParsePeriod(s string) (Period, bool) {
	p := Period(s)
	switch p {
	case PeriodWeek, PeriodMonth:
		return p, true
	}
	return "", false
}

// Start returns the UTC beginning of the period containing t.
func (p Period) Start(t time.Time) time.Time {
	t = t.UTC()
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	switch p {
	case PeriodWeek:
		return d.AddDate(0, 0, -(int(d.Weekday())+6)%7)
	case PeriodMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Time{}
}

// Next returns the UTC beginning of the following period.
func (p Period) Next(t time.Time) time.Time {
	d := p.Start(t)
	switch p {
	case PeriodWeek:
		return d.AddDate(0, 0, 7)
	case PeriodMonth:
		return d.AddDate(0, 1, 0)
	}
	return time.Time{}
}

// ParseDate accepts exactly a valid ten-byte Gregorian calendar date.
func ParseDate(s string) (time.Time, bool) {
	if len(s) != 10 {
		return time.Time{}, false
	}
	for i := range len(s) {
		if i == 4 || i == 7 {
			if s[i] != '-' {
				return time.Time{}, false
			}
		} else if s[i] < '0' || s[i] > '9' {
			return time.Time{}, false
		}
	}
	t, e := time.Parse("2006-01-02", s)
	if e != nil {
		return time.Time{}, false
	}
	return t, true
}

// Seconds drops the subsecond remainder of a duration.
func Seconds(d time.Duration) int64 { return int64(d / time.Second) }

// Tokens holds the four non-overlapping token counts.
type Tokens struct{ Input, Cached, Output, Reasoning int64 }

const largest = int64(1<<63 - 1)

func sat(a, b int64) int64 {
	if b > largest-a {
		return largest
	}
	return a + b
}

// Add returns the componentwise sum, saturating at each bound.
func (t Tokens) Add(u Tokens) Tokens {
	return Tokens{sat(t.Input, u.Input), sat(t.Cached, u.Cached), sat(t.Output, u.Output), sat(t.Reasoning, u.Reasoning)}
}

// Total returns the saturating sum of all token counts.
func (t Tokens) Total() int64 { return sat(sat(t.Input, t.Cached), sat(t.Output, t.Reasoning)) }

// Rule assigns a kind to a host, directory and optional command.
type Rule struct {
	Host, Directory, Command string
	Kind                     Kind
}

// Price holds per-million-token rates from an effective UTC date.
type Price struct {
	Model                            string
	Effective                        time.Time
	Input, Cached, Output, Reasoning Rate
}

// Turn holds content-free usage at one instant.
type Turn struct {
	Moment time.Time
	Whose  Whose
	Model  string
	Tokens Tokens
}

// SessionKey identifies a session across tools.
type SessionKey struct {
	Tool Tool
	ID   string
}

// Session holds the metadata and turns of one session.
type Session struct {
	Key                      SessionKey
	Parent                   SessionKey
	Host, Directory, Command string
	Headless                 bool
	Turns                    []Turn
}

// Figures holds the additive measurements of a session tree.
type Figures struct {
	Attention, Wall, Agent time.Duration
	Tokens                 Tokens
	Dollars                Money
	Unpriced               int64
}

// Add returns the componentwise sum, saturating at each bound.
func (f Figures) Add(g Figures) Figures {
	return Figures{time.Duration(sat(int64(f.Attention), int64(g.Attention))), time.Duration(sat(int64(f.Wall), int64(g.Wall))), time.Duration(sat(int64(f.Agent), int64(g.Agent))), f.Tokens.Add(g.Tokens), f.Dollars.Add(g.Dollars), sat(f.Unpriced, g.Unpriced)}
}

// Summary holds a root session tree’s classification and figures.
type Summary struct {
	Kind      Kind
	Figures   Figures
	Subagents int
}
