module wiki

go 1.26

require modernc.org/sqlite v1.50.1

// Shared sibling source trees, not published modules. go.work resolves them for
// local dev; these committed replaces keep bin/build deterministic with or
// without the workspace. agentkit is not imported yet (it lands in Phase 4), but
// the replace is declared now so the seam is ready.
replace eventplane => ../eventplane

replace agentkit => ../agentkit

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.42.0 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
