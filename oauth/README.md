> [!WARNING]
> This is unsupported AI slop.

# oauth

`oauth` is a CLI that runs the OAuth 2.0 authorization-code + PKCE flow
against any compliant service and hands back the token response. A person runs
it at a terminal to log in; a program shells out to it instead of carrying its
own OAuth implementation. It holds no provider-specific knowledge and does not
store or refresh tokens.

## Installing it

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/oauth/install.sh | sh
```

This installs the newest stable release (Linux and macOS, amd64 and arm64) to
`~/.local/bin`. Set `OAUTH_VERSION=vX.Y.Z` to pin a version, or `BINDIR` to
change the destination.

## Using it

```sh
oauth --help
```
