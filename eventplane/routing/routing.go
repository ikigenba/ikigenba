// Package routing defines canonical event routing keys and their matching rules.
package routing

import "fmt"

// Key renders the canonical routing key.
func Key(source, kind, subject string) string {
	return source + ":" + kind + subject
}

// ValidKind reports whether kind is non-empty lowercase [a-z0-9_.-]+.
func ValidKind(kind string) bool {
	if kind == "" {
		return false
	}
	for _, r := range kind {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

// ValidSubject reports whether subject is empty or a single-line /-rooted path.
func ValidSubject(subject string) bool {
	if subject == "" {
		return true
	}
	if subject[0] != '/' {
		return false
	}
	for _, r := range subject {
		if r == '\n' || r == '\r' {
			return false
		}
	}
	return true
}

type tokenKind uint8

const (
	literal tokenKind = iota
	star
	doubleStar
	question
	class
)

type token struct {
	kind   tokenKind
	char   rune
	negate bool
	ranges [][2]rune
}

// Match reports whether pattern matches the whole key.
func Match(pattern, key string) (bool, error) {
	tokens, err := compile(pattern)
	if err != nil {
		return false, err
	}
	chars := []rune(key)
	type state struct{ pattern, key int }
	memo := make(map[state]bool)
	seen := make(map[state]bool)
	var match func(int, int) bool
	match = func(pi, ki int) bool {
		s := state{pi, ki}
		if seen[s] {
			return memo[s]
		}
		seen[s] = true
		var ok bool
		if pi == len(tokens) {
			ok = ki == len(chars)
		} else {
			t := tokens[pi]
			switch t.kind {
			case literal:
				ok = ki < len(chars) && chars[ki] == t.char && match(pi+1, ki+1)
			case question:
				ok = ki < len(chars) && chars[ki] != '/' && match(pi+1, ki+1)
			case star:
				ok = match(pi+1, ki) || ki < len(chars) && chars[ki] != '/' && match(pi, ki+1)
			case doubleStar:
				// In **/ the slash belongs to the zero-segment form too.
				if pi+1 < len(tokens) && tokens[pi+1].kind == literal && tokens[pi+1].char == '/' {
					ok = match(pi+2, ki)
				}
				ok = ok || match(pi+1, ki) || ki < len(chars) && match(pi, ki+1)
			case class:
				if ki < len(chars) && chars[ki] != '/' {
					inside := false
					for _, pair := range t.ranges {
						inside = inside || pair[0] <= chars[ki] && chars[ki] <= pair[1]
					}
					ok = inside != t.negate && match(pi+1, ki+1)
				}
			}
		}
		memo[s] = ok
		return ok
	}
	return match(0, 0), nil
}

func compile(pattern string) ([]token, error) {
	runes := []rune(pattern)
	tokens := make([]token, 0, len(runes))
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				for i+1 < len(runes) && runes[i+1] == '*' {
					i++
				}
				tokens = append(tokens, token{kind: doubleStar})
			} else {
				tokens = append(tokens, token{kind: star})
			}
		case '?':
			tokens = append(tokens, token{kind: question})
		case '[':
			t, next, err := compileClass(runes, i+1)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, t)
			i = next
		default:
			tokens = append(tokens, token{kind: literal, char: runes[i]})
		}
	}
	return tokens, nil
}

func compileClass(pattern []rune, start int) (token, int, error) {
	t := token{kind: class}
	i := start
	if i < len(pattern) && pattern[i] == '^' {
		t.negate = true
		i++
	}
	for i < len(pattern) && pattern[i] != ']' {
		lo := pattern[i]
		hi := lo
		i++
		if i+1 < len(pattern) && pattern[i] == '-' && pattern[i+1] != ']' {
			hi = pattern[i+1]
			if hi < lo {
				return token{}, 0, fmt.Errorf("routing: invalid character range %q-%q", lo, hi)
			}
			i += 2
		}
		t.ranges = append(t.ranges, [2]rune{lo, hi})
	}
	if i == len(pattern) {
		return token{}, 0, fmt.Errorf("routing: unterminated character class")
	}
	if len(t.ranges) == 0 {
		return token{}, 0, fmt.Errorf("routing: empty character class")
	}
	return t, i, nil
}
