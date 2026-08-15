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

type matchState struct{ pattern, key int }

type matcher struct {
	tokens []token
	chars  []rune
	memo   map[matchState]bool
	seen   map[matchState]bool
}

// Match reports whether pattern matches the whole key.
func Match(pattern, key string) (bool, error) {
	tokens, err := compile(pattern)
	if err != nil {
		return false, err
	}
	m := matcher{tokens: tokens, chars: []rune(key), memo: map[matchState]bool{}, seen: map[matchState]bool{}}
	return m.match(0, 0), nil
}

func (m *matcher) match(pi, ki int) bool {
	state := matchState{pi, ki}
	if m.seen[state] {
		return m.memo[state]
	}
	m.seen[state] = true
	ok := ki == len(m.chars)
	if pi < len(m.tokens) {
		ok = m.matchToken(pi, ki)
	}
	m.memo[state] = ok
	return ok
}

func (m *matcher) matchToken(pi, ki int) bool {
	t := m.tokens[pi]
	switch t.kind {
	case literal:
		return m.matchLiteral(pi, ki, t.char)
	case question:
		return m.matchQuestion(pi, ki)
	case star:
		return m.matchStar(pi, ki)
	case doubleStar:
		return m.matchDoubleStar(pi, ki)
	case class:
		return m.matchClass(pi, ki, t)
	default:
		return false
	}
}

func (m *matcher) matchLiteral(pi, ki int, char rune) bool {
	return ki < len(m.chars) && m.chars[ki] == char && m.match(pi+1, ki+1)
}

func (m *matcher) matchQuestion(pi, ki int) bool {
	return ki < len(m.chars) && m.chars[ki] != '/' && m.match(pi+1, ki+1)
}

func (m *matcher) matchStar(pi, ki int) bool {
	return m.match(pi+1, ki) || ki < len(m.chars) && m.chars[ki] != '/' && m.match(pi, ki+1)
}

func (m *matcher) matchDoubleStar(pi, ki int) bool {
	if pi+1 < len(m.tokens) && m.tokens[pi+1].kind == literal && m.tokens[pi+1].char == '/' && m.match(pi+2, ki) {
		return true
	}
	return m.match(pi+1, ki) || ki < len(m.chars) && m.match(pi, ki+1)
}

func (m *matcher) matchClass(pi, ki int, t token) bool {
	return ki < len(m.chars) && m.chars[ki] != '/' && inClass(t, m.chars[ki]) && m.match(pi+1, ki+1)
}

// CouldMatchSubject reports whether pattern matches prefix followed by either
// an empty subject or some slash-rooted subject.
func CouldMatchSubject(pattern, prefix string) (bool, error) {
	tokens, err := compile(pattern)
	if err != nil {
		return false, err
	}
	return couldReachSubject(tokens, prefix), nil
}

func couldReachSubject(tokens []token, prefix string) bool {
	states := epsilonClosure(tokens, map[int]bool{0: true})
	for _, ch := range prefix {
		states = epsilonClosure(tokens, advance(tokens, states, ch))
	}
	if states[len(tokens)] {
		return true
	}
	states = epsilonClosure(tokens, advance(tokens, states, '/'))
	return reachableAccept(tokens, states)
}

func reachableAccept(tokens []token, states map[int]bool) bool {
	seen := map[int]bool{}
	queue := make([]int, 0, len(states))
	for state := range states {
		queue = append(queue, state)
	}
	alphabet := []rune{'a', '/', ':', '0', '-', '_', '.'}
	for _, tok := range tokens {
		if tok.kind == literal {
			alphabet = append(alphabet, tok.char)
		}
		for _, pair := range tok.ranges {
			alphabet = append(alphabet, pair[0], pair[1])
		}
	}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		if seen[state] {
			continue
		}
		seen[state] = true
		closure := epsilonClosure(tokens, map[int]bool{state: true})
		if closure[len(tokens)] {
			return true
		}
		for _, ch := range alphabet {
			if ch == '\n' || ch == '\r' {
				continue
			}
			for next := range epsilonClosure(tokens, advance(tokens, closure, ch)) {
				if !seen[next] {
					queue = append(queue, next)
				}
			}
		}
	}
	return false
}

func epsilonClosure(tokens []token, states map[int]bool) map[int]bool {
	out := make(map[int]bool, len(states))
	queue := make([]int, 0, len(states))
	for state := range states {
		queue = append(queue, state)
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		if out[i] {
			continue
		}
		out[i] = true
		if i < len(tokens) && (tokens[i].kind == star || tokens[i].kind == doubleStar) {
			queue = append(queue, i+1)
		}
	}
	return out
}

func advance(tokens []token, states map[int]bool, ch rune) map[int]bool {
	out := map[int]bool{}
	for i := range states {
		if i == len(tokens) {
			continue
		}
		t := tokens[i]
		if tokenMatches(t, ch) {
			if t.kind == star || t.kind == doubleStar {
				out[i] = true
			} else {
				out[i+1] = true
			}
		}
	}
	return out
}

func tokenMatches(t token, ch rune) bool {
	switch t.kind {
	case literal:
		return t.char == ch
	case question, star:
		return ch != '/'
	case doubleStar:
		return true
	case class:
		return ch != '/' && inClass(t, ch)
	default:
		return false
	}
}

func inClass(t token, ch rune) bool {
	inside := false
	for _, pair := range t.ranges {
		inside = inside || pair[0] <= ch && ch <= pair[1]
	}
	return inside != t.negate
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
