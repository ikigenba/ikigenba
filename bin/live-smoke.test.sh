#!/usr/bin/env bash
#
# live-smoke.test.sh — starts the local suite, verifies the staged runtime
# manifest root and the dashboard's public /services inventory, then tears down
# only the suite started by this script.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
RUN_DIR="$REPO/tmp"
MANIFEST_ROOT="$RUN_DIR/opt"
REPO_REAL="$(realpath -m "$REPO")"
RUN_DIR_REAL="$(realpath -m "$RUN_DIR")"

SERVICES=(dashboard crm ledger notify prompts wiki dropbox cron gmail scripts sites webhooks)
MCP_SERVICES=(crm ledger notify prompts wiki dropbox cron gmail scripts sites webhooks)
BASE_PORTS=(3000 3100 3101 3201 3002 3001 3200 3005 3202 3003 3004 3006 8080)
BASE_SERVICE_PORTS=(dashboard:3000 crm:3100 ledger:3101 notify:3201 prompts:3002 wiki:3001 dropbox:3200 cron:3005 gmail:3202 scripts:3003 sites:3004 webhooks:3006 nginx:8080)
PORT_OFFSET="${APPKIT_PORT_OFFSET:-0}"
PORT_OFFSET_EXPLICIT=0
[ "${APPKIT_PORT_OFFSET+x}" = x ] && PORT_OFFSET_EXPLICIT=1

fails=0
started=0

case "$PORT_OFFSET" in
	'' | *[!0-9]*)
		echo "FAIL - APPKIT_PORT_OFFSET must be a non-negative integer"
		exit 1
		;;
esac

port() {
	echo "$(($1 + PORT_OFFSET))"
}

cleanup() {
	if [ "$started" -eq 1 ]; then
		"$HERE/stop" --clean >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

port_open() {
	(exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

port_pids() {
	ss -ltnp "sport = :$1" 2>/dev/null | grep -o 'pid=[0-9][0-9]*' | cut -d= -f2 | sort -u
}

port_owners() {
	ss -ltnp "sport = :$1" 2>/dev/null || true
}

pid_cmdline() {
	tr '\0' ' ' <"/proc/$1/cmdline" 2>/dev/null || true
}

proc_link_real() {
	local target
	target="$(readlink "/proc/$1/$2" 2>/dev/null || true)"
	target="${target% (deleted)}"
	[ -n "$target" ] && realpath -m "$target"
}

path_under() {
	local path="$1" root="$2"
	[[ "$path" == "$root" ]] || [[ "$path" == "$root/"* ]]
}

port_owner_details() {
	local port="$1" pid cmd exe cwd
	while read -r pid; do
		[ -n "$pid" ] || continue
		cmd="$(pid_cmdline "$pid")"
		exe="$(proc_link_real "$pid" exe)"
		cwd="$(proc_link_real "$pid" cwd)"
		echo "       pid=$pid exe=$exe cwd=$cwd cmd=$cmd"
	done < <(port_pids "$port")
}

pid_from_this_worktree() {
	local pid="$1" cmd exe cwd
	cmd="$(pid_cmdline "$pid")"
	exe="$(proc_link_real "$pid" exe)"
	cwd="$(proc_link_real "$pid" cwd)"

	path_under "$exe" "$RUN_DIR_REAL/bin" ||
		[[ "$cmd" == "$RUN_DIR/bin/"* ]] ||
		[[ "$cmd" == *" $RUN_DIR/bin/"* ]] ||
		[[ "$cmd" == "$RUN_DIR_REAL/bin/"* ]] ||
		[[ "$cmd" == *" $RUN_DIR_REAL/bin/"* ]] ||
		[[ "$cmd" == *"$REPO/nginx"* ]] ||
		[[ "$cmd" == *"$REPO_REAL/nginx"* ]] ||
		{ [[ "$cmd" == nginx:* ]] && path_under "$cwd" "$REPO_REAL"; }
}

worktree_pid_for_port() {
	local port="$1" pid
	while read -r pid; do
		[ -n "$pid" ] || continue
		if pid_from_this_worktree "$pid"; then
			echo "$pid"
			return 0
		fi
	done < <(port_pids "$port")
	return 1
}

tracked_stack_running() {
	local pf pid
	for pf in "$RUN_DIR"/*.pid; do
		[ -e "$pf" ] || continue
		pid="$(cat "$pf" 2>/dev/null || true)"
		[ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && return 0
	done
	return 1
}

recover_worktree_pidfiles() {
	local spec name base pid recovered=1

	for spec in "${BASE_SERVICE_PORTS[@]}"; do
		name="${spec%%:*}"
		base="${spec##*:}"
		pid="$(worktree_pid_for_port "$(port "$base")" || true)"
		[ -n "$pid" ] || continue

		mkdir -p "$RUN_DIR"
		echo "$pid" >"$RUN_DIR/$name.pid"
		recovered=0
	done

	return "$recovered"
}

assert_ok() {
	local label="$1"
	shift
	if "$@"; then
		echo "ok   - $label"
	else
		echo "FAIL - $label"
		fails=$((fails + 1))
	fi
}

chosen_ports_available() {
	local base
	for base in "${BASE_PORTS[@]}"; do
		if port_open "$(port "$base")"; then
			return 1
		fi
	done
	return 0
}

choose_port_offset() {
	local offset base busy

	for offset in 10000 11000 12000 13000 14000 15000 16000 17000 18000 19000 20000; do
		busy=0
		for base in "${BASE_PORTS[@]}"; do
			if port_open "$((base + offset))"; then
				busy=1
				break
			fi
		done
		if [ "$busy" -eq 0 ]; then
			PORT_OFFSET="$offset"
			return 0
		fi
	done

	return 1
}

assert_contains() {
	local label="$1" needle="$2" haystack="$3"
	if [[ "$haystack" == *"$needle"* ]]; then
		echo "ok   - $label"
	else
		echo "FAIL - $label: missing [$needle]"
		fails=$((fails + 1))
	fi
}

recover_worktree_pidfiles || true
if tracked_stack_running; then
	"$HERE/stop" --clean
fi

if ! chosen_ports_available; then
	if [ "$PORT_OFFSET_EXPLICIT" -eq 1 ]; then
		for base in "${BASE_PORTS[@]}"; do
			candidate="$(port "$base")"
			if port_open "$candidate"; then
				echo "FAIL - port :$candidate is already in use by another worktree; not stopping it"
				port_owners "$candidate" | sed 's/^/       /'
				port_owner_details "$candidate"
			fi
		done
		exit 1
	fi

	for base in "${BASE_PORTS[@]}"; do
		if port_open "$base"; then
			echo "ok   - default port :$base is already in use by another worktree; not stopping it"
			port_owners "$base" | sed 's/^/       /'
			port_owner_details "$base"
		fi
	done
	if choose_port_offset; then
		echo "ok   - using APPKIT_PORT_OFFSET=$PORT_OFFSET for this worktree smoke"
	else
		echo "FAIL - no free alternate port block found for this worktree smoke"
		exit 1
	fi
fi

started=1
if ! APPKIT_PORT_OFFSET="$PORT_OFFSET" "$HERE/start"; then
	echo "FAIL - bin/start exited non-zero"
	exit 1
fi

# R-YRNV-ET9B
for svc in "${SERVICES[@]}"; do
	current="$MANIFEST_ROOT/$svc/etc/current"
	manifest="$current/manifest.env"
	target="$(readlink "$current" 2>/dev/null || true)"

	assert_ok "$svc current is a symlink" test -L "$current"
	assert_ok "$svc current manifest exists" test -f "$manifest"
	assert_ok "$svc current symlink is relative" test "${target#/}" = "$target"
done

dashboard_pid="$(cat "$RUN_DIR/dashboard.pid" 2>/dev/null || true)"
if [ -n "$dashboard_pid" ] && [ -r "/proc/$dashboard_pid/environ" ]; then
	dashboard_env="$(tr '\0' '\n' <"/proc/$dashboard_pid/environ")"
	assert_contains "dashboard uses staged manifest root" "DASHBOARD_MANIFEST_ROOT=$MANIFEST_ROOT" "$dashboard_env"
	assert_contains "dashboard inherits staged registry root" "REGISTRY_ROOT=$MANIFEST_ROOT" "$dashboard_env"
else
	echo "FAIL - cannot inspect dashboard process environment"
	fails=$((fails + 1))
fi

services_json="$(curl -fsS "http://127.0.0.1:$(port 3000)/services" 2>/dev/null || true)"
for svc in "${MCP_SERVICES[@]}"; do
	assert_contains "/services lists $svc" "\"name\":\"$svc\"" "$services_json"
done

echo
if [ "$fails" -eq 0 ]; then
	echo "PASS"
	exit 0
else
	echo "FAIL ($fails failing assertions)"
	exit 1
fi
