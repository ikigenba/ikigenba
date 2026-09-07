# D26-savepoint-and-restore

A conversation's transcript only ever grows. That is right for a dialogue and
wrong for a consumer that wants to ask many independent questions against one
expensive body of context. `llm-lint` is the motivating case: load every source
file under review, then check it against rule 1, rule 2, rule 3 — where each
rule's answer must not colour the next, and where re-loading the files for every
rule is the cost that makes the tool impractical.

**Savepoint and restore** give that consumer a transcript it can rewind. The
consumer marks a point in `History`; later it puts `History` back exactly as it
was at that point, as many times as it likes. The vocabulary is SQL's —
`SAVEPOINT` and `ROLLBACK TO` — because the semantics are SQL's: a named point
you return to, not a copy you hold.

```go
sp, err := conv.Savepoint()          // everything up to here is the stable part
for _, rule := range rules {
	// ask about one rule, read the answer
	if err := conv.Restore(sp); err != nil { return err }
}
err = conv.Release(sp)               // keep the history, drop the savepoint
```

**What a restore restores.** A `Conversation` carries two kinds of state. Some
of it describes the conversation *as it is now* — the transcript, and the
measurements D25's `Limits` reads. `Restore` puts all of that back. The rest
describes the conversation's *whole life* — the cumulative `Usage` and `Cost`
the `Log` accumulates for its `summary` record (D15). `Restore` never touches
that: the provider was really called and the money was really spent, and a
rewound transcript does not unspend it. Everything else about a `Conversation`
— its wire, endpoint, model, settings, and registered tool set — is fixed at
construction (D12) and cannot move in either direction.

Loaded deferred tools are the one piece of current-history state a restore
leaves alone, and not by choice: D16's R-5UG8-E9UB forbids unloading outright,
because removing a tool would strand the `tool_use`/`tool_result` pairs that
`History` still carries. A restore therefore cannot un-load, and must not
pretend to.

**A savepoint locks the request prefix, not just the transcript.** The reason
to rewind is that the part before the mark is worth reusing, and every wire
that can make reuse cheap (D27) keys on the *whole rendered request prefix* —
tools, then system content, then messages. A savepoint that promised only a
stable message prefix would promise nothing usable: one deferred tool loaded
mid-loop shifts every byte after it. So a live savepoint freezes what precedes
its mark. `AddSystem` is refused; `load_tools` loads nothing and says so in
band; a direct call to a deferred-but-unloaded tool is refused without the
usual load-as-a-side-effect. The tool array a round-trip advertises is the same
array for as long as the savepoint lives.

This costs a consumer nothing it cannot plan around — load the tools and write
the system message before taking the savepoint — and it costs `llm-lint`
nothing at all, since a consumer that registers no deferred groups has no
`load_tools` to begin with (D16).

**One at a time.** A conversation holds at most one live savepoint. Nesting
would buy a consumer little and would force D27 to answer questions no consumer
is asking — which of several marks owns the wire's cache, how many of
Anthropic's four breakpoints a stack may claim, how many billed Gemini cache
resources one conversation may hold at once.

**Ending a savepoint, and ending a conversation.** `Restore` does not end a
savepoint; that is the whole point, since `llm-lint` restores to the same mark
on every rule. A savepoint ends at `Release`, which keeps the history built
since the mark and simply stops offering to rewind it, or at `Close`.

`Conversation` gains a `Close` because it needs an end of life of its own.
Until now "closed" was the log's property: `Send` and `AddSystem` consult the
`Log` (D15), so a conversation configured without one could never be closed at
all and `ErrClosed` was unreachable for it. `Close` fixes that, and D27 gives it
real work — a provider-side cache is a billed resource that must be released
even when nothing else is. It does not close the `Log`: the consumer built the
log and the consumer closes it. A log closed first still stops the conversation,
because a `Send` that cannot be recorded loses its account of itself.

**Refusals are loud and specific.** Three ways to call these operations wrongly
each get their own sentinel, so a consumer can tell them apart with `errors.Is`
rather than by reading a message: a savepoint is live and the operation would
break its promise (`ErrSavepointActive`), a turn is in flight so no state may
move (`ErrTurnInFlight`), and the conversation is closed (`ErrClosed`, D4). A
handle that is not this conversation's live savepoint is an `ErrInvalidArgument`
like any other bad argument.

The in-flight rule matters more than it looks. `Send` returns a `*Stream` that
has done nothing yet; the provider call, the tool loop and the commit onto
`History` all happen while the consumer drains it. A savepoint taken in that
window would capture a transcript the running turn is about to append to from a
base it no longer matches. So a turn is in flight from the moment `Send` returns
until the `*Stream` it returned has finished producing events, and none of these
four operations may run inside it.

**The log stays replayable.** `Restore` removes messages the log already
recorded, so a reader replaying `message` records in `Seq` order would otherwise
rebuild a `History` that was never sent to any provider. Three new record types
(D15) keep the trace exact: a reader that honours them reconstructs the real
transcript, and a reader that ignores them still sees everything that happened,
which is what cost and audit want.

## REQUIREMENTS

- R-7J67-WBYA: `agentkit` MUST export `type Savepoint` as an opaque type with no exported fields and no exported methods.
- R-7KE4-A3OZ: `agentkit` MUST export the methods `func (c *Conversation) Savepoint() (Savepoint, error)`, `func (c *Conversation) Restore(sp Savepoint) error`, `func (c *Conversation) Release(sp Savepoint) error`, and `func (c *Conversation) Close() error`.
- R-7LM0-NVFO: `agentkit` MUST export the sentinel errors `ErrSavepointActive` and `ErrTurnInFlight`, each an `error` created with `errors.New` and comparable via `errors.Is`, including when wrapped in `*Error`.
- R-7MTX-1N6D: A successful `Savepoint` MUST record the conversation's current `History` as the point a later `Restore` returns to, MUST make no provider call, and MUST leave `History` unchanged.
- R-7O1T-FEX2: A `Conversation` MUST hold at most one live savepoint; `Savepoint` called while one is live MUST return the zero `Savepoint` and an error satisfying `errors.Is(err, ErrSavepointActive)`, and MUST leave `History` and the live savepoint unchanged.
- R-7P9P-T6NR: `Restore` or `Release` given a `Savepoint` that is not the conversation's live savepoint — the zero value, one already released, or one taken from another `Conversation` — MUST return an error satisfying `errors.Is(err, ErrInvalidArgument)` and leave `History`, the live savepoint, and every `Limits` counter unchanged.
- R-7QHM-6YEG: `Savepoint`, `Restore`, `Release`, and `Close` MUST each return an error satisfying `errors.Is(err, ErrTurnInFlight)` and change no conversation state when called while a turn is in flight, where a turn is in flight from the moment `Send` returns until the `*Stream` it returned has finished producing events.
- R-7RPI-KQ55: A successful `Restore` MUST leave `History` equal, message for message and block for block, to the `History` at the moment its savepoint was taken, and MUST leave that savepoint live so a later `Restore` with the same handle succeeds again.
- R-7SXE-YHVU: A successful `Restore` MUST set the most-recently-completed-round-trip context total that D25's `MaxContextTokens` checkpoint reads to zero, so the next checkpoint can refuse work only on a measurement taken after that `Restore`.
- R-7VD7-Q1D8: A successful `Restore` MUST set the dispatched-tool-call count that D25's `MaxToolCalls` checkpoint reads back to the value it held when the savepoint was taken.
- R-7WL4-3T3X: A successful `Release` MUST leave `History` unchanged, MUST leave every `Limits` counter unchanged, and MUST end the savepoint's life so that a subsequent `Restore` or `Release` with the same handle fails per R-7P9P-T6NR.
- R-7XT0-HKUM: While a savepoint is live, `AddSystem` MUST return an error satisfying `errors.Is(err, ErrSavepointActive)`, MUST leave `History` unchanged, and MUST write no log record.
- R-7Z0W-VCLB: While a savepoint is live, a `load_tools` call MUST load no tool and MUST return a `ToolResult` with `IsError` set whose content names the live savepoint as the reason, and MUST NOT end the turn.
- R-808T-94C0: While a savepoint is live, a direct call to a deferred-but-unloaded tool MUST NOT load that tool and MUST return a `ToolResult` with `IsError` set, and MUST NOT end the turn.
- R-81GP-MW2P: While a savepoint is live, every round-trip MUST advertise the same tool sequence, in the same order, as the round-trip that followed the savepoint's creation.
- R-82OM-0NTE: A successful `Restore` MUST leave the set of loaded deferred tools unchanged, and no operation in this design may unload a deferred tool (D16, R-5UG8-E9UB).
- R-83WI-EFK3: `Close` MUST be idempotent — a second `Close` MUST change nothing and return `nil` — and MUST NOT close the conversation's `Log`.
- R-854E-S7AS: After `Close`, each of `Send`, `AddSystem`, `Savepoint`, `Restore`, and `Release` MUST return an error satisfying `errors.Is(err, ErrClosed)`, make no provider call, and leave `History` unchanged.
- R-86CB-5Z1H: A successful `Savepoint`, `Restore`, and `Release` MUST each write exactly one log record (D15), of type `RecordSavepoint`, `RecordRestore`, and `RecordRelease` respectively, outside any `turn_start`/`turn_end` pair; a call that returns an error MUST write none.
- R-6N6M-6P9I: Replaying a log's records in `Seq` order — collecting each `message` record's `Message`, on each `turn_end` record discarding those collected since its `turn_start` when an `error` or `limit` record was written between the two, and on each `restore` record discarding those collected since the preceding `savepoint` record — MUST yield exactly the conversation's `History`.
- R-6OEI-KH07: `Savepoint` MUST succeed on a `Conversation` that has completed no `Send`, and a `Restore` to such a savepoint MUST leave `History` holding exactly the `RoleSystem` messages `AddSystem` had appended before the savepoint was taken — empty when there were none.
This design also revises requirements owned by other documents; each revision is
made in its own document, not here. D18 re-mints the `Conversation` method set to
name the four new operations. D15 re-mints `RecordType` for the three new record
kinds. D25 re-mints both `Limits` checkpoints so their counters are scoped to the
current history rather than the conversation's life. D16 re-mints both of its
loading paths so a live savepoint suspends them.

## Canonical usage

The `llm-lint` loop — load the corpus once, check it against every rule, and
keep each rule's verdict out of the next rule's context:

```go
conv, err := agentkit.New(wire, endpoint, model, agentkit.Config{
	Tools:  []agentkit.Tool{readTool, globTool},
	Log:    log,
	Limits: agentkit.Limits{MaxToolCalls: 200},
})
if err != nil {
	return err
}
defer func() { _ = conv.Close() }()

if err := conv.AddSystem(reviewerInstructions); err != nil {
	return err
}

// One turn that reads the corpus into the transcript.
for range conv.Send(ctx, agentkit.Text{Text: "Read every file under ./idgen."}).Events() {
}

sp, err := conv.Savepoint()
if err != nil {
	return err
}

for _, rule := range rules {
	stream := conv.Send(ctx, agentkit.Text{Text: rule.Prompt})
	for event := range stream.Events() {
		record(event)
	}
	if err := stream.Err(); err != nil {
		return err
	}
	if err := conv.Restore(sp); err != nil {
		return err
	}
}

if err := conv.Release(sp); err != nil {
	return err
}
```

A consumer that decides mid-run it will not rewind after all keeps its work and
stops holding the mark:

```go
sp, err := conv.Savepoint()
if err != nil {
	return err
}
if !worthRewinding(corpus) {
	if err := conv.Release(sp); err != nil {   // history kept, savepoint gone
		return err
	}
}
```

And the refusals a consumer can tell apart:

```go
_, err := conv.Savepoint()
switch {
case errors.Is(err, agentkit.ErrSavepointActive):  // one is already live
case errors.Is(err, agentkit.ErrTurnInFlight):     // drain the Stream first
case errors.Is(err, agentkit.ErrClosed):           // conversation is over
}
```
