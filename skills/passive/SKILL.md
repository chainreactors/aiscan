---
name: passive
description: Use passive to expand company names into subsidiary/ICP/domain leads (ani-go) or expand domains/ICPs into cyberspace assets like IPs, URLs, ports (ina-go). Run before active scanners (gogo, spray, katana).
---

# Passive

`passive` is a unified aiscan command backed by `ani-go` (company graph) and
`ina-go` (cyberspace search). Pick a source with `-s`.

## Sources

### Company Recon (ani-go)

| Source       | Provider                         | Credential                            |
| ------------ | -------------------------------- | ------------------------------------- |
| `aqc_unauth` | 爱企查 (qiye.baidu.com)          | none                                  |
| `tyc_unauth` | 天眼查 WeChat mini-program API   | none                                  |
| `tyc`        | 天眼查 (www, authed)             | `recon.ani_tyc_token` (auth_token JWT)|
| `qcc`        | 企查查                            | `recon.ani_qcc_cookie` (QCCSESSID)   |
| `aqc`        | 爱企查 (authed)                   | `recon.ani_aqc_cookie` (BAIDUID)      |

### Cyberspace Recon (ina-go)

| Source   | Provider | Credential                                          |
| -------- | -------- | --------------------------------------------------- |
| `fofa`   | FOFA     | `recon.fofa_email` + `recon.fofa_key` or env vars   |
| `hunter` | Hunter   | `recon.hunter_api_key` or env `HUNTER_API_KEY`       |

Sources without credentials are silently skipped at init.

## When to Use

- **Company name → subsidiaries + ICP domains**: use a company source (`aqc_unauth`, `tyc`, etc.)
- **Domain/ICP → IPs, URLs, ports**: use a cyberspace source (`fofa`, `hunter`)
- Feed output into `gogo`, `spray`, `katana`, or `neutron` for active scanning.

Skip `passive` when the user already provided concrete IPs or private ranges.

## Usage

### Company recon

```bash
passive -s aqc_unauth -n "默安科技"
passive -s tyc -n "深信服" -d 2 -p 0.5
```

Flags for company sources:

- `-n <name>` — target company name (required)
- `-d <int>` — recursion depth, overriding `recon.ani_depth` (default 1)
- `-p <float>` — min ownership ratio 0–1, overriding `recon.ani_percent` (default 0.5)

### Cyberspace recon

```bash
passive -s fofa 'domain="example.com"'
passive -s fofa 'icp="浙ICP备16020926号"'
passive -s hunter 'domain.suffix="example.com"'
```

The positional argument is the source-native query string.

## Output

### Company sources

JSON object keyed by company name (Python `ani -t json` compatible):

```json
{"默安科技":{"name":"默安科技","perc":1.0,"aqcid":"22712286710526","icp":"浙ICP备16020926号","icps":[{"icp":"浙ICP备16020926号","domain":"moresec.com.cn","title":"默安科技"}],"parent":null}}
```

Extract domains: `passive -s aqc_unauth -n "默安科技" | jq -r 'to_entries[] | .value.icps[]? | .domain' | sort -u`

### Cyberspace sources

JSON array (Python `InaData.to_dict()` compatible):

```json
[{"ip":"1.2.3.4","port":"443","url":"https://example.com","domain":"example.com","title":"Example","icp":"..."}]
```

Hunter includes extra fields: `status`, `company`, `frame`.

## Typical Pipeline

1. `passive -s aqc_unauth -n "目标公司"` → get ICP domains
2. `passive -s fofa 'icp="京ICP备xxx号"'` → get IPs/URLs
3. `gogo` / `spray` / `katana` → active scan discovered assets

## Notes

- AQC unauth endpoints rate-limit by IP; set `recon.ani_proxy` if blocked.
- Hunter blocks overseas IPs; use `recon.proxy=socks5://...` for Hunter from abroad.
- ICP data may lag reality; treat company→domain mapping as leads, not authoritative.
- For wide investment graphs, raising `-d` past 2 explodes quickly; keep `-p 0.5` as default.
