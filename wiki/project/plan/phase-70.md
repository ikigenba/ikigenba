# Phase 70 — The ask result page + the `MentionsIn` seam

*Realizes design Decision 44 (the ask result page + the `MentionsIn` seam) and
Decision 42's root-dispatch slice (`R-WFPZ-2LTM`). Touches `internal/wiki` (the
new `MentionsIn` service method) and `internal/web` (the ask path), plus the
`cmd/wiki/main.go` Mentioner adapter. No migration, no new LLM call site (reuses
D9 `ask`). Depends on Phase 69 (the handler skeleton + seams), Phase 60 (D9
`asker.Ask`), and Phase 49 (D12 `Mentions`/`SubjectKeys`).*

The root route gains its `?q` behavior: a non-empty query runs `ask` and renders
the answer, with a footer linking every subject/alias the answer prose names.

In **`internal/wiki`** add the mention projection over arbitrary text (reuses the
exact D12 matcher; no new matching logic, no own R-id — proven via the web ids
below):

```go
func (s *Service) MentionsIn(ctx context.Context, text string) ([]Ref, error)
```

It loads the current `SubjectKeys` (subjects ∪ alias keys) and returns every
subject whose name/alias occurs (whole-run, D12) in `text`, alias matches
resolving to the **canonical** subject.

In **`internal/web`**: the root handler dispatches on `r.URL.Query().Get("q")` —
empty → home (Phase 69); non-empty → call `Asker.Ask(ctx, owner, q)` and render
`answer.tmpl`: an **"ask another question"** control (href `.` → base) at top,
`Answer.Text` as prose, and a footer (outbound only) listing the `Mentioner`
Refs as `<a href="{{.Href}}">{{.Name}}</a>`, **omitted when empty**. Honest-empty
(`Found:false`) renders its text as the ordinary body — no separate branch.

In **`cmd/wiki/main.go`**: pass `web.WithMentioner(mentionAdapter{svc})` (mapping
`svc.MentionsIn` → `[]web.Ref{Href:"subject/"+Path, Name}`); `owner` is the
nginx-supplied `X-Owner-Email`.

**Done when:** the suite is green (per design *Conventions*) and these ids are
covered by clearly-named tests:

- **R-WFPZ-2LTM** — `GET /?q=who%20is%20acme` invokes the injected `Asker.Ask`
  exactly once with `"who is acme"`; `GET /` and `GET /?q=` invoke it **zero**
  times. *(httptest, stub Asker)*
- **R-ARN9-5YPS** — a stub `Asker` returning `Answer{Found:true,
  Text:"Acme Corp makes widgets."}` yields a 200 `text/html` page containing that
  text. *(httptest, stub)*
- **R-ASV5-JQGH** — every answer page (`Found` true or false) has an "ask another
  question" control whose href resolves to the base. *(httptest, stub)*
- **R-AU31-XI76** — a stub `Mentioner` returning two Refs renders both as
  `<a href="subject/…">Name</a>`. *(httptest, stub)*
- **R-AVAY-B9XV** — a `Mentioner` returning an empty slice renders the prose +
  control and **no** outbound footer markup. *(httptest, stub)*
- **R-AWIU-P1OK** — a stub `Asker` returning `Answer{Found:false, Text:<honest-
  empty sentence>}` yields a 200 page containing that sentence and the control,
  with no footer. *(httptest, stub)*
- **R-AXQR-2TF9** — driving the web handler wired to the **real** `asker.Ask` +
  **real temp SQLite** (a subject `entity/acme-corp` with a page) + the **mock
  LLM provider** (scripted to synthesize an answer naming "Acme Corp" with a
  citation) + the **real `MentionsIn`**: `GET /?q=…` returns a 200 page with the
  synthesized answer **and** an outbound footer linking `subject/entity/acme-corp`
  — proving the composition-root adapters connect `Asker`/`Mentioner` over real
  data. *(integration: real DB + real asker/MentionsIn + mock provider; no live
  LLM call)*
