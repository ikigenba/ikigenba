# D16-release-layout

A suite release reaches a host as one folder, `/opt/ikigenba/releases/<sha>/`,
named by the full 40-character lowercase commit sha it was built from. devctl
unpacks it; opsctl never fetches or unpacks a release. The folder holds
`release.json` (the suite build's record, whose `sha` member names the commit),
`opsctl/bin/opsctl`, the opsctl that belongs to that release, and one *app tree*
per app, `<app>/`, holding only `bin/`, `libexec/`, `lib/`, `share/` and `etc/`,
with `bin/` holding exactly the app's own binary. After the release's first
`activate` it may also hold `label`, the display label the last activate of
that sha gave. Package `internal/release` owns this layout and reading it; it
imports nothing else of opsctl, so the app model (D08) and every command can use
it.

Two symbolic links beside `releases/` are the only record of what the host
runs: `/opt/ikigenba/current` names the running release's folder and
`/opt/ikigenba/previous` the one before it, each as the relative target
`releases/<sha>`, so a link means the same thing under any root. Nothing else
on the host says which release runs. `/usr/local/bin/opsctl` is a link to
`/opt/ikigenba/current/opsctl/bin/opsctl`, so `opsctl` on the PATH is always
current's, through every later activate and rollback. A host where `current`
exists is a *released host* (D08's `Layout`).

A release is shown as its label and short sha, `r142 (c604e32)`, or the short
sha alone, the first seven characters, when it has no label. An opsctl finds the
release it belongs to from where its own executable is, symbolic links
resolved; one that is not inside a release folder belongs to none. That is how
`opsctl version` answers (D02) and how `activate` and `rollback` know they are
run by the right opsctl (D18).

The layout only grows. A later opsctl may add members to `release.json` and
entries beside the links and folders; an older one ignores what it does not
know, and retention (D18) judges only folders named by a full sha. An entry of `releases/` whose name begins with `.` is never a release: devctl unpacks into `releases/.unpack.XXXXXXXXXX` and renames it into place, and everything that lists `releases/` ignores such an entry.

## REQUIREMENTS

- R-9OPF-VAST: Package `internal/release` MUST export the constants `Dir = "/opt/ikigenba"`, `ReleasesDir = "/opt/ikigenba/releases"`, `CurrentLink = "/opt/ikigenba/current"`, `PreviousLink = "/opt/ikigenba/previous"`, `OpsctlLink = "/usr/local/bin/opsctl"`, `CurrentOpsctl = "/opt/ikigenba/current/opsctl/bin/opsctl"`, `OpsctlPath = "opsctl/bin/opsctl"`, `MetadataName = "release.json"`, and `LabelName = "label"`.
- R-9PXC-92JI: A *full sha* MUST be a string of exactly 40 characters each in `0`-`9` or `a`-`f`; a *release folder* MUST be `ReleasesDir` followed by `/<sha>` for a full sha, resolved under the root an operation is given, and an entry of `ReleasesDir` whose name begins with `.` MUST never be taken for one or otherwise acted on by any operation that lists `ReleasesDir`; its *metadata file* is `<folder>/release.json`, its *label file* `<folder>/label`, its *opsctl* `<folder>/opsctl/bin/opsctl`, and every other immediate entry of it that is a directory, judged without following a symbolic link, except `opsctl`, is the *app tree* of the app it names; an immediate symbolic link, even to a directory, is no app tree.
- R-9R58-MUA7: Package `internal/release` MUST export `ValidSHA(s string) bool`, returning true exactly when `s` is a full sha, so an uppercase, 39-character, or 41-character string is false.
- R-9SD5-0M0W: Package `internal/release` MUST export `ValidLabel(s string) bool`, returning true exactly when `s` is one or more characters each an ASCII letter, an ASCII digit, `.`, `_`, `/`, or `-`, so `r142`, `r142-rc1`, `v1.2_3`, and `rc/142` are true and the empty string, `r 142`, `r:142`, and a string holding a non-ASCII letter are false. A label holding `/` is written to the label file and to `IKIGENBA_RELEASE` as it is, unquoted, since systemd.exec(5) reads a bare `EnvironmentFile=` value literally apart from backslash escapes and surrounding whitespace, neither of which a label can hold.
- R-9TL1-EDRL: Package `internal/release` MUST export `Release` as a struct with `SHA string` and `Label string` fields, and the methods `(Release) Short() string` and `(Release) Display() string`.
- R-9USX-S5IA: `Release.Short` MUST return the first seven characters of `SHA`, or `SHA` whole when it is shorter, and `Release.Display` MUST return `Short()` when `Label` is empty, and otherwise `Label`, ` (`, `Short()`, and `)`, so `Release{SHA: "c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18", Label: "r142"}` gives `r142 (c604e32)` and the same with an empty `Label` gives `c604e32`.
- R-9W0U-5X8Z: Package `internal/release` MUST export the sentinel error `ErrNoMetadata` and `Read(root, sha string) (Release, error)`.
- R-9X8Q-JOZO: `Read` MUST read the metadata file of the release folder for `sha` under `root` and return a `Release` whose `SHA` is the string value of the file's top-level `sha` member, ignoring every other member; when the file is absent, cannot be read, or is not one JSON object whose `sha` member is a string, it MUST return an error matching `ErrNoMetadata` through `errors.Is`. `Read` MUST NOT require that member to equal `sha`: a caller compares them.
- R-9YGM-XGQD: The `Label` that `Read`, `Current`, `Previous`, and `Of` return MUST be the release folder's label file content with one trailing LF removed when that result is accepted by `ValidLabel`, and empty when the file is absent, cannot be read, or its content so trimmed is not accepted by `ValidLabel`; a label file never makes these functions fail.
- R-A0WF-P07R: Package `internal/release` MUST export `Current(root string) (Release, bool, error)` and `Previous(root string) (Release, bool, error)`.
- R-A24C-2RYG: `Current` and `Previous` MUST examine `CurrentLink` and `PreviousLink` respectively under `root` without following them first: when no entry exists there they MUST return a zero `Release`, false, and a nil error; when the entry is a symbolic link whose target, read without resolving it, is exactly `releases/` followed by a full sha, they MUST return a `Release` whose `SHA` is that sha and whose `Label` is that release folder's label (R-9YGM-XGQD), true, and a nil error, whether or not the folder or its metadata file exists; for any other entry they MUST return false and an error whose `Error()` is exactly `/opt/ikigenba/current does not name a release` or `/opt/ikigenba/previous does not name a release` respectively, the host path written rather than resolved under `root`.
- R-A3C8-GJP5: Package `internal/release` MUST export `Of(root, executable string) (Release, bool)`.
- R-SAHR-MESY: `Of` MUST return true exactly when `executable` resolved under `root` (R-S99V-8N29) equals the opsctl of the release folder for some full sha `<sha>` resolved under `root`, so `<root>/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl` reached directly or through `current`, `previous`, or `/usr/local/bin/opsctl` under `root` all qualify, and `Read(root, <sha>)` succeeds with `SHA` equal to `<sha>`; it MUST then return `Release{SHA: <sha>, Label: <label>}` with that folder's label. In every other case, including an executable that does not resolve, one outside every release folder, and a release folder whose metadata file is missing or names another sha, it MUST return a zero `Release` and false.
- R-A5S1-836J: Package `internal/release` MUST export `LinkOpsctl(root, target string) error`.
- R-A6ZX-LUX8: `LinkOpsctl` MUST leave `OpsctlLink` under `root` a symbolic link whose target is exactly `target`, unresolved, replacing whatever entry was there, a regular file or a link, by creating a temporary symbolic link in `/usr/local/bin/` and renaming it over `opsctl`, so a reader finds either the old entry or the new link and never no entry; it MUST create `/usr/local/bin/` and any missing ancestor with mode `0755` when absent, never follow an existing link at `OpsctlLink`, leave no temporary entry behind, and change nothing else.
- R-S99V-8N29: A path is *resolved under a root* when every symbolic link in it is followed with a relative target taken against the link's own directory and an absolute target taken under the root, never leaving the root, so `/usr/local/bin/opsctl`, a link whose target is the absolute host path `/opt/ikigenba/current/opsctl/bin/opsctl`, resolves under a temporary root to `<root>/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl` when `current` there names `releases/<sha>`; a path given as a path on the machine, such as an executable's, is resolved under the root by resolving its part below the root, and a path that does not lie below the root, that names a missing entry, or whose links loop does not resolve.
- R-A87T-ZMNX: `Read`, `Current`, `Previous`, and `Of` MUST perform no filesystem write, process execution, or network access, and MUST read nothing outside `ReleasesDir`, `CurrentLink`, `PreviousLink`, and, for `Of`, the path given and the links it passes through, all under `root`.
