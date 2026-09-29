package browse

import "unicode/utf8"

type key int

const (
	keyIgnore key = iota
	keyQuit
	keyUp
	keyDown
	keyOpen
	keyBack
	keyPageUp
	keyPageDown
	keyFirst
	keyLast
)

func decodeKeys(value []byte) []key {
	if len(value) == 1 && value[0] == '\x1b' {
		return []key{keyQuit}
	}
	var keys []key
	for len(value) > 0 {
		n := tokenLength(value)
		keys = append(keys, tokenKey(string(value[:n])))
		value = value[n:]
	}
	return keys
}

func tokenLength(value []byte) int {
	i := 0
	for value[i] == '\x1b' {
		i++
		if i == len(value) {
			return i
		}
		switch value[i] {
		case '[':
			i++
			for i < len(value) && value[i] >= 0x20 && value[i] <= 0x3f {
				i++
			}
			if i < len(value) && value[i] >= 0x40 && value[i] <= 0x7e {
				i++
			}
			return i
		case 'O':
			return min(i+2, len(value))
		}
	}
	_, n := utf8.DecodeRune(value[i:])
	return i + n
}

func tokenKey(token string) key {
	switch token {
	case "q", "\x03":
		return keyQuit
	case "\x1b[A", "\x1bOA", "k":
		return keyUp
	case "\x1b[B", "\x1bOB", "j":
		return keyDown
	case "\x1b[C", "\x1bOC", "\r", "\x1bOM", "l":
		return keyOpen
	case "\x1b[D", "\x1bOD", "h":
		return keyBack
	case "\x1b[5~", "\x02":
		return keyPageUp
	case "\x1b[6~", "\x06":
		return keyPageDown
	case "g":
		return keyFirst
	case "G":
		return keyLast
	default:
		return keyIgnore
	}
}
