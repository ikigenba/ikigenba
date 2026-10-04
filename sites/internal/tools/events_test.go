package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func eventCall(t *testing.T, h *harness, id, tool, args string) (mcp.Result, []telemetry.Event) {
	t.Helper()
	r, err := h.client.CallTool(context.Background(), identity.Caller{UserID: "alice", RequestID: id}, tool, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var events []telemetry.Event
	for _, e := range h.capture.Events() {
		if e.RequestID == id {
			events = append(events, e)
		}
	}
	return r, events
}
func checkSiteEvents(t *testing.T, events []telemetry.Event, names []string, attrs []telemetry.Attrs) {
	t.Helper()
	var got []telemetry.Event
	called := -1
	for i, e := range events {
		if e.Name == "tool.called" {
			called = i
		}
		if strings.HasPrefix(e.Name, "site.") {
			if called != -1 {
				t.Fatal("site event after tool.called")
			}
			got = append(got, e)
		}
	}
	if called < 0 {
		t.Fatal("no tool.called")
	}
	if len(got) != len(names) {
		t.Fatalf("site events %+v want %v", got, names)
	}
	for _, e := range got {
		if len(attrs) == 0 {
			t.Fatal("missing expected attributes")
			return
		}
		expected := attrs[0]
		attrs = attrs[1:]
		if len(names) == 0 {
			t.Fatal("missing expected event")
			return
		}
		name := names[0]
		names = names[1:]
		if e.Name != name || !reflect.DeepEqual(e.Attrs, expected) {
			t.Fatalf("event %+v want %s %+v", e, name, expected)
		}
	}
	if len(attrs) != 0 {
		t.Fatal("unused expected attributes")
	}

}

// R-FGAC-85RJ
func TestCreatedEventMatchesObject(t *testing.T) {
	h := newHarness(t)
	repo := "rep_0123456789abcdef"
	h.repo(t, repo, "alice")
	r, events := eventCall(t, h, "event-create", "create", fmt.Sprintf(`{"name":"blog","repo":%q,"visibility":"private","listed":false}`, repo))
	o := mutationObject(t, r)
	checkSiteEvents(t, events, []string{"site.created"}, []telemetry.Attrs{{"site": o.ID, "repo": o.Repo, "visibility": o.Visibility, "listed": o.Listed}})
}

// R-FIQ4-ZP8X
func TestUpdatedEventsOnlyChangedFieldsInOrder(t *testing.T) {
	h := newHarness(t)
	h.add(t, "alice", "blog", false)
	for i, args := range []string{`{"name":"blog","visibility":"private","listed":true,"ref":"future"}`, `{"name":"blog","visibility":"private","listed":true,"ref":"future"}`, `{"name":"blog","visibility":"private","listed":false,"ref":"main"}`} {
		old := mutationObject(t, h.call(t, "alice", "show", `{"name":"blog"}`))
		r, events := eventCall(t, h, fmt.Sprintf("event-update-%d", i), "update", args)
		o := mutationObject(t, r)
		names := []string{}
		attrs := []telemetry.Attrs{}
		for _, change := range []struct{ field, before, after string }{{"visibility", old.Visibility, o.Visibility}, {"listed", fmt.Sprint(old.Listed), fmt.Sprint(o.Listed)}, {"ref", old.Ref, o.Ref}} {
			if change.before != change.after {
				names = append(names, "site.updated")
				attrs = append(attrs, telemetry.Attrs{"site": o.ID, "field": change.field, "value": change.after})
			}
		}
		checkSiteEvents(t, events, names, attrs)
	}
}

// R-FJY1-DGZM
func TestDeletedEventsWithAndWithoutApex(t *testing.T) {
	for _, apex := range []bool{false, true} {
		t.Run(fmt.Sprint(apex), func(t *testing.T) {
			h := newHarness(t)
			s := h.add(t, "alice", "blog", true)
			if apex {
				resultObject(t, h.call(t, "alice", "apex", `{"name":"blog"}`))
			}
			before := h.call(t, "alice", "apex", `{}`)
			var a struct {
				Apex *struct{ ID string } `json:"apex"`
			}
			if err := json.Unmarshal(resultObject(t, before), &a); err != nil {
				t.Fatal(err)
			}
			r, events := eventCall(t, h, "event-delete", "delete", `{"name":"blog"}`)
			var o struct {
				Deleted bool
				ID      string
			}
			if err := json.Unmarshal(resultObject(t, r), &o); err != nil || !o.Deleted || o.ID != s.ID {
				t.Fatal("delete object")
			}
			names := []string{"site.deleted"}
			attrs := []telemetry.Attrs{{"site": o.ID}}
			if a.Apex != nil && a.Apex.ID == o.ID {
				names = append(names, "site.apex")
				attrs = append(attrs, telemetry.Attrs{"site": ""})
			}
			checkSiteEvents(t, events, names, attrs)
		})
	}
}

// R-FMDU-50H0 R-FNLQ-IS7P
func TestApexMutationEventsEvenWhenUnchanged(t *testing.T) {
	h := newHarness(t)
	s := h.add(t, "alice", "blog", true)
	for i, args := range []string{`{"name":"blog"}`, `{"name":"blog"}`, `{"clear":true}`, `{"clear":true}`} {
		r, events := eventCall(t, h, fmt.Sprintf("event-apex-%d", i), "apex", args)
		var o struct {
			Apex *struct{ ID string } `json:"apex"`
		}
		if err := json.Unmarshal(resultObject(t, r), &o); err != nil {
			t.Fatal(err)
		}
		id := ""
		if i < 2 {
			if o.Apex == nil || o.Apex.ID != s.ID {
				t.Fatal("wrong apex")
			}
			id = o.Apex.ID
		} else if o.Apex != nil {
			t.Fatal("not cleared")
		}
		checkSiteEvents(t, events, []string{"site.apex"}, []telemetry.Attrs{{"site": id}})
	}
}

// R-FOTM-WJYE
func TestReadsAndRefusalsHaveNoSiteEvent(t *testing.T) {
	h := newHarness(t)
	h.add(t, "alice", "blog", true)
	calls := []struct {
		tool, args string
		refused    bool
	}{{"list", `{}`, false}, {"show", `{"name":"blog"}`, false}, {"apex", `{}`, false}, {"apex", `{"clear":false}`, false}, {"create", `{"name":"Bad","repo":"bad"}`, true}, {"publish", `{"name":"missing"}`, true}, {"update", `{"name":"blog"}`, true}, {"delete", `{"name":"missing"}`, true}, {"apex", `{"name":"blog","clear":true}`, true}, {"show", `{"name":"missing"}`, true}, {"list", `{"name":"blog"}`, true}}
	other := h.add(t, "bob", "other", true)
	if _, err := h.cfg.Store.SetApex(context.Background(), other.ID); err != nil {
		t.Fatal(err)
	}
	r, events := eventCall(t, h, "event-populated-apex", "apex", `{}`)
	assertMutationWire(t, r, `{"apex":`+expectedSiteWire(other)+`}`)
	checkSiteEvents(t, events, nil, nil)
	if err := h.cfg.Store.ClearApex(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i, c := range calls {
		r, events := eventCall(t, h, fmt.Sprintf("event-none-%d", i), c.tool, c.args)
		if r.IsError() != c.refused {
			t.Fatalf("unexpected result %s", c.tool)
		}
		checkSiteEvents(t, events, nil, nil)
	}
}

// R-FHI8-LXI8
func TestPublishedEventUsesRequestedRefAndRepeats(t *testing.T) {
	h := newHarness(t)
	repo := "rep_0123456789abcdef"
	p := h.repo(t, repo, "alice")
	sha := commitFile(t, h, p, "published")
	h.git(t, p, "update-ref", "refs/tags/alternate", sha)
	h.add(t, "alice", "blog", true)
	for i, args := range []string{`{"name":"blog","ref":"alternate"}`, `{"name":"blog"}`, `{"name":"blog"}`} {
		r, events := eventCall(t, h, fmt.Sprintf("event-publish-%d", i), "publish", args)
		o := mutationObject(t, r)
		if o.Ref != "main" || o.Commit == nil || *o.Commit != sha {
			t.Fatalf("wrong object %+v", o)
		}
		ref := o.Ref
		if i == 0 {
			ref = "alternate"
		}
		checkSiteEvents(t, events, []string{"site.published"}, []telemetry.Attrs{{"site": o.ID, "commit": *o.Commit, "ref": ref}})
	}
}
