# Chainreactors SDK 文档

本文档面向 SDK 使用者，系统性地介绍 SDK 的核心抽象、各引擎用法及实战示例。

## 目录

| 文档 | 内容 |
|------|------|
| [快速开始](quickstart.md) | 安装、最小示例、5 分钟上手 |
| [核心概念](concepts.md) | Engine / Context / Task / Result 四层抽象 |
| [Fingers - 指纹识别](fingers.md) | 被动匹配、主动探测、Favicon 识别 |
| [Neutron - POC 扫描](neutron.md) | 模板加载、漏洞检测、结果事件 |
| [GoGo - 端口扫描](gogo.md) | 端口扫描、指纹+POC 一体化 |
| [Spray - HTTP 检测](spray.md) | URL 存活检测、路径爆破、插件体系 |
| [Zombie - 弱口令检测](zombie.md) | Brute/Pitchfork/Sniper 三种模式 |
| [Cyberhub 数据源](cyberhub.md) | 远程加载指纹/POC、过滤条件 |
| [Association 关联查询](association.md) | 指纹/别名/模板/CVE 跨域关联 |

## 项目结构速览

```
sdk/
  client/         # 统一客户端入口
  fingers/        # 指纹识别引擎
  neutron/        # POC/漏洞扫描引擎
  gogo/           # 端口扫描引擎（集成 fingers + neutron）
  spray/          # HTTP 批量检测/爆破引擎
  zombie/         # 弱口令检测引擎
  pkg/
    types/        # 核心接口与类型定义
    cyberhub/     # Cyberhub 远程数据源
    association/  # 关联索引与查询
  examples/       # 可运行的示例程序
```

## 阅读建议

- **想快速跑起来** → 从 [快速开始](quickstart.md) 开始
- **想理解设计思路** → 先读 [核心概念](concepts.md)，再按需查阅各引擎文档
- **想看实际代码** → 每篇文档都引用了 `examples/` 下的完整示例，可以直接运行
