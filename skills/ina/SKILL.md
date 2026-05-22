---
name: ina
description: Use ina to expand a domain or ICP seed into cyberspace-search assets through the ina-go SDK. Run after ani when you have ICP/domain leads, and before gogo/spray when you need IPs, URLs, domains, or ports.
---

# Ina

`ina` is an aiscan pseudo-command backed by `github.com/chainreactors/ina-go`.
It does not shell out to Python.

## When to Use

- The user gives a domain and asks for external asset expansion.
- `ani` returned ICP/domain leads and you need IPs, URLs, domains, titles, or ports.
- You need source-derived leads before active checks with `gogo`, `spray`, `katana`, or `neutron`.

Do not use `ina` when the user already provided concrete IPs or private ranges. Go directly to `gogo` for active scanning.

## Credentials

`ina` is only registered when at least one source has credentials.

- FOFA: `recon.fofa_email` + `recon.fofa_key`, or env `FOFA_EMAIL` + `FOFA_KEY`
- Hunter: `recon.hunter_api_key`, or env `HUNTER_API_KEY`
- Optional proxy for ina sources: `recon.proxy` or `RECON_PROXY`

## Usage

```bash
ina 'domain="example.com"' -s fofa
ina 'icp="浙ICP备16020926号"' -s fofa
ina 'domain.suffix="example.com"' -s hunter
```

Supported wrapper flags:

- `-s <source>` / `--source <source>`: `fofa` or `hunter`
- `-h` / `--help`

## Output

Output matches Python `InaData.to_dict()`: a JSON array of asset objects.

```json
[{"ip":"1.2.3.4","port":"443","url":"https://example.com","domain":"example.com","title":"Example","icp":"..."}]
```

Extract unique IPs/domains and pass them downstream to `gogo`, `spray`, or `katana`.
