# OAuth secret conflicts with verbatim log identity

Filed during the build-spec run scoped to design commit f3465318647b58fbec7a7ce2bd07645ffb1b3908. Two independent verifiers reproduced the conflict using the public API and loopback provider failures.

Requirements involved: R-F24E-R2I0, R-T8UR-8BFL, R-EYGP-LR9X, R-T6FE-G1JX.

R-T8UR-8BFL requires every log record to carry the consumer's NewLog id verbatim. R-F24E-R2I0 prohibits every string in a decoded error record from containing an OAuth secret of the OAuthRotator(store) used by the authenticator, including ordinary provider failures covered by R-T6FE-G1JX. R-EYGP-LR9X defines an OAuth secret as a top-level access_token or refresh_token string of at least 16 bytes in the store or refresh response; this definition has no consumer-input exclusion.

Construct an OAuthRotator with a fake TokenStore holding distinct access and refresh tokens longer than 16 bytes. Pass that refresh token as the id to NewLog, construct a conversation with the rotator's authenticator, and Send to a loopback server returning HTTP 500. The request needs no rotation or vendor secret echo. Independent public-use probes observed:

```text
terminal_failure=true
error_record_id="refresh-secret-0123456789"
id_contains_oauth_secret=true
```

The error record must simultaneously preserve and exclude the same ID string. Redacting only its error payload leaves the forbidden ID intact; JSON escaping cannot help because the requirement concerns decoded strings. The consumer-input exclusion in R-T6FE-G1JX applies to credential secrets, not the distinct OAuth-secret definition. Rejecting or changing the ID adds an unstated restriction or violates the verbatim requirement. This cannot be resolved within the build run's read-only design authority.

Suggested resolution: decide whether log identity remains verbatim or participates in redaction, then replace the conflicting requirement with a newly minted ID. Explicitly exempting LogRecord.ID from R-F24E-R2I0 preserves the current identity contract; extending the OAuth-secret definition's consumer-input exclusion would change the security contract more broadly.

The run leaves R-F24E-R2I0 mechanically open while retaining useful error-payload redaction tests without claiming that requirement is fully proved.
