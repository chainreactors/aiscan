---
name: search
description: Use this skill to learn how to use the search pseudo-command for web search, URL fetching, and cyberhub queries.
internal: true
---

# search

Unified search across web and local resources. Three subcommands:

## search web

Search the web via DeepSeek web_search (Anthropic-compatible endpoint).

```bash
search web <query> [--num <N>]
```

- `<query>`: positional, multi-word auto-concatenated.
- `--num <N>`: max search round-trips, 1–10, default 5.

```bash
search web "CVE-2024-1234 exploit"
search web nginx misconfiguration --num 10
```

## search fetch

Fetch a URL and return content as readable text. HTML is auto-converted to Markdown.

```bash
search fetch <url> [--extract <hint>]
```

- `<url>`: target URL. If no scheme is provided, HTTPS is assumed; explicit HTTP is preserved.
- `--extract <hint>`: optional focus hint to return matching sections when possible.

```bash
search fetch https://nvd.nist.gov/vuln/detail/CVE-2024-1234
search fetch https://example.com/advisory --extract "CVSS score"
```

## search cyberhub

Search and list loaded fingerprints and POC templates.

```bash
search cyberhub list [finger|poc|all] [options]
search cyberhub search [finger|poc|all] <query> [options]
```

Options:
- `-t, --type`: Resource type: finger, poc, or all.
- `-q, --query`: Search query.
- `--tag`: Filter by tag. Can be comma-separated or repeated.
- `--protocol`: Filter fingerprints by protocol: http or tcp.
- `--finger`: Filter POCs by fingerprint name.
- `-s, --severity`: Filter POCs by severity.
- `--limit`: Maximum rows (default: 50, 0 for all).
- `-j, --json`: Output JSON Lines.

```bash
search cyberhub list finger --limit 20
search cyberhub search finger nginx
search cyberhub list poc --severity critical,high
search cyberhub search poc spring --tag rce -j
```
