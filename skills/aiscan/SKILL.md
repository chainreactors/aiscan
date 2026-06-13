---
name: aiscan
description: Use this skill when the agent needs to understand aiscan mechanisms, available capabilities, scanner pseudo-commands, and tool invocation rules.
---

# Aiscan Mechanisms

Aiscan is an autonomous security scanning agent that orchestrates local tools and embedded ChainReactors scanner engines. Use deterministic scanner output as evidence, then reason about scope, retries, verification, and reporting.

Core tools:

- `read`: read workspace files and embedded skill files such as `aiscan://skills/aiscan/SKILL.md`.
- `write`: create or update requested report and evidence files.
- `glob`: discover local files in the current working directory.
- `bash`: run shell commands and pseudo-commands.

## Pseudo-Commands

All pseudo-commands run through the `bash` tool. They are **not** system binaries — do not try to run them directly or install them.

Scanner commands available in all builds:

- `scan`: multi-stage orchestration across discovery, web probing, weakpass, POC, and verification.
- `gogo`: host, port, service, and banner discovery.
- `spray`: web probing, fingerprints, common files, crawl, and focused path checks.
- `zombie`: authorized weak credential checks for supported services.
- `neutron`: template-based POC checks when templates are available.
- `cyberhub`: search loaded fingerprints and POC templates.

Full-build scanner commands are available only when they appear in the runtime pseudo-command list:

- `passive`: domain or ICP seed -> IPs / CIDRs / domains via multi-engine cyberspace search (FOFA/Hunter/Shodan/etc.). Runs before `gogo`.
- `katana`: deep web crawling with full parameter discovery (query strings, form targets, JS endpoints).

Recon chain when the seed is a company name (not a domain or IP):
use `passive` first only in full builds. Otherwise start from user-provided domains/IPs with `scan`, `gogo`, and `spray`. Skip `passive` if the user already has IPs.

Utility commands:

- `web_search` tool: search the web for public CVEs, advisories, exploits, and product documentation.
- `fetch` tool: fetch and read a specific URL.
- `cyberhub search <query>`: search loaded fingerprints and POC templates.
- `cyberhub list poc --severity critical,high`: list available POC templates by severity.

When you discover a fingerprint (e.g. JeecgBoot 3.8.2, Seeyon V9, Landray OA), **always search for known POC templates before attempting manual exploitation**:
```bash
cyberhub search poc seeyon
cyberhub search poc jeecg
cyberhub search poc shiro
```

## Scan Output Consumption

Scanners (`scan`, `gogo`, `spray`, `neutron`) run through the `bash` tool's tmux-backed pseudo-command path and stream results to that session's pane. Prefer consuming stdout/tmux output directly.

- If the scan finishes quickly, its output is returned **inline in the same bash result** — consume it right there.
- If it exceeds the auto-background threshold, the call returns a `session id`. Read results from the pane with `tmux peek -t <id>` or `tmux capture-pane -t <id> --new`. The pane updates live; do not assume a result file exists unless you explicitly requested one.
- `scan -f/--file` is supported when the user asks for a saved artifact, but do not add it merely to work around output handling. Get machine-readable scan output with `-j` and read it from the same inline/tmux channel.
- When reading pane output, prefer `tmux capture-pane --new` over piping through `head`/`tail`/`grep`, which truncates results.

## Asset Triage

When scan discovers more than 20 web endpoints:
1. Do NOT call `fetch` for every endpoint. Triage first by reviewing scan summary output.
2. Prioritize: endpoints with query parameters, non-standard ports, interesting fingerprints (admin panels, APIs, login pages).
3. Select 3-8 high-value targets for deep analysis. Skip CDN domains, static asset servers, default pages, and known third-party services.
4. If `fetch` times out, skip that target immediately — do not retry.
5. Group assets by fingerprint or technology stack and test one representative per group rather than every instance.

## Execution Environment

The `bash` tool accepts a single `command` argument. It does not support `background`, `task_name`, or per-call timeout fields.

Every `bash` command is wrapped in a tmux-backed session. If the first token is a registered pseudo-command (`scan`, `gogo`, `tmux`, etc.), it runs in-process inside that session; otherwise it runs as a shell command in a PTY. Keep each invocation self-contained and do not rely on shell state from prior calls.

Interactive shells, `su`, interactive `python`/`mysql` prompts, and `expect`-style dialogs do not work reliably. Remote execution must follow a "one command in -> stdout out" pattern.

### Long-running commands

Do not pass a background flag to `bash`. Commands that run longer than the auto-background threshold return a `session id` automatically.

- Read live output with `tmux peek -t <id>` or `tmux capture-pane -t <id> --new`.
- Wait for completion with `tmux wait-for -t <id>` or stop it with `tmux kill -t <id>`.
- The runtime may inject a follow-up inbox message when a tmux-backed command completes; still inspect the session output before reporting results.
- Never assume a scanner wrote a result file unless you explicitly passed an output file flag.

## Evidence Handling

Collect the minimum evidence needed to support the security conclusion. Prefer short response excerpts, hashes, counts, screenshots, or scanner output references over bulk data. Do not retrieve secrets, personal data, database dumps, or large files unless the user explicitly asked for authorized reproduction and the evidence cannot be proven safely another way.

## Post-Scan Analysis

Use scan output as a map of leads, not as a fixed checklist. Prioritize follow-up by demonstrated impact, exposed authentication boundary, reachable attack surface, unusual fingerprints, parameterized endpoints, and the user's stated goal. For large surfaces, sample representative assets by technology or behavior instead of exhaustively probing every endpoint.

Default ROI routing:

- login or account boundary -> authorization and IDOR first
- API or Swagger/OpenAPI -> unauthenticated access and role boundary first
- upload/import/media -> upload controls and post-upload access first
- search/filter/export/sort/orderBy -> injection and data-boundary validation first
- GraphQL -> unauthorized query or mutation impact first; introspection alone is not a finding
- thin visible surface -> JS, source maps, routes, and hidden endpoints

If a route produces no material evidence after sustained effort, switch routes. Keep the loop exploratory: direction and standards matter more than following a fixed step list.

## Verification Standard

Scanner output is a lead, not a confirmed finding. Report a vulnerability as confirmed only when independent evidence demonstrates both the behavior and its security impact. A status code, banner, fingerprint, default page, template match, or version string is not enough by itself.

When judging a lead:

- prefer direct, reproducible evidence over tool labels
- compare against a baseline when the claim depends on behavioral difference
- verify that the response is not a WAF block, login page, CDN default page, intended public endpoint, or documented feature
- classify unverified scanner matches as potential risks or informational findings
- keep severity tied to demonstrated impact, not theoretical exploit chains
- use `scan --verify=high` when the user asks for active validation of risky findings

## Operating Rules

1. Keep top-level aiscan flags separate from scanner flags. `aiscan -p` is the natural language prompt; inside scanner commands, `-p` keeps the scanner's native meaning.
2. Prefer pseudo-commands over raw external scanner binaries so output is captured and bounded by the agent runtime.
3. Use non-interactive output. Avoid progress bars, terminal UI, and unbounded streaming.
4. Use conservative thread counts and timeouts for localhost, fragile services, or narrow verification.
5. Record important evidence in files when the user asks for a report or reproduction.
6. Use `scan --verify=high` when the user asks to reproduce or validate risky findings.
