package gmail

import (
	"html"
	"strings"
)

func asciiLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
func space(c byte) bool       { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }

func tagName(s string) string {
	s = strings.TrimPrefix(s, "/")
	n := 0
	for n < len(s) && !space(s[n]) && s[n] != '/' {
		n++
	}
	name := []byte(s[:n])
	for i, c := range name {
		if c >= 'A' && c <= 'Z' {
			name[i] = c + ('a' - 'A')
		}
	}
	return string(name)
}

func removeIgnoredHTML(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "<!--") {
			j := strings.Index(s[i+4:], "-->")
			if j < 0 {
				break
			}
			i += 4 + j + 3
			continue
		}
		if s[i] == '<' && i+1 < len(s) && (asciiLetter(s[i+1]) || s[i+1] == '/' || s[i+1] == '!') {
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				end = len(s) - i - 1
			}
			name := tagName(s[i+1 : i+end])
			if (name == "script" || name == "style") && s[i+1] != '/' {
				tail := s[i+end+1:]
				closingEnd := -1
				for start := 0; start < len(tail); {
					j := strings.Index(tail[start:], "</")
					if j < 0 {
						break
					}
					j += start
					k := strings.IndexByte(tail[j:], '>')
					if k < 0 {
						break
					}
					if tagName(tail[j+1:j+k]) == name {
						closingEnd = j + k + 1
						break
					}
					start = j + k + 1
				}
				if closingEnd < 0 {
					break
				}
				i += end + 1 + closingEnd
				continue
			}
			out.WriteString(s[i : i+end+1])
			i += end + 1
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// ReduceHTML is a deliberately lossy scanner for mail's readable text.
func ReduceHTML(s string) string {
	s = removeIgnoredHTML(s)
	blocks := map[string]bool{}
	for _, name := range strings.Fields("address article aside blockquote br dd div dl dt footer h1 h2 h3 h4 h5 h6 header hr li nav ol p pre section table td th tr ul") {
		blocks[name] = true
	}
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '<' && i+1 < len(s) && (asciiLetter(s[i+1]) || s[i+1] == '/' || s[i+1] == '!') {
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				end = len(s) - i - 1
			}
			name := tagName(s[i+1 : i+end])
			if blocks[name] {
				out.WriteByte('\n')
			}
			i += end + 1
			continue
		}
		if space(s[i]) {
			out.WriteByte(' ')
			for i < len(s) && space(s[i]) {
				i++
			}
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	lines := strings.Split(html.UnescapeString(out.String()), "\n")
	for i := range lines {
		lines[i] = strings.Trim(lines[i], " ")
	}
	text := strings.Join(lines, "\n")
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return strings.Trim(text, " \n")
}
