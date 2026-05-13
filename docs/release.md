# 发布和 CI 使用说明

本文档记录 aiscan 当前 GitHub Actions 发布机制。

## Workflow

仓库包含三个 workflow：

| Workflow | 文件 | 触发 | 用途 |
| --- | --- | --- | --- |
| `ci` | `.github/workflows/ci.yml` | `push` 到 `master`、PR 到 `master`、手动触发 | 编译全部 release target，不发布 |
| `nightly` | `.github/workflows/nightly.yml` | `push` 到 `master`、手动触发 | 更新当天 nightly tag，生成 draft prerelease |
| `goreleaser` | `.github/workflows/go-release.yml` | 推送 `v*.*.*` tag、手动触发 | 发布正式版本 |

## CI 编译

每次 push 到 `master` 会触发 `ci`：

```text
goreleaser build --snapshot --clean
```

它会使用 `.goreleaser.yml` 编译：

- Linux amd64
- Linux arm64
- macOS amd64
- macOS arm64
- Windows amd64

`ci` 不会创建 tag、release 或上传资产。

## Nightly 发布

`nightly` 会在 `master` 上创建或更新当天 tag：

```text
v0.0.0-nightly.YYYYMMDD
```

然后运行：

```text
goreleaser release --clean --skip=validate
```

release 配置会把 nightly 标记为 prerelease，并保持 draft 状态。重复运行同一天 nightly 会替换已有 draft 和资产。

## 正式发布

推荐通过 GitHub Actions 手动触发 `goreleaser` workflow：

1. 打开 `Actions -> goreleaser -> Run workflow`。
2. `tag` 填写语义化版本，例如 `v0.0.1`。
3. `target` 填写要发布的分支或 commit，通常是 `master`。
4. 运行 workflow。
5. workflow 成功后会创建 draft release 并上传资产。
6. 检查资产无误后，将 draft 发布为正式 release，并设为 latest。

也可以直接推送 tag：

```bash
git tag -a v0.0.2 -m "Release v0.0.2"
git push origin v0.0.2
```

推送符合 `v*.*.*` 的 tag 会触发正式发布 workflow。

## 发布资产

GoReleaser 会上传：

| 文件 | 说明 |
| --- | --- |
| `aiscan_linux_amd64` | Linux amd64 二进制 |
| `aiscan_linux_arm64` | Linux arm64 二进制 |
| `aiscan_darwin_amd64` | macOS Intel 二进制 |
| `aiscan_darwin_arm64` | macOS Apple Silicon 二进制 |
| `aiscan_windows_amd64.exe` | Windows amd64 二进制 |
| `aiscan_checksums.txt` | SHA256 校验文件 |

当前配置发布的是裸二进制，不打包 tar.gz 或 zip。

## 版本规则

正式版本使用：

```text
vMAJOR.MINOR.PATCH
```

示例：

```text
v0.0.1
v0.1.0
v1.0.0
```

nightly 版本保留给自动构建：

```text
v0.0.0-nightly.YYYYMMDD
```

正式发布 workflow 会拒绝包含 `nightly` 的 tag。

## 本地验证

提交 workflow 或 GoReleaser 配置前建议执行：

```bash
go run github.com/rhysd/actionlint/cmd/actionlint@latest
goreleaser check
goreleaser build --snapshot --clean
```

本地构建会生成 `dist/`，验证后可删除。

## 常见问题

如果 workflow 在 1 到 5 秒内失败且没有进入任何 step，通常是 GitHub Actions 账单、预算或 runner 调度限制，不是代码错误。

如果 `goreleaser` 在发布阶段返回 404，通常是 release target 指向了没有权限的仓库，或 `GITHUB_TOKEN` 没有 `contents: write` 权限。

如果 release 已存在，当前 `.goreleaser.yml` 会替换已有 draft 和资产：

```yaml
release:
  draft: true
  prerelease: auto
  replace_existing_draft: true
  replace_existing_artifacts: true
  mode: replace
```
