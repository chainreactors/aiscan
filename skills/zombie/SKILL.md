---
name: zombie
description: Use this skill when working with zombie for authorized weak credential checks and authentication result analysis.
internal: true
---

# Zombie

Zombie is the weak credential checking tool in aiscan.

Capabilities:

- test supported network services for weak credentials when authorized
- report service URI, protocol, host, port, module, username, password, and authentication result
- distinguish successful credentials, failed attempts, connection errors, lockouts, and unsupported services
- expose retry, timeout, and module-specific messages

Common usage:

```bash
zombie -i 192.168.1.1:3306 -s mysql
zombie -i 192.168.1.1:22 -s ssh -u root -p admin123,123456,root
zombie -i 192.168.1.1:6379 -s redis
zombie -i 192.168.1.1:15672 -s rabbitmq
zombie -I targets.txt -s ssh --top 10
zombie -i 192.168.1.1:8080 -s tomcat -u admin,tomcat -p admin,tomcat,s3cret
```

Key flags:

- `-i`: target ip:port (can repeat: `-i host1:3306 -i host2:3306`). Also accepts `ssh://user@host:22` URL format.
- `-s`: service name — **required** when target has no scheme prefix. Services: ssh, mysql, redis, ftp, rdp, smb, tomcat, nacos, minio, rabbitmq, etc.
- `-u`: username(s), comma-separated or repeated.
- `-p`: password(s), comma-separated or repeated.
- `-I`: target file (uppercase I, one ip:port per line).
- `--top N`: use top N common passwords from built-in dictionary.
- `-t`: thread count (default 100). **Not** target — use `-i` for target.
- `-l`: **list supported services and exit** (not a file flag!).

Common mistakes:

```bash
# WRONG:
zombie -t 10.0.0.1 -p 3306 -s mysql    # -t is threads, not target
zombie -l targets.txt -s ssh            # -l lists services, not reads file
zombie -i 10.0.0.1:3306                 # missing -s mysql

# RIGHT:
zombie -i 10.0.0.1:3306 -s mysql
zombie -I targets.txt -s ssh
zombie -i 10.0.0.1:3306 -s mysql -t 50
```
