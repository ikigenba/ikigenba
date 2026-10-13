package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func pageArguments(t *testing.T, page any, limit any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"page": page, "limit": limit})
	toolsMust(t, err)
	return string(raw)
}

func pageAnswer(t *testing.T, f *toolsFixture, tool string, repos []store.Repo, next string) string {
	t.Helper()
	entries := make([]string, 0, len(repos))
	for _, r := range repos {
		if tool == "list" {
			head, err := f.Store.Head(toolsContext(t), r.ID)
			toolsMust(t, err)
			entries = append(entries, queryListing(t, f, r, head))
		} else {
			entries = append(entries, statusRepository(t, f, r, f.Limits.Settings().RepoMaxBytes, f.Limits.Busy(r.ID)))
		}
	}
	suffix := ""
	if next != "" {
		suffix = `,"next":` + pagingCopy(next)
	}
	if tool == "list" {
		return `{"repos":[` + strings.Join(entries, ",") + `]` + suffix + `}`
	}
	p := f.Limits.Pressure()
	return fmt.Sprintf(`{"read":{"slots":%d,"active":%d,"queued":%d},"write":{"slots":%d,"active":%d,"queued":%d},"repos":[%s]%s}`, p.Read.Slots, p.Read.Active, p.Read.Queued, p.Write.Slots, p.Write.Active, p.Write.Queued, strings.Join(entries, ","), suffix)
}

// R-FMW4-FSUG R-FPBX-7CBU R-FRRP-YVT8 R-195N-6SLQ
func TestPagingWindowsAndToolBoundContinuation(t *testing.T) {
	f := newToolsFixture(t)
	n, err := paging.Window(nil)
	toolsMust(t, err)
	// Reverse insertion ensures the cursor follows byte ordered names, not ids.
	ordered := make([]store.Repo, n+2)
	for i := len(ordered) - 1; i >= 0; i-- {
		ordered[i] = f.create(t, f.Caller.UserID, fmt.Sprintf("repo-%03d", i))
	}
	f.create(t, "other", ordered[0].Name)
	f.create(t, "other", "repo-000-extra")
	queryHead(t, f, ordered[0], "first head")
	for _, tool := range []string{"list", "status"} {
		t.Run(tool, func(t *testing.T) {
			token := paging.Token{Tool: tool}
			for _, args := range []string{"", "{}", `{"page":null,"limit":null}`, `{"page":""}`} {
				toolsEqual(t, string(successObject(t, f.call(t, tool, args))), pageAnswer(t, f, tool, ordered[:n], token.Encode(ordered[n-1].Name)))
			}
			for _, size := range []int{1, 2, len(ordered), len(ordered) + 1, paging.MaxLimit} {
				count := min(size, len(ordered))
				next := ""
				if count < len(ordered) {
					next = token.Encode(ordered[count-1].Name)
				}
				toolsEqual(t, string(successObject(t, f.call(t, tool, pageArguments(t, nil, size)))), pageAnswer(t, f, tool, ordered[:count], next))
			}
			collected := make([]store.Repo, 0, len(ordered))
			page := ""
			for len(collected) < len(ordered) {
				result := successObject(t, f.call(t, tool, pageArguments(t, page, 2)))
				count := min(2, len(ordered)-len(collected))
				next := ""
				if len(collected)+count < len(ordered) {
					next = token.Encode(ordered[len(collected)+count-1].Name)
				}
				toolsEqual(t, string(result), pageAnswer(t, f, tool, ordered[len(collected):len(collected)+count], next))
				collected = append(collected, ordered[len(collected):len(collected)+count]...)
				page = next
			}
			// A cursor is a name boundary, even when no catalog entry has that name.
			for _, key := range []string{"", "repo-000-extra", ordered[len(ordered)-1].Name, "zzzz", "é"} {
				remainder := make([]store.Repo, 0)
				for _, r := range ordered {
					if r.Name > key {
						remainder = append(remainder, r)
					}
				}
				toolsEqual(t, string(successObject(t, f.call(t, tool, pageArguments(t, token.Encode(key), paging.MaxLimit)))), pageAnswer(t, f, tool, remainder, ""))
			}
			result, err := f.Client.CallTool(toolsContext(t), identity.Caller{UserID: "empty", RequestID: "empty-page"}, tool, json.RawMessage(pageArguments(t, token.Encode(ordered[0].Name), 1)))
			toolsMust(t, err)
			toolsEqual(t, string(successObject(t, result)), pageAnswer(t, f, tool, nil, ""))
		})
	}
}

// R-FKGB-O9D2 R-FLO8-213R R-FQJT-L42J R-FSZM-CNJX
func TestPagingRefusalsPrecedeEveryStoreAccess(t *testing.T) {
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	for _, tool := range []string{"list", "status"} {
		other := "list"
		if tool == other {
			other = "status"
		}
		token := paging.Token{Tool: tool}
		badPages := []string{"not a token", token.Encode(int64(1)), token.Encode("notes", "extra"), paging.Token{Tool: other}.Encode("notes"), token.Encode("notes") + "="}
		for _, failing := range []bool{false, true} {
			f.DB.SetFailing(failing)
			for _, page := range append(badPages, token.Encode("notes")) {
				for _, limit := range []int{math.MinInt, -1, 0, paging.MaxLimit + 1, math.MaxInt} {
					_, err := paging.Window(&limit)
					refusal(t, f.call(t, tool, pageArguments(t, page, limit)), err.Error())
				}
			}
			for _, page := range badPages {
				for _, limit := range []any{nil, 1, paging.MaxLimit} {
					refusal(t, f.call(t, tool, pageArguments(t, page, limit)), paging.BadPage)
				}
			}
		}
		f.DB.SetFailing(false)
		// The context's Err is observed by store reads. No refusal may reach it.
		probe := &statusCancelOnAccess{Context: context.Background()}
		f.Client = queryContextClient(t, f, func(ctx context.Context) (context.Context, context.CancelFunc) {
			probe.Context = ctx
			return probe, func() {}
		})
		refusal(t, f.call(t, tool, pageArguments(t, "broken", 0)), fmt.Sprintf(paging.BadLimit, 1, paging.MaxLimit, 0))
		refusal(t, f.call(t, tool, pageArguments(t, "broken", nil)), paging.BadPage)
		toolsEqual(t, probe.calls.Load(), int64(0))
	}
}

// R-FMW4-FSUG
func TestPagingContinuationReadsCurrentCatalog(t *testing.T) {
	for _, tool := range []string{"list", "status"} {
		t.Run(tool, func(t *testing.T) {
			f := newToolsFixture(t)
			zeta := f.create(t, f.Caller.UserID, "zeta")
			alpha := f.create(t, f.Caller.UserID, "alpha")
			beta := f.create(t, f.Caller.UserID, "beta")
			next := paging.Token{Tool: tool}.Encode(alpha.Name)
			toolsEqual(t, string(successObject(t, f.call(t, tool, pageArguments(t, nil, 1)))), pageAnswer(t, f, tool, []store.Repo{alpha}, next))
			toolsMust(t, f.Store.Delete(toolsContext(t), alpha.ID))
			inserted := f.create(t, f.Caller.UserID, "alpha-1")
			f.create(t, f.Caller.UserID, "0-before")
			renamed, err := f.Store.Rename(toolsContext(t), zeta.ID, "gamma")
			toolsMust(t, err)
			toolsEqual(t, string(successObject(t, f.call(t, tool, pageArguments(t, next, 2)))), pageAnswer(t, f, tool, []store.Repo{inserted, beta}, paging.Token{Tool: tool}.Encode(beta.Name)))
			toolsEqual(t, string(successObject(t, f.call(t, tool, pageArguments(t, next, 3)))), pageAnswer(t, f, tool, []store.Repo{inserted, beta, renamed}, ""))
		})
	}
}
