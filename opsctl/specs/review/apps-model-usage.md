# Apps model consumer review

## Discover routing and database candidates from the same host

```go
package example

import (
    "fmt"
    "io"

    "github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

func describe(root string, out io.Writer) error {
    services, err := apps.Discover(root)
    if err != nil { return err }
    for _, service := range services {
        if service.ManifestError != nil { return service.ManifestError }
        manifest := service.Manifest
        if manifest == nil {
            fmt.Fprintf(out, "%s: files only\n", service.Name)
            continue
        }
        if manifest.Port != 0 {
            fmt.Fprintf(out, "%s: port %d, default %t\n", service.Name, manifest.Port, manifest.Default)
        }
        if manifest.Database != nil {
            fmt.Fprintf(out, "%s: %s at %s\n", service.Name, manifest.Database.Engine, manifest.Database.Path)
        }
    }
    return nil
}
```

This consumer fails when it cannot safely interpret a manifest; status can
instead retain the row and report an unavailable database field. D08 does not
require all consumers to apply the same error policy.

## Validate artifact manifest input before installation

```go
func installationManifest(data []byte) (apps.Manifest, error) {
    manifest, err := apps.ParseManifest(data)
    if err != nil { return apps.Manifest{}, err }
    if err := apps.ValidateName(manifest.App); err != nil { return apps.Manifest{}, err }
    if manifest.Port == 0 { return apps.Manifest{}, fmt.Errorf("app manifest needs port") }
    return manifest, nil
}
```

The caller supplies the artifact's manifest bytes through the installation
contract. A discovered backup-only manifest can omit app/port; an installation
requires both. The complete artifact download and lifecycle task is D09's
consumer exercise. Secret retrieval and environment serialization consume
`Manifest.Secrets` and `Manifest.Env` there.

## Provenance and criteria

Sources: `specs/stories/apps.md` introduction; status discovery stories;
`specs/stories/backup.md` introduction and “What is backed up, and what is not”;
`specs/stories/nginx.md` introduction (port-bearing manifests).

| Source criterion | D08 requirements / downstream owner |
|---|---|
| apps intro: app identity and prohibited names; input cannot trust prior build | R-A2TS-K4M0, R-XUVN-2GXA; D09 invokes validation before mutation |
| apps intro: TOML fields app/port/default/secrets/env/database | R-XNK8-RUH4, R-XOS5-5M7T, R-XSFU-AXFW, R-XTNQ-OP6L, R-XUVN-2GXA, R-XW3J-G8NZ |
| apps intro: artifact layout and no archive version | D09 artifact layout; R-Y0Z4-ZBMR denies manifest version authority; D10 executes binary |
| apps intro: at most one default, zero permitted | D09 conflict prevention; nginx renders zero-default baseline; D08 provides Default field |
| apps intro: plain env differs from retrieved secret values | D09 retrieval/writing; D08 distinguishes Secrets and Env |
| apps intro: database is app fact used for regeneration | R-XNK8-RUH4, R-XW3J-G8NZ; D11 consumes model |
| apps intro: schema/migrations/seeds belong app | R-Y0Z4-ZBMR for model; D09/D13 lifecycle preservation and start ordering |
| apps intro: host is read directly, no remote running-version record | R-XMCC-E2QF, R-XZR8-LJW2, R-Y0Z4-ZBMR; D10 probes state/version |
| backup intro/model: service is /opt child holding etc or state, not registration | R-XQ01-JDYI, R-XXBF-U0EO, R-XZR8-LJW2 |
| backup model: absent manifest or database means ordinary state files | R-XTNQ-OP6L, R-XYJC-7S5D; D11/D12 determine exclusions |
| status: alphabetical discovery includes restored/uninstalled data-only services | R-XXBF-U0EO, R-XZR8-LJW2; D10 report and fallback fields |
| nginx intro: port capability independent of database and binary presence | R-XTNQ-OP6L, R-XUVN-2GXA; nginx consumes Port != 0 |

Partial manifests, read-error retention, directory-name consistency, and
contained SQLite paths are necessary shared decoding/discovery decisions, not
new deployment features. No third-party module is introduced.

The artifact/manifest schema is explicitly supplied by the opsctl stories and
treated as this program's accepted input contract, per root's boundary
decision. No sibling source or design was read. Compatibility with installed
devctl remains an external observation to collect before check-spec; this
review claims no observed devctl production behavior.


## Final local scope status

Completed contracts passed independent verification. See the corresponding `*-verification.md` reports (D14 also `backup-restore-correction-verification.md`). Product/evidence issues remain explicitly scoped. Root owns the final cross-command help-key inventory correction and verification; historical author-pending labels above are superseded by those verdicts.
