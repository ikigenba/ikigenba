package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// R-FXO1-1O9L R-HFBL-VKW5
func TestFirstLine(t *testing.T) {
	for _, s := range []string{"", "a", "a\n\nb", "\nsecond", "a\r\nb"} {
		want, _, _ := strings.Cut(s, "\n")
		equal(t, tools.FirstLine(s), want)
	}
}

// R-FU0B-WD1I R-FV88-A4S7 R-FYVX-FG0A R-G03T-T7QZ R-HE3P-HT5G
func TestCatalog(t *testing.T) {
	r := newRig(t)
	infos, err := r.client.ListTools(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{tools.ToolPushTurns, tools.ToolGetLatestTurns, tools.ToolListSessions, tools.ToolTotalSessions, tools.ToolListRules, tools.ToolAddRule, tools.ToolRemoveRule, tools.ToolListPrices, tools.ToolSetPrice, tools.ToolRemovePrice, tools.ToolGetAwayLimit, tools.ToolSetAwayLimit}
	equal(t, names, []string{"push_turns", "get_latest_turns", "list_sessions", "total_sessions", "list_rules", "add_rule", "remove_rule", "list_prices", "set_price", "remove_price", "get_away_limit", "set_away_limit"})
	descriptions := []string{tools.DescribePushTurns, tools.DescribeGetLatestTurns, tools.DescribeListSessions, tools.DescribeTotalSessions, tools.DescribeListRules, tools.DescribeAddRule, tools.DescribeRemoveRule, tools.DescribeListPrices, tools.DescribeSetPrice, tools.DescribeRemovePrice, tools.DescribeGetAwayLimit, tools.DescribeSetAwayLimit}
	effects := []mcp.Effect{mcp.Additive, mcp.Read, mcp.Read, mcp.Read, mcp.Read, mcp.Additive, mcp.Destructive, mcp.Read, mcp.Additive, mcp.Destructive, mcp.Read, mcp.Additive}
	if len(infos) != 12 || len(r.srv.Tools()) != 12 {
		t.Fatalf("catalog lengths %d/%d", len(infos), len(r.srv.Tools()))
	}
	for i, v := range infos {
		equal(t, v.Name, names[i])
		equal(t, v.Description, descriptions[i])
		equal(t, v.Effect(), effects[i])
		equal(t, r.srv.Tools()[i], mcp.RegisteredTool{Name: names[i], Description: descriptions[i]})
		if tools.FirstLine(descriptions[i]) == "" {
			t.Fatalf("empty first line %s", v.Name)
		}
	}
}

// R-G1BQ-6ZHO R-G2JM-KR8D R-G3RI-YIZ2
func TestArgumentCopyConstants(t *testing.T) {
	for _, s := range []string{tools.ArgPushSessions, tools.ArgLatestSessions, tools.ArgTool, tools.ArgID, tools.ArgParent, tools.ArgHost, tools.ArgDirectory, tools.ArgHeadless, tools.ArgCommand, tools.ArgCommandName, tools.ArgCommandArgument, tools.ArgTurns, tools.ArgMoment, tools.ArgWhose, tools.ArgModel, tools.ArgInputTokens, tools.ArgCachedTokens, tools.ArgOutputTokens, tools.ArgReasoningTokens, tools.ArgPeriod, tools.ArgOf, tools.ArgFrom, tools.ArgTo, tools.ArgGroup, tools.ArgFilterPerson, tools.ArgFilterKind, tools.ArgFilterTool, tools.ArgFilterHost, tools.ArgFilterCommand, tools.ArgRuleHost, tools.ArgRuleDirectory, tools.ArgRuleCommand, tools.ArgRuleKind, tools.ArgPriceModel, tools.ArgEffective, tools.ArgInputRate, tools.ArgCachedRate, tools.ArgOutputRate, tools.ArgReasoningRate, tools.ArgMinutes} {
		if s == "" || strings.ContainsAny(s, "\n\"`\\") {
			t.Fatalf("invalid argument copy %q", s)
		}
	}
}

type schema struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Properties  json.RawMessage `json:"properties"`
	Required    []string        `json:"required"`
	Items       *schema         `json:"items"`
}
type field struct {
	name, kind, description string
	optional                bool
	nested                  []field
	array                   bool
}

func objectFields(t *testing.T, raw json.RawMessage) ([]string, map[string]schema) {
	t.Helper()
	if len(raw) == 0 {
		return nil, map[string]schema{}
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var names []string
	out := map[string]schema{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		name, ok := key.(string)
		if !ok {
			t.Fatal("non-string key")
		}
		var v schema
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
		out[name] = v
	}
	return names, out
}
func checkSchema(t *testing.T, raw json.RawMessage, fields []field) {
	t.Helper()
	var root schema
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	equal(t, root.Type, "object")
	names, props := objectFields(t, root.Properties)
	var wantNames, required []string
	for _, f := range fields {
		wantNames = append(wantNames, f.name)
		if !f.optional {
			required = append(required, f.name)
		}
		v, ok := props[f.name]
		if !ok {
			t.Fatalf("missing %s in %s", f.name, raw)
		}
		equal(t, v.Type, f.kind)
		equal(t, v.Description, f.description)
		if f.nested != nil {
			if f.array {
				if v.Items == nil {
					t.Fatalf("missing items %s", f.name)
				}
				checkSchema(t, rawSchema(t, *v.Items), f.nested)
			} else {
				checkSchema(t, rawSchema(t, v), f.nested)
			}
		}
	}
	equal(t, names, wantNames)
	if len(required) == 0 {
		if len(root.Required) != 0 {
			t.Fatalf("unexpected required %v", root.Required)
		}
	} else {
		equal(t, root.Required, required)
	}
}
func rawSchema(t *testing.T, s schema) json.RawMessage { t.Helper(); return raw(t, s) }
func sf(n, d string, optional bool) field              { return field{n, "string", d, optional, nil, false} }
func nf(n, d, kind string) field                       { return field{n, kind, d, false, nil, false} }
func obj(n, d string, optional bool, fs []field) field {
	return field{n, "object", d, optional, fs, false}
}
func arr(n, d string, fs []field) field { return field{n, "array", d, false, fs, true} }

// R-G4ZF-CAPR R-G67B-Q2GG R-G7F8-3U75 R-G8N4-HLXU R-G9V0-VDOJ R-GB2X-95F8 R-GCAT-MX5X R-GDIQ-0OWM R-GEQM-EGNB R-GFYI-S8E0 R-GIEB-JRVE R-GJM7-XJM3 R-GKU4-BBCS R-GM20-P33H R-GN9X-2UU6
func TestInputSchemas(t *testing.T) {
	r := newRig(t)
	infos, err := r.client.ListTools(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	ref := []field{sf("tool", tools.ArgTool, false), sf("id", tools.ArgID, false)}
	command := []field{sf("name", tools.ArgCommandName, false), sf("argument", tools.ArgCommandArgument, false)}
	turn := []field{sf("moment", tools.ArgMoment, false), sf("whose", tools.ArgWhose, false), sf("model", tools.ArgModel, false), nf("input", tools.ArgInputTokens, "integer"), nf("cached", tools.ArgCachedTokens, "integer"), nf("output", tools.ArgOutputTokens, "integer"), nf("reasoning", tools.ArgReasoningTokens, "integer")}
	session := append(append([]field{}, ref...), obj("parent", tools.ArgParent, true, ref), sf("host", tools.ArgHost, false), sf("directory", tools.ArgDirectory, false), nf("headless", tools.ArgHeadless, "boolean"), obj("command", tools.ArgCommand, true, command), arr("turns", tools.ArgTurns, turn))
	filters := []field{sf("person", tools.ArgFilterPerson, true), sf("kind", tools.ArgFilterKind, true), sf("tool", tools.ArgFilterTool, true), sf("host", tools.ArgFilterHost, true), sf("command", tools.ArgFilterCommand, true)}
	list := append([]field{sf("period", tools.ArgPeriod, true), sf("of", tools.ArgOf, true)}, filters...)
	list = append(list, sf("page", paging.PageDescription, true), field{"limit", "integer", paging.LimitDescription, true, nil, false})
	total := append([]field{sf("period", tools.ArgPeriod, false), sf("from", tools.ArgFrom, false), sf("to", tools.ArgTo, true), sf("group", tools.ArgGroup, false)}, filters...)
	rule := []field{sf("host", tools.ArgRuleHost, false), sf("directory", tools.ArgRuleDirectory, false), sf("command", tools.ArgRuleCommand, true)}
	price := []field{sf("model", tools.ArgPriceModel, false), sf("effective", tools.ArgEffective, false), sf("input", tools.ArgInputRate, false), sf("cached", tools.ArgCachedRate, false), sf("output", tools.ArgOutputRate, false), sf("reasoning", tools.ArgReasoningRate, false)}
	wants := [][]field{{arr("sessions", tools.ArgPushSessions, session)}, {arr("sessions", tools.ArgLatestSessions, ref)}, list, total, {}, append(append([]field{}, rule...), sf("kind", tools.ArgRuleKind, false)), rule, {}, price, price[:2], {}, {nf("minutes", tools.ArgMinutes, "integer")}}
	for i, info := range infos {
		t.Run(info.Name, func(t *testing.T) { checkSchema(t, info.InputSchema, wants[i]) })
	}
	// Positional construction makes the declared public field order usable.
	_ = tools.SessionRef{"claude", "s"}
	_ = tools.Command{"/x", "arg"}
	_ = tools.PushTurn{"moment", "agent", "model", 1, 2, 3, 4}
	_ = tools.PushSession{"claude", "s", nil, "h", "/w", false, nil, nil}
	_ = tools.PushTurnsArgs{nil}
	_ = tools.LatestTurnsArgs{nil}
	_ = tools.ListSessionsArgs{nil, nil, nil, nil, nil, nil, nil, nil, nil}
	_ = tools.TotalSessionsArgs{"week", "2026-10-13", nil, "kind", nil, nil, nil, nil, nil}
	_ = tools.NoArgs{}
	_ = tools.AddRuleArgs{"h", "/d", nil, "build"}
	_ = tools.RemoveRuleArgs{"h", "/d", nil}
	_ = tools.SetPriceArgs{"m", "2026-01-01", "1", "2", "3", "4"}
	_ = tools.RemovePriceArgs{"m", "2026-01-01"}
	_ = tools.SetAwayLimitArgs{15}
}
