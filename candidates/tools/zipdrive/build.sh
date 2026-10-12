#!/usr/bin/env bash
# Builds the ZIP drive-time table: driving minutes from Sandstone city, MN
# (its Census place interior point) to the interior point of every ZCTA whose
# code begins 53, 54, 55 or 56.
# Human-maintained; see README.md beside this script.
#
# Usage: build.sh [-o OUTPUT] [WORKDIR]
#   OUTPUT   CSV path, or - for stdout (default: ../../share/zip_drive.csv
#            relative to this script)
#   WORKDIR  where inputs are downloaded and OSRM files are built (default: a
#            fresh mktemp directory, kept afterwards). Inputs already present
#            in WORKDIR are reused.
set -euo pipefail

OSRM_IMAGE="ghcr.io/project-osrm/osrm-backend:v26.10.0-debian@sha256:55dbfd47d984c2cacd64e32901b4321c86b5ec761438e69aa053dc78698a786f"
OSMIUM_IMAGE="stefda/osmium-tool:latest@sha256:d2321d0e926f77ead7547b4b35f5cf98d9fd74043673cecc4fc2bb7cce06ff63"
GAZETTEER_URL="https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2024_Gazetteer/2024_Gaz_zcta_national.zip"
GAZETTEER_TXT="2024_Gaz_zcta_national.txt"
PLACES_URL="https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2024_Gazetteer/2024_gaz_place_27.txt"
GEOFABRIK_BASE="https://download.geofabrik.de/north-america/us"
STATES=(minnesota wisconsin)
ORIGIN_GEOID="2758396" # Sandstone city, MN
ZIP_PATTERN='^5[3-6]'
MAX_TABLE_SIZE=10000

prog="build.sh"
die() { printf '%s: %s\n' "$prog" "$1" >&2; exit "${2:-1}"; }
say() { printf '%s: %s\n' "$prog" "$1" >&2; }
usage() { printf 'usage: %s [-o OUTPUT] [WORKDIR]\n' "$prog"; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
output="$script_dir/../../share/zip_drive.csv"
while getopts ':o:h' opt; do
  case "$opt" in
    o) output="$OPTARG" ;;
    h) usage; exit 0 ;;
    :) die "option -$OPTARG needs a value; see $prog -h" 2 ;;
    *) die "unknown option -$OPTARG; see $prog -h" 2 ;;
  esac
done
shift $((OPTIND - 1))
[ $# -le 1 ] || die "too many arguments; see $prog -h" 2

for cmd in docker curl unzip jq awk; do
  command -v "$cmd" >/dev/null || die "missing required command: $cmd"
done

if [ $# -eq 1 ]; then
  work="$1"
  mkdir -p "$work" 2>/dev/null || die "cannot create work dir $work"
else
  work="$(mktemp -d "${TMPDIR:-/tmp}/zipdrive.XXXXXX")" || die "cannot create a work dir"
fi
work="$(cd "$work" && pwd)" || die "cannot enter work dir $work"
say "work dir: $work"

container=""
cleanup() {
  if [ -n "$container" ]; then
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# step WHAT CMD...: runs CMD with its stdout in $work/step.out and its stderr
# in $work/step.err. On failure it reports "build.sh: WHAT failed", then one
# empty line, then the command's output quoted with "> ", and exits 1.
step() {
  local what="$1"; shift
  if ! "$@" > "$work/step.out" 2> "$work/step.err"; then
    {
      printf '%s: %s failed\n' "$prog" "$what"
      if [ -s "$work/step.err" ] || [ -s "$work/step.out" ]; then
        printf '\n'
        cat "$work/step.err" "$work/step.out" | tail -n 40 | sed 's/^/> /'
      fi
    } >&2
    exit 1
  fi
}

fetch() { # url dest
  if [ -s "$2" ]; then
    say "reusing $(basename "$2")"
  else
    say "downloading $1"
    step "download of $1" curl -fsSL --retry 3 -o "$2.part" "$1"
    step "rename of $2.part" mv "$2.part" "$2"
  fi
}

# 1. Inputs.
fetch "$GAZETTEER_URL" "$work/gazetteer.zip"
step "unzip of $GAZETTEER_TXT" unzip -o -q "$work/gazetteer.zip" "$GAZETTEER_TXT" -d "$work"
fetch "$PLACES_URL" "$work/places_mn.txt"
pbfs=()
for s in "${STATES[@]}"; do
  fetch "$GEOFABRIK_BASE/$s-latest.osm.pbf" "$work/$s-latest.osm.pbf"
  pbfs+=("/data/$s-latest.osm.pbf")
done

run() { # image args...
  local image="$1"; shift
  docker run --rm --user "$(id -u):$(id -g)" -v "$work:/data" "$image" "$@"
}

# 2. Merge the extracts and build the MLD graph.
say "merging extracts"
step "osmium merge" run "$OSMIUM_IMAGE" osmium merge --overwrite -o /data/region.osm.pbf "${pbfs[@]}"
for s in "${STATES[@]}"; do
  step "osmium fileinfo of $s" run "$OSMIUM_IMAGE" osmium fileinfo "/data/$s-latest.osm.pbf"
  ts="$(sed -n 's/^ *osmosis_replication_timestamp=//p' "$work/step.out")"
  say "$s extract timestamp: ${ts:-unknown}"
done
say "osrm-extract"
step osrm-extract run "$OSRM_IMAGE" osrm-extract -p /opt/car.lua /data/region.osm.pbf
say "osrm-partition"
step osrm-partition run "$OSRM_IMAGE" osrm-partition /data/region.osrm
say "osrm-customize"
step osrm-customize run "$OSRM_IMAGE" osrm-customize /data/region.osrm

# 3. Coordinates: origin first, then every ZCTA in range sorted by code.
awk -F'\t' -v pat="$ZIP_PATTERN" '
  NR == 1 {
    for (i = 1; i <= NF; i++) { h = $i; gsub(/[ \t\r]+/, "", h); col[h] = i }
    if (!("GEOID" in col) || !("INTPTLAT" in col) || !("INTPTLONG" in col)) exit 3
    next
  }
  {
    z = $col["GEOID"]; gsub(/[ \r]+/, "", z)
    lat = $col["INTPTLAT"]; gsub(/[ \r]+/, "", lat)
    lon = $col["INTPTLONG"]; gsub(/[ \r]+/, "", lon)
    if (z ~ pat) print z "\t" lon "," lat
  }' "$work/$GAZETTEER_TXT" | sort -t$'\t' -k1,1 > "$work/zctas.tsv" \
  || die "cannot read GEOID, INTPTLAT and INTPTLONG from the gazetteer"
origin="$(awk -F'\t' -v g="$ORIGIN_GEOID" '
  NR == 1 { for (i = 1; i <= NF; i++) { h = $i; gsub(/[ \t\r]+/, "", h); col[h] = i }; next }
  {
    id = $col["GEOID"]; gsub(/[ \r]+/, "", id)
    lat = $col["INTPTLAT"]; gsub(/[ \r]+/, "", lat)
    lon = $col["INTPTLONG"]; gsub(/[ \r]+/, "", lon)
    if (id == g) print lon "," lat
  }' "$work/places_mn.txt")"
[ -n "$origin" ] || die "origin place $ORIGIN_GEOID not in the places gazetteer"
count="$(wc -l < "$work/zctas.tsv")"
[ "$count" -lt "$MAX_TABLE_SIZE" ] || die "$count ZCTAs exceed max table size $MAX_TABLE_SIZE"
say "$count destination ZCTAs; origin place $ORIGIN_GEOID at $origin"
coords="$origin;$(cut -f2 "$work/zctas.tsv" | paste -sd';')"

# 4. Serve and query once.
step "start of osrm-routed" docker run -d --rm --user "$(id -u):$(id -g)" -v "$work:/data" -p 127.0.0.1::5000 \
  "$OSRM_IMAGE" osrm-routed --algorithm mld --max-table-size "$MAX_TABLE_SIZE" /data/region.osrm
container="$(cat "$work/step.out")"
step "docker port" docker port "$container" 5000/tcp
port="$(head -n1 "$work/step.out" | sed 's/.*://')"
url="http://127.0.0.1:$port"
say "waiting for osrm-routed on $url"
for _ in $(seq 1 120); do
  curl -fs "$url/nearest/v1/driving/$origin" >/dev/null 2>&1 && break
  docker inspect "$container" >/dev/null 2>&1 || die "osrm-routed exited during startup"
  sleep 1
done
step "readiness check of osrm-routed" curl -fsS -o /dev/null "$url/nearest/v1/driving/$origin"

say "requesting table (one GET)"
step "table request" curl -sS -o "$work/table.json" "$url/table/v1/driving/$coords?sources=0&annotations=duration"
step "parse of table response" jq -e . "$work/table.json"
[ "$(jq -r .code "$work/table.json")" = "Ok" ] || die "table response code: $(jq -r '.code + " " + (.message // "")' "$work/table.json")"
n="$(jq '.durations[0] | length' "$work/table.json")"
[ "$n" -eq $((count + 1)) ] || die "table returned $n durations for $((count + 1)) coordinates"

# 5. Pair durations (skipping the origin's own column) with zips; drop nulls.
pair() {
  jq -r '.durations[0][1:][] | if . == null then "null" else tostring end' "$work/table.json" \
    | paste <(cut -f1 "$work/zctas.tsv") - \
    | awk -F'\t' 'BEGIN { print "zip,minutes" } $2 != "null" { printf "%s,%d\n", $1, int($2 / 60 + 0.5) }' \
    > "$work/zip_drive.csv"
}
step "pairing of durations with zips" pair
rows=$(($(wc -l < "$work/zip_drive.csv") - 1))
say "$rows of $count ZCTAs routed"

if [ "$output" = "-" ]; then
  cat "$work/zip_drive.csv"
else
  step "creation of $(dirname "$output")" mkdir -p "$(dirname "$output")"
  step "write of $output" cp "$work/zip_drive.csv" "$output"
  say "wrote $output"
fi
