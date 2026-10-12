# zipdrive

`build.sh` builds `candidates/share/zip_drive.csv`, the ZIP drive-time table the `candidates` app embeds. Each row is a ZIP Code Tabulation Area (ZCTA) whose code begins 53, 54, 55 or 56 and the driving time in whole minutes (rounded to nearest) from Sandstone city, MN to it. The CSV has the header `zip,minutes`, is sorted by zip, and omits any ZCTA OSRM could not route. The current table has 1,664 rows, the gazetteer's full set of ZCTAs in 53–56; none was unroutable.

This script is human-maintained: it is not derived from `specs/` and the build run does not write it.

## Method

The origin is the interior point of Sandstone city, MN (place GEOID 2758396) in the Census 2024 Minnesota places gazetteer: latitude 46.129036, longitude -92.864553. The script reads it from that file. It does not use the interior point of ZCTA 55072, because that point lies about 22 km east of town in rural land; routing from it added 7 to 31 minutes to the times spot-checked. Each destination is its ZCTA's interior point (`INTPTLAT`, `INTPTLONG`), so the 55072 row is the drive from town to that rural point (31 minutes), not zero.

The script merges the Minnesota and Wisconsin OpenStreetMap extracts with osmium, builds an OSRM graph with the stock car profile (`/opt/car.lua`, MLD algorithm), serves it locally in a container, and makes one OSRM `table` GET request (`sources=0&annotations=duration`) covering all destinations. The routing server is removed on exit.

The car profile is untuned and its times stand as OSRM gives them. They run conservative against consumer maps, most on rural roads: for example 104 minutes to 55401 (Minneapolis) and 195 to 54701 (Eau Claire).

## Inputs used

- Census 2024 ZCTA gazetteer: https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2024_Gazetteer/2024_Gaz_zcta_national.zip (Last-Modified 2024-08-30). It lists 1,664 ZCTAs in 53–56.
- Census 2024 Minnesota places gazetteer (origin): https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2024_Gazetteer/2024_gaz_place_27.txt (Last-Modified 2024-08-30).
- Geofabrik Minnesota extract: https://download.geofabrik.de/north-america/us/minnesota-latest.osm.pbf (replication timestamp 2026-10-11T20:21:06Z).
- Geofabrik Wisconsin extract: https://download.geofabrik.de/north-america/us/wisconsin-latest.osm.pbf (replication timestamp 2026-10-11T20:21:06Z).
- OSRM image: `ghcr.io/project-osrm/osrm-backend:v26.10.0-debian@sha256:55dbfd47d984c2cacd64e32901b4321c86b5ec761438e69aa053dc78698a786f` (osrm-routed v26.10.0).
- osmium image: `stefda/osmium-tool:latest@sha256:d2321d0e926f77ead7547b4b35f5cf98d9fd74043673cecc4fc2bb7cce06ff63` (osmium 1.7.1; the image publishes only `latest`, so the digest is the pin).

The current table was built on 2026-10-12. The `-latest` extracts change daily, so a rebuild routes over newer map data; the script logs each extract's timestamp to stderr.

## Host dependencies

Docker, bash, curl, unzip, jq and awk, plus coreutils (`sort`, `cut`, `paste`, `mktemp`). About 4 GB of disk in the work dir and a few GB of memory. A full run took about 3.5 minutes on a 48-core machine with downloads, and under 3 minutes with the inputs reused.

## Running

```
candidates/tools/zipdrive/build.sh [-o OUTPUT] [WORKDIR]
```

`OUTPUT` defaults to `candidates/share/zip_drive.csv` (resolved from the script's location); `-o -` writes the CSV to stdout. `WORKDIR` defaults to a fresh `mktemp -d` directory, which is kept; inputs already in it are reused, so pass the same one to rebuild without downloading. Progress and errors go to stderr, prefixed `build.sh: `, and the script exits non-zero on failure.
