#!/usr/bin/env python3
"""Alibaba Cloud ECS helper for Butian/Kali/Claude Code spot workers.

Credentials are read from environment variables and never written to disk:
  ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET
  ALIBABACLOUD_ACCESS_KEY_ID / ALIBABACLOUD_ACCESS_KEY_SECRET
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import hmac
import json
import os
import sys
import time
import uuid
from pathlib import Path
from typing import Any
from urllib import parse, request, error


ROOT = Path(__file__).resolve().parents[1]
BUILDER_USER_DATA = ROOT / "cloud" / "aliyun" / "butian-kali-cloud-init.sh"
ECS_VERSION = "2014-05-26"
STS_VERSION = "2015-04-01"


def getenv(*names: str, default: str = "") -> str:
    for name in names:
        value = os.environ.get(name)
        if value:
            return value
    return default


def required_env(*names: str) -> str:
    value = getenv(*names)
    if not value:
        joined = " or ".join(names)
        raise SystemExit(f"missing environment variable: {joined}")
    return value


def percent_encode(value: Any) -> str:
    return parse.quote(str(value), safe="~")


def sign(method: str, params: dict[str, Any], secret: str) -> str:
    canonical = "&".join(
        f"{percent_encode(k)}={percent_encode(params[k])}"
        for k in sorted(params)
        if params[k] is not None
    )
    string_to_sign = f"{method}&%2F&{percent_encode(canonical)}"
    digest = hmac.new((secret + "&").encode(), string_to_sign.encode(), hashlib.sha1).digest()
    return base64.b64encode(digest).decode()


def common_params(action: str, version: str, region: str, access_key: str) -> dict[str, Any]:
    return {
        "Action": action,
        "Version": version,
        "Format": "JSON",
        "SignatureMethod": "HMAC-SHA1",
        "SignatureNonce": str(uuid.uuid4()),
        "SignatureVersion": "1.0",
        "Timestamp": dt.datetime.now(dt.UTC).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "AccessKeyId": access_key,
        "RegionId": region,
    }


class AliyunClient:
    def __init__(self, region: str) -> None:
        self.region = region
        self.access_key = required_env("ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID")
        self.secret = required_env("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET")

    def call(self, service: str, action: str, version: str, params: dict[str, Any]) -> dict[str, Any]:
        endpoint = {
            "ecs": f"https://ecs.{self.region}.aliyuncs.com/",
            "sts": "https://sts.aliyuncs.com/",
        }[service]
        all_params = common_params(action, version, self.region, self.access_key)
        all_params.update({k: v for k, v in params.items() if v is not None and v != ""})
        all_params["Signature"] = sign("POST", all_params, self.secret)
        body = parse.urlencode(all_params).encode()
        req = request.Request(endpoint, data=body, method="POST")
        req.add_header("Content-Type", "application/x-www-form-urlencoded")
        try:
            with request.urlopen(req, timeout=60) as resp:
                return json.loads(resp.read().decode())
        except error.HTTPError as exc:
            detail = exc.read().decode(errors="replace")
            raise SystemExit(f"Aliyun API error {exc.code}: {detail}") from exc


def b64_file(path: Path) -> str:
    return base64.b64encode(path.read_bytes()).decode()


def env_config() -> dict[str, str]:
    return {
        "region": getenv("ALIYUN_REGION", "ALIBABA_CLOUD_REGION", default="cn-hangzhou"),
        "zone_id": getenv("ALIYUN_ZONE_ID"),
        "vswitch_id": getenv("ALIYUN_VSWITCH_ID"),
        "security_group_id": getenv("ALIYUN_SECURITY_GROUP_ID"),
        "base_image_id": getenv("ALIYUN_BASE_IMAGE_ID"),
        "instance_type": getenv("ALIYUN_INSTANCE_TYPE", default="ecs.c8i.large"),
        "key_pair_name": getenv("ALIYUN_KEY_PAIR_NAME"),
        "system_disk_size": getenv("ALIYUN_SYSTEM_DISK_SIZE", default="80"),
        "system_disk_category": getenv("ALIYUN_SYSTEM_DISK_CATEGORY", default="cloud_essd"),
        "bandwidth_out": getenv("ALIYUN_BANDWIDTH_OUT", default="10"),
        "image_name": getenv("ALIYUN_SRC_IMAGE_NAME", default=f"src-kali-aide-{time.strftime('%Y%m%d%H%M%S')}"),
        "spot_strategy": getenv("ALIYUN_SPOT_STRATEGY", default="SpotAsPriceGo"),
        "swarm_amount": getenv("ALIYUN_SWARM_AMOUNT", default="3"),
    }


def require_network(cfg: dict[str, str]) -> None:
    for key, env_name in [
        ("vswitch_id", "ALIYUN_VSWITCH_ID"),
        ("security_group_id", "ALIYUN_SECURITY_GROUP_ID"),
    ]:
        if not cfg[key]:
            raise SystemExit(f"missing {env_name}")


def instance_params(cfg: dict[str, str], name: str, amount: int = 1) -> dict[str, Any]:
    params: dict[str, Any] = {
        "InstanceType": cfg["instance_type"],
        "SecurityGroupId": cfg["security_group_id"],
        "VSwitchId": cfg["vswitch_id"],
        "InstanceName": name,
        "SystemDisk.Size": cfg["system_disk_size"],
        "SystemDisk.Category": cfg["system_disk_category"],
        "InternetMaxBandwidthOut": cfg["bandwidth_out"],
        "Amount": amount,
    }
    if amount == 1:
        params["HostName"] = name
    else:
        params["UniqueSuffix"] = "true"
    if cfg["zone_id"]:
        params["ZoneId"] = cfg["zone_id"]
    if cfg["key_pair_name"]:
        params["KeyPairName"] = cfg["key_pair_name"]
    return params


def runtime_user_data(node_name: str) -> str | None:
    targets = getenv("SRC_TARGETS")
    program = getenv("SRC_PROGRAM")
    task = getenv("SRC_TASK")
    claude_key = getenv("CLAUDECODE_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY")
    base_url = getenv("CLAUDECODE_ANTHROPIC_BASE_URL", "ANTHROPIC_BASE_URL", default="https://kiro.chainreactors.cn")
    model = getenv("CLAUDECODE_MODEL", default="claude-opus-4.8")
    budget = getenv("CLAUDECODE_MAX_BUDGET_USD", default="10")
    optional_runtime_env = {
        "SRC_PROGRAM": program,
        "FOFA_EMAIL": getenv("FOFA_EMAIL"),
        "FOFA_KEY": getenv("FOFA_KEY", "FOFA_API_KEY"),
        "FOFA_API_KEY": getenv("FOFA_API_KEY", "FOFA_KEY"),
        "HUNTER_API_KEY": getenv("HUNTER_API_KEY", "HUNTER_KEY"),
        "KALI_CONTAINER": getenv("KALI_CONTAINER"),
        "OSS_ACCESS_KEY_ID": getenv(
            "OSS_ACCESS_KEY_ID",
            "ALIBABA_CLOUD_ACCESS_KEY_ID",
            "ALIBABACLOUD_ACCESS_KEY_ID",
        ),
        "OSS_ACCESS_KEY_SECRET": getenv(
            "OSS_ACCESS_KEY_SECRET",
            "ALIBABA_CLOUD_ACCESS_KEY_SECRET",
            "ALIBABACLOUD_ACCESS_KEY_SECRET",
        ),
        "OSS_BUCKET": getenv("OSS_BUCKET"),
        "OSS_ENDPOINT": getenv("OSS_ENDPOINT"),
        "OSS_PREFIX": getenv("OSS_PREFIX"),
        "SYNC_INTERVAL": getenv("SYNC_INTERVAL"),
        "SRC_DISABLE_SYNC": getenv("SRC_DISABLE_SYNC"),
    }
    if not targets and not program:
        return None

    def sq(value: str) -> str:
        return "'" + value.replace("'", "'\"'\"'") + "'"

    lines = [
        "#!/usr/bin/env bash",
        "set -euxo pipefail",
        "mkdir -p /opt/butian/runtime /opt/butian/logs",
        "cat > /opt/butian/runtime/worker.env <<'ENV_EOF'",
        f"export SRC_NODE_NAME={sq(node_name)}",
        f"export SRC_TARGETS={sq(targets)}",
        f"export SRC_TASK={sq(task)}",
        f"export CLAUDECODE_ANTHROPIC_BASE_URL={sq(base_url)}",
        f"export CLAUDECODE_MODEL={sq(model)}",
        f"export CLAUDECODE_MAX_BUDGET_USD={sq(budget)}",
    ]
    for env_name, env_value in optional_runtime_env.items():
        if env_value:
            lines.append(f"export {env_name}={sq(env_value)}")
    if claude_key:
        lines.append(f"export CLAUDECODE_ANTHROPIC_API_KEY={sq(claude_key)}")
    lines += [
        "ENV_EOF",
        "chmod 600 /opt/butian/runtime/worker.env",
        "cat > /opt/butian/runtime/start-worker.sh <<'RUN_EOF'",
        "#!/usr/bin/env bash",
        "set -euo pipefail",
        "source /opt/butian/runtime/worker.env",
        "exec /opt/butian/bin/start-butian-worker",
        "RUN_EOF",
        "chmod 700 /opt/butian/runtime/start-worker.sh",
        f"nohup /opt/butian/runtime/start-worker.sh > /opt/butian/logs/{node_name}.log 2>&1 &",
    ]
    return base64.b64encode(("\n".join(lines) + "\n").encode()).decode()


def print_json(data: Any) -> None:
    print(json.dumps(data, ensure_ascii=False, indent=2))


def cmd_check(client: AliyunClient, _cfg: dict[str, str], _args: argparse.Namespace) -> None:
    print_json(client.call("sts", "GetCallerIdentity", STS_VERSION, {}))


def cmd_create_builder(client: AliyunClient, cfg: dict[str, str], _args: argparse.Namespace) -> None:
    require_network(cfg)
    if not cfg["base_image_id"]:
        raise SystemExit("missing ALIYUN_BASE_IMAGE_ID")
    params = instance_params(cfg, "src-kali-builder", 1)
    params.update(
        {
            "ImageId": cfg["base_image_id"],
            "InstanceChargeType": "PostPaid",
            "UserData": b64_file(BUILDER_USER_DATA),
        }
    )
    print_json(client.call("ecs", "RunInstances", ECS_VERSION, params))


def cmd_create_image(client: AliyunClient, cfg: dict[str, str], args: argparse.Namespace) -> None:
    params = {
        "InstanceId": args.instance_id,
        "ImageName": cfg["image_name"],
        "Description": "SRC Kali + Claude Code tooling image; no secrets baked",
    }
    print_json(client.call("ecs", "CreateImage", ECS_VERSION, params))


def cmd_launch_spot(client: AliyunClient, cfg: dict[str, str], args: argparse.Namespace) -> None:
    require_network(cfg)
    count = args.count or int(cfg["swarm_amount"])
    responses = []
    for idx in range(1, count + 1):
        node_prefix = getenv("SRC_NODE_PREFIX", default="src-spot")
        node_name = f"{node_prefix}-{idx}"
        params = instance_params(cfg, node_name, 1)
        params.update(
            {
                "ImageId": args.image_id,
                "InstanceChargeType": "PostPaid",
                "SpotStrategy": cfg["spot_strategy"],
            }
        )
        userdata = runtime_user_data(node_name)
        if userdata:
            params["UserData"] = userdata
        responses.append(client.call("ecs", "RunInstances", ECS_VERSION, params))
    print_json({"responses": responses})


def cmd_describe_instances(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    params: dict[str, Any] = {"PageSize": args.page_size}
    if args.instance_ids:
        params["InstanceIds"] = json.dumps(args.instance_ids)
    if args.name:
        params["InstanceName"] = args.name
    print_json(client.call("ecs", "DescribeInstances", ECS_VERSION, params))


def cmd_describe_images(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    params: dict[str, Any] = {
        "ImageOwnerAlias": args.owner,
        "OSType": "linux",
        "Architecture": args.arch,
        "PageSize": args.page_size,
    }
    if args.name:
        params["ImageName"] = args.name
    if args.image_id:
        params["ImageId"] = args.image_id
    print_json(client.call("ecs", "DescribeImages", ECS_VERSION, params))


def cmd_describe_vswitches(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    params: dict[str, Any] = {"PageSize": args.page_size}
    if args.vpc_id:
        params["VpcId"] = args.vpc_id
    print_json(client.call("ecs", "DescribeVSwitches", ECS_VERSION, params))


def cmd_describe_security_groups(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    params: dict[str, Any] = {"PageSize": args.page_size}
    if args.vpc_id:
        params["VpcId"] = args.vpc_id
    print_json(client.call("ecs", "DescribeSecurityGroups", ECS_VERSION, params))


def cmd_run_command(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    if args.shell_command == "-":
        content = sys.stdin.read()
    else:
        content = args.shell_command
    params: dict[str, Any] = {
        "Type": "RunShellScript",
        "CommandContent": base64.b64encode(content.encode()).decode(),
        "ContentEncoding": "Base64",
        "Timeout": args.timeout,
    }
    for idx, instance_id in enumerate(args.instance_ids, start=1):
        params[f"InstanceId.{idx}"] = instance_id
    print_json(client.call("ecs", "RunCommand", ECS_VERSION, params))


def send_file_content(
    client: AliyunClient,
    instance_id: str,
    name: str,
    target_dir: str,
    content: bytes,
    timeout: int,
    mode: str,
) -> dict[str, Any]:
    encoded = base64.b64encode(content).decode()
    if len(encoded.encode()) > 32768:
        raise SystemExit(f"file chunk too large after base64 encoding: {len(encoded.encode())} bytes")
    params: dict[str, Any] = {
        "Name": name,
        "TargetDir": target_dir,
        "ContentType": "Base64",
        "Content": encoded,
        "Overwrite": "true",
        "FileMode": mode,
        "Timeout": timeout,
        "InstanceId.1": instance_id,
    }
    return client.call("ecs", "SendFile", ECS_VERSION, params)


def cmd_send_file(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    path = Path(args.local_path)
    name = args.name or path.name
    print_json(
        send_file_content(
            client,
            args.instance_id,
            name,
            args.target_dir,
            path.read_bytes(),
            args.timeout,
            args.mode,
        )
    )


def cmd_upload_file(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    path = Path(args.local_path)
    data = path.read_bytes()
    chunk_size = args.chunk_size
    total = (len(data) + chunk_size - 1) // chunk_size
    prefix = args.name or path.name
    responses = []
    for idx in range(total):
        chunk = data[idx * chunk_size : (idx + 1) * chunk_size]
        name = f"{prefix}.part{idx:04d}"
        responses.append(
            send_file_content(
                client,
                args.instance_id,
                name,
                args.target_dir,
                chunk,
                args.timeout,
                args.mode,
            )
        )
        if args.pause:
            time.sleep(args.pause)
    print_json(
        {
            "local_path": str(path),
            "target_dir": args.target_dir,
            "prefix": prefix,
            "bytes": len(data),
            "chunk_size": chunk_size,
            "chunks": total,
            "responses": responses,
        }
    )


def cmd_describe_send_file_results(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    params: dict[str, Any] = {"InvokeId": args.invoke_id}
    if args.instance_id:
        params["InstanceId"] = args.instance_id
    print_json(client.call("ecs", "DescribeSendFileResults", ECS_VERSION, params))


def cmd_invocation_results(client: AliyunClient, _cfg: dict[str, str], args: argparse.Namespace) -> None:
    params: dict[str, Any] = {"InvokeId": args.invoke_id}
    if args.instance_id:
        params["InstanceId"] = args.instance_id
    data = client.call("ecs", "DescribeInvocationResults", ECS_VERSION, params)
    if args.decode:
        for item in data.get("Invocation", {}).get("InvocationResults", {}).get("InvocationResult", []):
            output = item.get("Output")
            if output:
                try:
                    item["OutputDecoded"] = base64.b64decode(output).decode(errors="replace")
                except Exception:
                    item["OutputDecoded"] = "<decode failed>"
    print_json(data)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)

    sub.add_parser("check")
    sub.add_parser("create-builder")

    p = sub.add_parser("create-image")
    p.add_argument("instance_id")

    p = sub.add_parser("launch-spot")
    p.add_argument("image_id")
    p.add_argument("--count", type=int)

    p = sub.add_parser("describe-instances")
    p.add_argument("--instance-ids", nargs="*")
    p.add_argument("--name")
    p.add_argument("--page-size", type=int, default=20)

    p = sub.add_parser("describe-images")
    p.add_argument("--owner", default="system")
    p.add_argument("--arch", default="x86_64")
    p.add_argument("--name")
    p.add_argument("--image-id")
    p.add_argument("--page-size", type=int, default=20)

    p = sub.add_parser("describe-vswitches")
    p.add_argument("--vpc-id")
    p.add_argument("--page-size", type=int, default=50)

    p = sub.add_parser("describe-security-groups")
    p.add_argument("--vpc-id")
    p.add_argument("--page-size", type=int, default=50)

    p = sub.add_parser("run-command")
    p.add_argument("instance_ids", nargs="+")
    p.add_argument("--timeout", type=int, default=60)
    p.add_argument("shell_command", help="Shell command, or '-' to read from stdin")

    p = sub.add_parser("send-file")
    p.add_argument("instance_id")
    p.add_argument("local_path")
    p.add_argument("--target-dir", default="/tmp")
    p.add_argument("--name")
    p.add_argument("--timeout", type=int, default=60)
    p.add_argument("--mode", default="0644")

    p = sub.add_parser("upload-file")
    p.add_argument("instance_id")
    p.add_argument("local_path")
    p.add_argument("--target-dir", default="/tmp")
    p.add_argument("--name")
    p.add_argument("--timeout", type=int, default=60)
    p.add_argument("--mode", default="0644")
    p.add_argument("--chunk-size", type=int, default=24000)
    p.add_argument("--pause", type=float, default=0.05)

    p = sub.add_parser("send-file-results")
    p.add_argument("invoke_id")
    p.add_argument("--instance-id")

    p = sub.add_parser("invocation-results")
    p.add_argument("invoke_id")
    p.add_argument("--instance-id")
    p.add_argument("--decode", action="store_true")

    return parser


COMMANDS = {
    "check": cmd_check,
    "create-builder": cmd_create_builder,
    "create-image": cmd_create_image,
    "launch-spot": cmd_launch_spot,
    "describe-instances": cmd_describe_instances,
    "describe-images": cmd_describe_images,
    "describe-vswitches": cmd_describe_vswitches,
    "describe-security-groups": cmd_describe_security_groups,
    "run-command": cmd_run_command,
    "send-file": cmd_send_file,
    "upload-file": cmd_upload_file,
    "send-file-results": cmd_describe_send_file_results,
    "invocation-results": cmd_invocation_results,
}


def main() -> int:
    args = build_parser().parse_args()
    cfg = env_config()
    client = AliyunClient(cfg["region"])
    COMMANDS[args.action](client, cfg, args)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
