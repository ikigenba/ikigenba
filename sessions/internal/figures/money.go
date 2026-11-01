package figures

import (
	"math/big"
	"strings"
)

// Rate stores millionths of a dollar per million tokens.
type Rate struct{ units uint64 }

// Money stores picodollars in a canonical, comparable representation.
type Money struct{ units [16]byte }

func moneyBound() *big.Int {
	n := new(big.Int).Exp(big.NewInt(10), big.NewInt(38), nil)
	return n.Sub(n, big.NewInt(1))
}
func decimal(s string, places int) (*big.Int, bool) {
	parts := strings.Split(s, ".")
	if len(parts) > 2 || len(parts[0]) == 0 {
		return nil, false
	}
	for _, p := range parts {
		if len(p) == 0 {
			return nil, false
		}
		for i := range len(p) {
			if p[i] < '0' || p[i] > '9' {
				return nil, false
			}
		}
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) > places {
			return nil, false
		}
	}
	n, ok := new(big.Int).SetString(parts[0]+fraction+strings.Repeat("0", places-len(fraction)), 10)
	return n, ok
}

// ParseRate accepts a nonnegative decimal with at most six fractional digits.
func ParseRate(s string) (Rate, bool) {
	n, ok := decimal(s, 6)
	if !ok || n.Cmp(big.NewInt(1000000000000)) > 0 {
		return Rate{}, false
	}
	return Rate{n.Uint64()}, true
}
func scaledString(n *big.Int, places int) string {
	s := n.String()
	if len(s) <= places {
		s = strings.Repeat("0", places+1-len(s)) + s
	}
	p := len(s) - places
	fraction := strings.TrimRight(s[p:], "0")
	if fraction == "" {
		return s[:p]
	}
	return s[:p] + "." + fraction
}
func (r Rate) String() string { return scaledString(new(big.Int).SetUint64(r.units), 6) }

// ParseMoney accepts a bounded decimal with at most twelve fractional digits.
func ParseMoney(s string) (Money, bool) {
	n, ok := decimal(s, 12)
	if !ok || n.Cmp(moneyBound()) > 0 {
		return Money{}, false
	}
	return money(n), true
}
func (m Money) number() *big.Int { return new(big.Int).SetBytes(m.units[:]) }
func money(n *big.Int) Money {
	if n.Cmp(moneyBound()) > 0 {
		n = moneyBound()
	}
	var m Money
	n.FillBytes(m.units[:])
	return m
}

// Add returns the componentwise sum, saturating at each bound.
func (m Money) Add(n Money) Money { return money(new(big.Int).Add(m.number(), n.number())) }
func (m Money) String() string    { return scaledString(m.number(), 12) }

// Dollars renders money rounded half up to exactly two decimal places.
func (m Money) Dollars() string {
	n := m.number()
	n.Add(n, big.NewInt(5000000000))
	n.Quo(n, big.NewInt(10000000000))
	s := n.String()
	if len(s) < 3 {
		s = strings.Repeat("0", 3-len(s)) + s
	}
	return s[:len(s)-2] + "." + s[len(s)-2:]
}

// Cost returns exact priced cost and the saturating total of unpriced tokens.
func Cost(turns []Turn, prices []Price) (Money, int64) {
	sum := new(big.Int)
	var unpriced int64
	for _, t := range turns {
		p, ok := PriceAt(prices, t.Model, t.Moment)
		if !ok {
			unpriced = sat(unpriced, t.Tokens.Total())
			continue
		}
		counts := []int64{t.Tokens.Input, t.Tokens.Cached, t.Tokens.Output, t.Tokens.Reasoning}
		rates := []Rate{p.Input, p.Cached, p.Output, p.Reasoning}
		for i, c := range counts {
			sum.Add(sum, new(big.Int).Mul(big.NewInt(c), new(big.Int).SetUint64(rates[i].units)))
		}
	}
	return money(sum), unpriced
}
