package script

import "context"

// VersionPlane is the repository/version surface used by scripts. The domain
// owns this seam; the loopback HTTP implementation lives in internal/repos.
type VersionPlane interface {
	Create(ctx context.Context, key, clientID string) error
	Commit(ctx context.Context, key string, files map[string]string, message, clientID string) (sha string, err error)
	Head(ctx context.Context, key, branch string) (sha string, err error)
	ReadFile(ctx context.Context, key, ref, path string) ([]byte, error)
	Rename(ctx context.Context, oldKey, newKey, clientID string) error
	Delete(ctx context.Context, key, clientID string) error
	RunToken(ctx context.Context, key string) (token, cloneURL string, err error)
}
