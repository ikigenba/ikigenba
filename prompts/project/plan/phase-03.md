# Phase 3 — MCP schema expansion

*Realizes design Decision 9 (MCP schema). Depends on Phase 01.*

**`prompts/internal/mcp/tools.go`**: expand `configSchema()` to declare all 16 fields from the D2 Config struct — `provider` and `model` in the required array, the 11 optional generation/retry/tuning keys and `base_url` present in the schema object but not required. No structural changes to the function; only its return value grows.

Update the `describe` tool's static prose to reflect four valid provider strings, the 11 optional config keys and their semantics, and the LogRecord JSONL run output format.

**Done when:** R-KE1K-MUZ4 and R-KF9H-0MPT are each covered by a clearly-named test and `go test ./...` is green.
