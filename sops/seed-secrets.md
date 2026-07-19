# SOP: seed or update a service's secrets in AWS

Every deployed service reads its secrets from its own SSM parameter:

    /ikigenba/<ACCOUNT>/app-config/<app>        (SecureString, region us-east-2)

The value is a flat JSON object with only that app's pushed secret keys:

```json
{ "ANTHROPIC_API_KEY": "..." }
```

Secret-less apps are seeded as `{}`. Parameter existence and content are both
script-managed from the workstation by `bin/push-secrets`; Terraform owns only
the one-time IAM path grant. `ikigenba-launch` on the box hard-fails at service
start if the app's parameter cannot be fetched, including `ParameterNotFound`,
so every app must be seeded **before** first deploy.

## Preconditions

- A live SSO session: the **operator** runs `aws sso login --profile int`
  interactively. You cannot do this; if the fetch fails with an auth error,
  stop and ask.
- Secret values live in `~/.secrets/<NAME>` (one file per secret, contents =
  value). **Never** read, print, echo, or log a value — scripts may `cat` a
  secret into a variable that goes straight to AWS, but only masked forms
  (`sk-a…BgAA`) may ever appear in output you see.

## Procedure

1. **Seed one app.** Run:

   ```sh
   bin/push-secrets <app>
   ```

   The tool parses `<app>/.envrc` for lines that exactly match
   `export NAME="$(cat ~/.secrets/NAME)"`, resolves each value from the
   same-named environment variable or `~/.secrets/<NAME>`, and overwrites only
   `/ikigenba/<ACCOUNT>/app-config/<app>`. Apps with no matching lines are
   pushed as `{}`.

2. **Seed the whole suite.** Run:

   ```sh
   bin/push-secrets --all
   ```

   This sweeps every top-level deployable app carrying a `VERSION`, reports each
   app, and exits non-zero if any push fails.

3. **Preview without AWS.** Run:

   ```sh
   bin/push-secrets --dry-run <app>
   bin/push-secrets --dry-run --all
   ```

   Dry runs print the parameter name and pushed key names only. Normal pushes
   print key names and masked values; full secret values must never appear on a
   command line, in output, or in logs.

4. **Verify by key names only:**

   ```sh
   aws --profile int --region us-east-2 ssm get-parameter \
     --name /ikigenba/int/app-config/<app> --with-decryption \
     --query Parameter.Value --output text | jq -r 'keys'
   ```

   Never `jq .` the decrypted object -- that prints values.

5. **Verify a multi-line secret round trip by shape only.** After a live push
   of an app carrying a PEM-style multi-line secret, read back only that key's
   length and line count, then confirm the line count is greater than 1:

   ```sh
   MULTI_LINE_KEY=<key-name>
   aws --profile int --region us-east-2 ssm get-parameter \
     --name /ikigenba/int/app-config/<app> --with-decryption \
     --query Parameter.Value --output text \
     | jq -r --arg key "$MULTI_LINE_KEY" \
         '.[$key] | "length=\(length) line count=\(split("\n") | length)"'
   ```

   Never print the decrypted key value itself.

## After seeding

Secrets take effect on the box at the next service (re)start:
`bin/ship` → `opsctl stage/deploy`, or `systemctl restart <svc>` for a
running service.
