#!/usr/bin/env bash
set -euxo pipefail

export DEBIAN_FRONTEND=noninteractive

BUTIAN_HOME="${BUTIAN_HOME:-/opt/butian}"
KALI_CONTAINER="${BUTIAN_KALI_CONTAINER:-kali-tools}"
KALI_APT_MIRROR="${BUTIAN_KALI_APT_MIRROR:-http://mirrors.aliyun.com/kali}"
PIP_INDEX_URL="${BUTIAN_PIP_INDEX_URL:-https://mirrors.aliyun.com/pypi/simple}"
YSOSERIAL_REPO="${BUTIAN_YSOSERIAL_REPO:-https://github.com/frohoff/ysoserial.git}"

mkdir -p "$BUTIAN_HOME"/{bin,logs}

apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl python3 python3-venv python3-pip \
  chromium xvfb fonts-liberation fonts-noto-core fonts-noto-cjk fonts-noto-color-emoji

python3 -m venv "$BUTIAN_HOME/playwright-venv"
timeout 10m "$BUTIAN_HOME/playwright-venv/bin/pip" install -i "$PIP_INDEX_URL" --upgrade pip setuptools wheel || true
timeout 10m "$BUTIAN_HOME/playwright-venv/bin/pip" install -i "$PIP_INDEX_URL" playwright || true

cat > "$BUTIAN_HOME/bin/pw-python" <<EOF
#!/usr/bin/env bash
set -euo pipefail
export PLAYWRIGHT_CHROMIUM_PATH="\${PLAYWRIGHT_CHROMIUM_PATH:-/usr/bin/chromium}"
exec "$BUTIAN_HOME/playwright-venv/bin/python" "\$@"
EOF
chmod +x "$BUTIAN_HOME/bin/pw-python"

cat > "$BUTIAN_HOME/bin/pw-screenshot" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
url="${1:?usage: pw-screenshot <url> [output.png]}"
out="${2:-screenshot.png}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$script_dir/pw-python" - "$url" "$out" <<'PY'
import asyncio
import os
import sys
from pathlib import Path
from playwright.async_api import async_playwright

async def main() -> None:
    url = sys.argv[1]
    out = Path(sys.argv[2])
    out.parent.mkdir(parents=True, exist_ok=True)
    async with async_playwright() as p:
        browser = await p.chromium.launch(
            headless=True,
            executable_path=os.environ.get("PLAYWRIGHT_CHROMIUM_PATH", "/usr/bin/chromium"),
            args=["--no-sandbox", "--disable-dev-shm-usage"],
        )
        page = await browser.new_page(viewport={"width": 1365, "height": 768}, ignore_https_errors=True)
        await page.goto(url, wait_until="networkidle", timeout=30000)
        await page.screenshot(path=str(out), full_page=True)
        title = await page.title()
        print(f"{out}\t{title}")
        await browser.close()

asyncio.run(main())
PY
EOF
chmod +x "$BUTIAN_HOME/bin/pw-screenshot"

if docker ps --format '{{.Names}}' | grep -qx "$KALI_CONTAINER"; then
  while docker exec "$KALI_CONTAINER" bash -lc 'pgrep -x apt-get >/dev/null || pgrep -x dpkg >/dev/null'; do
    sleep 20
  done
  docker exec "$KALI_CONTAINER" bash -lc "
    set -euo pipefail
    rm -f /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources
    cat > /etc/apt/sources.list <<'EOF'
deb ${KALI_APT_MIRROR} kali-rolling main contrib non-free non-free-firmware
EOF
    apt-get -o Acquire::Retries=5 -o Acquire::ForceIPv4=true -o Dpkg::Use-Pty=0 update
    export DEBIAN_FRONTEND=noninteractive
    apt-get -o Acquire::Retries=5 -o Acquire::ForceIPv4=true -o Dpkg::Use-Pty=0 install -y --no-install-recommends default-jdk-headless maven git ca-certificates
  "
  docker exec -e YSOSERIAL_REPO="$YSOSERIAL_REPO" "$KALI_CONTAINER" bash -lc '
    set -euo pipefail
    mkdir -p /usr/local/share/ysoserial
    if [ ! -f /usr/local/share/ysoserial/ysoserial.jar ]; then
      rm -rf /tmp/ysoserial-src
      if timeout 5m git clone --depth=1 "$YSOSERIAL_REPO" /tmp/ysoserial-src; then
        cd /tmp/ysoserial-src
        if timeout 15m mvn -q -DskipTests package; then
          jar="$(find target -maxdepth 1 -type f -name "*all*.jar" | sort | head -1)"
          if [ -z "$jar" ]; then
            jar="$(find target -maxdepth 1 -type f -name "ysoserial-*.jar" | sort | head -1)"
          fi
          if [ -n "$jar" ]; then
            cp "$jar" /usr/local/share/ysoserial/ysoserial.jar
          fi
        fi
      fi
    fi
    if [ -f /usr/local/share/ysoserial/ysoserial.jar ]; then
      cat > /usr/local/bin/ysoserial <<'"'"'YSO_EOF'"'"'
#!/usr/bin/env bash
default_opts="--add-opens java.base/sun.reflect.annotation=ALL-UNNAMED --add-opens java.base/java.lang=ALL-UNNAMED --add-opens java.xml/com.sun.org.apache.xalan.internal.xsltc.trax=ALL-UNNAMED"
opts="${YSOSERIAL_JAVA_OPTS:-$default_opts}"
exec java $opts -jar /usr/local/share/ysoserial/ysoserial.jar "$@"
YSO_EOF
      chmod +x /usr/local/bin/ysoserial
    else
      echo "WARN: ysoserial build failed; continuing without jar"
    fi
  '
fi

if [ -f "$BUTIAN_HOME/bin/start-butian-worker" ]; then
  if ! grep -q 'pw-screenshot <url>' "$BUTIAN_HOME/bin/start-butian-worker"; then
    sed -i "/kali-shell 可进入 Kali 容器。/a - pw-screenshot <url> <output.png> 可以用 Playwright/Chromium 做页面渲染截图。\\n- pw-python <script.py> 可以运行 Playwright Python 脚本。\\n- kali 'ysoserial' 仅用于授权范围内、已确认入口的 Java 反序列化验证，不要默认批量触发 payload。" "$BUTIAN_HOME/bin/start-butian-worker"
  fi
fi

cat > /etc/profile.d/butian-path.sh <<EOF
export PATH="$BUTIAN_HOME/bin:\$PATH"
export KALI_CONTAINER="$KALI_CONTAINER"
EOF

{
  echo "extra playwright/ysoserial setup complete at $(date -Is)"
  command -v chromium || true
  "$BUTIAN_HOME/bin/pw-python" - <<'PY' || true
import playwright
print("playwright", playwright.__version__ if hasattr(playwright, "__version__") else "installed")
PY
  docker exec "$KALI_CONTAINER" bash -lc 'command -v java; command -v mvn; command -v ysoserial || true' || true
} | tee "$BUTIAN_HOME/logs/extra-playwright-ysoserial.done"
