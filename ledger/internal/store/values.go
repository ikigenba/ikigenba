package store

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Constants define ledger identifiers, account roots and input limits.
const (
	TransactionIDPrefix    = "txn_"
	AssertionIDPrefix      = "bal_"
	NoteIDPrefix           = "note_"
	DocumentIDPrefix       = "doc_"
	FlagPending            = "pending"
	FlagCleared            = "cleared"
	StatusHolds            = "holds"
	StatusFails            = "fails"
	DateLayout             = "2006-01-02"
	DefaultFiscalYearStart = "01-01"
	RootAssets             = "Assets"
	RootLiabilities        = "Liabilities"
	RootEquity             = "Equity"
	RootIncome             = "Income"
	RootExpenses           = "Expenses"
	EarningsCurrent        = "Equity:Earnings:Current"
	EarningsPrevious       = "Equity:Earnings:Previous"
	MaxText                = 200
	MaxToken               = 50
	MaxTokens              = 20
	MaxMetadata            = 20
	MaxMetadataValue       = 500
	MaxNote                = 2000
)

// Unreachable is the response copy for an unavailable ledger.
const Unreachable string = "The ledger cannot be reached."

// These sentinels distinguish ledger refusals from infrastructure failures.
var (
	ErrNotFound    = errors.New("record not found")
	ErrInvalid     = errors.New("invalid ledger value")
	ErrLocked      = errors.New("date is locked")
	ErrUnbalanced  = errors.New("postings do not balance")
	ErrUnreachable = errors.New("ledger unreachable")
)

func (a Amount) String() string {
	s := strconv.FormatInt(a.Units, 10)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign = "-"
		s = s[1:]
	}
	if a.Precision <= 0 {
		return sign + s
	}
	if len(s) <= a.Precision {
		s = strings.Repeat("0", a.Precision-len(s)+1) + s
	}
	p := len(s) - a.Precision
	return sign + s[:p] + "." + s[p:]
}

// ParseAmount parses decimal text into bounded minor units.
func ParseAmount(s string, precision int) (int64, bool) {
	if precision < 0 {
		return 0, false
	}
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, false
	}
	frac := 0
	if len(parts) == 2 {
		frac = len(parts[1])
		if frac > precision {
			return 0, false
		}
	}
	digits := strings.Join(parts, "")
	var n int64
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
		if n > 999999999999999 {
			return 0, false
		}
	}
	if n != 0 {
		for i := frac; i < precision; i++ {
			n *= 10
			if n > 999999999999999 {
				return 0, false
			}
		}
	}
	if negative {
		n = -n
	}
	return n, true
}

// ValidDate reports whether s has the required form.
func ValidDate(s string) bool {
	t, e := time.Parse(DateLayout, s)
	return e == nil && t.Format(DateLayout) == s
}

// ValidMonthDay reports whether s has the required form.
func ValidMonthDay(s string) bool { return len(s) == 5 && ValidDate("2001-"+s) }
func root(s string) bool {
	return s == RootAssets || s == RootLiabilities || s == RootEquity || s == RootIncome || s == RootExpenses
}
func accountPrefix(s string) bool      { return root(s) || ValidAccountName(s) }
func subtree(name, prefix string) bool { return name == prefix || strings.HasPrefix(name, prefix+":") }
func upper(c byte) bool                { return c >= 'A' && c <= 'Z' }
func lower(c byte) bool                { return c >= 'a' && c <= 'z' }
func digit(c byte) bool                { return c >= '0' && c <= '9' }
func alnum(c byte) bool                { return upper(c) || lower(c) || digit(c) }

// ValidAccountName reports whether s has the required form.
func ValidAccountName(s string) bool {
	p := strings.Split(s, ":")
	if len(p) < 2 || !root(p[0]) {
		return false
	}
	for _, v := range p {
		if v == "" || (!upper(v[0]) && !digit(v[0])) {
			return false
		}
		for i := 1; i < len(v); i++ {
			if !alnum(v[i]) && v[i] != '-' {
				return false
			}
		}
	}
	return true
}

// ValidFlag reports whether s has the required form.
func ValidFlag(s string) bool { return s == FlagPending || s == FlagCleared }
func token(s string, limit int, extra string) bool {
	if len(s) == 0 || len(s) > limit {
		return false
	}
	for i := range len(s) {
		if !alnum(s[i]) && !strings.ContainsRune(extra, rune(s[i])) {
			return false
		}
	}
	return true
}

// ValidToken reports whether s has the required form.
func ValidToken(s string) bool { return token(s, MaxToken, "-_./") }

// ValidMetadataKey reports whether s has the required form.
func ValidMetadataKey(s string) bool {
	if s == "" || !lower(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !alnum(s[i]) && s[i] != '-' && s[i] != '_' {
			return false
		}
	}
	return true
}

// ValidKey reports whether s has the required form.
func ValidKey(s string) bool { return token(s, 100, "-_.:/") }
func validID(s, prefix string) bool {
	if !strings.HasPrefix(s, prefix) || len(s) != len(prefix)+16 {
		return false
	}
	for _, c := range s[len(prefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidTransactionID reports whether s has the required form.
func ValidTransactionID(s string) bool { return validID(s, TransactionIDPrefix) }

// ValidAssertionID reports whether s has the required form.
func ValidAssertionID(s string) bool { return validID(s, AssertionIDPrefix) }

// ValidNoteID reports whether s has the required form.
func ValidNoteID(s string) bool { return validID(s, NoteIDPrefix) }

// ValidDocumentID reports whether s has the required form.
func ValidDocumentID(s string) bool { return validID(s, DocumentIDPrefix) }
func normalizeWords(payee, narration string, tags, links []string, metadata map[string]string) (Annotation, bool) {
	a := Annotation{Payee: strings.TrimSpace(payee), Narration: strings.TrimSpace(narration), Tags: []string{}, Links: []string{}, Metadata: map[string]string{}}
	if utf8.RuneCountInString(a.Payee) > MaxText || utf8.RuneCountInString(a.Narration) > MaxText || len(metadata) > MaxMetadata {
		return Annotation{}, false
	}
	for _, pair := range []struct {
		src []string
		dst *[]string
	}{{tags, &a.Tags}, {links, &a.Links}} {
		seen := map[string]bool{}
		for _, v := range pair.src {
			if !ValidToken(v) {
				return Annotation{}, false
			}
			if !seen[v] {
				seen[v] = true
				*pair.dst = append(*pair.dst, v)
			}
		}
		if len(*pair.dst) > MaxTokens {
			return Annotation{}, false
		}
		sort.Strings(*pair.dst)
	}
	for k, v := range metadata {
		if !ValidMetadataKey(k) || utf8.RuneCountInString(v) > MaxMetadataValue {
			return Annotation{}, false
		}
		a.Metadata[k] = v
	}
	return a, true
}
