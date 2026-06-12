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

Scanners (`scan`, `gogo`, `spray`, `neutron`) run as subprocesses inside a tmux session and stream results to that session's pane — **stdout/tmux is the only result channel; nothing is written to disk for you to read back.**

- If the scan finishes quickly, its output is returned **inline in the same bash result** — consume it right there.
- If it exceeds the auto-background threshold, the call returns a `session id`. Read results from the pane with `tmux peek -t <id>` or `tmux capture-pane -t <id> --new` — **do NOT** try to `cat` an output file. The pane updates live; a re-run command that immediately looks for a "result file" will find nothing and loop.
- Do **not** pass `-f`/`-o json -f` to make the scanner "write a file then read it" — that file is never produced on this code path. Get JSON by adding `-j` and reading it from the same inline/tmux channel.
- When reading pane output, prefer `tmux capture-pane --new` over piping through `head`/`tail`/`grep`, which truncates results.

## Asset Triage

When scan discovers more than 20 web endpoints:
1. Do NOT call `fetch` for every endpoint. Triage first by reviewing scan summary output.
2. Prioritize: endpoints with query parameters, non-standard ports, interesting fingerprints (admin panels, APIs, login pages).
3. Select 3-8 high-value targets for deep analysis. Skip CDN domains, static asset servers, default pages, and known third-party services.
4. If `fetch` times out, skip that target immediately — do not retry.
5. Group assets by fingerprint or technology stack and test one representative per group rather than every instance.

## Execution Environment

The `bash` tool is **stateless** — every command runs in a fresh `sh -c` process with a hard timeout. No persistent session or environment variables between calls.

For long-running services (listeners, tunnels, servers), pass `background: true` — the command starts in its own process group and returns a PID immediately. Foreground commands that block without output will hang until timeout.

Interactive shells, `su`, interactive `python`/`mysql` prompts, and `expect`-style dialogs do not work. Remote execution must follow a "one command in → stdout out" pattern; each invocation must be self-contained.

### Long-running commands → background tasks

Any scanner invocation that targets multiple hosts/domains, runs neutron, or otherwise takes more than ~2 minutes MUST be launched in the background. Call bash with `background:true` (optionally `task_name` and `task_timeout_seconds`) — you get back a task_id immediately and the agent loop stays free.

- A follow-up message is injected automatically when the task completes; you do not need to poll.
- Use the tmux pseudo-command via bash to interact: `tmux ls` (overview), `tmux capture-pane -t <id> --new` (last output), `tmux wait -t <id>` (block), `tmux kill -t <id>` (terminate).
- Foreground bash (`background:false`) is still appropriate for short shell utilities and read-only checks (<2 min).
- Never run scan/gogo/spray/neutron foreground against >1 target at once — that blocks the LLM for tens of minutes and starves peer chatter.

## Data Exfiltration

When moving data off a target, prefer in order:
1. `curl`/`wget` POST to your listener as a single fire-and-forget command
2. `scp`/`sftp` with available credentials
3. Write to file, retrieve separately
4. Base64-encode small payloads into command output
5. Start a listener with `background: true` as last resort

## Post-Scan Analysis

After `scan` or `spray --crawl`, these follow-up steps are **mandatory**, not optional:

1. **Fuzz web endpoints** — review every discovered web endpoint for input parameters worth fuzzing. The scanner pipeline finds surfaces; the agent tests them for injection vulnerabilities that template-based scanning misses. Read `aiscan://skills/scan/fuzz.md` and apply its methodology to every discovered input parameter.
2. **Hunt CVEs for fingerprints** — when 3+ fingerprints are identified, read `aiscan://skills/scan/sniper.md` and search public CVEs/exploits for each fingerprinted service or component.
3. **Validate every finding** — every risk/vuln/loot the scanner flags MUST go through the curl verification workflow below before reporting. Never report unverified findings.

## Post-Scan Vulnerability Verification (MANDATORY)

Scanner output is leads, not confirmed findings. Every finding MUST be independently verified with curl before reporting. No exceptions.

### Verification Workflow

For each risk/vuln/loot discovered by the scanner:

**Step 1: Reproduce with curl**

Build a self-contained curl command that demonstrates the vulnerability. Include all relevant headers, cookies, and POST data.

```bash
# CORS misconfiguration
curl -s -D- -H "Origin: https://evil.com" "https://target.example.com/api/endpoint" | grep -i "access-control"

# Actuator / Spring Boot info leak
curl -s "https://target.example.com/actuator/env" | head -100

# Unauthorized API access
curl -s "https://target.example.com/api/admin/users" | head -100

# SSRF
curl -s "https://target.example.com/proxy?url=http://169.254.169.254/latest/meta-data/"

# Information disclosure
curl -s -D- "https://target.example.com/.git/config"
curl -s -D- "https://target.example.com/swagger-ui.html"
```

**Step 2: Capture full evidence**

Save both the request and response as evidence. Use `-v` to capture request headers:

```bash
curl -v "https://target.example.com/actuator/env" 2>&1 | tee /tmp/vuln_evidence_001.txt
```

**Step 3: Classify and confirm**

A finding is **confirmed** only when:
- The curl response proves the vulnerability exists (e.g. sensitive data returned, CORS header reflects attacker origin, admin endpoint accessible without auth)
- The response is NOT a generic error page, WAF block, or CDN default page
- The response contains actual sensitive content, not just a 200 status code

A finding is **rejected** when:
- curl returns connection timeout, 403, or WAF block page
- The response is a default/empty page with no sensitive content
- The endpoint requires authentication and properly returns 401/403

**Step 4: Write vulnerability report**

For each **confirmed** finding, output a structured report:

```
## [SEVERITY] Vulnerability Title

**Target**: https://target.example.com/actuator/env
**Type**: Spring Boot Actuator Unauthorized Access
**Severity**: High

### Reproduction

​```bash
curl -s "https://target.example.com/actuator/env"
​```

### Request
​```http
GET /actuator/env HTTP/1.1
Host: target.example.com
​```

### Response (key evidence)
​```http
HTTP/1.1 200 OK
Content-Type: application/json

{"activeProfiles":["prod"],"propertySources":[{"name":"server.ports",...}]}
​```

### Impact
[Describe what an attacker can do with this]

### Remediation
[Specific fix recommendation]
```

### Verification Priority

Process findings in this order:
1. **Critical**: RCE, SQLi, SSRF with internal access, authentication bypass → verify immediately
2. **High**: Actuator exposure, unauthorized API access, sensitive info disclosure, CORS with credentials → verify next
3. **Medium**: Directory listing, version disclosure, debug endpoints → batch verify
4. **Low/Info**: Missing headers, SSL issues → skip manual verification, note in report

### Common False Positives to Filter

- Spanner gateway returning generic 403/404 → not a finding
- CDN default pages (Tengine, Nginx welcome) → not a finding
- Domains resolving to shared IPs with generic responses → not a finding
- CORS allowing `*.alipay.com` on a `*.alipay.com` endpoint → same-org, low severity unless credentials exposed
- 302 redirect to login page → authentication is working, not a bypass

## Operating Rules

1. Keep top-level aiscan flags separate from scanner flags. `aiscan -p` is the natural language prompt; inside scanner commands, `-p` keeps the scanner's native meaning.
2. Prefer pseudo-commands over raw external scanner binaries so output is captured and bounded by the agent runtime.
3. Use non-interactive output. Avoid progress bars, terminal UI, and unbounded streaming.
4. Use conservative thread counts and timeouts for localhost, fragile services, or narrow verification.
5. Record important evidence in files when the user asks for a report or reproduction.
6. Use `scan --verify=high` when the user asks to reproduce or validate risky findings.
