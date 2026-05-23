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

- `passive`: domain or ICP seed → IPs / CIDRs / domains via multi-engine cyberspace search (FOFA/Hunter/Shodan/etc.). Runs before `gogo`.
- `scan`: multi-stage orchestration across discovery, web probing, weakpass, POC, and verification.
- `gogo`: host, port, service, and banner discovery.
- `spray`: web probing, fingerprints, common files, crawl, and focused path checks.
- `katana`: deep web crawling with full parameter discovery (query strings, form targets, JS endpoints).
- `zombie`: authorized weak credential checks for supported services.
- `neutron`: template-based POC checks when templates are available.
- `cyberhub`: search loaded fingerprints and POC templates.

Recon chain when the seed is a company name (not a domain or IP):
`passive` → `gogo` / `spray` / `katana` → `neutron` / `fuzz`. Skip `passive` if the user already has IPs.

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

After `scan` or `spray --crawl`, review discovered web endpoints for parameters worth fuzzing. The scanner pipeline finds surfaces; the agent tests them for injection vulnerabilities that template-based scanning misses. See `fuzz` skill for methodology.

## Verification

Scanner output is leads, not confirmed findings. Apply the `verify` skill's validation rules before reporting anything as confirmed.

## Operating Rules

1. Keep top-level aiscan flags separate from scanner flags. `aiscan -p` is the natural language prompt; inside scanner commands, `-p` keeps the scanner's native meaning.
2. Prefer pseudo-commands over raw external scanner binaries so output is captured and bounded by the agent runtime.
3. Use non-interactive output. Avoid progress bars, terminal UI, and unbounded streaming.
4. Use conservative thread counts and timeouts for localhost, fragile services, or narrow verification.
5. Record important evidence in files when the user asks for a report or reproduction.
6. Use `scan --verify=high` when the user asks to reproduce or validate risky findings.
