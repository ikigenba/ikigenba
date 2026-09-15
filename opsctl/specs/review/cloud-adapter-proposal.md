# Production cloud adapter proposal for human review

This is a non-normative proposal, not an approved dependency or implemented adapter.

## First decision: direct dependencies

Add these exact direct module paths to the existing approved set:

- `github.com/aws/aws-sdk-go-v2/service/s3`
- `github.com/aws/aws-sdk-go-v2/service/ssm`

Keep the existing SDK core, config, and Route 53 modules. Releases remain dependency data in `go.mod`, outside normative requirements. No module file is changed by this draft.

## Intended contract after approval

A production adapter would implement the already-designed `cloud.Client` operations for S3 object retrieval, atomic create-only upload, complete listing and SSM parameter retrieval. It would use the SDK default credential chain and configured region. Command wiring and the allowed import graph would be revised with new requirement IDs; the domain interface stays stable. A dedicated adapter package would own these service SDK imports, preserving domain test isolation.

## Separate remaining decision

The parameter's published encoding is still unspecified. Confirm its actual producer contract before designing the secret-map decoding behavior. Approving the two modules does not approve an assumed encoding. Live adapter operations and permissions require recorded observations before check-spec.

## Why approval is required

The project AGENTS.md says: “Every direct dependency is approved by a human, and the approval is recorded in the design.” Existing D01 approves only SDK core, config and Route 53. This proposal therefore requires the user's explicit approval before those two modules enter the normative approved set.
