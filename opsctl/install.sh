#!/usr/bin/env bash

set -u

program=opsctl-install
repository=https://github.com/ikigenba/ikigenba/releases/download/opsctl
binary_path=/usr/local/bin/opsctl
installer_path=/usr/local/share/ikigenba/opsctl-install.sh

if [[ $# -eq 0 ]]; then
  printf '%s: needs a version\n' "$program" >&2
  exit 2
fi
if [[ $# -ne 1 ]]; then
  printf '%s: needs exactly one version\n' "$program" >&2
  exit 2
fi

version=$1
if [[ $EUID -ne 0 ]]; then
  printf '%s: must run as root\n' "$program" >&2
  exit 3
fi

asset=opsctl-$version-linux-amd64
release_url=$repository/$version
scratch=/tmp/opsctl-install.$$
downloaded_binary=$scratch/$asset
candidate=$scratch/opsctl
checksums=$scratch/checksums.txt
candidate_stderr=$scratch/candidate.stderr
actual_checksum=$scratch/actual.sha256
command_stderr=/tmp/.opsctl-install.$$.stderr
downloaded_installer=$scratch/install.sh
staged_binary=/usr/local/bin/.opsctl-install.$$
staged_installer=/usr/local/share/ikigenba/.opsctl-install.$$

cleanup() {
  rm -rf -- "$scratch"
  rm -f -- "$command_stderr"
  rm -f -- "$staged_binary" "$staged_installer"
}
trap cleanup EXIT HUP INT TERM

write_failure_outcome() {
  local reason=$1
  reason=${reason//$'\r'/\\r}
  reason=${reason//$'\n'/\\n}
  printf 'install: failed: %s\n' "$reason" 2>/dev/null
}

fail_install() {
  local reason=$1
  local detail=${2:-}
  if ! write_failure_outcome "$reason"; then
    printf '%s: installation failed\n' "$program" >&2 || true
    exit 1
  fi
  printf '%s: installation failed\n' "$program" >&2 || exit 1
  if [[ -n $detail && -s $detail ]]; then
    printf '\n' >&2
    while IFS= read -r line || [[ -n $line ]]; do
      printf '> %s\n' "$line" >&2
    done < "$detail"
  fi
  exit 1
}

if ! install -d -m 0700 -- "$scratch" 2>"$command_stderr"; then
  fail_install 'could not create scratch directory' "$command_stderr"
fi

checksum_url=$release_url/checksums.txt
http_status=
if ! http_status=$(curl --fail --silent --show-error --location --write-out '%{http_code}' --output "$checksums" "$checksum_url" 2>"$command_stderr"); then
  if [[ $http_status == 404 ]]; then
    if ! write_failure_outcome "no release for $version"; then
      printf '%s: installation failed\n' "$program" >&2 || true
      exit 1
    fi
    printf '%s: installation failed\n\n%s: 404\n' "$program" "$checksum_url" >&2
    exit 1
  fi
  fail_install "could not download checksums for $version" "$command_stderr"
fi
if ! http_status=$(curl --fail --silent --show-error --location --write-out '%{http_code}' --output "$downloaded_binary" "$release_url/$asset" 2>"$command_stderr"); then
  fail_install "could not download $asset" "$command_stderr"
fi

expected=
matches=0
while IFS= read -r line || [[ -n $line ]]; do
  if [[ $line =~ ^([[:xdigit:]]{64})[[:space:]][[:space:]](.+)$ ]] && [[ ${BASH_REMATCH[2]} == "$asset" ]]; then
    expected=${BASH_REMATCH[1],,}
    ((matches += 1))
  fi
done < "$checksums"
if [[ $matches -ne 1 ]]; then
  fail_install "no checksum for $asset"
fi

actual=
if ! sha256sum "$downloaded_binary" >"$actual_checksum" 2>"$command_stderr"; then
  fail_install "could not checksum $asset" "$command_stderr"
fi
if ! read -r actual _ < "$actual_checksum"; then
  fail_install "could not checksum $asset" "$command_stderr"
fi
actual=${actual,,}
if [[ $actual != "$expected" ]]; then
  if ! write_failure_outcome "checksum mismatch for $asset"; then
    printf '%s: installation failed\n' "$program" >&2 || true
    exit 1
  fi
  printf '%s: installation failed\n\nexpected %s\n     got %s\n' "$program" "$expected" "$actual" >&2
  exit 1
fi

if ! install -m 0755 -- "$downloaded_binary" "$candidate" 2>"$command_stderr"; then
  fail_install "could not prepare $asset" "$command_stderr"
fi
reported=
if ! reported=$("$candidate" version 2>"$candidate_stderr"); then
  fail_install "$asset version command failed" "$candidate_stderr"
fi
if [[ $reported != "$version" ]]; then
  if ! write_failure_outcome "asked for $version but the binary reports $reported"; then
    printf '%s: installation failed\n' "$program" >&2 || true
    exit 1
  fi
  printf '%s: installation failed\n' "$program" >&2
  exit 1
fi

installed_version=
if [[ -x $binary_path ]]; then
  installed_version=$("$binary_path" version 2>/dev/null || true)
fi
had_saved=0
if [[ -f $installer_path ]]; then
  had_saved=1
fi
if [[ $had_saved -eq 1 && $installed_version != "$version" ]]; then
  if ! http_status=$(curl --fail --silent --show-error --location --write-out '%{http_code}' --output "$downloaded_installer" "$release_url/install.sh" 2>"$command_stderr"); then
    fail_install "could not download install.sh for $version" "$command_stderr"
  fi
fi

if ! install -d -m 0755 -- /usr/local/bin /usr/local/share/ikigenba 2>"$command_stderr"; then
  fail_install 'could not create installation directories' "$command_stderr"
fi
if ! install -m 0755 -- "$candidate" "$staged_binary" 2>"$command_stderr"; then
  fail_install 'could not stage opsctl' "$command_stderr"
fi
if [[ $had_saved -eq 0 ]]; then
  if ! install -m 0755 -- "$0" "$staged_installer" 2>"$command_stderr"; then
    fail_install 'could not stage installer' "$command_stderr"
  fi
elif [[ $installed_version != "$version" ]]; then
  if ! install -m 0755 -- "$downloaded_installer" "$staged_installer" 2>"$command_stderr"; then
    fail_install 'could not stage installer' "$command_stderr"
  fi
fi

if ! mv -f -- "$staged_binary" "$binary_path" 2>"$command_stderr"; then
  fail_install 'could not publish opsctl' "$command_stderr"
fi
if [[ -f $staged_installer ]]; then
  if ! mv -f -- "$staged_installer" "$installer_path" 2>"$command_stderr"; then
    fail_install 'could not publish installer' "$command_stderr"
  fi
fi

if ! printf 'install: ok (opsctl %s -> /usr/local/bin/opsctl)\n' "$version" 2>/dev/null; then
  printf '%s: installation failed\n' "$program" >&2 || true
  exit 1
fi
