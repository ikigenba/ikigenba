{{/* The installer at GET /setup.sh. Data: .Space, .Origin, .Endpoint, .TokenURL, .Variable, .Server. */}}{{define "installer"}}#!/usr/bin/env bash
# ikigenba-setup: store this space's identity and a token for your agent.
MCP_URL='{{.Endpoint}}'
AUTH_URL='{{.TokenURL}}'
SPACE='{{.Space}}'
VARIABLE='{{.Variable}}'
SERVER='{{.Server}}'

set -uo pipefail

die() {
    printf 'ikigenba-setup: %s\n' "$1" >&2
    [[ -n ${2:-} ]] && printf '\n%s\n' "$2" >&2
    exit "${3:-1}"
}

[[ $(uname -s) == Linux ]] || die "only Linux is supported for now"
command -v curl >/dev/null 2>&1 || die "curl is not installed"
(: </dev/tty) 2>/dev/null || die "no terminal to read the token from" "Run this in a terminal."
exec 3</dev/tty 4>/dev/tty

dir="${XDG_CONFIG_HOME:-$HOME/.config}/ikigenba/$SPACE"

printf 'Space:   %s\nGateway: %s\nSaves:   %s/{config,token}\n\n' "$SPACE" "$MCP_URL" "$dir" >&4
printf 'Create a token at %s and paste it here.\n' "$AUTH_URL" >&4

tty_saved=
restore_tty() { [[ -n $tty_saved ]] && stty "$tty_saved" <&3 2>/dev/null; tty_saved=; }
trap restore_tty EXIT
trap 'restore_tty; printf "\n" >&4; die "interrupted; nothing was changed"' INT TERM HUP

printf 'Token: ' >&4
tty_saved=$(stty -g <&3 2>/dev/null) || tty_saved=
stty -echo -icanon min 1 time 0 <&3 2>/dev/null
token=
while IFS= read -r -n 1 -d '' ch <&3; do
    case $ch in
    $'\n' | $'\r' | $'\x04') break ;;
    $'\x7f' | $'\b') [[ -n $token ]] && { token=${token%?}; printf '\b \b' >&4; } ;;
    $'\x15') while [[ -n $token ]]; do token=${token%?}; printf '\b \b' >&4; done ;;
    [[:cntrl:]] | '') ;;
    *) token+=$ch; printf '*' >&4 ;;
    esac
done
restore_tty
printf '\n' >&4

token=${token#"${token%%[![:space:]]*}"}
token=${token%"${token##*[![:space:]]}"}
[[ -n $token ]] || die "no token given; nothing was changed"

# The header goes to curl on stdin, never in its arguments.
printf 'Checking the token with the gateway... ' >&4
status=$(printf 'Authorization: Bearer %s\n' "$token" |
    curl -sS -o /dev/null -w '%{http_code}' --max-time 30 \
        -X POST -H @- \
        -H 'Content-Type: application/json' \
        -H 'Accept: application/json, text/event-stream' \
        --data '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
        "$MCP_URL" 2>/dev/null) || status=000
case $status in
2??) printf 'ok\n' >&4 ;;
401 | 403) printf 'failed\n' >&4; die "the gateway refused the token; nothing was changed" "Create a token at $AUTH_URL and run this again." ;;
*) printf 'failed\n' >&4; die "cannot reach $MCP_URL (HTTP $status); nothing was changed" ;;
esac

umask 077
[[ -d $dir ]] || mkdir -m 0700 -p -- "$dir" || die "cannot create $dir"
printf '%s\n' "$token" >"$dir/token.new" && mv -f -- "$dir/token.new" "$dir/token" || die "cannot write $dir/token"
cat >"$dir/config.new" <<CONFIG && mv -f -- "$dir/config.new" "$dir/config" || die "cannot write $dir/config"
space=$SPACE
mcp=$MCP_URL
auth=$AUTH_URL
variable=$VARIABLE
server=$SERVER
CONFIG
chmod 0644 "$dir/config"

printf '\nSaved. Tell your agent setup is done; it will take it from here.\n' >&4
{{end}}
