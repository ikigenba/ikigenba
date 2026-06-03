package crm

import (
	"errors"
	"testing"
)

func TestTask_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	var contactID, taskID string

	// A contact to be the task's subject.
	withTx(t, s, func(tx *txAlias) {
		c, err := s.contacts.Save(tx, "", ContactInput{DisplayName: sp("Alice")}, s.Now())
		if err != nil {
			t.Fatalf("create contact: %v", err)
		}
		contactID = c.ID
	})

	// Create a task with a contact subject; status defaults to open.
	withTx(t, s, func(tx *txAlias) {
		sum, err := s.tasks.Save(tx, "", TaskInput{
			Title:     sp("Follow up with Alice"),
			ContactID: sp(contactID),
			DueAt:     sp("2026-07-01T09:00:00.000000000Z"),
		}, s.Now())
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		if sum.ID == "" || sum.Type != "task" || sum.Label != "Follow up with Alice" {
			t.Fatalf("bad summary: %+v", sum)
		}
		if sum.Fields["status"] != "open" {
			t.Fatalf("default status not open: %+v", sum.Fields)
		}
		if sum.Fields["due_at"] != "2026-07-01T09:00:00.000000000Z" {
			t.Fatalf("due_at missing: %+v", sum.Fields)
		}
		if !sum.isCreate {
			t.Errorf("create should set isCreate")
		}
		taskID = sum.ID
	})

	// Get card: status open, subject is the contact.
	withTx(t, s, func(tx *txAlias) {
		card, err := s.tasks.Get(tx, taskID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if card["title"] != "Follow up with Alice" || card["status"] != "open" {
			t.Fatalf("bad card: %+v", card)
		}
		if card["contact_id"] != contactID {
			t.Fatalf("bad contact_id: %+v", card)
		}
		subj, ok := card["subject"].(map[string]any)
		if !ok {
			t.Fatalf("missing subject: %+v", card["subject"])
		}
		if subj["type"] != "contact" || subj["id"] != contactID || subj["label"] != "Alice" {
			t.Fatalf("bad subject: %+v", subj)
		}
		if _, present := card["done_at"]; present {
			t.Errorf("done_at should be absent on open task: %+v", card)
		}
	})

	// Complete via Save with status:"done" → done_at stamped.
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.tasks.Save(tx, taskID, TaskInput{Status: sp("done")}, s.Now()); err != nil {
			t.Fatalf("complete task: %v", err)
		}
	})
	withTx(t, s, func(tx *txAlias) {
		card, err := s.tasks.Get(tx, taskID)
		if err != nil {
			t.Fatalf("get after complete: %v", err)
		}
		if card["status"] != "done" {
			t.Fatalf("status not done: %+v", card)
		}
		done, ok := card["done_at"].(string)
		if !ok || done == "" {
			t.Fatalf("done_at not stamped: %+v", card["done_at"])
		}
	})

	// Search by status:"open" → no hit (task is done now).
	withTx(t, s, func(tx *txAlias) {
		got, err := s.tasks.Search(tx, SearchParams{Filters: map[string]any{"status": "open"}})
		if err != nil {
			t.Fatalf("search open: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("search status:open want 0 (task done), got %+v", got)
		}
	})

	// Search by status:"done" → the one task.
	withTx(t, s, func(tx *txAlias) {
		got, err := s.tasks.Search(tx, SearchParams{Filters: map[string]any{"status": "done"}})
		if err != nil {
			t.Fatalf("search done: %v", err)
		}
		if len(got) != 1 || got[0].ID != taskID {
			t.Fatalf("search status:done want 1 hit, got %+v", got)
		}
	})

	// Delete → Get is not_found.
	withTx(t, s, func(tx *txAlias) {
		if err := s.tasks.Delete(tx, taskID, s.Now()); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.tasks.Get(tx, taskID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("get after delete: want ErrNotFound, got %v", err)
		}
	})
}

func TestTask_CreateRequiresTitle(t *testing.T) {
	s := newTestStore(t)
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.tasks.Save(tx, "", TaskInput{}, s.Now()); !errors.Is(err, ErrValidation) {
			t.Fatalf("want ErrValidation, got %v", err)
		}
	})
}

func TestTask_UpdateMissingIsNotFound(t *testing.T) {
	s := newTestStore(t)
	withTx(t, s, func(tx *txAlias) {
		if _, err := s.tasks.Save(tx, "NOPE", TaskInput{Title: sp("x")}, s.Now()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
