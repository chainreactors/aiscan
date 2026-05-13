# Cloudproxy container remount runbook

This note records the manual recovery flow for the managed cloudproxy worker:

```text
cloudproxy-fce92efd-098c-4489-b333-13faa14160d9
```

## Symptoms

- `/workspace/asm` disappears after the host is restarted or recreated.
- `ssh admin@127.0.0.1 -p 2222` hangs or times out.
- `docker inspect` shows only the default workspace mount:

```text
/var/lib/cloud-cli-proxy/hosts/fce92efd-098c-4489-b333-13faa14160d9/home -> /workspace
```

## Root cause

Docker bind mounts are fixed at container creation time. A running container cannot receive a new bind mount, so adding local directories requires removing and recreating the same-named container.

The `2222` SSH port is owned by `cloud-cli-proxy-control-plane-1`, not by the worker container. The command below goes through the control-plane SSH proxy:

```powershell
ssh admin@127.0.0.1 -p 2222
```

For that proxy to work, the worker container must be attached to the cloudproxy private network with the expected worker IP:

```text
network: cloudproxy-net-fce92efd-098c-4489-b333-13faa14160d9
worker:  10.99.72.3
```

If the worker is recreated only on Docker's default `bridge` network, the control-plane proxy may try to reach the wrong IP and SSH will fail.

## Quick repair

Run this from PowerShell. It recreates the worker with the required mounts and reconnects it to the cloudproxy network.

```powershell
$ErrorActionPreference = 'Stop'
$Name = 'cloudproxy-fce92efd-098c-4489-b333-13faa14160d9'
$HostId = 'fce92efd-098c-4489-b333-13faa14160d9'
$CloudNet = "cloudproxy-net-$HostId"
$CloudIp = '10.99.72.3'
$Asm = 'D:\Programing\asm'
$Chainreactors = 'D:\Programing\go\chainreactors'

New-Item -ItemType Directory -Force -Path $Asm | Out-Null

$c = docker inspect $Name | ConvertFrom-Json
$image = $c.Config.Image
$workspaceMount = ($c.Mounts | Where-Object { $_.Destination -eq '/workspace' } | Select-Object -First 1).Source
if (-not $workspaceMount) { $workspaceMount = "/var/lib/cloud-cli-proxy/hosts/$HostId/home" }

$args = @('run','-d','--name',$Name)
if ($c.Config.Hostname) { $args += @('--hostname', $c.Config.Hostname) }
if ($c.Config.WorkingDir) { $args += @('--workdir', $c.Config.WorkingDir) }
foreach ($cap in @($c.HostConfig.CapAdd)) { if ($cap) { $args += @('--cap-add', $cap) } }
foreach ($dev in @($c.HostConfig.Devices)) {
  if ($dev.PathOnHost) { $args += @('--device', "$($dev.PathOnHost):$($dev.PathInContainer):$($dev.CgroupPermissions)") }
}
$args += @('--mount', "type=bind,source=$workspaceMount,target=/workspace")
$args += @('--mount', "type=bind,source=$Asm,target=/mnt/asm")
$args += @('--mount', "type=bind,source=$Chainreactors,target=/mnt/chainreactors")
foreach ($envVar in @($c.Config.Env)) { if ($envVar) { $args += @('-e', $envVar) } }
foreach ($label in $c.Config.Labels.PSObject.Properties) { $args += @('--label', "$($label.Name)=$($label.Value)") }
$args += $image

docker stop $Name | Out-Null
docker rm $Name | Out-Null
& docker @args
docker network connect --ip $CloudIp $CloudNet $Name

docker exec $Name sh -lc '
set -e
for item in asm chainreactors; do
  target="/workspace/$item"
  if [ -e "$target" ] && [ ! -L "$target" ]; then
    rmdir "$target" 2>/dev/null || mv "$target" "$target.before-symlink.$(date +%Y%m%d%H%M%S)"
  fi
done
ln -sfn /mnt/asm /workspace/asm
ln -sfn /mnt/chainreactors /workspace/chainreactors
chown -h workspace:workspace /workspace/asm /workspace/chainreactors
'
```

The directories are mounted under `/mnt` and exposed through `/workspace` symlinks because the image entrypoint runs `chown -R workspace:workspace /workspace` before starting SSH. Mounting large host directories directly under `/workspace` can delay startup and make `ssh admin@127.0.0.1 -p 2222` appear broken.

## Verify

Check mounts:

```powershell
docker inspect cloudproxy-fce92efd-098c-4489-b333-13faa14160d9 --format '{{json .Mounts}}'
docker exec cloudproxy-fce92efd-098c-4489-b333-13faa14160d9 sh -lc 'ls -ld /workspace/asm /workspace/chainreactors /mnt/asm /mnt/chainreactors'
```

Check cloudproxy network:

```powershell
docker inspect cloudproxy-fce92efd-098c-4489-b333-13faa14160d9 --format '{{json .NetworkSettings.Networks}}'
docker exec cloud-cli-proxy-control-plane-1 bash -lc 'timeout 5 bash -c "cat < /dev/tcp/10.99.72.3/22" 2>/dev/null | head -n 1 || true'
```

Expected SSH banner:

```text
SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.15
```

Then the control-plane proxy should work again:

```powershell
ssh admin@127.0.0.1 -p 2222
```

## Notes

- Do not map the worker directly to host port `2222`; that port belongs to `cloud-cli-proxy-control-plane-1`.
- If direct worker SSH is needed, use another host port such as `2223`, and log in as the container user `workspace`.
- Recreating the container always changes the container ID. The stable identity for the scheduler is the container name, labels, and cloudproxy host ID.
