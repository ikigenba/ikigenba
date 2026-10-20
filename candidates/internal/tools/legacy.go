package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// LegacyArgs names a list using the previous release's argument.
type LegacyArgs struct {
	Cohort string `json:"cohort" mcp:"required"`
}

// DescribeLegacy describes the compatibility reader.
const DescribeLegacy string = "Read a prospect list for a caller of the previous release."

// LegacyNames returns the unlisted compatibility tool names.
func LegacyNames() []string { return []string{"cohort"} }

// RegisterLegacy registers the compatibility reader on its separate server.
func RegisterLegacy(srv *mcp.Server, cfg Config) {
	add(srv, "cohort", DescribeLegacy, mcp.Read, func(ctx context.Context, _ identity.Caller, a LegacyArgs) mcp.Result {
		id := store.MapListID(a.Cohort)
		if !store.ValidProspectListID(id) {
			return refuse(passes.RefusalNotFound)
		}
		r, err := cfg.Store.ReadProspectList(ctx, id)
		if err != nil {
			return failure(err)
		}
		return answer(prospectListReadOf(r))
	})
}

// Dispatch selects the compatibility server only for its named tool calls.
func Dispatch(main, legacy *mcp.Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix, err := io.ReadAll(io.LimitReader(r.Body, 65537))
		r.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(prefix), r.Body), Closer: r.Body}
		target := main
		if err == nil && len(prefix) <= 65536 {
			var msg map[string]json.RawMessage
			var method, name string
			var params map[string]json.RawMessage
			if json.Unmarshal(prefix, &msg) == nil && json.Unmarshal(msg["method"], &method) == nil && method == "tools/call" && json.Unmarshal(msg["params"], &params) == nil && json.Unmarshal(params["name"], &name) == nil && slices.Contains(LegacyNames(), name) {
				target = legacy
			}
		}
		target.ServeHTTP(w, r)
	})
}

type replayBody struct {
	io.Reader
	io.Closer
}
