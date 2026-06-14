package config

// The lint config-default prompts (design §6). lint-dups runs TWO separate
// tool-less calls (design §6: "Judge and fold are two separate tool-less calls"):
// the dup JUDGE (a ternary identity verdict on a flagged pair, plus the canonical
// name pick) and the FOLD (two page bodies in → one merged body out, run only on a
// merge verdict, inheriting the §6.1 citation-preservation obligation). Each is a
// config default P9a fills here; config may override via WIKI_LINT_DUP_PROMPT /
// WIKI_LINT_FOLD_PROMPT, and Part II sweeps alternatives.

// DefaultLintDupJudgePrompt is the config-default prompt for the dup-judge call
// site (design §6). The judge is the better-informed second look a `dup_flags` row
// (evidence, never a verdict) demands: it reads BOTH full pages + complete alias
// lists and returns one of three verdicts — merge | dismiss | can't-tell-yet —
// PLUS, on a merge, the canonical name to keep (a FIELD of this output, not a
// separate call — design §6). The five `## N` markers are load-bearing: the
// offline prompt-default gate asserts all five are present.
const DefaultLintDupJudgePrompt = `You judge whether two subject pages in a knowledge base describe the SAME
real-world subject. You are the careful second look: a duplicate FLAG is only a
suspicion, never a verdict. You are given both subjects' full pages and their
complete alias lists. Return ONLY JSON matching the provided schema — no prose
outside the JSON.

## 1. Task framing — identity, not similarity
Two pages are the SAME subject only if they refer to the same real-world thing,
not merely if they look alike or share a topic. Two different people with the
same name are NOT the same subject; a company and its founder are NOT the same
subject. Weigh the names, aliases, and the corroborating facts in both bodies.

## 2. The three verdicts
Return exactly one verdict:
- "merge" — you are confident the two pages are the same subject and should be
  combined into one.
- "dismiss" — you are confident they are DIFFERENT subjects; this is permanent
  and blocks the pair from being re-flagged, so use it only when sure.
- "cant_tell" — the evidence is insufficient to decide right now. This is the
  safe default under doubt; the pair will be re-judged when either page gains
  new evidence.

## 3. The asymmetry — doubt favors keeping them apart
A false merge poisons a page by fusing two distinct subjects and is expensive to
undo; a false split is cheap and self-healing (the pair stays flagged and is
re-judged later). So when uncertain, prefer "cant_tell" over "merge", and never
emit "merge" on a weak guess.

## 4. The canonical name (merge only)
On a "merge" verdict, also return "canonical_name": the single best display name
for the combined subject — the clearest, most complete, most current of the two
pages' names. This name choice is the only naming decision you make; which
subject id survives is decided mechanically, not by you.

## 5. Output schema and example
Return a single JSON object: {"verdict": "merge" | "dismiss" | "cant_tell",
"canonical_name": "<string, required on merge, omit otherwise>"}.

Worked example.
Subject A: name "Acme Corp", aliases ["acme", "acme corporation"],
  body: "Acme Corp is a hardware manufacturer founded in 1990. [01HX...]"
Subject B: name "ACME Corporation", aliases ["acme corp"],
  body: "ACME Corporation makes industrial hardware; founded 1990. [01HY...]"
Output:
{"verdict": "merge", "canonical_name": "Acme Corporation"}`

// DefaultLintFoldPrompt is the config-default prompt for the fold call site
// (design §6, §6.1). Fold runs ONLY on a merge verdict: it takes both subjects'
// page bodies and produces ONE merged page, inheriting the merge craft obligations
// — prose not a ledger, every fact keeps its inline citation, and the §6.1
// citation-preservation obligation (declare every dropped citation in "superseded").
// The five `## N` markers are load-bearing for the offline prompt-default gate.
const DefaultLintFoldPrompt = `You fold two knowledge-base pages that describe the SAME subject into a single
coherent prose page. A separate judgment already confirmed they are the same
subject and picked the canonical name; your job is only to merge the prose.
Return ONLY JSON matching the provided schema — no prose outside the JSON.

## 1. Task framing — one subject, one page
You are given two page bodies for the same subject plus the chosen canonical
name. Produce ONE merged body that reads as a single page about one subject, not
two pages stapled together. Do not invent facts neither page states.

## 2. Fold discipline — prose, not a ledger
Write flowing prose. Weave the two narratives together; where both pages assert
the same fact, state it once and keep BOTH citations. Where they genuinely
CONTRADICT, keep both statements with their citations in a clearly marked
"Conflicting accounts" section rather than silently dropping one. The first
paragraph must state the subject's identity and the names it is known by (so a
later match can recover the page from its lead).

## 3. Citation preservation — declare every dropped citation (superseded)
Every inline [inbox-id] citation present in EITHER source body must survive into
the merged body OR be listed in "superseded" with nothing dropped silently. A
citation is evidence; paraphrasing it away without declaring it is a failed
fold. At commit a set difference (old citations − new citations) must equal your
declared superseded list exactly — an undeclared loss fails the call. If you
drop none, emit an empty list.

## 4. Compression mandate — merge, don't accumulate
One subject is one page forever (pages never split). Merge redundant sentences
rather than concatenating; the result should be tighter than the two inputs
combined, never a mechanical append.

## 5. Output schema and example
Return a single JSON object: {"title": "<canonical title>", "body": "<merged
prose page>", "superseded": ["<dropped inbox id>", ...]}.

Worked example.
Page A body: "Acme Corp is a hardware maker founded in 1990. [01HX...]"
Page B body: "ACME Corporation makes industrial hardware. [01HY...]"
canonical name: "Acme Corporation"
Output:
{"title": "Acme Corporation", "body": "Acme Corporation, also known as Acme Corp, is a hardware manufacturer founded in 1990 that makes industrial hardware. [01HX...] [01HY...]", "superseded": []}`
