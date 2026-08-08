# Phase 37 — The nginx fragment: the git-door location and correlation capture

*Realizes design Decision 10 (nginx fragment & landing) and Decision 13
(correlation capture). Depends on Phase 36.*

`etc/nginx.conf` gains the `location /srv/repos/git/` block D10 specifies —
`auth_request /_authn`, the owner and client header capture-and-forward, the
correlation capture, `proxy_pass http://127.0.0.1:3007/git/`, and the four
streaming directives (`client_max_body_size 0`, `proxy_request_buffering off`,
`proxy_buffering off`, a long `proxy_read_timeout`) without which a real clone
or push fails. `cmd/repos/nginx_test.go` is extended to cover it, and the
landing-page assertions are re-confirmed against the v2 service.

**Done when:** the suite is green and these ids are each covered by a
clearly-named test —

- R-G1OF-AAC8 — the fragment's tier set, PRM openness, feed 404, session gates,
  bearer identity replacement, and the `127.0.0.1:3007` upstream.
- R-J3QD-3HFN — the git-door location's gate, its four forwarded headers, its
  `/git/` upstream, and all four streaming directives.
- R-UZVS-S08C — the session landing block forwards all four owner headers via
  its own variables.
- R-V13P-5RZ1 — the bearer prefix block forwards all four owner headers plus
  `X-Client-Id`.
- R-G2WB-O22X — the landing renders with service name and running version.
- R-G448-1TTM — the static assets serve with correct content types.
- R-9DUI-TUQJ — all four gated locations capture and forward the minted
  correlation id.
- R-9F2F-7MH8 — the ungated PRM bootstrap strips the header and no gated
  location carries the empty-value form.
