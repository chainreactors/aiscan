---
name: fuzz
description: Post-scan parameter fuzzing methodology. After scan/spray discovers web endpoints, evaluate and fuzz interesting parameters for injection vulnerabilities.
internal: true
---

# Parameter Fuzzing — Methodology

## When to Fuzz

After `scan` or `spray --crawl` completes, run `katana` on discovered web targets to enumerate parameterized URLs. Spray crawl finds paths and fingerprints but strips query parameters by design; katana preserves them. Use `katana -u <target> -d 2 -f qurl` to get URLs with parameters, then evaluate each as a fuzz candidate:

- Query parameters (`?id=1`, `?search=keyword`, `?page=2`)
- Path segments that look dynamic (`/api/user/123`, `/item/abc-def`)
- POST bodies observed in crawl results (form actions, JSON endpoints)
- Headers that influence output (Host, Referer, X-Forwarded-For)

Prioritize parameters that reflect in the response, interact with backend data, or influence server-side behavior. Skip static assets, CDN endpoints, and known third-party services.

## Decision Framework

1. **Baseline**: Understand the normal response before injecting anything. Know the expected status code, body length, and response time.
2. **Reflection check**: Does user input appear in the response body, headers, or error messages? If yes, test for XSS and SSTI.
3. **Backend interaction**: Does the parameter influence database queries, file paths, or system commands? If yes, test for SQLi, LFI, and RCE.
4. **Timing sensitivity**: Can you measure response time differences? Time-based blind techniques work when output is opaque.
5. **Error verbosity**: Do malformed inputs trigger stack traces, SQL errors, or debug output? Error-based techniques are fastest to confirm.

## Vulnerability Classes to Consider

- **SQL Injection** — error-based, boolean-blind, time-blind
- **Reflected/Stored XSS** — reflected canary detection, DOM context analysis
- **Server-Side Template Injection** — expression evaluation in template engines
- **Local File Inclusion / Path Traversal** — directory escape in file parameters
- **Command Injection** — OS command concatenation in shell-interacting parameters
- **SSRF** — parameters accepting URLs or hostnames
- **IDOR** — sequential or predictable identifiers in authenticated contexts

## Principles

- One variable at a time. Hold everything else constant.
- Confirm, don't assume. A single anomalous response is a lead, not a finding. Reproduce with a distinct payload before reporting.
- Respect rate limits and WAF behavior. If you get blocked, slow down or vary your approach — don't just retry the same payload.
- Record evidence: the exact request that triggered the behavior and the response that proves it.

## Mandatory Verification Protocol

### Rule 1: Unique Canary Markers

NEVER grep for generic strings (`alert(1)`, `<script>`, `onerror=`) as confirmation — the page's own JavaScript or HTML may contain these naturally. Instead, generate a unique random canary per test (e.g., `aiscan_xss_a7f3b2c9` or `aiscan_sqli_9d4e1f`). Only consider a vulnerability confirmed when the exact canary string reflects in the response body where it should not appear naturally.

### Rule 2: Baseline Comparison

Before claiming any vulnerability, capture a baseline response using the same endpoint with a normal, benign parameter value. Then compare against the injected response:

- Status code difference (200 vs 500 = interesting)
- Body length difference (±10% tolerance for dynamic content)
- Specific content diff (error messages, canary reflection, new HTML elements)

A confirmed finding requires a **measurable, reproducible** difference between baseline and injected responses.

### Rule 3: Tool Output ≠ Vulnerability Confirmation

Scanner tool output is a **lead**, not proof:

- `neutron` template match or "template-selected" → potential lead that needs independent verification via manual request. "No templates selected" means nothing matched — do NOT report this as a finding.
- `zombie` HTTP 200 response → check the response **body** for actual authenticated content (admin panel, dashboard, user data). HTTP 200 on a login page is the normal unauthenticated response, not a successful login.
- `spray` fingerprint detection → informational asset intelligence, not a vulnerability.
- `grep` match on page content → may be the page's own JS/HTML/CSS, not reflected user injection. Verify by checking whether your unique canary is present, not a generic keyword.

### Rule 4: Evidence Requirements

A confirmed vulnerability report MUST include all three:

1. **Exact payload**: the curl-reproducible request (method, URL, headers, body)
2. **Response evidence**: the specific response fragment proving exploitation (canary reflection, SQL error, file content)
3. **Baseline comparison**: the normal response showing the difference

Without all three, classify the finding as **"potential/unverified"** and include the raw tool output for human review. Never escalate unverified findings to confirmed status.
