---
name: scan
description: Use this skill when working with scan for the multi-stage aiscan pipeline across discovery, web probing, weak credentials, POC checks, and verification.
internal: true
---

# Scan

Scan is the multi-stage orchestration pipeline in aiscan.

Capabilities:

- combine discovery, web probing, weak credential checks, POC execution, and optional AI verification
- produce discovered targets, services, web endpoints, fingerprints, weak credentials, POC matches, errors, and final stats
- expose capability names such as gogo portscan, spray web probing, zombie weakpass, neutron POC, and agent verification
- use deep testing for discovered web endpoints and fingerprinted assets when requested
- run quick or full profiles depending on depth needs

Common usage:

```bash
# Single target (-i)
scan -i 10.0.0.1 --mode quick
scan -i 10.0.0.0/24 --mode full
scan -i 10.0.0.1 --mode full --ports top1000
scan -i 10.0.0.1,10.0.0.2,10.0.0.3 --mode quick
scan -i http://10.0.0.1:8080 --mode quick
scan -i https://example.com --mode quick

# Target list file (-l) — one target per line
scan -l /tmp/targets.txt --mode quick
scan -l /tmp/targets.txt --mode full --thread 4 --timeout 10

# AI features
scan -i 10.0.0.1 --verify=high
scan -i 10.0.0.1 --sniper
scan -i 10.0.0.1 --mode full --deep
scan -i 10.0.0.1 -j
```

**CRITICAL: `-i` vs `-l`**:
- `-i` is for inline targets: IP, CIDR, URL, or comma-separated list. Does **NOT** accept file paths.
- `-l` is for target list files (one target per line). **Always use `-l` when scanning from a file.**
- `scan -i /tmp/targets.txt` will FAIL — use `scan -l /tmp/targets.txt` instead.

**Input format for `-i`**:
- IP: `10.0.0.1`
- CIDR: `10.0.0.0/24`
- Comma-separated: `10.0.0.1,10.0.0.2`
- URL (with port): `http://10.0.0.1:8080`
- Domain: `example.com` (scan discovers ports itself)
- **NOT** bare `ip:port` — `10.0.0.1:8080` without `http://` prefix will fail. Use `http://10.0.0.1:8080`.
- **NOT** file paths — use `-l` for files.

Notes:

- `quick` uses gogo `-p all -v`; `full` uses gogo `-p -` and adds spray default-dictionary probing.
- Spray web capabilities run with recon enabled in both profiles.
- `--verify=<level>` enables active validation for loots at or above the selected priority.
- `--sniper` enables fingerprint vulnerability intelligence.
- `--deep` enables browser-backed testing for discovered websites and fingerprint-based deep assessment for fingerprinted assets.
- User intent decides whether scan output should be summarized, analyzed, validated, reported, or used to choose follow-up commands.

## AI Sub-Skills

The scan AI sub-skills are independent options:

- `aiscan://skills/scan/verify.md` - Active loot validation: probes targets to confirm or reject scanner leads
- `aiscan://skills/scan/sniper.md` — Vulnerability intelligence: searches for known CVEs based on discovered fingerprints
- `aiscan://skills/scan/deep.md` — Deep testing for discovered web endpoints and fingerprinted assets
- `aiscan://skills/scan/fuzz.md` — Post-scan parameter fuzzing for injection vulnerabilities
