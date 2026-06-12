# Verify

Verify is aiscan's active loot validation skill. Scanner output is a **lead**, not proof. This skill probes the target to determine whether the evidence actually supports a confirmed security issue.

## Core Rule

NEVER report a vulnerability as "confirmed" based solely on scanner tool output. You MUST actively probe the target.

## Active Verification Protocol

You have access to bash tools. Use them to verify loots against live targets.

### Step 1: Reachability Check

Before anything else, confirm the target is reachable:
- TCP: `nc -zw3 <host> <port>` or `timeout 5 bash -c 'echo > /dev/tcp/<host>/<port>'`
- HTTP: `curl -sk --connect-timeout 5 -o /dev/null -w '%{http_code}' <url>`

If the target is unreachable, return `not_confirmed` with evidence "target unreachable - port closed or filtered".

### Step 2: Service Identity

Confirm the actual service matches the scanner's claim:
- Banner grab: `echo | nc -w3 <host> <port>` or `curl -sI <url>`
- Compare response against the claimed service (e.g., scanner says "Nacos" but response is Spring Boot)

If service identity doesn't match, return `not_confirmed`.

### Step 3: Claim-Specific Tests

Test the specific vulnerability claim:

| Claim Type | How to Verify |
|-----------|---------------|
| Unauthorized access (Redis/MongoDB/etc.) | Connect and check for auth requirement: `redis-cli -h <host> -p <port> ping` — look for `-NOAUTH` vs `PONG` |
| Default credentials | Test claimed credentials against actual login |
| Information disclosure | Fetch the specific endpoint, confirm sensitive data is present |
| Known CVE | Check version string against the affected range and attempt a safe PoC if possible |
| Web vulnerability (XSS/SQLi) | Send unique canary, compare against baseline |
| Open management console | Fetch URL, confirm it returns admin/management interface content |
| **XSS (reflected/stored)** | Use playwright session: `open --record` → `discover` → `dialog --arm` → `fill` payload → `click` submit → `dialog --check` for alert → if confirmed, `record --save` |
| **SQLi via login** | Use playwright session: `open --record` → `autofill --data "username=admin' OR 1=1--"` → `click` submit → check `url`/`goto` for admin content → if confirmed, `record --save` |
| **Weak creds + CAPTCHA** | Use playwright session: `open --record` → `discover` → `screenshot --selector` captcha → LLM vision to solve → `autofill --data` with creds + captcha → `click` submit → if confirmed, `record --save` |
| **Auth bypass via cookies** | Use playwright session: `open --record` → `cookies --set role=admin` → `eval` navigate to admin → check `goto` text → if confirmed, `record --save` |

### Tool Selection Decision Tree

```
Is the target a simple HTTP endpoint?
├── YES → use curl/nc (faster, lighter)
└── NO (JS-rendered, SPA, form submission needed)
    ├── Just need rendered content? → playwright goto/content
    ├── Have a nuclei headless template for this vuln?
    │   └── playwright template <poc.yaml> <target-url>
    └── Need multi-step interaction?
        └── playwright open --record → discover → fill/autofill → click → check results
            ├── Page has CAPTCHA? → playwright screenshot --selector + LLM vision
            ├── Need XSS dialog detection? → playwright dialog --arm before payload
            ├── Need to track requests? → playwright network --start before action
            └── Confirmed? → playwright record --save <poc.yaml> to persist the POC
```

### Step 4: Baseline Comparison

When verifying web loots:
- Compare suspicious endpoint response against a normal endpoint on the same host
- For injection claims: compare response with payload vs response with benign input

### Step 5: Triage Gate (Self-Adversarial Check)

Technical reproduction (Steps 1-4) proves "it works." This step asks: "is it worth reporting?"

Before marking ANY finding as `confirmed`, answer these five gates. One NO = downgrade or kill.

**Gate 1 — Evidence**: Do you have a complete curl/PoC with request AND response proving the issue? A 200 status code alone is NOT evidence. Scanner output alone is NOT evidence.

**Gate 2 — Real Impact**: Does the response contain actual sensitive data, authenticated content, or demonstrate real unauthorized action? "Technically possible" without demonstrated impact = `info` at best.

**Gate 3 — Not on the Kill List**: Check against the NEVER REPORT list below. If the finding matches and has no exploitation chain = kill it.

**Gate 4 — Correct Severity**: Does your severity label match the demonstrated (not theoretical) impact?
- Information disclosure without sensitive data ≠ High
- Version/banner exposure without working exploit ≠ Critical
- Requires authentication to exploit → cannot be labeled "unauthenticated"
- Single primitive in a multi-step chain → severity of the primitive, not the full chain

**Gate 5 — Not a Feature**: Is this genuinely unintended behavior? API documentation pages, public status endpoints, intended CORS configurations, and design-documented behaviors are not vulnerabilities.

#### NEVER REPORT (auto-kill without chain)

- Missing security headers alone (CSP, HSTS, X-Frame-Options, X-Content-Type-Options)
- Missing SPF/DKIM/DMARC records
- SSL/TLS cipher weakness or certificate issues
- Version/banner disclosure without a working exploit for that version
- GraphQL introspection enabled without demonstrated auth bypass or data leak
- Clickjacking on non-sensitive pages without action PoC
- Open redirect alone without OAuth token theft or ATO chain
- CORS header reflection without credentialed data exfiltration PoC
- Self-XSS (only affects attacker's own session)
- Logout CSRF
- Rate limit absence on non-critical forms (search, contact)
- Session not invalidated on logout / concurrent sessions allowed
- Internal IP address in error messages or headers
- HTTP 200 on admin/management path where response body is a login page or default page
- Directory listing of static assets without sensitive file content
- SSRF with DNS callback only — no internal service data returned

#### CHAIN REQUIRED (conditional — build the chain or kill)

| Standalone Finding | Required Chain | If Chain Works |
|---|---|---|
| Open redirect | + OAuth redirect_uri theft → token capture | Report as ATO chain |
| CORS reflection | + credentialed request exfils user PII/session | Report with exfil PoC |
| SSRF DNS-only | + internal service access with data returned | Report with data evidence |
| Host header injection | + password reset email uses injected host | Report with email PoC |
| Clickjacking | + sensitive action PoC (transfer, delete, change email) | Report with action PoC |
| GraphQL introspection | + auth bypass mutation or IDOR via node() | Report with data PoC |
| Prompt injection (LLM) | + reads other user data or executes unauth action | Report with impact PoC |

If you cannot build the chain today, kill the finding. Do not report standalone primitives as confirmed vulnerabilities.

## Engine-Specific Interpretation

- **neutron** template match = potential lead requiring independent verification. "no templates selected" = nothing matched, not a loot.
- **zombie** HTTP 200 = check response BODY for authenticated content. A login page returns 200 normally — that is NOT a successful login.
- **spray** fingerprint = informational asset intelligence, not a vulnerability.
- **gogo** port open = service exposure, confirm what's actually running.
- **POC/exploit** output can be confirmed only when the evidence shows successful exploitation, not just that a template matched.
- **weak credential** output can be confirmed only when evidence shows valid authentication or authenticated content.

## Verifying Injection Loots

When verifying XSS/SQLi/injection-type scanner output:

- Grep for a unique random canary string (e.g. `aiscan_xss_a7f3b2`), not generic payloads like `alert(1)`.
- Compare the injected response against a baseline (same endpoint, normal parameter value).
- A loot requires a measurable difference.

## Status Determination

- **confirmed**: evidence directly supports the security risk with reproducible proof from your active probing
- **info**: lead is real but informational (fingerprint, version disclosure, non-exploitable exposure)
- **not_confirmed**: probing completed but did not support the claim; use this for target unreachable, service mismatch, auth required, 401/403/WAF denial without protected data, unaffected version, or insufficient evidence
- **inconclusive**: rare; probing could not be completed or evaluated because of tool failure, unstable connectivity, or contradictory responses

## POC Persistence

When a browser-based vulnerability is **confirmed** via playwright session, save the POC as a nuclei headless template for reproducibility:

```bash
# Always use --record when opening sessions for verification
playwright open http://target.com/vuln --session s1 --record
# ... verification steps ...
# On confirmed loot:
playwright record s1 --save <vuln-type>-poc.yaml --id <vuln-id>
playwright close s1
```

The saved template can be replayed against other targets with `playwright template <poc.yaml> <url>`, enabling batch verification without repeating manual steps.

## Output Format

When you have completed verification, call the `checkpoint` tool:

- **kind**: "verify"
- **target**: the host:port or URL you verified
- **status**: confirmed, not_confirmed, info, or inconclusive
- **title**: one-sentence loot summary
- **content**: markdown body with exact command output as evidence
- **labels**: severity and classification tags (e.g. "high", "critical")

Do not output raw JSON. Always use the checkpoint tool to report your results.
