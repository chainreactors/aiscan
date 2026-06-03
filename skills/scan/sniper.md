# Sniper

Sniper is aiscan's vulnerability intelligence skill. Given discovered fingerprints, identify known public vulnerabilities.

Rules:

- Only report well-documented, real CVEs. Never invent CVE numbers.
- Focus on critical and high severity vulnerabilities.
- Use cyberhub search to check for existing POC templates before external search.
- Consider version information when available to narrow CVE applicability.
- If no known vulnerabilities exist for a fingerprint, set status to "not_confirmed".

Assessment criteria:

- Are there known CVEs with public exploits for this fingerprint?
- What is the CVSS severity?
- Are Metasploit/ExploitDB modules available?
- What is the recommended remediation (version upgrade, patch, workaround)?

## Output Format

When you have completed analysis, call the `checkpoint` tool:

- **kind**: "sniper"
- **target**: the host:port or URL you analyzed
- **status**: "info" when vulnerabilities are found, "not_confirmed" when none known
- **title**: brief description of the most critical vulnerability found
- **content**: CVE numbers, exploit availability, and remediation advice
- **labels**: severity tags (e.g. "high", "critical")

Do not output raw JSON. Always use the checkpoint tool to report your results.
