#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
USER_DATA="$ROOT_DIR/cloud/aliyun/butian-kali-cloud-init.sh"

REGION="${ALIYUN_REGION:-${ALIBABA_CLOUD_REGION:-cn-hangzhou}}"
ZONE_ID="${ALIYUN_ZONE_ID:-}"
VSWITCH_ID="${ALIYUN_VSWITCH_ID:-}"
SECURITY_GROUP_ID="${ALIYUN_SECURITY_GROUP_ID:-}"
BASE_IMAGE_ID="${ALIYUN_BASE_IMAGE_ID:-}"
INSTANCE_TYPE="${ALIYUN_INSTANCE_TYPE:-ecs.c8i.large}"
KEY_PAIR_NAME="${ALIYUN_KEY_PAIR_NAME:-}"
SYSTEM_DISK_SIZE="${ALIYUN_SYSTEM_DISK_SIZE:-80}"
SYSTEM_DISK_CATEGORY="${ALIYUN_SYSTEM_DISK_CATEGORY:-cloud_essd}"
BANDWIDTH_OUT="${ALIYUN_BANDWIDTH_OUT:-10}"
IMAGE_NAME="${ALIYUN_BUTIAN_IMAGE_NAME:-butian-kali-claudecode-$(date +%Y%m%d%H%M%S)}"
SPOT_STRATEGY="${ALIYUN_SPOT_STRATEGY:-SpotAsPriceGo}"
SWARM_AMOUNT="${ALIYUN_SWARM_AMOUNT:-3}"

usage() {
  cat <<'EOF'
Usage:
  scripts/aliyun_butian_image.sh check
  scripts/aliyun_butian_image.sh create-builder
  scripts/aliyun_butian_image.sh create-image <instance-id>
  scripts/aliyun_butian_image.sh launch-spot <custom-image-id>

Credentials:
  Export ALIBABA_CLOUD_ACCESS_KEY_ID and ALIBABA_CLOUD_ACCESS_KEY_SECRET.
  ALIBABACLOUD_ACCESS_KEY_ID / ALIBABACLOUD_ACCESS_KEY_SECRET are also accepted.
  The script does not write credentials to files.

Required ECS environment:
  ALIYUN_REGION              Default: cn-hangzhou
  ALIYUN_VSWITCH_ID          Required for create-builder/launch-spot
  ALIYUN_SECURITY_GROUP_ID   Required for create-builder/launch-spot
  ALIYUN_BASE_IMAGE_ID       Required for create-builder, use an Alibaba Linux/Debian/Ubuntu image with cloud-init

Optional:
  ALIYUN_ZONE_ID
  ALIYUN_INSTANCE_TYPE       Default: ecs.c8i.large
  ALIYUN_KEY_PAIR_NAME
  ALIYUN_SYSTEM_DISK_SIZE    Default: 80
  ALIYUN_SYSTEM_DISK_CATEGORY Default: cloud_essd
  ALIYUN_BANDWIDTH_OUT       Default: 10
  ALIYUN_BUTIAN_IMAGE_NAME
  ALIYUN_SWARM_AMOUNT        Default: 3
  ALIYUN_SPOT_STRATEGY       Default: SpotAsPriceGo

Flow:
  1. create-builder: create a pay-as-you-go template ECS and run cloud-init to install Docker, Kali tools, and Claude Code helper.
  2. create-image <instance-id>: create a custom image from the prepared template instance.
  3. launch-spot <image-id>: launch 3 preemptible instances from the custom image.

Notes:
  - Do not bake AK/SK, Claude keys, target scopes, cookies, or account credentials into the image.
  - Custom image snapshots continue to incur storage charges until deleted.
EOF
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing command: $1" >&2
    echo "Install Alibaba Cloud CLI from the official docs, then rerun." >&2
    exit 1
  }
}

need_creds() {
  if [ -z "${ALIBABA_CLOUD_ACCESS_KEY_ID:-}" ] && [ -n "${ALIBABACLOUD_ACCESS_KEY_ID:-}" ]; then
    export ALIBABA_CLOUD_ACCESS_KEY_ID="$ALIBABACLOUD_ACCESS_KEY_ID"
  fi
  if [ -z "${ALIBABA_CLOUD_ACCESS_KEY_SECRET:-}" ] && [ -n "${ALIBABACLOUD_ACCESS_KEY_SECRET:-}" ]; then
    export ALIBABA_CLOUD_ACCESS_KEY_SECRET="$ALIBABACLOUD_ACCESS_KEY_SECRET"
  fi
  if [ -z "${ALIBABACLOUD_ACCESS_KEY_ID:-}" ] && [ -n "${ALIBABA_CLOUD_ACCESS_KEY_ID:-}" ]; then
    export ALIBABACLOUD_ACCESS_KEY_ID="$ALIBABA_CLOUD_ACCESS_KEY_ID"
  fi
  if [ -z "${ALIBABACLOUD_ACCESS_KEY_SECRET:-}" ] && [ -n "${ALIBABA_CLOUD_ACCESS_KEY_SECRET:-}" ]; then
    export ALIBABACLOUD_ACCESS_KEY_SECRET="$ALIBABA_CLOUD_ACCESS_KEY_SECRET"
  fi
  if [ -z "${ALIBABA_CLOUD_ACCESS_KEY_ID:-}" ] || [ -z "${ALIBABA_CLOUD_ACCESS_KEY_SECRET:-}" ]; then
    echo "missing ALIBABA_CLOUD_ACCESS_KEY_ID/SECRET or ALIBABACLOUD_ACCESS_KEY_ID/SECRET" >&2
    exit 1
  fi
}

need_ecs_network() {
  [ -n "$VSWITCH_ID" ] || { echo "missing ALIYUN_VSWITCH_ID" >&2; exit 1; }
  [ -n "$SECURITY_GROUP_ID" ] || { echo "missing ALIYUN_SECURITY_GROUP_ID" >&2; exit 1; }
}

need_base_image() {
  [ -n "$BASE_IMAGE_ID" ] || { echo "missing ALIYUN_BASE_IMAGE_ID" >&2; exit 1; }
}

userdata_b64() {
  base64 -w0 "$USER_DATA"
}

common_instance_args() {
  local name="$1"
  printf '%s\0' \
    --RegionId "$REGION" \
    --InstanceType "$INSTANCE_TYPE" \
    --SecurityGroupId "$SECURITY_GROUP_ID" \
    --VSwitchId "$VSWITCH_ID" \
    --InstanceName "$name" \
    --HostName "$name" \
    --SystemDisk.Size "$SYSTEM_DISK_SIZE" \
    --SystemDisk.Category "$SYSTEM_DISK_CATEGORY" \
    --InternetMaxBandwidthOut "$BANDWIDTH_OUT"
  if [ -n "$ZONE_ID" ]; then
    printf '%s\0' --ZoneId "$ZONE_ID"
  fi
  if [ -n "$KEY_PAIR_NAME" ]; then
    printf '%s\0' --KeyPairName "$KEY_PAIR_NAME"
  fi
}

run_aliyun() {
  aliyun "$@"
}

action="${1:-}"
case "$action" in
  -h|--help|"")
    usage
    ;;
  check)
    need_cmd aliyun
    need_creds
    run_aliyun sts GetCallerIdentity --region "$REGION"
    ;;
  create-builder)
    need_cmd aliyun
    need_creds
    need_ecs_network
    need_base_image
    mapfile -d '' args < <(common_instance_args "butian-kali-builder")
    run_aliyun ecs RunInstances \
      --region "$REGION" \
      "${args[@]}" \
      --ImageId "$BASE_IMAGE_ID" \
      --InstanceChargeType PostPaid \
      --Amount 1 \
      --UserData "$(userdata_b64)"
    ;;
  create-image)
    need_cmd aliyun
    need_creds
    instance_id="${2:-}"
    [ -n "$instance_id" ] || { echo "missing instance id" >&2; exit 2; }
    run_aliyun ecs CreateImage \
      --region "$REGION" \
      --RegionId "$REGION" \
      --InstanceId "$instance_id" \
      --ImageName "$IMAGE_NAME" \
      --Description "Butian Kali + Claude Code tooling image; no secrets baked"
    ;;
  launch-spot)
    need_cmd aliyun
    need_creds
    need_ecs_network
    image_id="${2:-}"
    [ -n "$image_id" ] || { echo "missing custom image id" >&2; exit 2; }
    mapfile -d '' args < <(common_instance_args "butian-spot")
    run_aliyun ecs RunInstances \
      --region "$REGION" \
      "${args[@]}" \
      --ImageId "$image_id" \
      --InstanceChargeType PostPaid \
      --SpotStrategy "$SPOT_STRATEGY" \
      --Amount "$SWARM_AMOUNT"
    ;;
  *)
    echo "unknown action: $action" >&2
    usage >&2
    exit 2
    ;;
esac
