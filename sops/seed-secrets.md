# SOP: seed or update a service's secrets in AWS

Every deployed service reads its secrets from one shared SSM parameter:

    /ikigenba/<ACCOUNT>/app-config        (SecureString, region us-east-2)

It is a single JSON blob with one top-level key per app:

```json
{ "prompts": { "ANTHROPIC_API_KEY": "…" }, "gmail": { … }, … }
```

Terraform (in `~/projects/metaspot`) owns only the parameter's *existence*;
the content is script-managed from the workstation. `ikigenba-launch` on the
box hard-fails at service start if the service's key is missing, so secrets
must be seeded **before** first deploy.

## Preconditions

- A live SSO session: the **operator** runs `aws sso login --profile int`
  interactively. You cannot do this; if the fetch fails with an auth error,
  stop and ask.
- Secret values live in `~/.secrets/<NAME>` (one file per secret, contents =
  value). **Never** read, print, echo, or log a value — scripts may `cat` a
  secret into a variable that goes straight to AWS, but only masked forms
  (`sk-a…BgAA`) may ever appear in output you see.

## Procedure

1. **Prefer the service's own script.** If `<svc>/bin/secrets` exists, run it
   and stop — it does everything below correctly and prompts for confirmation.

2. **No script yet (greenfield service)?** Do a one-off read-modify-write
   modeled on `prompts/bin/secrets` (the reference implementation — read it
   first). The non-negotiable parts:

   - **Fetch with failure classification.** `ssm get-parameter
     --with-decryption` on the blob, capturing stderr:
     - success → parse stdout as the base JSON;
     - stderr contains `ParameterNotFound` → base is `{}` (first-time seed);
     - **any other failure (expired SSO, AccessDenied, throttle) → abort
       before writing.** Falling through to `{}` here would clobber every
       sibling app's secrets on the next write.
   - **Merge, never replace.** `jq '.<app> = {KEY: $val, …}'` over the fetched
     base, preserving all sibling keys. Print the sibling key *names* in the
     summary as proof.
   - **Write via temp file**, never inline on the command line:
     `put-parameter --name <PARAM> --type SecureString --overwrite
     --value file://<tmpfile>` (tmpfile `chmod 600`, cleaned by trap).
   - **Mask every value** in output (first 4 + last 4 chars).

3. **Verify by key names only:**

   ```sh
   aws --profile int --region us-east-2 ssm get-parameter \
     --name /ikigenba/int/app-config --with-decryption \
     --query Parameter.Value --output text | jq -r 'keys'
   ```

   and `.<app> | keys` for the app's own entry. Never `jq .` the decrypted
   blob — that prints values.

## After seeding

Secrets take effect on the box at the next service (re)start:
`bin/ship` → `opsctl stage/deploy`, or `systemctl restart <svc>` for a
running service.
