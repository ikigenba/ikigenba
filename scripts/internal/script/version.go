package script

import (
	"context"
	"time"
)

// Owner is the identity a repository domain verb is scoped to.
type Owner struct {
	ID    string
	Email string
}

// VersionPlane is the repository/version surface used by scripts. The domain
// owns this seam; the loopback HTTP implementation lives in internal/repos.
type VersionPlane interface {
	Create(ctx context.Context, nameKey string, owner Owner, clientID string) error
	Commit(ctx context.Context, key string, files map[string]string, message, clientID string) (sha string, err error)
	Head(ctx context.Context, key, branch string) (sha string, err error)
	ReadFile(ctx context.Context, key, ref, path string) ([]byte, error)
	Rename(ctx context.Context, oldNameKey, newNameKey string, owner Owner, clientID string) error
	Delete(ctx context.Context, nameKey string, owner Owner, clientID string) error
	RunToken(ctx context.Context, nameKey string, ttl time.Duration) (token, cloneURL string, err error)
}
