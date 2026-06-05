package ingest

import "strings"

// builder is a tiny string assembler for the per-run user message.
type builder struct{ sb strings.Builder }

func (b *builder) line(s string) {
	b.sb.WriteString(s)
	b.sb.WriteByte('\n')
}

func (b *builder) kv(k, v string) {
	b.sb.WriteString("- ")
	b.sb.WriteString(k)
	b.sb.WriteString(": ")
	b.sb.WriteString(v)
	b.sb.WriteByte('\n')
}

func (b *builder) String() string { return b.sb.String() }

// joinTags renders a tag slice as a comma-separated list for the user message.
func joinTags(tags []string) string { return strings.Join(tags, ", ") }
