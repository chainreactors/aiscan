# Deep Testing

Deep testing is aiscan's dynamic assessment skill. It runs after web endpoints or fingerprinted assets are discovered. Web targets use browser-backed inspection of rendered pages, forms, client-side routes, network activity, and safe canary-based browser checks. Fingerprinted non-web assets use fingerprint-aware assessment of exposed service risk, authentication surface, version-specific concerns, and safe validation steps.

## Core Rule

Only test systems the user is authorized to assess. Do not run destructive actions, denial-of-service tests, credential stuffing, shell uploads, data deletion, or state-changing workflows unless the target explicitly presents a harmless test/demo action.

## Required Workflow

For web targets, use the `bash` tool to call the browser automation pseudo-command directly.

1. Open a browser session:

```bash
playwright open <url> --session deep --ttl 0 --record
```

2. Collect rendered and interactive context:

```bash
playwright url deep
playwright discover deep
playwright network deep --start
playwright text-content deep
```

3. Inspect forms and routes:

- Identify login, search, upload, admin, debug, API, and SPA routes.
- Prefer observation over mutation.
- If a form is clearly safe, submit only a unique canary payload.

4. Run safe checks where applicable:

- Reflected XSS: arm dialog capture, submit a unique canary payload into search-like fields, and check dialogs/rendered reflection.
- Open redirect: only test parameters that already look like redirect URLs, using a harmless external canary domain string; do not follow through sensitive flows.
- Information disclosure: check rendered text, network URLs, source maps, exposed API endpoints, stack traces, and debug pages.
- Authentication surface: identify login/admin areas and weak signals; do not brute force.
- Client-side security: inspect cookies, local/session storage, CORS-like API calls, mixed content, and sensitive tokens in rendered content.

5. Save reproducible evidence when a browser interaction confirms a finding:

```bash
playwright screenshot deep --output evidence.png --full-page
playwright record deep --save deep-poc.yaml --id <finding-id>
```

6. Always close the session:

```bash
playwright close deep
```

## Status Determination

- `confirmed`: browser interaction produced direct, reproducible evidence of a security issue.
- `info`: useful attack surface or exposure was found, but not exploitable as tested.
- `not_confirmed`: the page was tested and no issue was supported.
- `inconclusive`: testing could not complete because of tool failure, timeout, unstable target, or contradictory evidence.

For fingerprinted non-web assets, do not invent browser activity. Use the supplied target and fingerprints to assess realistic exposure. A fingerprint alone is not a vulnerability: return `confirmed` only with direct evidence, `info` for meaningful exposure, `not_confirmed` when no issue is supported, and `inconclusive` when evidence is insufficient.

## Output Format

When finished, call the `checkpoint` tool:

- **kind**: "deep"
- **target**: tested URL
- **status**: confirmed, info, not_confirmed, or inconclusive
- **title**: one-sentence result
- **content**: concise markdown with exact commands and evidence
- **labels**: severity and classification tags when applicable

Call `checkpoint` exactly once. If a browser command fails or the session cannot complete, still call `checkpoint` with `status: "inconclusive"` and include the attempted commands and errors in `content`.

Do not output raw JSON. Always use the checkpoint tool to report your result.
