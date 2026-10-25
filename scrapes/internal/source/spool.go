package source

import (
	"context"
	"os"
)

// Spool makes a source file that its caller closes and removes.
type Spool func() (*os.File, error)

type spoolKey struct{}

// WithSpool supplies the spool used by a source walk.
func WithSpool(ctx context.Context, s Spool) context.Context {
	return context.WithValue(ctx, spoolKey{}, s)
}

// SpoolFrom returns the nearest spool carried by the context.
func SpoolFrom(ctx context.Context) Spool {
	s, _ := ctx.Value(spoolKey{}).(Spool)
	return s
}
