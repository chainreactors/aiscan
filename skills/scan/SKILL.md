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
scan -i <target> --mode quick
scan -i <target> --mode full
scan -i <target> --verify=high
scan -i <target> --sniper
scan -i <target> --mode full --deep
scan -i <target> --mode full --port top1000
scan -i <target> -j
```

Notes:

- `quick` uses gogo `-p all -v`; `full` uses gogo `-p -` and adds spray default-dictionary probing.
- Spray web capabilities run with recon enabled in both profiles.
- `--verify=<level>` enables active validation for findings at or above the selected priority.
- `--sniper` enables fingerprint vulnerability intelligence.
- `--deep` enables browser-backed testing for discovered websites and fingerprint-based deep assessment for fingerprinted assets.
- `--ai` is a compatibility alias for `--verify=high --sniper`. It does not enable `--deep`.
- User intent decides whether scan output should be summarized, analyzed, validated, reported, or used to choose follow-up commands.

## AI Sub-Skills

The scan AI sub-skills are independent options:

- `aiscan://skills/scan/verify.md` — Active finding validation: probes targets to confirm or reject scanner findings
- `aiscan://skills/scan/sniper.md` — Vulnerability intelligence: searches for known CVEs based on discovered fingerprints
- `aiscan://skills/scan/deep.md` — Deep testing for discovered web endpoints and fingerprinted assets
