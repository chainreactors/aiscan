#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_FILE="${AISCAN_CONFIG:-$ROOT_DIR/config.yaml}"

MODEL="${CLAUDECODE_MODEL:-}"
BASE_URL="${CLAUDECODE_ANTHROPIC_BASE_URL:-${ANTHROPIC_BASE_URL:-}}"
API_KEY="${CLAUDECODE_ANTHROPIC_API_KEY:-${ANTHROPIC_API_KEY:-${AISCAN_API_KEY:-}}}"
BUDGET="${CLAUDECODE_MAX_BUDGET_USD:-2.00}"
PERMISSION_MODE="${CLAUDECODE_PERMISSION_MODE:-bypassPermissions}"
OUTPUT_DIR="${CLAUDECODE_BUTIAN_OUT:-$ROOT_DIR/out/claudecode-butian}"

TASK=""
TARGETS=()
CLAUDE_EXTRA=()

usage() {
  cat <<'EOF'
Usage:
  scripts/butian_claudecode.sh -i <target> [-i <target> ...] [-p <task>] [-- <extra claude args>]

Runs Claude Code against the local aiscan workspace for an authorized SRC/Butian-style assessment.

Environment overrides:
  CLAUDECODE_ANTHROPIC_BASE_URL  Anthropic-compatible base URL for Claude Code.
                                 Use host root, not /v1. If config.yaml has /v1,
                                 this script strips it automatically.
  CLAUDECODE_ANTHROPIC_API_KEY   API key. Falls back to ANTHROPIC_API_KEY,
                                 AISCAN_API_KEY, then config.yaml llm.api_key.
  CLAUDECODE_MODEL               Default: config.yaml llm.model if it starts
                                 with claude-, otherwise claude-opus-4.8.
  CLAUDECODE_MAX_BUDGET_USD      Default: 2.00. Set empty to disable the flag.
  CLAUDECODE_PERMISSION_MODE     Default: bypassPermissions.
  CLAUDECODE_BUTIAN_OUT          Default: out/claudecode-butian.

Examples:
  scripts/butian_claudecode.sh -i https://example.com
  scripts/butian_claudecode.sh -i example.com -p "只做信息泄露和弱口令验证"
  CLAUDECODE_MAX_BUDGET_USD=10 scripts/butian_claudecode.sh -i https://example.com -- --verbose
EOF
}

yaml_string() {
  local name="$1"
  local file="$2"
  [ -f "$file" ] || return 0
  awk -F'"' -v name="$name" '
    $0 ~ "^[[:space:]]+" name ":[[:space:]]*\"" { print $2; exit }
  ' "$file"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -i|--input|--target)
      [ "$#" -ge 2 ] || { echo "missing value for $1" >&2; exit 2; }
      TARGETS+=("$2")
      shift 2
      ;;
    -p|--prompt|--task)
      [ "$#" -ge 2 ] || { echo "missing value for $1" >&2; exit 2; }
      TASK="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      CLAUDE_EXTRA+=("$@")
      break
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ "${#TARGETS[@]}" -eq 0 ]; then
  echo "at least one -i/--input target is required" >&2
  usage >&2
  exit 2
fi

if ! command -v claude >/dev/null 2>&1; then
  echo "claude command not found in PATH" >&2
  exit 1
fi

if [ -z "$BASE_URL" ]; then
  BASE_URL="$(yaml_string base_url "$CONFIG_FILE")"
fi
BASE_URL="${BASE_URL%/}"
BASE_URL="${BASE_URL%/v1}"

if [ -z "$BASE_URL" ]; then
  BASE_URL="https://api.anthropic.com"
fi

if [ -z "$API_KEY" ]; then
  API_KEY="$(yaml_string api_key "$CONFIG_FILE")"
fi

if [ -z "$API_KEY" ]; then
  echo "missing Claude Code API key: set CLAUDECODE_ANTHROPIC_API_KEY, ANTHROPIC_API_KEY, AISCAN_API_KEY, or llm.api_key in config.yaml" >&2
  exit 1
fi

if [ -z "$MODEL" ]; then
  cfg_model="$(yaml_string model "$CONFIG_FILE")"
  if [[ "$cfg_model" == claude-* ]]; then
    MODEL="$cfg_model"
  else
    MODEL="claude-opus-4.8"
  fi
fi

mkdir -p "$OUTPUT_DIR"

settings_file="$(mktemp /tmp/claude-kiro-settings.XXXXXX.json)"
cleanup() {
  rm -f "$settings_file"
}
trap cleanup EXIT
chmod 600 "$settings_file"
printf '{"env":{"ANTHROPIC_BASE_URL":"%s","ANTHROPIC_API_KEY":"%s","ANTHROPIC_AUTH_TOKEN":"","CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":"1"}}\n' \
  "$BASE_URL" "$API_KEY" > "$settings_file"

target_lines=""
for target in "${TARGETS[@]}"; do
  target_lines+="- ${target}"$'\n'
done

if [ -z "$TASK" ]; then
  TASK="对授权范围内目标做补天/SRC 风格漏洞挖掘：先快速摸清资产与技术栈，再优先验证高价值、可复现、低破坏的漏洞线索，最后输出可提交报告草稿。"
fi

read -r -d '' PROMPT <<EOF || true
你现在在 aiscan 仓库根目录执行一个授权的补天/SRC 风格安全评估任务。只允许测试用户给出的目标范围，不要扩展到无关资产；不要做破坏性操作、持久化、数据删除、批量撞库、拒绝服务或越权读取敏感业务数据。需要高风险验证时，优先选择非破坏性证明。

目标范围：
${target_lines}
任务要求：
${TASK}

可用本地工具：
- ./aiscan scan -i <target> --mode quick|full --report
- ./aiscan agent -p "<task>" -i <target> -s scan
- ./aiscan gogo / spray / zombie / neutron 可按需直接调用

建议执行方式：
1. 在 $OUTPUT_DIR 下为本次任务创建记录目录，保存关键命令输出、报告和证据。
2. 先用 ./aiscan scan 对每个目标做 quick 初筛；有明显 Web 面再按需 full、sniper 或 deep。
3. 对命中的漏洞线索做最小化复现，记录请求、响应、影响、复现步骤、修复建议和不确定点。
4. 最终输出中文报告：已测范围、发现列表、每个发现的严重性、证据、复现步骤、风险影响、修复建议；没有确认漏洞时说明已测试内容和剩余可跟进线索。
EOF

cmd=(
  claude
  --bare
  --setting-sources project,local
  --settings "$settings_file"
  --print
  --model "$MODEL"
  --permission-mode "$PERMISSION_MODE"
  --add-dir "$ROOT_DIR"
  --output-format text
)

if [ -n "$BUDGET" ]; then
  cmd+=(--max-budget-usd "$BUDGET")
fi

cmd+=("${CLAUDE_EXTRA[@]}")

echo "[butian-claudecode] base=$BASE_URL model=$MODEL budget=${BUDGET:-none} out=$OUTPUT_DIR" >&2
cd "$ROOT_DIR"
"${cmd[@]}" "$PROMPT"
