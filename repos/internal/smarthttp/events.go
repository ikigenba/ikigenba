package smarthttp

import (
	"context"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func (cfg Config) refs(ctx context.Context, id string) (map[string]string, error) {
	b, err := cfg.Git.Output(ctx, "", "--git-dir="+cfg.Store.Dir(id), "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return nil, err
	}
	refs := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		name, value, ok := strings.Cut(line, " ")
		if ok {
			refs[name] = value
		}
	}
	return refs, nil
}

func (cfg Config) pushed(ctx context.Context, id string, before map[string]string) {
	after, err := cfg.refs(context.WithoutCancel(ctx), id)
	if err != nil {
		return
	}
	const absent = "0000000000000000000000000000000000000000"
	for ref, old := range before {
		newValue := after[ref]
		if newValue != old {
			if newValue == "" {
				newValue = absent
			}
			cfg.Telemetry.Emit(ctx, "repo.pushed", telemetry.Attrs{"repo": id, "ref": ref, "old": old, "new": newValue})
		}
		delete(after, ref)
	}
	for ref, value := range after {
		cfg.Telemetry.Emit(ctx, "repo.pushed", telemetry.Attrs{"repo": id, "ref": ref, "old": absent, "new": value})
	}
}

// wireCounter retains only one packet prefix and one rejection phrase suffix.
type wireCounter struct {
	header          [4]byte
	have, remaining int
	first           bool
	pack            int64
	sideband        bool
	tail            string
	rejected        bool
	invalid         bool
}

func (c *wireCounter) observe(p []byte) {
	const phrase = "pack exceeds maximum allowed size"
	if !c.rejected {
		joined := c.tail + string(p)
		c.rejected = strings.Contains(joined, phrase)
		if len(joined) >= len(phrase) {
			joined = joined[len(joined)-len(phrase)+1:]
		}
		c.tail = joined
	}
	for _, b := range p {
		if c.invalid {
			break
		}
		if c.remaining > 0 {
			if c.first {
				c.sideband = b == 1
				c.first = false
			} else if c.sideband {
				c.pack++
			}
			c.remaining--
			continue
		}
		c.header[c.have] = b
		c.have++
		if c.have != 4 {
			continue
		}
		n := 0
		for _, h := range c.header {
			n *= 16
			switch {
			case h >= '0' && h <= '9':
				n += int(h - '0')
			case h >= 'a' && h <= 'f':
				n += int(h - 'a' + 10)
			case h >= 'A' && h <= 'F':
				n += int(h - 'A' + 10)
			default:
				c.invalid = true
			}
		}
		c.have = 0
		if n >= 4 {
			c.remaining = n - 4
			c.first = true
			c.sideband = false
		}
	}
}
