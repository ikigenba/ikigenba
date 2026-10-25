package store

import (
	"context"
	"database/sql"
	"slices"
	"strings"

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
	cols := make([]string, 0, len(linkColumns))
	for _, col := range linkColumns {
		cols = append(cols, "l."+`"`+col.name+`"`)
	}
	selectLinks := "SELECT " + strings.Join(cols, ",") + " FROM links l WHERE (SELECT status FROM candidates WHERE id=l.candidate)=? AND "
	order := " ORDER BY (SELECT created FROM candidates WHERE id=l.candidate),l.candidate"
	if reading.EINKey != "" {
		links, err := readQueryRecords[Link](ctx, tx, linkColumns, selectLinks+"l.ein_key=?"+order, StatusActive, reading.EINKey)
		if err != nil {
			return "", "", 0, err
		}
		for _, l := range links {
			if l.Reading.EINKey == reading.EINKey {
				return l.Candidate, LinkEIN, 0.9, nil
			}
		}
	}
	if reading.State != "" && validZip(reading.Zip) {
		for _, key := range readingNameKeys(reading) {
			query := selectLinks + `l.state=? AND l.zip=? AND l.rowid IN (SELECT rowid FROM links WHERE name_key=? UNION SELECT n.rowid FROM link_name_keys k JOIN links n ON n.scraper=k.scraper AND n.source_id=k.source_id WHERE k.name_key=?)` + order
			links, err := readQueryRecords[Link](ctx, tx, linkColumns, query, StatusActive, reading.State, reading.Zip, key, key)
			if err != nil {
				return "", "", 0, err
			}
			for _, l := range links {
				if l.Reading.State == reading.State && l.Reading.Zip == reading.Zip && (l.Reading.NameKey == key || slices.Contains(l.Reading.NameKeys, key)) {
					return l.Candidate, LinkMatchKey, 0.8, nil
				}
			}
		}
	}
	return "", LinkSourceKey, 1, nil
}
