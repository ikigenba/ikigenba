// Package cloud defines the injected object and secret access boundary.
package cloud

import (
	"context"
	"errors"
	"io"
	"time"
)

// Env opens a client using the deployment's configured region.
type Env struct {
	Open func(ctx context.Context, region string) (Client, error)
}

// Client accesses objects and named secret parameters. Implementations must
// return all matching objects from ListObjects, including across pagination.
// PutObject creates an absent URI atomically and accepts the complete body;
// it must preserve existing objects and return ErrAlreadyExists on collision.
// Missing objects and secret parameters must match ErrNotFound via errors.Is.
type Client interface {
	GetObject(ctx context.Context, uri string) (io.ReadCloser, error)
	PutObject(ctx context.Context, uri string, body io.Reader) error
	ListObjects(ctx context.Context, prefix string) ([]Object, error)
	ReadSecrets(ctx context.Context, parameter string) (map[string]string, error)
}

// Object describes a stored object.
type Object struct {
	URI      string
	Size     int64
	Modified time.Time
}

// ErrNotFound identifies a missing object or secret parameter.
var ErrNotFound = errors.New("cloud resource not found")

// ErrAlreadyExists identifies a create-only object upload collision.
var ErrAlreadyExists = errors.New("cloud object already exists")
