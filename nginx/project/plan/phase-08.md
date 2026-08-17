# Phase 08 — The `/t` tracking-beacon proxy on the `michaelgreenly.dev` vhost

*Realizes design Decision 5 (the michaelgreenly.dev vhost — the `/t` beacon
proxy extension). The site-serving half of D5 already exists in the tree; this
phase adds the reserved beacon path to that same committed file.*

Extend the committed vhost `nginx/michaelgreenly.dev/nginx.conf`: inside the
`:443` server block, add an exact-match `location = /t` that (1) `limit_except
POST { deny all; }`, (2) `proxy_pass http://127.0.0.1:3006/in/mg-dev-track;`,
(3) `include`s the untracked box-local secret file
`/etc/nginx/conf.d/michaelgreenly.dev.secret.conf` that sets the
`Authorization` bearer, and (4) mirrors the site proxy's headers (empty
`X-Correlation-Id`, forwarded `Host` and `X-Forwarded-Proto`, HTTP/1.1). The
existing `location /` site proxy and both `server` blocks are otherwise
unchanged. No secret appears in the committed bytes. This tree mints no ids;
the deploy runbook update (placing the secret include, and the `/t` live-box
verification) is an operator step in the repo-root `deploy.md`, outside this
tree.

**Done when** (deterministic structural checks — this tree has no test suite and
no id tags):

- The committed `nginx/michaelgreenly.dev/nginx.conf` contains an exact-match
  `location = /t` block whose `proxy_pass` targets
  `http://127.0.0.1:3006/in/mg-dev-track`, which contains both a `limit_except
  POST` guard and an `include` of `michaelgreenly.dev.secret.conf`, and which
  precedes the `location /` site block in the `:443` server.
- The committed file contains **no** `Authorization` header value and no bearer
  literal — `grep -i 'authorization\|bearer' nginx/michaelgreenly.dev/nginx.conf`
  returns nothing; the secret is referenced only by the `include` directive.
- The site proxy is intact: `location /` still `proxy_pass`es to
  `http://127.0.0.1:3004/public/michaelgreenly-dev/`.
- The tree is green: from `nginx/`, `bash -n run` exits 0 and `mkdir -p tmp &&
  nginx -p . -c nginx.conf -t` exits 0 (the main dev config; the vhost fragment
  is syntax-checked on the box per the runbook, where its `include` is present).
