---
description: a test whose name or doc claims completeness ("every", "all", "exhaustive") while the same file shows a case it does not cover
severity: error
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
You see one test file at a time. Judge every finding using only what is written
in this file. Never assume you can read the implementation, another test file,
or a package you cannot see; do not guess at cases the file does not show.

Flag a test whose name or doc comment claims to cover a whole closed set while
the same file contains evidence of a member of that set the test does not
exercise. Such a name is read as a coverage guarantee: a maintainer sees
"handles every error" and stops checking, so an untested member goes untested
forever and nobody adds a case when a new one appears.

Decide it in three mechanical steps, all inside this file:

1. **Does the name or doc comment claim completeness?** Look for a quantifier
   word applied to a set: `every`, `all`, `each`, `exhaustive`, `complete`,
   `full`, `any`, `always`, `never`. No such word — do not flag; a narrow name
   like `TestRejectsMissingSeparator` makes an honest partial claim and is
   always spared.
2. **What closed set does the name name?** It must be a finite, enumerable set:
   error reasons or sentinels, enum variants, flag spellings, rejection
   categories, boundary values. If the named domain is open-ended — arbitrary
   strings, all inputs, any timestamp — completeness is understood as
   aspirational; do not flag.
3. **Does this file show a member the test omits?** Search the file for
   evidence of a case missing from the flagged test's table or assertions:
   - a sibling test in this file that exercises a case (an error string, a flag
     spelling, an input shape) the "complete" test's table does not include;
   - a constant, slice, map, or `var` in this file that lists members the table
     does not enumerate;
   - a value the file itself references (an error sentinel, a reason string,
     an alias) that the table never produces.
   If you find such a member, flag it and name both the omitted member and the
   file location that evidences it. If the file shows no such gap, do not
   flag — you cannot see the implementation, so absence of in-file evidence is
   not evidence of completeness, and you must not invent a missing case.

The fix is to rename the test to the subset it actually covers, or to add a
case for the evidenced member so the claim becomes true. Do not flag a name
that already matches its table. Do not flag a golden or round-trip test whose
"every" ranges over a domain the file defines and fully sweeps.

```go
// Same file names the full set of rejection reasons the validator emits:
var allRejectionReasons = []string{"empty", "too long", "bad char", "out of range"}

// Flagged: the name claims "every reason", but the table omits "out of range",
// which allRejectionReasons (declared above, in THIS file) lists. Rename to the
// covered subset, or add the missing case.
func TestValidateRejectsEveryReason(t *testing.T) {
	for _, tc := range []struct{ name, input, reason string }{
		{"empty", "", "empty"},
		{"too long", "AAAAAAAAA", "too long"},
		{"bad char", "ab!", "bad char"},
		// no case yields "out of range"
	} {
		// ...
	}
}
```

```go
// Spared: the name claims exactly what the table covers — no quantifier.
func TestValidateRejectsMissingAndNonNumeric(t *testing.T) {
	// ...
}

// Spared: the completeness claim is backed by a case for every member the
// file lists, so the name is true from this file alone.
func TestValidateRejectsEveryReason(t *testing.T) {
	for _, tc := range []struct{ name, input, reason string }{
		{"empty", "", "empty"},
		{"too long", "AAAAAAAAA", "too long"},
		{"bad char", "ab!", "bad char"},
		{"out of range", "999999999", "out of range"},
	} {
		// ...
	}
}

// Spared: "every" ranges over an open-ended domain (arbitrary strings); the
// name is understood as aspirational and no finite table could close it.
func TestTimeOfNeverPanicsForEveryInput(t *testing.T) {
	// fuzz-style sweep over random strings
}
```
