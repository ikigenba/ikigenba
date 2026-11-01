package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

func probe[T any](srv *mcp.Server, name string, value T) {
	mcp.AddTool(srv, mcp.Tool[tools.NoArgs, T]{Name: name, Description: tools.DescribeListSessions, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, tools.NoArgs) (T, error) { return value, nil }})
}

// R-GOHT-GMKV R-GPPP-UEBK R-GQXM-8629 R-GS5I-LXSY R-GTDE-ZPJN R-GULB-DHAC R-GVT7-R911 R-GX14-50RQ R-GY90-ISIF R-H0OT-ABZT R-H1WP-O3QI R-H34M-1VH7 R-H4CI-FN7W R-H5KE-TEYL
func TestAnswerTypesEncoding(t *testing.T) {
	r := newRig(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "probe", Telemetry: r.w})
	probe(srv, "latest_none", tools.Latest{"claude", "s", nil})
	probe(srv, "latest", tools.Latest{"codex", "s", ptr("instant")})
	probe(srv, "latests", tools.LatestList{nil})
	probe(srv, "counts", tools.Tokens{1, 2, 3, 4, 10})
	probe(srv, "command", tools.Command{"/x", "argument"})
	probe(srv, "session", tools.Session{"claude", "s", "h", "e", "/w", true, nil, "first", "last", "build", 1, 2, 3, tools.Tokens{4, 5, 6, 7, 22}, 8, "9.10", 11})
	probe(srv, "session_command", tools.Session{Command: &tools.Command{"/x", "arg"}})
	probe(srv, "sessions", tools.SessionList{nil, nil})
	probe(srv, "sessions_next", tools.SessionList{[]tools.Session{}, ptr("token")})
	probe(srv, "group", tools.GroupTotals{"g", 1, 2, 3, tools.Tokens{4, 5, 6, 7, 22}, 8, "9.10", 11})
	probe(srv, "period", tools.PeriodTotals{"date", nil})
	probe(srv, "totals", tools.Totals{nil})
	probe(srv, "rule", tools.Rule{"host", "/dir", "", "build"})
	probe(srv, "rules", tools.RuleList{nil})
	probe(srv, "price", tools.Price{"model", "date", "1", "2", "3", "4"})
	probe(srv, "prices", tools.PriceList{nil})
	probe(srv, "price_ref", tools.PriceRef{"model", "date"})
	probe(srv, "away", tools.AwayLimit{15})
	client := serve(t, srv)
	wants := map[string]string{
		"latest_none": `{"tool":"claude","id":"s"}`, "latest": `{"tool":"codex","id":"s","latest":"instant"}`, "latests": `{"sessions":[]}`, "counts": `{"input":1,"cached":2,"output":3,"reasoning":4,"total":10}`, "command": `{"name":"/x","argument":"argument"}`,
		"session":         `{"tool":"claude","id":"s","host":"h","person":"e","directory":"/w","headless":true,"first":"first","last":"last","kind":"build","attention_seconds":1,"wall_seconds":2,"agent_seconds":3,"tokens":{"input":4,"cached":5,"output":6,"reasoning":7,"total":22},"unpriced":8,"dollars":"9.10","subagents":11}`,
		"session_command": `{"tool":"","id":"","host":"","person":"","directory":"","headless":false,"command":{"name":"/x","argument":"arg"},"first":"","last":"","kind":"","attention_seconds":0,"wall_seconds":0,"agent_seconds":0,"tokens":{"input":0,"cached":0,"output":0,"reasoning":0,"total":0},"unpriced":0,"dollars":"","subagents":0}`,
		"sessions":        `{"sessions":[]}`, "sessions_next": `{"sessions":[],"next":"token"}`, "group": `{"group":"g","attention_seconds":1,"wall_seconds":2,"agent_seconds":3,"tokens":{"input":4,"cached":5,"output":6,"reasoning":7,"total":22},"unpriced":8,"dollars":"9.10","sessions":11}`, "period": `{"start":"date","groups":[]}`, "totals": `{"periods":[]}`, "rule": `{"host":"host","directory":"/dir","command":"","kind":"build"}`, "rules": `{"rules":[]}`, "price": `{"model":"model","effective":"date","input":"1","cached":"2","output":"3","reasoning":"4"}`, "prices": `{"prices":[]}`, "price_ref": `{"model":"model","effective":"date"}`, "away": `{"minutes":15}`}
	for name, want := range wants {
		t.Run(name, func(t *testing.T) {
			v, err := client.CallTool(t.Context(), owner, name, json.RawMessage(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			if v.IsError() {
				t.Fatalf("%s", raw(t, v))
			}
			w := wire(t, v)
			equal(t, string(w.Structured), want)
			equal(t, w.Content[0].Text, want)
		})
	}
}

// R-H6SB-76PA R-H807-KYFZ R-GTDE-ZPJN
func TestOutputSchemas(t *testing.T) {
	r := newRig(t)
	infos, err := r.client.ListTools(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	// Build expectations from contract members; descriptions on answer members
	// are immaterial apart from the shared paging member.
	str := func(n string) field { return sf(n, "", false) }
	num := func(n string) field { return nf(n, "", "integer") }
	tokens := []field{num("input"), num("cached"), num("output"), num("reasoning"), num("total")}
	figures := []field{num("attention_seconds"), num("wall_seconds"), num("agent_seconds"), obj("tokens", "", false, tokens), num("unpriced"), str("dollars")}
	latest := []field{str("tool"), str("id"), sf("latest", "", true)}
	command := []field{str("name"), str("argument")}
	session := []field{str("tool"), str("id"), str("host"), str("person"), str("directory"), nf("headless", "", "boolean"), obj("command", "", true, command), str("first"), str("last"), str("kind")}
	session = append(session, figures...)
	session = append(session, num("subagents"))
	group := append([]field{str("group")}, figures...)
	group = append(group, num("sessions"))
	period := []field{str("start"), arr("groups", "", group)}
	rule := []field{str("host"), str("directory"), str("command"), str("kind")}
	price := []field{str("model"), str("effective"), str("input"), str("cached"), str("output"), str("reasoning")}
	wants := [][]field{{arr("sessions", "", latest)}, {arr("sessions", "", latest)}, {arr("sessions", "", session), sf("next", paging.NextDescription, true)}, {arr("periods", "", period)}, {arr("rules", "", rule)}, rule, rule, {arr("prices", "", price)}, price, price[:2], {num("minutes")}, {num("minutes")}}
	for i, info := range infos {
		t.Run(info.Name, func(t *testing.T) {
			var clean func(*schema)
			clean = func(s *schema) {
				if s.Description != paging.NextDescription {
					s.Description = ""
				}
				if len(s.Properties) > 0 {
					names, props := objectFields(t, s.Properties)
					b := []byte{'{'}
					for j, n := range names {
						v := props[n]
						clean(&v)
						if j > 0 {
							b = append(b, ',')
						}
						b = append(b, raw(t, n)...)
						b = append(b, ':')
						b = append(b, raw(t, v)...)
					}
					b = append(b, '}')
					s.Properties = b
				}
				if s.Items != nil {
					clean(s.Items)
				}
			}
			var s schema
			if err := json.Unmarshal(info.OutputSchema, &s); err != nil {
				t.Fatal(err)
			}
			clean(&s)
			checkSchema(t, raw(t, s), wants[i])
		})
	}
}
