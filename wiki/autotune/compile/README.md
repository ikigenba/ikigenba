# Compile tuning folder

The 14 development and 7 holdout cases are production-shaped subject identity cards and clean claim texts derived from the matching extract split. Each `gold.json` records key facts and review notes for the prose judge. These are seed golds pending operator review. Run the folder with `autotune autotune/compile` from the repository root.

The hybrid scorer assigns 60% to deterministic gates and 40% to a `gpt-5.6-sol` prose judge. The gate component equally checks the `{title, body}` shape with non-empty fields, the 12,000-character Unicode body cap, and that the body contains no leaked ULID. The judge equally weights coverage, factuality, natural-category lead discipline, and organization using the fixed `judge-prompt.txt` rubric.

Set `SCORE_SKIP_JUDGE=1` for deterministic offline scoring; in that mode the returned total is the gate score. `fixtures/gates` contains clean, malformed, over-cap, and leaked-id candidates with hand-computed expectations. A normal judge run reads `OPENAI_API_KEY` and makes one OpenAI Responses API call.
