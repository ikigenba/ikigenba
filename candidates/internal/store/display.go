package store

import "github.com/ikigenba/ikigenba/candidates/internal/sources"

type displayTracker struct{ records *records }

func newDisplayTracker(r *records, _ string) *displayTracker { return &displayTracker{records: r} }
func (d *displayTracker) apply(l Link)                       { d.records.links[sources.SourceKey(l.Scraper, l.SourceID)] = l }
