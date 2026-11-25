# Resolve ambiguous box JSON-RPC member names

During the accounts build from `0a129295`, D13 R-P8U2-PX1Z matched JSON envelope names by their exact bytes. R-PA1Z-3OSO uses that method to select the tool tier and R-PB9V-HGJD then hands the handler the unchanged bytes. Appkit v0.32.0 decodes escaped member names and uses the last decoded member. It interprets a body containing `"method":"tools/call","metho\u0064":"tools/list"` as a list after the route selects the call tier. That could expose the admin tool list to a non-admin, contrary to D13's tier decision.

The build cannot guarantee consistent interpretation with the unchanged bytes and the published appkit parser for all otherwise accepted envelopes. Declare that aliases and duplicates of reserved names are rejected, or specify a common decoding rule. Preserve the original bytes for unambiguous requests.
