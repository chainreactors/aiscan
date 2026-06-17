#!/usr/bin/env bash
set -euxo pipefail

export DEBIAN_FRONTEND=noninteractive

BUTIAN_HOME="${BUTIAN_HOME:-/opt/butian}"
KALI_IMAGE="${BUTIAN_KALI_IMAGE:-docker.m.daocloud.io/kalilinux/kali-rolling:latest}"
KALI_CONTAINER="${BUTIAN_KALI_CONTAINER:-kali-tools}"
KALI_PROFILE="${BUTIAN_KALI_PROFILE:-standard}"
KALI_APT_MIRROR="${BUTIAN_KALI_APT_MIRROR:-http://mirrors.aliyun.com/kali}"
CLAUDE_PACKAGE="${BUTIAN_CLAUDE_PACKAGE:-@anthropic-ai/claude-code}"
AISCAN_REPO="${BUTIAN_AISCAN_REPO:-https://github.com/chainreactors/aiscan.git}"
AISCAN_REF="${BUTIAN_AISCAN_REF:-}"
AISCAN_PROFILE="${BUTIAN_AISCAN_PROFILE:-mini}"
PIP_INDEX_URL="${BUTIAN_PIP_INDEX_URL:-https://mirrors.aliyun.com/pypi/simple}"
YSOSERIAL_REPO="${BUTIAN_YSOSERIAL_REPO:-https://github.com/frohoff/ysoserial.git}"

mkdir -p "$BUTIAN_HOME"/{bin,work,logs,config}

apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl wget git jq unzip zip tmux screen vim nano less \
  dnsutils whois iproute2 iputils-ping netcat-openbsd openssh-client \
  python3 python3-pip python3-venv pipx nodejs npm golang-go docker.io \
  chromium xvfb fonts-liberation fonts-noto-core fonts-noto-cjk fonts-noto-color-emoji

systemctl enable --now docker || service docker start || true

mkdir -p /etc/docker
cat > /etc/docker/daemon.json <<'EOF'
{
  "registry-mirrors": [
    "https://docker.1ms.run",
    "https://docker.xuanyuan.me",
    "https://docker.m.daocloud.io",
    "https://registry.docker-cn.com",
    "https://docker.mirrors.ustc.edu.cn"
  ]
}
EOF
systemctl daemon-reload || true
systemctl restart docker || service docker restart || true

docker_pull_retry() {
  local image="$1"
  local attempt
  if docker image inspect "$image" >/dev/null 2>&1; then
    return 0
  fi
  for attempt in 1 2 3 4 5; do
    if docker pull "$image"; then
      return 0
    fi
    sleep $((attempt * 10))
  done
  return 1
}

cat > "$BUTIAN_HOME/bin/kali" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
container="${KALI_CONTAINER:-kali-tools}"
exec docker exec -i "$container" bash -lc "$*"
EOF
chmod +x "$BUTIAN_HOME/bin/kali"

cat > "$BUTIAN_HOME/bin/kali-shell" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
container="${KALI_CONTAINER:-kali-tools}"
exec docker exec -it "$container" bash
EOF
chmod +x "$BUTIAN_HOME/bin/kali-shell"

cat > "$BUTIAN_HOME/bin/claude-kiro" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

base="${CLAUDECODE_ANTHROPIC_BASE_URL:-${ANTHROPIC_BASE_URL:-https://kiro.chainreactors.cn}}"
base="${base%/}"
base="${base%/v1}"
model="${CLAUDECODE_MODEL:-claude-opus-4.8}"
budget="${CLAUDECODE_MAX_BUDGET_USD:-}"

if [ -z "${CLAUDECODE_ANTHROPIC_API_KEY:-${ANTHROPIC_API_KEY:-}}" ]; then
  echo "missing CLAUDECODE_ANTHROPIC_API_KEY or ANTHROPIC_API_KEY" >&2
  exit 1
fi

settings="$(mktemp /tmp/claude-kiro-settings.XXXXXX.json)"
cleanup() { rm -f "$settings"; }
trap cleanup EXIT
chmod 600 "$settings"
printf '{"env":{"ANTHROPIC_BASE_URL":"%s","ANTHROPIC_API_KEY":"%s","ANTHROPIC_AUTH_TOKEN":"","CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":"1"}}\n' \
  "$base" "${CLAUDECODE_ANTHROPIC_API_KEY:-${ANTHROPIC_API_KEY:-}}" > "$settings"

cmd=(claude --bare --setting-sources project,local --settings "$settings" --model "$model" --permission-mode bypassPermissions)
if [ -n "$budget" ]; then
  cmd+=(--max-budget-usd "$budget")
fi
exec "${cmd[@]}" "$@"
EOF
chmod +x "$BUTIAN_HOME/bin/claude-kiro"

# ── srchunt skill(claude-code 原生方法论,纯 claude,无 aiscan)──
mkdir -p /root/.claude/skills/srchunt
base64 -d > /root/.claude/skills/srchunt/SKILL.md <<'SRCHUNT_B64'
LS0tCm5hbWU6IHNyY2h1bnQKZGVzY3JpcHRpb246ID4KICDmjojmnYMgU1JDIC8g5LyX5rWL5ryP5rSe5oyW5o6Y44CC55So5LqO5Zyo5o6I5p2D6IyD5Zu05YaF5a+5IFdlYiDnm67moIflgZrkvqblr5/jgIHlhYjnkIbop6PlkI7mjJbjgIEKICDmjInnm67moIfnibnlvoHpgInmlLvlh7vlkJHph48oUk9JIOi3r+eUsSnjgIHku6Xor4Hmja7noa7orqTmvI/mtJ7jgIHkuqflh7rlj6/lpI3njrDmiqXlkYrjgIIKICDnuq8gY2xhdWRlLWNvZGUg6amx5YqoLOaJq+aPj+W3peWFt+i1sCBga2FsaSAnPHRvb2w+J2AobnVjbGVpL2h0dHB4L3N1YmZpbmRlci9rYXRhbmEvbmFhYnUg562JKeOAggotLS0KCiMgU1JDIOa8j+a0nuaMluaOmAoK5L2g5Y2P5Yqp5a6J5YWo56CU56m25ZGY5ZyoKirlt7LmjojmnYMqKuebruagh+S4iuWPkeeOsOW5tumqjOivgea8j+a0nuOAguebruagh+eahOiMg+WbtOS4juWQiOazleaAp+eUseW5s+WPsC9TUkMg5Zyo6L+b5YWl5L2g5LmL5YmN5a6M5oiQO+S9oOeahOiBjOi0o+aYr+WPkeeOsOa8j+a0nuOAgeeUqOivgeaNrumqjOivgeOAgeS6p+WHuuWPr+WkjeeOsOaKpeWRiuOAguiHquS4u+aOqOi/m+ebtOWIsOS7u+WKoeWujOaIkOOAggoKIyMg6IyD5Zu0KOS4gOihjCkKCuWPquWcqOaOiOadg+iMg+WbtOWGheaJkyzliKvlgZrnoLTlnY/mgKfliqjkvZwoRG9TIC8g5Y6L5rWLIC8g5Yig5pS55rGh5p+T5pWw5o2uIC8g5pKe5bqTIC8g6LaK5p2D6K+755yf5a6e5pWP5oSf5Lia5Yqh5pWw5o2uKeOAguWFtuS9meS6pOe7meS9oOeahOWIpOaWreOAggoKIyMg5bel5YW3CgpjbGF1ZGUtY29kZSDljp/nlJ86YGJhc2hgKOaJp+ihjOWRveS7pCnjgIFgcmVhZGAvYHdyaXRlYC9gZ2xvYmAo5paH5Lu2KeOAgWB3ZWJfc2VhcmNoYCjmn6UgQ1ZFL+WFrOWRii9leHBsb2l0KeOAgWBmZXRjaGAo5Y+W5Y2V5LiqIFVSTCnjgIIKCuaJq+aPj+W3peWFt+WcqCBLYWxpIOWuueWZqOmHjCznu5/kuIDnlKggYGthbGkgJzxjbWQ+J2Ag6LCDKCoq5Yir6Ieq5bexIGFwdCDoo4XjgIHliKvmiYvmkrggc29ja2V0KiopOgoKYGBgYmFzaAprYWxpICdzdWJmaW5kZXIgLWQgZXhhbXBsZS5jb20gLXNpbGVudCcgICAgICAgICAgICAgICAgICMg5a2Q5Z+f5p6a5Li+CmthbGkgJ25hYWJ1IC1ob3N0IHgueC54LnggLXRvcC1wb3J0cyAxMDAwJyAgICAgICAgICAgICAgIyDnq6/lj6MKa2FsaSAnaHR0cHggLWwgaG9zdHMudHh0IC10aXRsZSAtdGVjaC1kZXRlY3QgLXNjJyAgICAgICAjIEhUVFAg5o6i5rS7K+aMh+e6uQprYWxpICdrYXRhbmEgLXUgaHR0cHM6Ly90IC1kIDIgLWpjIC10aW1lb3V0IDYwJyAgICAgICAgICMg54is6Jmr5oqT5o6l5Y+jL0pTKOimgeWPguaVsOWKoCAtZiBxdXJsKQprYWxpICdudWNsZWkgLXUgaHR0cHM6Ly90IC1zZXZlcml0eSBjcml0aWNhbCxoaWdoJyAgICAgICMg5qih5p2/5YyW5ryP5omrKC1jIDQwIOWIq+aLiea7oSkKa2FsaSAnbm1hcCAtc1YgLXAtIC0tbWluLXJhdGUgMzAwMCB4LngueC54JyAgICAgICAgICAgICAjIOerr+WPoy/mnI3liqEKa2FsaSAnc3FsbWFwIC11ICI8dXJsPj9pZD0xIiAtLWJhdGNoIC0tbGV2ZWwgMicgICAgICAgICAjIOazqOWFpemqjOivgQprYWxpICdmZnVmIC11IGh0dHBzOi8vdC9GVVpaIC13IDx3b3JkbGlzdD4nICAgICAgICAgICAgICMg55uu5b2VL+WPguaVsCBmdXp6CmBgYArmuLLmn5Mv5oiq5Zu+OmBwdy1zY3JlZW5zaG90IDx1cmw+IDxvdXQucG5nPmA75aSN5p2C5rWP6KeI5Zmo6ISa5pysOmBwdy1weXRob24gPHNjcmlwdC5weT5g44CCCgojIyDmjJbmtJ7pqbHliqjluo/liJco5YWI55CG6Kej5YaN5oyWLOWJjeWbm+atpeS4jeWHhui3sykKCuaooeWei+eahOacrOiDveaYryoq5qih5byP5Yy56YWN4oaS6LS05qCH562+Kioo55yL5YiwIENPUlMg5bCx5oOz5ZaK5ryP5rSeKeOAguWOi+S9j+WugyzlhYjnkIbop6Plho3liqjmiYs6CgoxLiAqKuS/oeaBr+aUtumbhioqIOKAlCDotYTkuqcv5a2Q5Z+fL+err+WPoy/mjIfnurkvV2ViIOi3r+eUsS9BUEkvSlMo5YWo6YePLOWIq+WPqueci+mmlumhtSBidW5kbGUpCjIuICoq5qKz55CG57O757uf5p625p6EKiog4oCUIOacieWTquS6m+aooeWdl+OAgeiwgeiwg+iwgeOAgeaVsOaNruaAjuS5iOa1geOAgeS/oeS7u+i+ueeVjOWcqOWTqgozLiAqKuWIhuaekOaOpeWPo+S4muWKoSoqIOKAlCDmr4/kuKrlhbPplK7mjqXlj6Mi5Zyo5bmy5ZibIjrovpPlhaXovpPlh7rjgIHku6PooajnmoTkuJrliqHliqjkvZzjgIHosIHmnKzor6Xog73osIMKNC4gKirnkIbop6PlvIDlj5HogIXpgLvovpEqKiDigJQg6Ym05p2D5Zyo5ZOq5YGa44CBSUQg5oCO5LmI55Sf5oiQ44CB5ZOq6YeM5Zu+55yB5LqL44CB5ZOq6YeM5YGH6K6+IuWJjeerr+S8muaLpiIKNS4gKirmvI/mtJ7mjJbmjpgqKiDigJQgKirlj6rmnInlnKjliY3lm5vmraXnkIbop6PkuYvkuIoqKizmiY3ov5vkuIvpnaLnmoQgUk9JIOi3r+eUsemAieaUu+WHu+WQkemHjwoK55CG6Kej5LiN5Yiw5L2N5bCx5byA5omTID0g6LS05qCH562+ID0g5oyH6bm/5Li66ams44CCCgojIyBST0kg6Lev55SxKOaMieebruagh+eJueW+gemAieaWueWQkSzkuI3mmK/lm7rlrprmtYHnqIspCgotIOacieeZu+W9lSAvIOi0puaIt+i+ueeVjCDihpIgKirotormnYMgLyBJRE9SKiog5LyY5YWICi0gQVBJIC8gU3dhZ2dlci9PcGVuQVBJIOKGkiAqKuacquaOiOadg+iuv+mXriArIOinkuiJsui+ueeVjCoqIOS8mOWFiAotIOS4iuS8oCAvIOWvvOWFpSAvIOWqkuS9kyDihpIgKirkuIrkvKDmjqfliLYgKyDkuIrkvKDlkI7orr/pl64qKiDkvJjlhYgKLSDmkJzntKIgLyDov4fmu6QgLyDlr7zlh7ogLyBzb3J0IC8gb3JkZXJCeSDihpIgKirms6jlhaUgKyDmlbDmja7ovrnnlYwqKiDkvJjlhYgKLSBHcmFwaFFMIOKGkiAqKuacquaOiOadgyBxdWVyeS9tdXRhdGlvbiDnmoTlvbHlk40qKiDkvJjlhYgoaW50cm9zcGVjdGlvbiDmnKzouqvkuI3mmK/mvI/mtJ4pCi0g5Y+v6KeB6Z2i5b6I6JaEIOKGkiDnlKjmnIDmt7HnmoTniKzomavnv7sgKipKUyDnq6/ngrkgLyBzb3VyY2UgbWFwIC8g6Lev55SxIC8g6ZqQ6JeP5Y+C5pWwKioKCuW8uuWBh+iuvue7mSAyLTQg5qyh57K+5YeG5bCd6K+VO+acieaUueWWhOa3seWFpSzml6DmlrDkv6Hlj7fmoIcgZGVhZCDmjaLmlrnlkJHjgIIqKjIwIOS4qui/keS8vCBwYXlsb2FkIOS4jeWmgiAzIOS4queyvuWHhuWwneivleOAgioqCgojIyDotYTkuqfliIbmtYEo5omr5Ye6ID4yMCDkuKogV2ViIOerr+eCueaXtikKCjEuIOWIq+Wvueavj+S4querr+eCuSBgZmV0Y2hgLOWFiOeci+aJq+aPj+axh+aAu+WIhua1geOAggoyLiDkvJjlhYg65bim5p+l6K+i5Y+C5pWw55qE44CB6Z2e5qCH5YeG56uv5Y+j44CB5pyJ5oSP5oCd55qE5oyH57q5KOWQjuWPsC9BUEkv55m75b2V6aG1KeOAggozLiDpgIkgMy04IOS4qumrmOS7t+WAvOebruagh+a3seaMljvot7Pov4cgQ0RO44CB6Z2Z5oCB6LWE5rqQ44CB6buY6K6k6aG144CB5bey55+l56ys5LiJ5pa544CCCjQuIOmAieS4reebruagh+i3r+eUsS/lj4LmlbDlsYLoloTlsLHlhYjnlKgga2F0YW5hIOeIrOS4gOmBjTvmjIkgaG9zdC9wYXRoL+WPguaVsOW9ouaAgeWIhue7hCzliKvpgJDkuKrlloLlm57mqKHlnovjgIIKNS4gYGZldGNoYCDotoXml7bnq4vliLvot7Pov4cs5LiN6YeN6K+V44CCCjYuIOaMieaMh+e6uS/mioDmnK/moIjliIbnu4Qs5q+P57uE5rWL5LiA5Liq5Luj6KGoLOS4jeaYr+avj+S4quWunuS+i+mDvea1i+OAggoKIyMg6aqM6K+B5qCH5YeGKOeOsOixoSB2cyDnu5PmnpwpCgrmiavmj4/lmajovpPlh7rmmK8qKue6v+e0oizkuI3mmK/noa7orqTmvI/mtJ4qKuOAgueKtuaAgeeggS9iYW5uZXIv5oyH57q5L+m7mOiupOmhtS/mqKHmnb/lkb3kuK0v54mI5pys5Y+34oCU4oCU6YO95LiN5aSf44CCCgoqKuehruiupOWPr+aKpea8j+a0niA9IOWboOaenOmTvioqOui/meS4quW6lOeUqOeahOiupOivgS/mjojmnYPmnLrliLYgKyDnoa7lrp7lrZjlnKjlj6/nqoPlj5Yv5Y+v56C05Z2P55qE5pWP5oSf5a+56LGhICsg5L2g5p6E6YCgIFBvQyDlrp7pmYXmi7/liLDkuoblroPjgIIqKuaXoOWPr+aJp+ihjCBQb0Mo6Ieq5YyF5ZCrIGN1cmwv5Y2P6K6u5ZG95LukL+a1j+iniOWZqOWbnuaUvik9IOS4jeWtmOWcqCzkuI3miqXjgIIqKgoKLSDmipHliLbni6znq4vnmoQgUDMv5L2O5Y2xL+S/oeaBr+exuyzpmaTpnZ7nlKjmiLfopoHotYTkuqfmuIXljZXjgIHmiJblroPog73kuLLmiJDmnInlvbHlk43nmoTpk77jgIIKLSDov5nkupvpu5jorqTlj6rlvZPnur/ntKIs6Zmk6Z2e6K+B5piO5LqG5b2x5ZONOmZpbmdlcnByaW5044CB54mI5pys44CB5byA5pS+56uv5Y+j44CBQ09SUy/lronlhajlpLTjgIHmqKHmnb/lkb3kuK3jgIHms5sgMjAw44CB55m75b2V6aG144CB6buY6K6k6aG144CBc2VsZi1YU1PjgIFvcGVuIHJlZGlyZWN044CBR3JhcGhRTCBpbnRyb3NwZWN0aW9u44CB5pyq5oiQ6ZO+55qE5Y6f6K+t44CCCi0gKirotormnYMvSURPUioqOuaUueS4gOS4qiBJRCDlj6rmmK/nur/ntKLjgILmnInmnaHku7blsLHmtYsgKiozLTUg5LiqKirnm7jpgrsv6Leo6LSm5oi3IElEIOWGjeS4i+e7k+iuuuOAggotICoqSlMg5Y+R546wKio65YiX5riF5L2g5p+l6L+H5ZOq5Lqb5p2l5rqQKOa4suafk+iEmuacrC9idW5kbGUvc291cmNlIG1hcC/ot6/nlLHmuIXljZUv572R57uc5rWB6YePL+W9kuahoyk75rKh5p+l5YWo5bCx6K+05piO5piv5oq95qC3LOWIq+WjsOensCLpmpDol4/nq6/ngrnlt7Lopobnm5blrowi44CCCi0g5Yik5pat57q/57Si5pe2OuS8mOWFiOebtOaOpeWPr+WkjeeOsOivgeaNruiAjOmdnuW3peWFt+agh+etvjvkvp3otZbooYzkuLrlt67lvILnmoTlr7nnhacgYmFzZWxpbmU756Gu6K6k5LiN5pivIFdBRiDmi6bmiKov55m75b2V6aG1L0NETiDpu5jorqTpobUv5pys5bCx5YWs5byA55qE56uv54K5L+aWh+aho+WMluWKn+iDveOAggoKIyMg6K+B5o2uCgrmlLbpm4bmlK/mkpHnu5PorrrnmoQqKuacgOWwj+ivgeaNrioqOuefreWTjeW6lOeJh+auteOAgeWTiOW4jOOAgeiuoeaVsOOAgeaIquWbvuOAgeaJq+aPj+i+k+WHuuW8leeUqOOAgioq5LiN6KaBKirmi4nlj5blr4bpkqXjgIHkuKrkurrmlbDmja7jgIHmlbDmja7lupMgZHVtcOOAgeWkp+aWh+S7tizpmaTpnZ7nlKjmiLfmmI7noa7opoHmsYLmjojmnYPlpI3njrDkuJTml6Dms5XnlKjmm7TlronlhajnmoTmlrnlvI/or4HmmI7jgIIKCiMjIGZpbmRpbmdzIOaXpeW/lwoK6ZW/5Lu75YqhL+W5v+imhuebluaXtizmiorlt7Lnoa7orqQgZmluZGluZyDorrDov5sgYGZpbmRpbmdzLm1kYDrnm67moIfjgIHmvI/mtJ7nsbvlnovjgIHkuKXph43luqbjgIHkuIDlj6Xor53mkZjopoHjgIHlj6/lpI3njrDlkb3ku6QvUG9D44CC5Ye65pyA57uI5oql5ZGK5YmN6YeN6K+744CC5Yir5Li65YeR5pWw57yW57q/57Si5oiW5omp6IyD5Zu044CCCgojIyDmiqXlkYooUDIrIOaJjeWGmSzlh7rmiqXlkYrliY3ph43or7vpqozor4HmoIflh4YpCgrmr4/ku73oh6rljIXlkKvjgIHlj6/lpI3njrAs5Lq65ou/5Yiw6IO955u05o6l5o+Q5LqkOgoKYGBgbWFya2Rvd24KIyMgW+S4pemHjeW6piBQWF0g5ryP5rSe5qCH6aKYKOivtOa4hee7k+aenCzkuI3mmK/njrDosaEpCioq6LWE5LqnKio6IDxpbi1zY29wZSBob3N0IC8gZW5kcG9pbnQ+Cioq57G75Z6LKio6IDxJRE9SIC8g5pyq5o6I5p2DIC8gU1FMaSAvIOS4muWKoemAu+i+kSDigKY+Cioq5b2x5ZONKioo57uT5p6cKTogPOWunumZheivu+WIsC/mlLnliLAv5ou/5Yiw5LqG5LuA5LmI44CC5L6LOuS7pSBBIOi2iuadg+ivuyBCIOeahOiuouWNlSvmiYvmnLrlj7c+Cioq5aSN546wKio6CjEuIDzliY3nva465aaC5L2V55m75b2VL+aLvyB0b2tlbj4KMi4gPOWFs+mUruivt+axgizlrozmlbQgY3VybD4KICAgYGBgYmFzaAogICBjdXJsIC1pICdodHRwczovLzxob3N0Pi9hcGkvb3JkZXI/aWQ9MTAwMicgLUggJ0F1dGhvcml6YXRpb246IEJlYXJlciA8QeeahHRva2VuPicKICAgYGBgCjMuIDzov5Tlm57ph4zlh7rnjrAgQiDnmoTmlbDmja4s6LS05YWz6ZSu5ZON5bqU54mH5q61PgoqKuWboOaenOivgeaYjioqOiDorqTor4HmnLrliLYgWCArIOaVj+aEn+WvueixoSBZICsg5a6e6ZmF6LaK5p2D5ou/5YiwIFrjgIIKKirkv67lpI3lu7rorq4qKjogPOS4gOWPpeivnT4KYGBgCgrlh7rmiqXlkYrliY3oh6rmo4A64pGgIOaYr+e7k+aenOS4jeaYr+eOsOixoT8g4pGhIFBvQyDog73nhafot5HlpI3njrA/IOKRoiDiiaVQMj8g4pGjIOi/h+S6humqjOivgeagh+WHhj/ku7vkuIDkuI3ov4cg4oaSIOS4ouW8g+OAggroi6XmiYDmnInmnZDmlpnpg73kvY7kuo4gUDIg5oiW5peg5Y+v5omn6KGM5aSN546wLCoq6ICB5a6e6K+0IuaXoOehruiupOWPr+aKpea8j+a0niIqKizkuI3opoHngYzmsLTmi5Tpq5jkuKXph43luqbjgIIKCiMjIOi/kOihjOinhOWImQoKMS4g5LyY5YWI6Z2e5Lqk5LqS6L6T5Ye6LOmBv+WFjei/m+W6puadoS9UVUkv5peg55WM6ZmQ5rWB44CCCjIuIOWvuSBsb2NhbGhvc3TjgIHohIblvLHmnI3liqHjgIHnqoTpqozor4HnlKjkv53lrojnur/nqIvlkozotoXml7bjgIIKMy4g6K+G5Yir5Ye65YW35L2T5Lqn5ZOBL+S4remXtOS7tuWQjizlhYggYHdlYl9zZWFyY2hgIOafpeW3suefpSBDVkUvUG9DLOWGjeaJi+WKqCBmdXp64oCU4oCU5b6I5aSa5bCx5piv5bey55+l5rSeLOWIq+ebsuaJk+OAggo0LiDkuIDkuKrmlrnlkJHnuqYgMjAg5YiG6ZKf5peg5pyJ55So6K+B5o2u5oiW5Yeg5qyh6LSf5ZCR5o6i5rWLIOKGkiDlrZjmoaPmjaLmlrnlkJHjgILkv53mjIHmjqLntKLmgKc6KirmlrnlkJHlkozmoIflh4bmr5Tlm7rlrprmraXpqqTph43opoHjgIIqKgo1LiDku7vliqHnm67moIfovr7miJDlsLHlgZw75bm/6KaG55uW5Lu75Yqh5Zyo6IyD5Zu05ZKM5pe26Ze05YWB6K645LiL6LaK6L+H56ys5LiA5Liq5Lil6YeNIGZpbmRpbmcg57un57utLOeqhOmqjOivgeS7u+WKoeebtOaOpeWbnuetlOWFt+S9k+mXrumimOOAggo2LiDor5rlrp465rKh5omT5LiL5p2l5YaZIHVuc29sdmVkICsg5pyq6K+V57q/57SiLOe7neS4jeaKiiLmjqXlj6PlrZjlnKgi5YyF6KOF5oiQIGZpbmRpbmcs57ud5LiN57yW6YCg57uT5p6c44CCCg==
SRCHUNT_B64

docker_pull_retry "$KALI_IMAGE"
if docker ps -a --format '{{.Names}}' | grep -qx "$KALI_CONTAINER"; then
  docker rm -f "$KALI_CONTAINER"
fi

docker run -d \
  --name "$KALI_CONTAINER" \
  --restart unless-stopped \
  --network host \
  -v "$BUTIAN_HOME/work:/work" \
  -w /work \
  "$KALI_IMAGE" \
  sleep infinity

docker exec "$KALI_CONTAINER" bash -lc "
  set -euo pipefail
  rm -f /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources
  cat > /etc/apt/sources.list <<'EOF'
deb ${KALI_APT_MIRROR} kali-rolling main contrib non-free non-free-firmware
EOF
  apt-get clean
  apt-get -o Acquire::Retries=5 -o Acquire::ForceIPv4=true -o Dpkg::Use-Pty=0 update
"
docker exec "$KALI_CONTAINER" bash -lc '
  export DEBIAN_FRONTEND=noninteractive
  apt-get -o Acquire::Retries=5 -o Acquire::ForceIPv4=true -o Dpkg::Use-Pty=0 install -y --no-install-recommends \
    ca-certificates curl wget git jq unzip zip vim nano less tmux screen \
    procps python3 python3-pip python3-venv pipx golang-go \
    dnsutils whois iproute2 iputils-ping netcat-openbsd openssh-client
'
docker exec "$KALI_CONTAINER" bash -lc '
  export DEBIAN_FRONTEND=noninteractive
  tools=(
    nmap masscan whatweb wafw00f nikto sqlmap wfuzz ffuf feroxbuster gobuster
    seclists wordlists hydra medusa testssl.sh dirb dirsearch enum4linux nbtscan
    smbclient ldap-utils redis-tools postgresql-client default-mysql-client
    default-jdk-headless maven
  )
  for pkg in "${tools[@]}"; do
    apt-get -o Acquire::Retries=5 -o Acquire::ForceIPv4=true -o Dpkg::Use-Pty=0 install -y --no-install-recommends "$pkg" || echo "WARN: apt package $pkg unavailable"
  done
'

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

if [ "$KALI_PROFILE" = "full" ]; then
  docker exec "$KALI_CONTAINER" bash -lc '
    export DEBIAN_FRONTEND=noninteractive
    apt-get -o Acquire::Retries=5 -o Acquire::ForceIPv4=true -o Dpkg::Use-Pty=0 install -y kali-tools-web kali-tools-information-gathering kali-tools-vulnerability || true
  '
fi

docker exec "$KALI_CONTAINER" bash -lc '
  export GOPATH=/opt/go
  export PATH=/usr/local/go/bin:/opt/go/bin:$PATH
  mkdir -p /opt/go/bin
  for pkg in \
    github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest \
    github.com/projectdiscovery/httpx/cmd/httpx@latest \
    github.com/projectdiscovery/subfinder/v2/cmd/subfinder@latest \
    github.com/projectdiscovery/katana/cmd/katana@latest \
    github.com/projectdiscovery/naabu/v2/cmd/naabu@latest \
    github.com/ffuf/ffuf/v2@latest
  do
    timeout 10m go install "$pkg" || echo "WARN: go install $pkg failed"
  done
  cp /opt/go/bin/* /usr/local/bin/ 2>/dev/null || true
  timeout 5m nuclei -update-templates || true
'

npm install -g "$CLAUDE_PACKAGE" || true

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

# (aiscan 不再构建 —— worker 纯 claude-code + srchunt skill,用不到 aiscan 二进制)

cat > "$BUTIAN_HOME/bin/start-butian-worker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

node="${BUTIAN_NODE_NAME:-$(hostname)}"
work_root="${BUTIAN_WORK_ROOT:-/opt/butian/work}"
work_dir="$work_root/$node"
targets="${BUTIAN_TARGETS:-}"
task="${BUTIAN_TASK:-对授权范围目标做补天/SRC 风格漏洞挖掘，优先找可复现、高价值、低破坏的漏洞线索。}"

mkdir -p "$work_dir"
cd "$work_dir"

if [ -z "$targets" ] && [ -f "$work_root/targets.txt" ]; then
  targets="$(cat "$work_root/targets.txt")"
fi

if [ -z "$targets" ]; then
  echo "BUTIAN_TARGETS is empty; write targets to $work_root/targets.txt or export BUTIAN_TARGETS" >&2
  exit 2
fi

cat > "$work_dir/targets.txt" <<TARGETS_EOF
$targets
TARGETS_EOF

target_lines="$(sed '/^[[:space:]]*$/d; s/^/- /' "$work_dir/targets.txt")"

read -r -d '' prompt <<PROMPT_EOF || true
/srchunt

你是补天/SRC 授权评估 worker：$node。按 srchunt skill 的方法论与标准执行(理解链五步、ROI 路由、现象 vs 结果、无 PoC=不存在、报告格式)。

只允许测试以下目标范围，不要扩展到无关资产：
$target_lines

任务：
$task

本机工具：
- kali '<command>' 在 Kali 容器内执行 nmap/nuclei/httpx/subfinder/katana/naabu/sqlmap/ffuf/feroxbuster/gobuster/whatweb/wafw00f/nikto 等。
- kali-shell 进入 Kali 容器。
- pw-screenshot <url> <output.png> 用 Playwright/Chromium 渲染截图;pw-python <script.py> 跑 Playwright 脚本。
- kali 'ysoserial' 仅用于授权范围内、已确认入口的 Java 反序列化验证,不默认批量触发 payload。

要求：
1. 关键输出/请求响应证据/截图/报告都存当前目录;确认 finding 记进 findings.md。
2. 严格按 srchunt:先理解四步再挖,只做非破坏性验证,P2+ 且有可复现 PoC 才写报告。
3. 最终中文报告:范围、方法、确认漏洞(带 curl/PoC)、未确认线索、证据、复现、影响、修复。
PROMPT_EOF

exec claude-kiro --print --output-format text --add-dir /opt/butian --add-dir "$work_dir" "$prompt"
EOF
chmod +x "$BUTIAN_HOME/bin/start-butian-worker"

cat > /etc/profile.d/butian-path.sh <<EOF
export PATH="$BUTIAN_HOME/bin:\$PATH"
export KALI_CONTAINER="$KALI_CONTAINER"
EOF

cat > "$BUTIAN_HOME/README.txt" <<EOF
Butian/Kali image bootstrap complete.

Host helpers:
  kali 'nmap -sV example.com'
  kali-shell
  claude-kiro --print '...'
  pw-screenshot https://example.com example.png
  pw-python script.py

Kali container:
  name: $KALI_CONTAINER
  image: $KALI_IMAGE
  workdir mounted at: $BUTIAN_HOME/work -> /work
  ysoserial is available as: kali 'ysoserial'

Secrets are intentionally not stored in this image. Export Claude/API keys at runtime.
EOF

docker exec "$KALI_CONTAINER" bash -lc 'command -v nmap; command -v nuclei; command -v httpx; command -v subfinder; command -v katana; command -v sqlmap; command -v ffuf'
echo "butian kali bootstrap complete at $(date -Is)" | tee "$BUTIAN_HOME/logs/bootstrap.done"
