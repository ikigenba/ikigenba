# Stories — on a space

The index reached through a space: the file `S4-package.md` describes,
deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at
`dummy.<domain>` over TLS. The story proves the whole path from checkout to
browser and nothing about dummy that the earlier groups do not already say.
devctl and opsctl are named only by their published commands.

## A visitor reaches dummy on a space

Request:

```
$ curl -si https://dummy.mg1.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is an HTML page whose visible text is `Hello from
Dummy!`.

Preconditions:

- The space `mg1.sbx.ikigenba.dev` exists in account `602773793009`, its
  instance is `running`, and `opsctl` is installed on it.
- A tag `dummy/v<semver>` points at the commit `devctl build dummy` was run
  at, and it wrote `dummy/dist/dummy-v<semver>.tar.xz`.
- `devctl --account 602773793009 deploy mg1.sbx.ikigenba.dev dummy/dist/dummy-v<semver>.tar.xz`
  exited 0.
- `devctl --account 602773793009 space status mg1.sbx.ikigenba.dev` shows
  `dummy v<semver> active -`.

Postconditions:

- Nothing has changed.
