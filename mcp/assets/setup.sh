{{/* GET /setup.sh: the installer a user runs in their own terminal as
     curl -fsSL <Origin>/setup.sh | bash -s -- --client <client> --scope <scope>
     It connects one client to the gateway through the variable
     IKIGENBA_TOKEN. Linux only.
     Data: .Origin (<scheme>://<Host>, the gateway origin); .Endpoint
     (<Origin>/mcp, written into the client's configuration and the URL
     the token is checked against); .TokenURL (auth's profile page, where
     a user creates a token). Text the tests read: the first line
     "#!/usr/bin/env bash", .Endpoint and .TokenURL. */}}
{{- define "installer"}}#!/usr/bin/env bash
# ikigenba-setup: connect an agent client to the ikigenba MCP gateway.
#
#   curl -fsSL {{.Origin}}/setup.sh | bash -s -- --client <client> --scope <scope>
#
# Keeps the token in ~/.config/environment.d/ikigenba.conf as IKIGENBA_TOKEN,
# sets it for the login session, and writes the client's "ikigenba" entry,
# which references the variable and never holds the token. Checks the token
# with the gateway before writing anything, and undoes its writes if a later
# one fails. Exits 0 when the client is set up, 1 when it stopped short, and
# 2 when it was run wrongly.

# One block, read whole before any of it runs, so a cut-off download does nothing.
{
set -u -o pipefail

ENDPOINT='{{.Endpoint}}'
TOKEN_URL='{{.TokenURL}}'
PROG=ikigenba-setup
CLIENTS='codex-cli, codex-desktop, claude-cli, claude-desktop, grok-cli'

# die CODE MESSAGE [DETAIL...]: the diagnostic on stderr, then exit.
die() {
    local code=$1
    printf '%s: %s\n' "$PROG" "$2" >&2
    shift 2
    if (($#)); then
        printf '\n' >&2
        printf '%s\n' "$@" >&2
    fi
    exit "$code"
}

# quoted TEXT: another program's output, every line prefixed "> ".
quoted() {
    printf '%s\n' "$1" | sed 's/^/> /'
}

# ---- arguments -------------------------------------------------------------

client= scope= have_client=0 have_scope=0
while (($#)); do
    case $1 in
    --client) (($# > 1)) || die 2 "missing --client" "The client is one of $CLIENTS."
        client=$2 have_client=1; shift 2 ;;
    --client=*) client=${1#*=} have_client=1; shift ;;
    --scope) (($# > 1)) || die 2 "missing --scope" "The scope is user or project."
        scope=$2 have_scope=1; shift 2 ;;
    --scope=*) scope=${1#*=} have_scope=1; shift ;;
    *) die 2 "unknown option '$1'" ;;
    esac
done

((have_client)) && [[ -n $client ]] || die 2 "missing --client" "The client is one of $CLIENTS."
case $client in
codex-cli) label='Codex CLI' family=codex ;;
codex-desktop) label='Codex desktop' family=codex ;;
claude-cli) label='Claude CLI' family=claude ;;
claude-desktop) label='Claude desktop' family=claude ;;
grok-cli) label='Grok CLI' family=grok ;;
*) die 2 "unknown client '$client'" "The client is one of $CLIENTS." ;;
esac
((have_scope)) && [[ -n $scope ]] || die 2 "missing --scope" "The scope is user or project."
case $scope in
user | project) ;;
*) die 2 "unknown scope '$scope'" "The scope is user or project." ;;
esac

# ---- the machine -----------------------------------------------------------

os=$(uname -s 2>/dev/null) || os=unknown
[[ $os == Linux ]] || die 1 "unsupported operating system: $os" "$PROG supports Linux only."

if ! (: </dev/tty) 2>/dev/null; then
    die 1 "no terminal to read from" "Run this command in a terminal."
fi
exec 3</dev/tty 4>/dev/tty

[[ -n ${HOME:-} ]] || die 1 "HOME is not set"
command -v curl >/dev/null 2>&1 || die 1 "curl is not installed" "Install curl and run this command again."
command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1 ||
    die 1 "no systemd user manager" \
        "$PROG keeps the token in ~/.config/environment.d, which needs a systemd user session."

# Where the entry goes, and whether the client's own command writes it.
case $family/$scope in
codex/user) cfg=${CODEX_HOME:-$HOME/.codex}/config.toml ;;
codex/project) cfg=$PWD/.codex/config.toml ;;
claude/user) cfg=$HOME/.claude.json ;;
claude/project) cfg=$PWD/.mcp.json ;;
grok/user) cfg=${GROK_HOME:-$HOME/.grok}/config.toml ;;
grok/project) cfg=$PWD/.grok/config.toml ;;
esac
use_cli=0
if command -v "$family" >/dev/null 2>&1; then
    # Codex's own command writes only the user's configuration.
    [[ $family/$scope == codex/project ]] || use_cli=1
fi
if [[ $family == claude ]] && ((!use_cli)) && ! command -v python3 >/dev/null 2>&1; then
    die 1 "cannot edit $cfg without python3" \
        "Install python3, or the Claude CLI, and run this command again."
fi

env_dir=$HOME/.config/environment.d
env_file=$env_dir/ikigenba.conf

# ---- confirm ---------------------------------------------------------------

printf 'ikigenba setup\n\n'
printf '  Client:   %s\n' "$label"
printf '  OS:       %s\n' "$os"
printf '  Scope:    %s, %s\n' "$scope" "$cfg"
printf '  Endpoint: %s\n\n' "$ENDPOINT"

printf 'Continue? [y/N] ' >&4
IFS= read -r answer <&3 || answer=
case ${answer,,} in
y | yes) ;;
*) die 1 "cancelled; nothing was changed" ;;
esac

# ---- the token -------------------------------------------------------------

tty_saved=
restore_tty() {
    if [[ -n $tty_saved ]]; then
        stty "$tty_saved" <&3 2>/dev/null
        tty_saved=
    fi
}

work=
writing=0
cleanup() {
    restore_tty
    if [[ -n $work ]]; then
        rm -rf -- "${work:?}"
    fi
}
trap cleanup EXIT
trap 'interrupted' INT TERM HUP

interrupted() {
    restore_tty
    printf '\n' >&4
    if ((writing)); then
        rollback
    fi
    die 1 "interrupted; nothing was changed"
}

printf 'Token from %s: ' "$TOKEN_URL" >&4
tty_saved=$(stty -g <&3 2>/dev/null) || tty_saved=
# No echo and one character at a time, set once so a paste never echoes.
stty -echo -icanon min 1 time 0 <&3 2>/dev/null
token=
while IFS= read -r -n 1 -d '' ch <&3; do
    case $ch in
    $'\n' | $'\r' | $'\x04') break ;;
    $'\x7f' | $'\b')
        if [[ -n $token ]]; then
            token=${token%?}
            printf '\b \b' >&4
        fi
        ;;
    $'\x15')
        while [[ -n $token ]]; do
            token=${token%?}
            printf '\b \b' >&4
        done
        ;;
    [[:cntrl:]] | '') ;;
    *)
        token+=$ch
        printf '*' >&4
        ;;
    esac
done
restore_tty
printf '\n' >&4

# A paste may carry spaces at either end.
token=${token#"${token%%[![:space:]]*}"}
token=${token%"${token##*[![:space:]]}"}
[[ -n $token ]] || die 1 "no token given; nothing was changed"

# The header goes to curl on its standard input, never in its arguments.
printf 'Checking the token with the gateway... '
status=$(printf 'Authorization: Bearer %s\n' "$token" |
    curl -sS -o /dev/null -w '%{http_code}' --max-time 30 \
        -X POST -H @- \
        -H 'Content-Type: application/json' \
        -H 'Accept: application/json, text/event-stream' \
        --data '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
        "$ENDPOINT" 2>/dev/null) || status=000
case $status in
2??) printf 'ok\n' ;;
401 | 403)
    printf 'failed\n'
    die 1 "the gateway refused the token" "Create a token at $TOKEN_URL and run this command again."
    ;;
*)
    printf 'failed\n'
    die 1 "cannot reach $ENDPOINT"
    ;;
esac

# ---- writes, undone together if one fails -----------------------------------

work=$(mktemp -d) || die 1 "cannot make a temporary directory; nothing was changed"
created_dirs=()
env_touched=0 env_had=0
session_touched=0 session_had=0 session_old= dbus_touched=0
cfg_touched=0 cfg_had=0

# make_dir DIR: create DIR and its missing parents, remembering each one made.
make_dir() {
    local d=$1 missing=() i
    while [[ ! -d $d ]]; do
        missing+=("$d")
        d=$(dirname -- "$d")
    done
    for ((i = ${#missing[@]} - 1; i >= 0; i--)); do
        mkdir -- "${missing[i]}" || return 1
        created_dirs+=("${missing[i]}")
    done
}

rollback() {
    local i
    if ((cfg_touched)); then
        if ((cfg_had)); then
            cat -- "$work/cfg" >"$cfg"
        else
            rm -f -- "$cfg"
        fi
    fi
    if ((session_touched)); then
        if ((session_had)); then
            IKIGENBA_TOKEN=$session_old systemctl --user import-environment IKIGENBA_TOKEN
        else
            systemctl --user unset-environment IKIGENBA_TOKEN
        fi
    fi 2>/dev/null
    if ((dbus_touched)); then
        IKIGENBA_TOKEN=$session_old dbus-update-activation-environment IKIGENBA_TOKEN >/dev/null 2>&1
    fi
    if ((env_touched)); then
        if ((env_had)); then
            cat -- "$work/env" >"$env_file"
        else
            rm -f -- "$env_file"
        fi
    fi
    for ((i = ${#created_dirs[@]} - 1; i >= 0; i--)); do
        rmdir -- "${created_dirs[i]}" 2>/dev/null
    done
}

# fail STEP [OUTPUT]: undo every write, then stop naming the step.
fail() {
    rollback
    writing=0
    if [[ -n ${2:-} ]]; then
        die 1 "$1 failed; nothing was changed" "$(quoted "$2")"
    fi
    die 1 "$1 failed; nothing was changed"
}

writing=1

# The token file, mode 0600 from the moment it exists.
make_dir "$env_dir" || fail "creating $env_dir"
if [[ -e $env_file ]]; then
    env_had=1
    cp -p -- "$env_file" "$work/env" || fail "saving $env_file"
fi
env_touched=1
tmp=$(mktemp "$env_dir/.ikigenba.conf.XXXXXX") || fail "writing $env_file"
if ! { chmod 600 "$tmp" && printf 'IKIGENBA_TOKEN=%s\n' "$token" >"$tmp" && mv -f -- "$tmp" "$env_file"; }; then
    rm -f -- "$tmp"
    fail "writing $env_file"
fi
printf 'Saved IKIGENBA_TOKEN in %s\n' "$env_file"

# The running session, from this script's environment, never its arguments.
if session_old=$(systemctl --user show-environment 2>/dev/null | grep '^IKIGENBA_TOKEN='); then
    session_had=1
    session_old=${session_old#IKIGENBA_TOKEN=}
fi
session_touched=1
out=$(IKIGENBA_TOKEN=$token systemctl --user import-environment IKIGENBA_TOKEN 2>&1) ||
    fail "systemctl --user import-environment" "$out"
if command -v dbus-update-activation-environment >/dev/null 2>&1 && [[ -n ${DBUS_SESSION_BUS_ADDRESS:-} ]]; then
    dbus_touched=1
    IKIGENBA_TOKEN=$token dbus-update-activation-environment IKIGENBA_TOKEN >/dev/null 2>&1 || dbus_touched=0
fi
printf 'Set IKIGENBA_TOKEN for this login session\n'

# The client's entry.
make_dir "$(dirname -- "$cfg")" || fail "creating $(dirname -- "$cfg")"
if [[ -e $cfg ]]; then
    cfg_had=1
    cp -p -- "$cfg" "$work/cfg" || fail "saving $cfg"
fi
cfg_touched=1

# toml_put ENTRY: replace the ikigenba table in $cfg, keeping everything else.
toml_put() {
    local kept=
    if ((cfg_had)); then
        kept=$(awk '
            /^[[:space:]]*\[/ {
                h = $0; gsub(/[[:space:]]/, "", h); sub(/#.*/, "", h)
                skip = (h ~ /^\[mcp_servers\.("ikigenba"|ikigenba)(\]|\.)/)
                servers = (h == "[mcp_servers]")
            }
            servers && /^[[:space:]]*("ikigenba"|ikigenba)[[:space:]]*=/ { next }
            !skip { print }
        ' "$work/cfg") || return 1
    fi
    if [[ -n $kept ]]; then
        printf '%s\n\n%s\n' "$kept" "$1" >"$cfg"
    else
        printf '%s\n' "$1" >"$cfg"
    fi
}

# json_put: replace mcpServers.ikigenba in $cfg, keeping everything else.
json_put() {
    python3 - "$cfg" "$ENDPOINT" <<'PY'
import json, os, sys
path, endpoint = sys.argv[1], sys.argv[2]
data = {}
if os.path.exists(path) and os.path.getsize(path) > 0:
    with open(path) as f:
        data = json.load(f)
if not isinstance(data, dict):
    sys.exit(path + " does not hold a JSON object")
servers = data.setdefault("mcpServers", dict())
if not isinstance(servers, dict):
    sys.exit(path + ": mcpServers is not an object")
servers["ikigenba"] = dict(type="http", url=endpoint,
                           headers=dict(Authorization="Bearer ${IKIGENBA_TOKEN}"))
with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PY
}

# Run the client's commands without the token in their environment.
case $family/$use_cli in
codex/1)
    out=$(env -u IKIGENBA_TOKEN codex mcp add ikigenba --url "$ENDPOINT" \
        --bearer-token-env-var IKIGENBA_TOKEN </dev/null 2>&1) || fail "codex mcp add" "$out"
    ;;
claude/1)
    env -u IKIGENBA_TOKEN claude mcp remove --scope "$scope" ikigenba </dev/null >/dev/null 2>&1
    out=$(env -u IKIGENBA_TOKEN claude mcp add --transport http --scope "$scope" ikigenba "$ENDPOINT" \
        --header 'Authorization: Bearer ${IKIGENBA_TOKEN}' </dev/null 2>&1) || fail "claude mcp add" "$out"
    ;;
grok/1)
    out=$(env -u IKIGENBA_TOKEN grok mcp add --transport http --scope "$scope" ikigenba "$ENDPOINT" \
        --header 'Authorization: Bearer ${IKIGENBA_TOKEN}' </dev/null 2>&1) || fail "grok mcp add" "$out"
    ;;
codex/0)
    toml_put "$(printf '[mcp_servers.ikigenba]\nurl = "%s"\nbearer_token_env_var = "IKIGENBA_TOKEN"' "$ENDPOINT")" ||
        fail "writing $cfg"
    ;;
grok/0)
    toml_put "$(printf '[mcp_servers.ikigenba]\nurl = "%s"\nenabled = true\nheaders = { "Authorization" = "Bearer ${IKIGENBA_TOKEN}" }' "$ENDPOINT")" ||
        fail "writing $cfg"
    ;;
claude/0)
    out=$(json_put 2>&1) || fail "writing $cfg" "$out"
    ((cfg_had)) || chmod 600 "$cfg"
    ;;
esac
writing=0
printf 'Added ikigenba to %s\n' "$cfg"

# ---- what the user does next -----------------------------------------------

printf '\nRestart %s, then tell your agent it'"'"'s back.\n' "$label"
printf 'If it can'"'"'t reach ikigenba, sign out of your desktop, sign back in, and restart it again.\n'
if [[ $scope == project ]]; then
    case $family in
    codex) printf 'Codex reads a project'"'"'s configuration only in a folder you trust.\n' ;;
    grok) printf 'Grok CLI reads a project'"'"'s configuration only in a folder you trust.\n' ;;
    esac
fi
exit 0
}
{{end}}
