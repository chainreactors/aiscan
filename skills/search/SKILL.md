---
name: search
description: Use this skill to learn how to use the cyberhub pseudo-command for querying loaded fingerprints and POC templates.
internal: true
---

# cyberhub (also: search cyberhub)

Search and list loaded fingerprints and POC templates.

`cyberhub` is available both as a standalone command and as `search cyberhub`. Both forms are identical.

## Quick examples (use these patterns)

```bash
# Search for POC templates by product name
cyberhub search poc seeyon
cyberhub search poc shiro
cyberhub search poc jeecg
cyberhub search poc spring

# List high-severity POC templates
cyberhub list poc --severity critical,high

# Search fingerprints
cyberhub search finger nginx
cyberhub search finger tomcat

# Filter POCs by tag
cyberhub search poc struts --tag rce

# JSON output
cyberhub search poc nacos -j
```

## Full syntax

```bash
cyberhub list [finger|poc|all] [options]
cyberhub search [finger|poc|all] <query> [options]
```

Options:
- `-s, --severity`: Filter POCs by severity (critical, high, medium, low).
- `--tag`: Filter by tag. Can be comma-separated or repeated.
- `--finger`: Filter POCs by fingerprint name.
- `--protocol`: Filter fingerprints by protocol: http or tcp.
- `--limit`: Maximum rows (default: 50, 0 for all).
- `-j, --json`: Output JSON Lines.

## Common mistakes to avoid

```bash
# WRONG — these flags don't exist:
cyberhub -t seeyon           # -t is not a top-level flag
cyberhub --search ecology    # --search is not a flag
cyberhub --type poc -k struts # -k doesn't exist

# RIGHT:
cyberhub search poc seeyon
cyberhub search poc ecology
cyberhub search poc struts --tag rce
```

**Note**: `cyberhub` is a pseudo-command — do NOT append `2>/dev/null`, pipe to `head`/`grep`, or use shell redirections. Output is returned directly in the tool result.
