# D4-store

The store is the session: one SQLite file holding everything every agent of
every pass wrote, searchable by full text. It is append-only. Entries are
never updated or deleted, and the package exports nothing that could. Four
kinds of entry exist:

- `prompt` — the text an agent was given: the pass's stdin for a root, the
  `delegate` call's prompt for a child. Written by the harness at the agent's
  address before the agent runs.
- `note` — written when a supervisor calls `remember`.
- `report` — the agent's final assistant text. Written by the harness when
  the agent ends cleanly.
- `transcript` — one agentkit event-log record. Written by the harness as
  the record is logged, through `LogWriter`.

Every entry has a SQLite row id, an address, a kind, a text, an optional raw
payload, and a creation time. The row id is the entry's identity: `search`
returns ids and previews, `fetch` returns one whole entry by id, and ids grow
monotonically, so "newest" is "highest id". There is no separate sequence.

```go
package store

type Kind string

const (
    KindNote       Kind = "note"
    KindPrompt     Kind = "prompt"
    KindReport     Kind = "report"
    KindTranscript Kind = "transcript"
)

const PageSize = 20      // hits per Search page
const PreviewRunes = 160 // runes of Text in a Hit.Preview

type Entry struct {
    ID      int64
    Address string
    Kind    Kind
    Text    string          // the searchable rendering
    Raw     json.RawMessage // the agentkit record for a transcript entry; nil otherwise
    Created time.Time
}

type Hit struct {
    ID      int64
    Address string
    Kind    Kind
    Preview string // Text collapsed to one line and cut to PreviewRunes
}

type Page struct {
    Hits  []Hit
    Page  int // 1-based page returned
    Pages int // total pages for this query; 0 when no hit
    Total int // total hits for this query
}

type Filter struct {
    Kinds   []Kind // keep only these kinds; empty for all
    Exclude []Kind // drop these kinds
    Address string // keep only this address and its descendants; "" for all
    Page    int    // 1-based; 0 means 1
}

var ErrNotFound error

type Store struct{ /* unexported */ }

func Create(path, id, root string, now func() time.Time) (*Store, error)
func Open(path string, now func() time.Time) (*Store, error)
func (s *Store) Close() error
func (s *Store) ID() string
func (s *Store) Root() string
func (s *Store) NextPass() (int, error)
func (s *Store) Add(address string, kind Kind, text string, raw json.RawMessage) (int64, error)
func (s *Store) Search(query string, f Filter) (Page, error)
func (s *Store) Fetch(id int64) (Entry, error)

// LogWriter is the io.Writer handed to agentkit.NewLog for one agent. Each
// Write is one record line; the writer stores it as a transcript entry at the
// agent's address, with Text from the supplied renderer, and remembers the
// summary record so the pass can total usage.
type LogWriter struct{ /* unexported */ }
func NewLogWriter(s *Store, address string, text func(agentkit.LogRecord) string) *LogWriter
func (w *LogWriter) Write(p []byte) (int, error)
func (w *LogWriter) Summary() (agentkit.Usage, agentkit.Cost, bool)
```

**Session metadata** lives in the same file: the session id, the tool root
the session was created in, and the creation time. `Create` fails if the file
exists and `Open` fails if it does not, so a typo in `-resume` cannot start a
new session by accident. `NextPass` is derived, not stored: one more than the
number of `prompt` entries at a root address (an address with no `.`), so an
interrupted pass still counts and the next pass gets a fresh number.

**Search** is SQLite FTS5 over `Text`, with the query passed as FTS5 `MATCH`
syntax, so `sdl3 AND headers`, `"make verify"`, and `error*` all work and a
query FTS5 rejects is an error the tool reports in-band. Hits are ordered by
`bm25()` rank, best first, then by id descending, and paginated at `PageSize`.
Filters are plain `WHERE` clauses. The address filter matches the address
itself and every descendant, so `3` matches `3`, `3.1`, and `3.1.2` but not
`31`. `Page` beyond the last returns no hits with the same `Pages` and `Total`
so a caller can tell it walked off the end.

**The log writer** is how transcripts get in without a second write path.
agentkit writes one JSON line per record (its D15), so each `Write` is one
record: the writer decodes it, stores the line as `Raw` and the renderer's
text as `Text`, at the writer's address. The renderer is injected because
what a record looks like as text is the render package's business (D6) and
the store must not import it. A summary record is stored like any other and
also retained for `Summary`. A write that cannot be stored returns the error
to agentkit, which by its own contract keeps the turn going.

The schema is not part of the contract; the `sqlite3` CLI is the only other
reader and it can `.schema`. What is contractual is the package above and
that the file is a single SQLite database.

## REQUIREMENTS

- R-IFMK-HS5B: Package `internal/store` MUST export `type Kind string` with exactly the constants `KindNote = "note"`, `KindPrompt = "prompt"`, `KindReport = "report"`, and `KindTranscript = "transcript"`, and the constants `PageSize = 20` and `PreviewRunes = 160`.
- R-IGUG-VJW0: Package `internal/store` MUST export an `Entry` struct whose fields are exactly `ID int64`, `Address string`, `Kind Kind`, `Text string`, `Raw json.RawMessage`, and `Created time.Time`; a `Hit` struct whose fields are exactly `ID int64`, `Address string`, `Kind Kind`, and `Preview string`; a `Page` struct whose fields are exactly `Hits []Hit`, `Page int`, `Pages int`, and `Total int`; and a `Filter` struct whose fields are exactly `Kinds []Kind`, `Exclude []Kind`, `Address string`, and `Page int`.
- R-IJA9-N3DE: Package `internal/store` MUST export `Store` as an opaque struct type, `var ErrNotFound error`, `Create(path, id, root string, now func() time.Time) (*Store, error)`, `Open(path string, now func() time.Time) (*Store, error)`, and on `*Store` the methods `Close() error`, `ID() string`, `Root() string`, `NextPass() (int, error)`, `Add(address string, kind Kind, text string, raw json.RawMessage) (int64, error)`, `Search(query string, f Filter) (Page, error)`, and `Fetch(id int64) (Entry, error)`, and MUST export no identifier that updates or deletes an entry.
- R-IKI6-0V43: `Create` MUST create a SQLite database at `path` recording `id` and `root`, MUST return an error when a file already exists at `path`, and `Open` MUST return an error when no file exists at `path`; after `Create` or `Open`, `ID()` and `Root()` MUST return the values given to `Create`, verified by opening the file `sqlite3`-style through the driver and reading them back through `Open`.
- R-ILQ2-EMUS: `Add` MUST return ids that strictly increase in call order within a store, and `Fetch` of a returned id MUST return an `Entry` whose `ID`, `Address`, `Kind`, `Text`, and `Raw` equal what was added and whose `Created` equals the `now()` value at the time of the `Add`; `Fetch` of an id no `Add` returned MUST return an error satisfying `errors.Is(err, ErrNotFound)`.
- R-IMXY-SELH: `NextPass` MUST return one more than the number of `KindPrompt` entries whose `Address` contains no `.`, so a new store returns `1`.
- R-IO5V-66C6: `Search` MUST return only entries whose `Text` matches `query` under SQLite FTS5 `MATCH` semantics, verified by a multi-term query, a quoted phrase, and a prefix query each returning exactly the matching entries, and MUST return an error for a query FTS5 rejects.
- R-IPDR-JY2V: `Search` MUST order hits by FTS5 `bm25()` rank ascending (best match first) and then by `ID` descending, verified by two entries with different match strength and two entries with equal match strength.
- R-IQLN-XPTK: `Search` MUST apply `Filter.Kinds` (keep only listed kinds; empty keeps all), `Filter.Exclude` (drop listed kinds), and `Filter.Address` (keep an entry iff its `Address` equals the filter or begins with the filter followed by `.`), verified by an address filter of `3` matching `3` and `3.1` and not `31`.
- R-IRTK-BHK9: `Search` MUST return at most `PageSize` hits, the page numbered from 1 with `Filter.Page` `0` treated as `1`, `Total` the count of all matching entries, `Pages` equal to `ceil(Total / PageSize)`, and for a `Filter.Page` past the last page an empty `Hits` with the same `Total` and `Pages`.
- R-IT1G-P9AY: Every `Hit.Preview` MUST be the entry's `Text` with each line terminator replaced by one space and truncated to its first `PreviewRunes` runes.
- R-IU9D-311N: Package `internal/store` MUST export `LogWriter` as an opaque struct type together with `NewLogWriter(s *Store, address string, text func(agentkit.LogRecord) string) *LogWriter`, the method `Write(p []byte) (int, error)` satisfying `io.Writer`, and the method `Summary() (agentkit.Usage, agentkit.Cost, bool)`.
- R-IVH9-GSSC: Each `LogWriter.Write` MUST store one `KindTranscript` entry at the writer's address whose `Raw` is the written line without its trailing newline and whose `Text` is the renderer's result for the decoded `agentkit.LogRecord`, and MUST return `len(p)` and a nil error when the entry was stored, verified by an `agentkit.Log` writing through it and the entries being fetchable.
- R-IWP5-UKJ1: After a line that decodes as an `agentkit.LogRecord` of type `summary` has been written, `LogWriter.Summary` MUST return that record's `Usage` and `Cost` with `true`; before any such line it MUST return zero values with `false`.
