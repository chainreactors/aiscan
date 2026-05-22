---
name: ani
description: Use ani to expand a Chinese company name into company/ICP/domain leads through the ani-go SDK. Run this before ina or active scanners when the seed is a registered company name rather than a domain or IP.
---

# Ani

`ani` is an aiscan pseudo-command backed by `github.com/chainreactors/ani-go`.
It does not shell out to Python.

Five sources are available — pick with `-s <source>`:

| Source       | Provider                             | Credential                                | Endpoint                 |
| ------------ | ------------------------------------ | ----------------------------------------- | ------------------------ |
| `aqc_unauth` | 爱企查 / 百度系企查 (no login)        | none                                      | `qiye.baidu.com`         |
| `tyc_unauth` | 天眼查 WeChat mini-program API        | none                                      | `api9.tianyancha.com`    |
| `tyc`        | 天眼查 (www, authed for higher quota) | `recon.ani_tyc_token` (auth_token JWT)    | `www.tianyancha.com`     |
| `qcc`        | 企查查                                | `recon.ani_qcc_cookie` (QCCSESSID)        | `www.qcc.com`            |
| `aqc`        | 爱企查 (authed)                       | `recon.ani_aqc_cookie` (BAIDUID)          | `aiqicha.baidu.com`      |

Default source is `aqc_unauth`. Credentials may also come from env vars `ANI_TYC_TOKEN`, `ANI_QCC_COOKIE`, `ANI_AQC_COOKIE`. Sources without credentials are silently skipped at engine init — you'll get `unknown source` if you ask for `tyc` / `qcc` / `aqc` with no creds configured.

## When to Use

- The engagement target is a Chinese company name.
- You need subsidiaries and ICP-registered domain leads before cyberspace search.
- The user has no authoritative domain or IP list yet.

Skip `ani` when the user already provided domains or IPs.

## Usage

```bash
ani -n "默安科技"
ani -n "深信服" -d 2 -p 0.5
ani -n "目标公司" -s aqc_unauth
```

Supported wrapper flags:

- `-n <name>` / `--name <name>`: target company name, required
- `-d <int>` / `--depth <int>`: recursion depth, overriding `recon.ani_depth`
- `-p <float>` / `--percent <float>`: minimum ownership ratio, overriding `recon.ani_percent`
- `-s <source>` / `--source <source>`: one of `aqc_unauth`, `tyc_unauth`, `tyc`, `qcc`, `aqc`. Defaults to `aqc_unauth`.
- `-h` / `--help`

`recon.ani_proxy` configures the ani-go HTTP proxy.

## Output

Output matches Python `ani -t json`: a JSON object keyed by company name. Companies without ICP data are omitted by default.

```json
{"默安科技":{"name":"默安科技","perc":1.0,"aqcid":"22712286710526","icp":"浙ICP备16020926号","icps":[{"icp":"浙ICP备16020926号","domain":"moresec.com.cn","title":"默安科技"}],"parent":null}}
```

Fields mirror the Python object shape: `name`, `perc`, one source-specific ID (`aqcid`, `tycid`, or `qccid`), root `icp`, nested `icps`, and `parent` (`null` for the root company).

Pipe to `jq` to extract just the domain list:

```bash
ani -n "默安科技" | jq -r 'to_entries[] | .value.icps[]? | .domain' | sort -u
```

Feed discovered domains or ICPs into `ina`, then pass `ina` assets to `gogo`, `spray`, or `katana`.

## Notes & Limits

- AQC unauth endpoints rate-limit by IP. Set `recon.ani_proxy` if you hit a wall.
- ICP data lags reality by a few hours; freshly registered domains may be missing.
- Treat the company → domain mapping as a lead, not authoritative — it is scraper-derived.
- For wide investment graphs, raising `-d` past 2 explodes quickly; keep `-p 0.5` (majority-owned only) as the default leash.
