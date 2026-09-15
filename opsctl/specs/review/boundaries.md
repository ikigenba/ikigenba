# Shared boundary review

## Consumer tasks

Existing in-process config invocation continues to work. Proposed process and
clock injection permits a certificate operation to run against a temporary
host tree and a supplied process result. `root` below is a caller-created
temporary directory; imports are standard library plus this module's packages.

```go
// Existing usage, unchanged.
var out, diagnostics bytes.Buffer
code := cli.Run([]string{"config", "set", "host.name=example.test"},
    strings.NewReader(""), &out, &diagnostics,
    cli.Deps{Root: root, EUID: 0})
if code != 0 { panic(diagnostics.String()) }

// Configure the other input needed to reach certificate obtaining.
code = cli.Run([]string{"config", "set", "acme.email=ops@example.test"},
    strings.NewReader(""), &out, &diagnostics,
    cli.Deps{Root: root, EUID: 0})
if code != 0 { panic(diagnostics.String()) }

// Proposed usage: obtain through the CLI with no real certbot execution.
var calls []host.Command
out.Reset()
diagnostics.Reset()
code = cli.Run([]string{"cert", "obtain"}, strings.NewReader(""),
    &out, &diagnostics, cli.Deps{
        Root: root, EUID: 0,
        Now: func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) },
        Execute: func(_ context.Context, command host.Command) (host.Result, error) {
            calls = append(calls, command)
            return host.Result{ExitCode: 1, Stderr: []byte("arbitrary tool output\n")}, nil
        },
    })
// The caller observes a returned failure and captured diagnostics; the process
// stays alive. The temporary host has no existing certificate.
```

Complete domain adapters implement `cloud.Client`. The consuming task is
fetching a configured object and closing its reader; `cloudEnv` is a caller's
injected `cloud.Env` whose Open returns that adapter.

```go
client, err := cloudEnv.Open(ctx, "us-east-2")
if err != nil { return err }
body, err := client.GetObject(ctx, "s3://example/host/deploy/app.tar.xz")
if err != nil { return err }
defer body.Close()
_, err = io.Copy(destination, body)
return err
```

The original `PutObject` boundary promised full-body acceptance but did not
exclude replacement of an existing URI. The corrected usage creates one
backup and handles a second writer's collision without replacing its bytes.
`cloudEnv` again supplies an injected implementation, and the URI starts absent.

```go
client, err := cloudEnv.Open(ctx, "us-east-2")
if err != nil { return err }
uri := "s3://example/host/backups/service/2026-09-14T00:00:00Z.tar.xz"
original := []byte("first complete archive fixture")
if err := client.PutObject(ctx, uri, bytes.NewReader(original)); err != nil {
    return err
}
err = client.PutObject(ctx, uri, strings.NewReader("competing archive fixture"))
if !errors.Is(err, cloud.ErrAlreadyExists) {
    return fmt.Errorf("expected object collision, got %v", err)
}
body, err := client.GetObject(ctx, uri)
if err != nil { return err }
defer body.Close()
saved, err := io.ReadAll(body)
if err != nil { return err }
if !bytes.Equal(saved, original) { return fmt.Errorf("existing backup changed") }
return nil
```

The same preservation guarantee applies if the two uploads race: only one
complete body can win creation, and the losing collision matches
`cloud.ErrAlreadyExists`. No preliminary existence check in the consumer is
required to preserve the winner.

Production cloud setup is unresolved beside this usage: see
[cloud adapter approval](../issues/cloud-adapter-approval.md). The example
assumes an injected implementation; it is not a claim of real AWS observation.

## Source outcomes and supporting requirements

| Source locator | Outcome supported | D01 requirement |
|---|---|---|
| README introduction; bootstrap ordinary-user/root outcomes | One CLI invocation returns a process code with isolated test environment | R-MUPN-JCBU, R-5CW6-TG0X, R-5E43-77RM, R-GU6L-Z0L7 |
| All story introductions, especially init setup composition | Domain ownership, acyclic dependencies and workflow orchestration | R-5FBZ-KZIB |
| nginx apply; certificate obtain; apps lifecycle; backup restore/retire | Injectable host process execution and retained child diagnostics | R-5GJV-YR90, R-5HRS-CIZP, R-5IZO-QAQE, R-5LFH-HU7S, R-AMEV-4J2G, R-GWME-QK2L, R-GXUB-4BTA, R-GZ27-I3JZ |
| certificates expiry; backup timestamped runs and restore cutoff | Injected current time | R-5CW6-TG0X, R-5GJV-YR90, R-AMEV-4J2G |
| apps install introduction and backup object operations | Region-selected cloud access and error ownership | R-5NVA-9DP6, R-5P36-N5FV, R-5QB3-0X6K, R-5RIZ-EOX9, R-ANMR-IAT5 |
| backup previous-backup preservation | Atomic create-only uploads and distinguishable collision | R-AOUN-W2JU, R-AQ2K-9UAJ |
| bootstrap installed CLI/help; project ground | Real process wiring and binary help smoke proof | R-5TYS-68EN, R-N0T5-G71B |
| AGENTS approved dependencies | Exact human-approved module set preserved | R-EL9M-CEGL |

These are boundary support claims, not claims of complete story coverage;
domain documents own command-specific behavior and independent verifiers judge
those outcomes. D00 was rewritten as a non-normative reading guide: old deferred
app/backup scope and per-zone certificate prose conflicted with newer stories.

## API and evidence notes

Current implementation inspected: `internal/cli/cli.go`, `cmd/opsctl/main.go`.
Existing `cli.Run`, root/env/DNS fields and default lookup behavior are retained.
No sibling source was read. No live cloud or process observation was performed.
Domain authors own exported operations; D01 owns their environmental seam only.

## ID revision

Preserved byte-identically: R-EL9M-CEGL, R-MUPN-JCBU, R-N0T5-G71B.
Removed: R-E9DW-L66P, R-MYDC-ONJX, R-LYOO-6IT6.
Initial draft: eighteen new D01 requirements. After the correction below, D01 has 23 requirements: three retained originals and twenty new current IDs. D00 has no IDs.

## Correction review

Addressed the two drafting failures in
[boundary verification 1](boundaries-verification-1.md):

- Replaced R-5MND-VLYH with R-AMEV-4J2G to scope environment exclusivity to
  domain operations receiving `host.Env`. DNS provider credentials retain
  their separate D04 R-LAAO-J3ZA SDK default-chain contract.
- Replaced R-5SQV-SGNY with R-ANMR-IAT5 to separate upload behavior from
  the remaining cloud access rules. Added R-AOUN-W2JU for the exported
  collision sentinel and R-AQ2K-9UAJ for atomic create-only upload behavior,
  including concurrent writers and preservation of existing bytes.
- Added successful-upload and collision consumer usage above. Cloud production
  adapter approval and observation remain unresolved; no new direct dependency
  or production adapter has been specified by this correction.

All four added IDs were minted with `idgen`; every other requirement was
left byte-identical. This correction changes design and review only and awaits
fresh independent verification.
