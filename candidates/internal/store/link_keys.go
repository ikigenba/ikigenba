package store

import (
	"context"
	"database/sql"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func readingNameKeys(r sources.Reading) []string {
	keys := []string{}
	for _, key := range r.NameKeys {
		if key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 && r.NameKey != "" {
		keys = append(keys, r.NameKey)
	}
	return keys
}

func matchCandidate(ctx context.Context, tx *sql.Tx, reading sources.Reading) (string, string, float64, error) {
	if reading.EINKey != "" {
		id, err := matchKeyCandidates(ctx, tx, LinkEIN, reading.EINKey, "", "")
		if err != nil || id != "" {
			return id, LinkEIN, .9, err
		}
	}
	if reading.State != "" && validZip(reading.Zip) {
		for _, key := range readingNameKeys(reading) {
			id, err := matchKeyCandidates(ctx, tx, LinkMatchKey, key, reading.State, reading.Zip)
			if err != nil || id != "" {
				return id, LinkMatchKey, .8, err
			}
		}
	}
	return "", LinkSourceKey, 1, nil
}
