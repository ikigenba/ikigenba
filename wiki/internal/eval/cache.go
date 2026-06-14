package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CacheKey is the load-bearing memoization key (eval design question 4):
// (test-set-bundle-hash, case_id, prompt-hash, model, effort), with the test-set
// and prompt resolved to content hashes so a bundle edit correctly misses and a
// prompt-only swap re-runs only what changed. Caching is load-bearing, not an
// optimization — the sweep is a cartesian product of paid calls; a re-score with a
// changed scorer must cost zero provider calls.
type CacheKey struct {
	DatasetHash string
	CaseID      string
	PromptHash  string
	Model       string
	Effort      string
}

// Hash returns the cache key's hex digest, used as the on-disk filename.
func (k CacheKey) Hash() string {
	h := sha256.New()
	for _, s := range []string{k.DatasetHash, k.CaseID, k.PromptHash, k.Model, k.Effort} {
		// length-prefix each field so distinct field boundaries can't collide.
		fmt.Fprintf(h, "%d:%s", len(s), s)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CachedOutput is the cache VALUE: the raw call-site output plus its captured
// cost + latency — NOT the score (eval design q4). Scoring runs over the cached
// raw output, so changing a scorer (P14) re-scores for free.
type CachedOutput struct {
	// Raw is the raw call-site output, site-shaped JSON (a json.RawMessage so the
	// site adapter unmarshals it into its own typed result).
	Raw json.RawMessage `json:"raw"`
	// CostUSD is the total provider cost for the call (from P0c's per-call
	// cost_usd accounting), or 0 when not captured.
	CostUSD float64 `json:"cost_usd"`
	// LatencyMS is the call's wall-clock latency in milliseconds.
	LatencyMS int64 `json:"latency_ms"`
}

// Cache is a content-addressed directory, one file per CacheKey. It is a pure
// memoization layer — deleting it only costs money, never correctness.
type Cache struct {
	dir string
}

// ErrCacheMiss is returned by Get when no entry exists for a key.
var ErrCacheMiss = errors.New("eval: cache miss")

// NewCache opens (and creates) a cache rooted at dir. The default location is a
// repo-local .wiki-eval-cache/ (gitignored) or ~/.cache/wiki-eval/.
func NewCache(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("eval: create cache dir %q: %w", dir, err)
	}
	return &Cache{dir: dir}, nil
}

// Get returns the cached output for a key, or ErrCacheMiss.
func (c *Cache) Get(k CacheKey) (CachedOutput, error) {
	var out CachedOutput
	raw, err := os.ReadFile(c.path(k))
	if errors.Is(err, os.ErrNotExist) {
		return out, ErrCacheMiss
	}
	if err != nil {
		return out, fmt.Errorf("eval: read cache: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("eval: decode cache entry: %w", err)
	}
	return out, nil
}

// Put stores the output for a key (atomic write via a temp file + rename).
func (c *Cache) Put(k CacheKey, out CachedOutput) error {
	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("eval: encode cache entry: %w", err)
	}
	dst := c.path(k)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("eval: write cache: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("eval: commit cache: %w", err)
	}
	return nil
}

func (c *Cache) path(k CacheKey) string {
	return filepath.Join(c.dir, k.Hash()+".json")
}

// HashBytes returns the hex SHA-256 of b — the content hash used for dataset and
// prompt artifacts (eval design q3: run-to-bundle pinning is by content hash).
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
