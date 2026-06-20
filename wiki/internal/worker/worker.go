// Package worker will run asynchronous wiki jobs.
package worker

import "context"

// Task is a unit of background work.
type Task func(context.Context) error
