module notify

go 1.26

require (
	appkit v0.0.0
	eventplane v0.0.0
	modernc.org/sqlite v1.50.1
)

// The shared event-plane and chassis libraries are sibling source trees, not
// published modules. go.work resolves them for local dev; these committed
// replaces make the prod build deterministic with or without the workspace
// (GOWORK=off). The ledger clone this repo was duplicated from is eventplane-free,
// so both the require above and the eventplane replace are ADDED here — notify is
// the first consumer of the library's consumer half (decision 12). appkit is the
// uniform chassis the E-phase conversion folds notify onto (PLAN §1.2, §1.6).
replace eventplane => ../eventplane

replace appkit => ../appkit

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
