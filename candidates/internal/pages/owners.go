package pages

import (
	"encoding/json"
	"slices"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func ownerNote(r store.OwnerRecord) OwnerNoteView {
	at, text := stamp(r.At, MinuteLayout)
	return OwnerNoteView{r.Note, at, text}
}
func ownerViews(id string, rs []store.OwnerRecord) ([]OwnerRecordRow, map[string]OwnerNoteView, *OwnerNoteView) {
	rows := []OwnerRecordRow{}
	set := map[string]OwnerNoteView{}
	var founded *OwnerNoteView
	for _, r := range rs {
		n := ownerNote(r)
		row := OwnerRecordRow{Founded: r.Founded && r.Candidate == id, Note: n.Note, At: n.At, AtText: n.AtText, Facts: []OwnerFactRow{}}
		if row.Founded {
			founded = &n
		}
		data, _ := json.Marshal(r.Set)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(data, &fields)
		for _, w := range sources.SettableFacts() {
			if slices.Contains(r.Cleared, w) {
				row.Facts = append(row.Facts, OwnerFactRow{Fact: w, Cleared: true})
			} else if v, ok := fields[w]; ok {
				row.Facts = append(row.Facts, OwnerFactRow{Fact: w, Value: string(v)})
			}
		}
		rows = append(rows, row)
	}
	for w, r := range store.OwnerSet(rs) {
		set[w] = ownerNote(r)
	}
	return rows, set, founded
}
func ownerURL(rs []store.OwnerRecord) string {
	if r, ok := store.OwnerSet(rs)[sources.FactWebsite]; ok && r.Set.Website != nil {
		return *r.Set.Website
	}
	return ""
}
