package wiki

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Ref is a markdown link target for another wiki subject.
type Ref struct {
	Path string
	Name string
}

// LinkedPage is a page plus its read-time link projection.
type LinkedPage struct {
	Page
	Mentions    []Ref
	MentionedBy []Ref
}

// SubjectKeys is a subject plus every normalized name that resolves to it.
type SubjectKeys struct {
	Subject
	Keys []string
}

// FirstMention is an eligible surface occurrence of a canonical subject.
// Start and End are byte offsets into the original markdown body.
type FirstMention struct {
	Start, End int
	Subject    Subject
}

// Mentions returns every subject whose normalized name appears as a whole
// alphanumeric-bounded phrase in body.
func Mentions(body string, others []SubjectKeys) []Subject {
	normalizedBody := Normalize(body)
	var out []Subject
	for _, subjectKeys := range others {
		for _, key := range subjectKeys.Keys {
			if key == "" {
				continue
			}
			if containsWholePhrase(normalizedBody, key) {
				out = append(out, subjectKeys.Subject)
				break
			}
		}
	}
	return out
}

func containsWholePhrase(body, phrase string) bool {
	for offset := 0; offset <= len(body); {
		i := strings.Index(body[offset:], phrase)
		if i < 0 {
			return false
		}
		start := offset + i
		end := start + len(phrase)
		if phraseBoundaryBefore(body, start) && phraseBoundaryAfter(body, end) {
			return true
		}
		if end >= len(body) {
			return false
		}
		_, width := utf8.DecodeRuneInString(body[start:])
		if width == 0 {
			width = 1
		}
		offset = start + width
	}
	return false
}

func phraseBoundaryBefore(s string, index int) bool {
	if index == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:index])
	return !isAlphaNumeric(r)
}

func phraseBoundaryAfter(s string, index int) bool {
	if index == len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[index:])
	return !isAlphaNumeric(r)
}

func isAlphaNumeric(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// LinkFirstMentions links the first eligible surface occurrence of each
// canonical subject. It deliberately works on markdown text rather than a
// rendered document so every read surface can share the same projection.
func LinkFirstMentions(body string, others []SubjectKeys, base, excludeID string) string {
	candidates := firstMentionCandidates(body, others, excludeID)
	if len(candidates) == 0 {
		return body
	}

	placed := make([]FirstMention, 0, len(candidates))
	linked := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if linked[candidate.Subject.ID] || overlapsAny(candidate, placed) {
			continue
		}
		placed = append(placed, candidate)
		linked[candidate.Subject.ID] = true
	}
	if len(placed) == 0 {
		return body
	}
	sort.Slice(placed, func(i, j int) bool { return placed[i].Start < placed[j].Start })

	var out strings.Builder
	out.Grow(len(body) + len(placed)*len(base))
	offset := 0
	for _, mention := range placed {
		out.WriteString(body[offset:mention.Start])
		out.WriteByte('[')
		out.WriteString(body[mention.Start:mention.End])
		out.WriteString("](")
		out.WriteString(base)
		out.WriteString(Path(mention.Subject))
		out.WriteByte(')')
		offset = mention.End
	}
	out.WriteString(body[offset:])
	return out.String()
}

func firstMentionCandidates(body string, others []SubjectKeys, excludeID string) []FirstMention {
	normalized, offsets := normalizedOffsets(body)
	skipped := markdownSkipRegions(body)
	var candidates []FirstMention
	seen := make(map[string]struct{})
	for _, subjectKeys := range others {
		if subjectKeys.ID == "" || subjectKeys.ID == excludeID {
			continue
		}
		for _, key := range subjectKeys.Keys {
			key = Normalize(key)
			if key == "" {
				continue
			}
			for offset := 0; offset <= len(normalized)-len(key); {
				i := strings.Index(normalized[offset:], key)
				if i < 0 {
					break
				}
				start := offset + i
				end := start + len(key)
				if phraseBoundaryBefore(normalized, start) && phraseBoundaryAfter(normalized, end) {
					mention := FirstMention{Start: offsets[start].start, End: offsets[end-1].end, Subject: subjectKeys.Subject}
					candidateKey := fmt.Sprintf("%d:%d:%s", mention.Start, mention.End, mention.Subject.ID)
					if !overlapsAny(mention, skipped) {
						if _, ok := seen[candidateKey]; !ok {
							candidates = append(candidates, mention)
							seen[candidateKey] = struct{}{}
						}
					}
				}
				offset = start + 1
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Start != candidates[j].Start {
			return candidates[i].Start < candidates[j].Start
		}
		if candidates[i].End != candidates[j].End {
			return candidates[i].End > candidates[j].End
		}
		return candidates[i].Subject.ID < candidates[j].Subject.ID
	})
	return candidates
}

type byteOffset struct{ start, end int }

// normalizedOffsets is Normalize with a source span for every normalized byte.
func normalizedOffsets(body string) (string, []byteOffset) {
	var normalized strings.Builder
	var offsets []byteOffset
	hyphen := true
	for start, r := range body {
		end := start + utf8.RuneLen(r)
		part := stripDiacritics(strings.ToLower(norm.NFKC.String(string(r))))
		for _, normalizedRune := range part {
			if normalizedRune >= 'a' && normalizedRune <= 'z' || normalizedRune >= '0' && normalizedRune <= '9' {
				text := string(normalizedRune)
				normalized.WriteString(text)
				for range []byte(text) {
					offsets = append(offsets, byteOffset{start: start, end: end})
				}
				hyphen = false
				continue
			}
			if !hyphen {
				normalized.WriteByte('-')
				offsets = append(offsets, byteOffset{start: start, end: end})
				hyphen = true
			}
		}
	}
	if hyphen && normalized.Len() > 0 {
		text := normalized.String()
		return strings.TrimSuffix(text, "-"), offsets[:len(offsets)-1]
	}
	return normalized.String(), offsets
}

func overlapsAny(mention FirstMention, regions []FirstMention) bool {
	for _, region := range regions {
		if mention.Start < region.End && region.Start < mention.End {
			return true
		}
	}
	return false
}

func markdownSkipRegions(body string) []FirstMention {
	var regions []FirstMention
	for lineStart := 0; lineStart < len(body); {
		lineEnd := strings.IndexByte(body[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(body)
		} else {
			lineEnd += lineStart + 1
		}
		line := body[lineStart:lineEnd]
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) >= 3 && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			fence := trimmed[:3]
			end := lineEnd
			for end < len(body) {
				nextEnd := strings.IndexByte(body[end:], '\n')
				if nextEnd < 0 {
					nextEnd = len(body)
				} else {
					nextEnd += end + 1
				}
				if strings.HasPrefix(strings.TrimLeft(body[end:nextEnd], " \t"), fence) {
					end = nextEnd
					break
				}
				end = nextEnd
			}
			regions = append(regions, FirstMention{Start: lineStart, End: end})
			lineStart = end
			continue
		}
		lineStart = lineEnd
	}
	for i := 0; i < len(body); i++ {
		if body[i] == '`' {
			end := strings.IndexByte(body[i+1:], '`')
			if end >= 0 {
				end += i + 2
				regions = append(regions, FirstMention{Start: i, End: end})
				i = end - 1
			}
			continue
		}
		if body[i] == '[' {
			if closeText := strings.IndexByte(body[i+1:], ']'); closeText >= 0 {
				closeText += i + 1
				if closeText+1 < len(body) && body[closeText+1] == '(' {
					if closeURL := strings.IndexByte(body[closeText+2:], ')'); closeURL >= 0 {
						end := closeText + 3 + closeURL
						regions = append(regions, FirstMention{Start: i, End: end})
						i = end - 1
					}
				}
			}
		}
	}
	for i := 0; i < len(body); {
		start := urlStart(body, i)
		if start < 0 {
			i++
			continue
		}
		end := start
		for end < len(body) && !unicode.IsSpace(rune(body[end])) && body[end] != '<' && body[end] != '>' {
			end++
		}
		regions = append(regions, FirstMention{Start: start, End: end})
		i = end
	}
	return regions
}

func urlStart(body string, i int) int {
	if i > 0 && isAlphaNumeric(rune(body[i-1])) {
		return -1
	}
	for _, scheme := range []string{"http://", "https://", "mailto:"} {
		if strings.HasPrefix(body[i:], scheme) {
			return i
		}
	}
	return -1
}

// PageWithLinks returns a stored page plus read-time outbound and inbound links.
func (s *Service) PageWithLinks(ctx context.Context, subjectID string) (LinkedPage, error) {
	if s == nil {
		return LinkedPage{}, fmt.Errorf("wiki: nil service")
	}
	subjectID = strings.TrimSpace(subjectID)
	subject, err := s.subjects.Get(ctx, subjectID)
	if err != nil {
		return LinkedPage{}, err
	}
	page, err := s.pages.GetBySubject(ctx, subjectID)
	if err != nil {
		return LinkedPage{}, err
	}
	subjects, err := listAllSubjects(ctx, s.subjects, "", "")
	if err != nil {
		return LinkedPage{}, err
	}
	aliases, err := s.aliases.ListAll(ctx)
	if err != nil {
		return LinkedPage{}, err
	}

	subjectKeys := subjectKeysFor(subjects, aliases)
	thisKeys := subjectKeys[subject.ID]
	var others []SubjectKeys
	for _, candidateKeys := range subjectKeys {
		if candidateKeys.ID != subject.ID {
			others = append(others, candidateKeys)
		}
	}

	linked := LinkedPage{
		Page:     page,
		Mentions: refsFor(Mentions(page.Body, others)),
	}
	for _, other := range others {
		otherPage, err := s.pages.GetBySubject(ctx, other.ID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return LinkedPage{}, err
		}
		if len(Mentions(otherPage.Body, []SubjectKeys{thisKeys})) > 0 {
			linked.MentionedBy = append(linked.MentionedBy, refFor(other.Subject))
		}
	}
	linked.MentionedBy = canonicalRefs(linked.MentionedBy)
	return linked, nil
}

// MentionsIn returns public read-surface links for subjects named in text.
func (s *Service) MentionsIn(ctx context.Context, text string) ([]Ref, error) {
	if s == nil {
		return nil, fmt.Errorf("wiki: nil service")
	}
	subjects, err := listAllSubjects(ctx, s.subjects, "", "")
	if err != nil {
		return nil, err
	}
	aliases, err := s.aliases.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	subjectKeysByID := subjectKeysFor(subjects, aliases)
	subjectKeys := make([]SubjectKeys, 0, len(subjectKeysByID))
	for _, keys := range subjectKeysByID {
		subjectKeys = append(subjectKeys, keys)
	}
	return refsFor(Mentions(text, subjectKeys)), nil
}

// LinkifyMentions loads the current canonical subjects and aliases before
// applying the shared, read-time markdown projection.
func (s *Service) LinkifyMentions(ctx context.Context, text, base, excludeID string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("wiki: nil service")
	}
	subjects, err := listAllSubjects(ctx, s.subjects, "", "")
	if err != nil {
		return "", err
	}
	aliases, err := s.aliases.ListAll(ctx)
	if err != nil {
		return "", err
	}

	byID := subjectKeysFor(subjects, aliases)
	others := make([]SubjectKeys, 0, len(byID))
	for _, keys := range byID {
		others = append(others, keys)
	}
	return LinkFirstMentions(text, others, base, excludeID), nil
}

func subjectKeysFor(subjects []Subject, aliases []Alias) map[string]SubjectKeys {
	byID := make(map[string]SubjectKeys, len(subjects))
	for _, subject := range subjects {
		keys := appendNormalizedKey(nil, subject.NormName)
		byID[subject.ID] = SubjectKeys{Subject: subject, Keys: keys}
	}
	for _, alias := range aliases {
		subjectKeys, ok := byID[alias.SubjectID]
		if !ok {
			continue
		}
		subjectKeys.Keys = appendNormalizedKey(subjectKeys.Keys, alias.NormName)
		byID[alias.SubjectID] = subjectKeys
	}
	return byID
}

func appendNormalizedKey(keys []string, key string) []string {
	key = Normalize(key)
	if key == "" {
		return keys
	}
	for _, existing := range keys {
		if existing == key {
			return keys
		}
	}
	return append(keys, key)
}

func refsFor(subjects []Subject) []Ref {
	refs := make([]Ref, 0, len(subjects))
	for _, subject := range subjects {
		refs = append(refs, refFor(subject))
	}
	return canonicalRefs(refs)
}

func refFor(subject Subject) Ref {
	return Ref{
		Path: Path(subject),
		Name: subject.Name,
	}
}

// RenderFooter appends a deterministic markdown link footer to body.
func RenderFooter(body string, mentions, mentionedBy []Ref) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n---\n\n## Links\n\n")
	writeRefSection(&b, "Mentions", mentions)
	b.WriteString("\n")
	writeRefSection(&b, "Mentioned by", mentionedBy)
	return b.String()
}

func writeRefSection(b *strings.Builder, title string, refs []Ref) {
	b.WriteString("### ")
	b.WriteString(title)
	b.WriteString("\n")
	refs = canonicalRefs(refs)
	if len(refs) == 0 {
		b.WriteString("- None\n")
		return
	}
	for _, ref := range refs {
		b.WriteString("- [")
		b.WriteString(escapeMarkdownLinkText(ref.Name))
		b.WriteString("](")
		b.WriteString(ref.Path)
		b.WriteString(")\n")
	}
}

func canonicalRefs(refs []Ref) []Ref {
	if len(refs) == 0 {
		return nil
	}
	out := append([]Ref(nil), refs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	n := 0
	for _, ref := range out {
		if n > 0 && out[n-1].Path == ref.Path {
			continue
		}
		out[n] = ref
		n++
	}
	return out[:n]
}

func escapeMarkdownLinkText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `]`, `\]`)
	return s
}
