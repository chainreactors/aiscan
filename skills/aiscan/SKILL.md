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

Scanner commands:

- `ani`: company name → subsidiary tree + ICP-registered domains (Tianyancha/Aiqicha scrapers). Entry point when the seed is a Chinese company.
- `ina`: domain or ICP seed → IPs / CIDRs / domains via multi-engine cyberspace search (FOFA/ZoomEye/Hunter). Runs after `ani`, before `gogo`.
- `scan`: multi-stage orchestration across discovery, web probing, weakpass, POC, and verification.
- `gogo`: host, port, service, and banner discovery.
- `spray`: web probing, fingerprints, common files, crawl, and focused path checks.
- `katana`: deep web crawling with full parameter discovery (query strings, form targets, JS endpoints).
- `zombie`: authorized weak credential checks for supported services.
- `neutron`: template-based POC checks when templates are available.
- `cyberhub`: search loaded fingerprints and POC templates.

Recon chain when the seed is a company name (not a domain or IP):
`ani` → `ina` → `gogo` / `spray` / `katana` → `neutron` / `fuzz`. Skip `ani` if the user already has domains, skip `ina` if they already have IPs.

Utility commands:

- `web_search`: search the web.
- `web_fetch`: fetch and read a URL.
- `vision`: analyze an image with a vision LLM.
- `parse_results`: parse JSON-lines scanner output.
- `filter_results`: filter JSON-lines scanner output.

Read the corresponding skill file for each command's usage: `aiscan://skills/<command>/SKILL.md`.

## Execution Environment

The `bash` tool is **stateless** — every command runs in a fresh `sh -c` process with a hard timeout. No persistent session or environment variables between calls.

For long-running services (listeners, tunnels, servers), pass `background: true` — the command starts in its own process group and returns a PID immediately. Foreground commands that block without output will hang until timeout.

Interactive shells, `su`, interactive `python`/`mysql` prompts, and `expect`-style dialogs do not work. Remote execution must follow a "one command in → stdout out" pattern.

## Data Exfiltration

When moving data off a target, prefer in order:
1. `curl`/`wget` POST to your listener
2. `scp`/`sftp` with available credentials
3. Write to file, retrieve separately
4. Base64-encode into command output
5. Start a listener with `background: true` as last resort

## Post-Scan Analysis

After `scan` or `spray --crawl` finishes, don't stop at the summary. Review discovered web endpoints for parameters worth fuzzing — query strings, dynamic path segments, form targets, API bodies. Load `aiscan://skills/fuzz/SKILL.md` for the methodology. The scanner pipeline finds surfaces; your job as an agent is to test them for injection vulnerabilities that template-based scanning misses.

## Verification Discipline

Scanner tools produce leads, not confirmed vulnerabilities. Before reporting any finding as confirmed:

- Verify independently: reproduce the behavior with a unique canary payload, not generic strings.
- Compare against baseline: same endpoint, normal parameter value. Measure the difference.
- Distinguish tool artifacts from real findings: neutron "no templates selected" is not a vulnerability. zombie HTTP 200 on a login page is normal. spray fingerprints are informational.
- Include evidence: exact payload, response fragment, and baseline diff. Without all three, label as "potential/unverified".

Load `aiscan://skills/fuzz/SKILL.md` for the full verification protocol.

## Operating Rules

1. Keep top-level aiscan flags separate from scanner flags. `aiscan -p` is the natural language prompt; inside scanner commands, `-p` keeps the scanner's native meaning.
2. Prefer pseudo-commands over raw external scanner binaries so output is captured and bounded by the agent runtime.
3. Use non-interactive output. Avoid progress bars, terminal UI, and unbounded streaming.
4. Use conservative thread counts and timeouts for localhost, fragile services, or narrow verification.
5. Record important evidence in files when the user asks for a report or reproduction.
6. Use `scan --verify=high` when the user asks to reproduce or validate risky findings.
