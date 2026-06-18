# Verify

Verify is aiscan's active loot validation skill. Scanner output is a lead, not proof. Use this skill to decide whether available evidence supports `confirmed`, `info`, `not_confirmed`, or `inconclusive`.

## Core Rule

Never report a vulnerability as `confirmed` from scanner output alone. A confirmed finding needs independent, reproducible evidence that demonstrates both the behavior and the security impact.

## Evidence Standard

Use the tools that fit the target and claim. Simple HTTP or TCP checks usually need curl, nc, or protocol-specific clients. Rendered pages, dialogs, client-side routing, or multi-step interactions may need the `playwright` pseudo-command when it is present in the runtime pseudo-command list. If browser automation is unavailable, use HTTP/manual evidence and mark browser-only claims `inconclusive` when they cannot be evaluated.

A finding can be `confirmed` only when the evidence shows:

- the target is reachable and the observed service matches the claim
- the request or interaction is reproducible with a self-contained curl/protocol command, saved browser replay, or equivalent executable PoC
- the response demonstrates real impact, such as sensitive data exposure, unauthorized access, valid authentication, or an unauthorized state change
- the result is not explained by a default page, login redirect, WAF block, CDN/shared-host response, intended public endpoint, or documented behavior
- severity matches the demonstrated impact rather than a theoretical chain

For claims based on behavior differences, compare against a baseline. For injection-style claims, use a unique canary or otherwise measurable signal; do not rely on generic payload strings, status code alone, or one-off anomalies.

For authorization and IDOR claims, one changed ID is a lead. Test 3-5 observed, adjacent, or cross-account identifiers when available, and compare owner, non-owner, anonymous, and baseline responses before marking impact.

If a verification branch produces no useful evidence after about 20 minutes or several negative probes, stop that branch and classify it as `not_confirmed` or `inconclusive` instead of continuing mechanically.

## Common Non-Findings

Do not report these as confirmed vulnerabilities unless there is an impact chain with direct evidence:

- missing security headers, SPF/DKIM/DMARC gaps, weak TLS settings, or certificate hygiene issues
- version or banner disclosure without a working exploit for the observed version
- fingerprints, open ports, template matches, or CVE intelligence without exploit evidence
- GraphQL introspection, open redirect, CORS reflection, clickjacking, host header behavior, or DNS-only SSRF without demonstrated data access, account impact, or sensitive action impact
- self-XSS, logout CSRF, rate-limit absence on low-value forms, or static directory listing without sensitive content
- HTTP 200 responses that are login pages, default pages, empty pages, or generic error pages

## Engine Interpretation

- **gogo** port/service output is exposure evidence, not a vulnerability.
- **spray** fingerprints and paths are attack-surface intelligence, not proof.
- **neutron** template matches are leads requiring independent validation.
- **zombie** success requires evidence of valid authentication or authenticated content; HTTP 200 alone is not enough.
- **sniper** CVE intelligence narrows research, but does not confirm exploitability.

## Status

- `confirmed`: active probing directly supports a security issue with reproducible impact evidence
- `info`: useful exposure or fingerprint is real, but exploitability or impact was not demonstrated
- `not_confirmed`: probing completed and did not support the claim
- `inconclusive`: probing could not complete or evidence is contradictory, unstable, or tool-limited

## Output Format

When verification is complete, call the `finish` tool. The summary must start with a structured header line:

```
status:<status> | target:<host:port or URL> | <one-sentence title>
```

Followed by concise markdown with the exact evidence used for the decision.

- **status**: confirmed, not_confirmed, info, or inconclusive
- **target**: host:port or URL verified

In IOA collaboration mode, use `ioa_send checkpoint` instead of `finish`.
