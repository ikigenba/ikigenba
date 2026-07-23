# Synthesis tuning folder

The 14 development and 7 holdout cases are production-shaped questions plus JSON page bodies derived from the same fictional universe and split as the other tune folders. Each `gold.json` records expected key points, expected `found`, allowed citations, and review notes. These are seed golds pending operator review. Run the folder with `autotune autotune/synthesis` from the repository root.

The hybrid scorer assigns 60% to deterministic gates and 40% to a `gpt-5.6-sol` judge. The gate component equally checks the exact `{found, text, citations}` shape, whether every citation exactly names a supplied page, whether a found answer cites at least one page, and whether an expected-empty case returns `found:false`. The judge equally weights groundedness and completeness using the fixed `judge-prompt.txt` rubric.

Set `SCORE_SKIP_JUDGE=1` for deterministic offline scoring; in that mode the returned total is the gate score. `fixtures/gates` contains clean, unsupplied-citation, citation-free, and expected-empty violations with hand-computed expectations. A normal judge run reads `OPENAI_API_KEY` and makes one OpenAI Responses API call.
