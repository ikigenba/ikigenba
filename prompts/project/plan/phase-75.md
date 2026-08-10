# Phase 75 — Runner cutover: gateway in, eager attachment out

*Realizes design Decision 19, slice R-OVMQ-VU50, R-P2Y5-6GL6, R-P461-K8BV, and
the Decision 45 slice R-P7TQ-PJJY. Depends on Phase 74.*

Cut the runner over to the gateway (D7/D19): the conversation's `Tools` is the
13 sandbox tools plus `gateway.Tools(...)`; `MCPServers` is no longer set; the
`discover` seam carries `[]suite.Peer`; the composition root wires the D60
client over the chassis's instrumented outbound client. Replace the framing
prompt's direct-attachment sentence with D19's gateway paragraph (no
individual service names, no `load_tools`). Delete everything the cutover
retires: the `MCPServer` attachment wiring and the tests tagged with the ten
retired ids (R-ZDZU-IC81, R-EF0V-TP9R, R-ZF7Q-W3YQ, R-ZGFN-9VPF, R-ZIVG-1F6T,
R-ZK3C-F6XI, R-ZLB8-SYO7, R-HPLU-D8WR, R-HS1N-4SE5, R-HT9J-IK4U), whose
behaviors design no longer mints.

**Done when:** R-OVMQ-VU50 (toolset composition, no `ikigenba_` names, no
spawn-time peer contact), R-P2Y5-6GL6 (framing prompt), R-P461-K8BV
(end-to-end discover→inspect→invoke run succeeds with all three gateway
`tool_use`/`tool_result` records), and R-P7TQ-PJJY (stored correlation id on
list and call, `correlation.FromContext` agreement) are each covered by a test
tagged with its id; `grep -rn 'R-ZDZU-IC81\|R-EF0V-TP9R\|R-ZF7Q-W3YQ\|R-ZGFN-9VPF\|R-ZIVG-1F6T\|R-ZK3C-F6XI\|R-ZLB8-SYO7\|R-HPLU-D8WR\|R-HS1N-4SE5\|R-HT9J-IK4U' --include='*_test.go' .`
run from `prompts/` returns no matches; `grep -rn "MCPServers" --include='*.go' cmd internal`
returns no matches; and the suite is green (design Conventions).
