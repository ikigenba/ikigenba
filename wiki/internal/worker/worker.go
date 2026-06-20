// Package worker will run asynchronous wiki jobs.
package worker

import "context"

// Task is a unit of background work.
type Task func(context.Context) error

// Run is the Phase 01 worker lifecycle hook. Later phases attach queue work here.
func Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
