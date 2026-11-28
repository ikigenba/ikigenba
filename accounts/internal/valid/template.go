package valid

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Team template validation errors.
var (
	ErrTemplateURL = errors.New("invalid team template URL")
	ErrTemplateRef = errors.New("invalid team template ref")
)

// Team template input limits in bytes.
const (
	MaxTemplateURL = 512
	MaxTemplateRef = 255
)

// TemplateURL validates the HTTPS URL of a team template repository.
func TemplateURL(s string) error {
	if len(s) > MaxTemplateURL || !strings.HasPrefix(s, "https://") {
		return ErrTemplateURL
	}
	for i := range len(s) {
		if s[i] < 0x21 || s[i] > 0x7e {
			return ErrTemplateURL
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" ||
		u.ForceQuery || u.User != nil || u.Hostname() == "" {
		return ErrTemplateURL
	}
	return nil
}

// TemplateRef validates a team template branch or tag name.
func TemplateRef(s string) error {
	if len(s) == 0 || len(s) > MaxTemplateRef || !utf8.ValidString(s) || s == "@" || s == "HEAD" {
		return ErrTemplateRef
	}
	if s[0] == '-' || s[0] == '/' || s[len(s)-1] == '/' || s[len(s)-1] == '.' ||
		strings.Contains(s, "//") || strings.Contains(s, "..") || strings.Contains(s, "@{") ||
		strings.ContainsAny(s, "~^:?*[\\") {
		return ErrTemplateRef
	}
	for i := range len(s) {
		if s[i] < 0x21 || s[i] == 0x7f {
			return ErrTemplateRef
		}
	}
	for part := range strings.SplitSeq(s, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return ErrTemplateRef
		}
	}
	return nil
}
