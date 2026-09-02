// Package toolkit provides composable tools for agent operations.
package toolkit

// GlobOption configures a Glob operation.
type GlobOption interface {
	applyGlob(*globConfig)
}

// GrepOption configures a Grep operation.
type GrepOption interface {
	applyGrep(*grepConfig)
}

type globConfig struct {
	skipPatterns []string
}

type grepConfig struct {
	skipPatterns []string
}

// SkipOption adds ignore patterns to filesystem searches.
type SkipOption struct {
	patterns []string
}

// WithSkip returns an option that adds patterns to filesystem searches.
func WithSkip(patterns ...string) SkipOption {
	return SkipOption{patterns: append([]string(nil), patterns...)}
}

func (option SkipOption) applyGlob(config *globConfig) {
	config.skipPatterns = append(config.skipPatterns, option.patterns...)
}

func (option SkipOption) applyGrep(config *grepConfig) {
	config.skipPatterns = append(config.skipPatterns, option.patterns...)
}
