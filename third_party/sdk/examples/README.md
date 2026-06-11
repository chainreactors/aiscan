# Chainreactors SDK Examples

这个目录包含了四个基于 Chainreactors SDK 构建的命令行工具，以及若干面向 SDK 包的最小示例。

## 目录结构

```
examples/
├── fingers/    # 指纹识别工具
├── neutron/    # POC 扫描工具
├── gogo/       # 端口扫描和指纹识别工具
├── spray/      # HTTP 批量探测工具
├── cyberhub/   # Cyberhub Provider 数据加载示例
├── association/ # 统一关联查询示例
└── cases/      # 小颗粒度使用案例（cookbook）
    ├── match_detail/  # 获取 fingers matcher 详情（cmd + test）
    └── spray_crawl_finger/  # 单 URL 爬虫 + 深度指纹探测（cmd + test）
```

## 快速开始

### 编译所有工具

```bash
cd examples
go build -o fingers/fingers.exe ./fingers/main.go
go build -o neutron/neutron.exe ./neutron/main.go
go build -o gogo/gogo.exe ./gogo/main.go
go build -o spray/spray.exe ./spray/main.go
go build -o cyberhub/cyberhub.exe ./cyberhub/main.go
go build -o association/association.exe ./association/main.go
```

---

## 1. Fingers - 指纹识别工具

识别 Web 应用的技术栈和框架。

### 基本用法

```bash
# 从 Cyberhub 加载指纹（CLI 当前仅支持该方式）
./fingers/fingers.exe -url http://127.0.0.1:8080 -key your_api_key -target http://127.0.0.1:3000

# 按 source 过滤
./fingers/fingers.exe -url http://127.0.0.1:8080 -key your_key -source github -target http://example.com

# JSON 输出
./fingers/fingers.exe -url http://127.0.0.1:8080 -key your_key -target http://127.0.0.1:3000 -json

# 显示详细信息
./fingers/fingers.exe -url http://127.0.0.1:8080 -key your_key -target http://127.0.0.1:3000 -info
```

### 参数说明

- `-url`: Cyberhub URL **(必需)**
- `-key`: Cyberhub API Key **(必需)** (与 -url 一起使用)
- `-source`: 按来源过滤指纹 (可选)
- `-engines`: 预留参数，当前未生效
- `-target`: 目标 URL **(必需)**
- `-json`: JSON 格式输出
- `-info`: 显示详细信息（版本、CPE 等）

---

## 2. Neutron - POC 扫描工具

使用 POC 对目标进行漏洞扫描。

### 基本用法

```bash
# 从 Cyberhub 加载 POC 并扫描
./neutron/neutron.exe -url http://127.0.0.1:8080 -key your_key -target http://127.0.0.1:3000

# 从本地目录加载 POC
./neutron/neutron.exe -path ./pocs -target http://127.0.0.1:3000

# 列出所有 POC
./neutron/neutron.exe -url http://127.0.0.1:8080 -key your_key -list

# 按严重程度过滤
./neutron/neutron.exe -url ... -key ... -target ... -severity critical,high

# 按标签过滤
./neutron/neutron.exe -url ... -key ... -target ... -tags cve,rce

# 按 source 过滤
./neutron/neutron.exe -url ... -key ... -source github -target ...

# 执行特定 POC
./neutron/neutron.exe -url ... -key ... -target ... -poc CVE-2021-12345

# 限制执行数量
./neutron/neutron.exe -url ... -key ... -target ... -max 10

# JSON 输出
./neutron/neutron.exe -url ... -key ... -target ... -json
```

### 参数说明

- `-url`: Cyberhub URL (可选)
- `-key`: Cyberhub API Key (与 -url 一起使用)
- `-source`: 按来源过滤 POC (可选)
- `-path`: 本地 POC 目录或文件 (可选)
- `-target`: 目标 URL **(扫描时必需)**
- `-poc`: 指定 POC ID (可选)
- `-list`: 列出所有 POC
- `-severity`: 按严重程度过滤 (info/low/medium/high/critical)
- `-tags`: 按标签过滤
- `-timeout`: 预留参数，当前未生效
- `-max`: 最大执行 POC 数量 (0 = 全部)
- `-json`: JSON 格式输出

---

## 3. GoGo - 端口扫描工具

扫描端口并识别服务指纹。

### 基本用法

```bash
# 基本扫描（CLI 需要 Cyberhub）
./gogo/gogo.exe -url http://127.0.0.1:8080 -key your_key -target 127.0.0.1 -ports 80,443,8080

# CIDR 扫描
./gogo/gogo.exe -url http://127.0.0.1:8080 -key your_key -target 192.168.1.0/24 -ports 80,443

# 同时加载 Fingers 和 Neutron POC
./gogo/gogo.exe -url ... -key ... -fingers -neutron -target 127.0.0.1

# 按 source 过滤
./gogo/gogo.exe -url ... -key ... -source github -target 127.0.0.1

# 高级选项
./gogo/gogo.exe -url http://127.0.0.1:8080 -key your_key -target 127.0.0.1 -ports 1-65535 -threads 2000 -version 2

# JSON 输出
./gogo/gogo.exe -url http://127.0.0.1:8080 -key your_key -target 127.0.0.1 -json
```

### 参数说明

- `-url`: Cyberhub URL **(必需)**
- `-key`: Cyberhub API Key **(必需)** (与 -url 一起使用)
- `-source`: 按来源过滤 (可选)
- `-fingers`: 从 Cyberhub 加载指纹 (默认: true)
- `-neutron`: 从 Cyberhub 加载 POC (默认: false)
- `-target`: 目标 IP 或 CIDR **(必需)**
- `-ports`: 端口列表 (默认: 80,443,8080,8443)
- `-threads`: 线程数 (默认: 1000)
- `-version`: 版本识别级别 0-3 (默认: 0)
- `-exploit`: 漏洞检测模式 (none/all/known)
- `-timeout`: 超时时间 (默认: 5秒)
- `-json`: JSON 格式输出
- `-v`: 详细输出

---

## 4. Spray - HTTP 批量探测工具

批量检测 HTTP 服务状态。

### 基本用法

```bash
# 单个 URL
./spray/spray.exe -u http://127.0.0.1:3000

# 从文件读取 URL
./spray/spray.exe -f urls.txt

# 自定义线程和超时
./spray/spray.exe -f urls.txt -threads 100 -timeout 5

# 匹配特定状态码
./spray/spray.exe -f urls.txt -mc 200,301,302

# 过滤特定状态码
./spray/spray.exe -f urls.txt -fc 404,403

# 静默模式（只显示匹配的 URL）
./spray/spray.exe -f urls.txt -q

# JSON 输出
./spray/spray.exe -f urls.txt -json

# 保存结果到文件
./spray/spray.exe -f urls.txt -o results.txt
```

### 参数说明

- `-u`: 单个目标 URL
- `-f`: 包含 URL 的文件 (每行一个)
- `-threads`: 线程数 (默认: 50)
- `-timeout`: 超时时间 (默认: 10秒)
- `-retries`: 预留参数，当前未生效
- `-method`: HTTP 方法 (默认: GET)
- `-headers`: 自定义请求头 (格式: 'Key1:Value1,Key2:Value2')
- `-ua`: 预留参数，当前未生效
- `-proxy`: 预留参数，当前未生效
- `-follow`: 预留参数，当前未生效
- `-fc`: 过滤状态码 (逗号分隔)
- `-mc`: 匹配状态码 (逗号分隔)
- `-fs`: 预留参数，当前未生效
- `-ms`: 预留参数，当前未生效
- `-json`: JSON 格式输出
- `-v`: 详细输出
- `-q`: 静默模式
- `-o`: 输出文件

---

## 5. Cyberhub - Provider 数据加载示例

直接使用 `pkg/cyberhub.Provider` 拉取指纹、alias 和 POC；筛选条件从 `types.NewExportFilter()` 创建。

```bash
# 拉取远程数据并打印样例
go run ./cyberhub -url http://127.0.0.1:8080 -key your_api_key -source github -limit 20

# 同时按 name/tag/severity 过滤
go run ./cyberhub -url http://127.0.0.1:8080 -key your_api_key -names tomcat -tags cve -severity high,critical
```

---

## 6. Association - 统一关联查询示例

通过 `pkg/association.Index` 在 finger、alias、template、CVE、tag、service、severity 等条件之间做统一关联。默认使用 inline 数据，不依赖 Cyberhub。

```bash
# 离线演示 alias 作为关联桥梁
go run ./association
go run ./association -finger "apache tomcat"
go run ./association -template CVE-2022-0001

# 从 Cyberhub 构建索引并查询
go run ./association -url http://127.0.0.1:8080 -key your_api_key -finger tomcat
go run ./association -url http://127.0.0.1:8080 -key your_api_key -template CVE-2022-0001
go run ./association -url http://127.0.0.1:8080 -key your_api_key -cve CVE-2021-44228
```

## Cases - 小颗粒度使用案例

`examples/cases/` 下放的是 cookbook 风格的最小可运行片段，每个 case 只演示一个 API 或一个用法要点，复制即可融入到自己的工程里。

### match_detail - 获取 matcher 详情

演示如何通过 `fingers.NewConfig().WithMatchDetail()` 打开底层 matcher detail，然后从 `types.Framework.MatchDetail` 读取 matcher 类型/值、rule_index、matcher_index 和 send_data。

```bash
# 跑命令行版（被动匹配真实 target）
go run ./cases/match_detail -url http://127.0.0.1:8080 -key your_api_key -target http://127.0.0.1:3000

# 跑测试版（inline finger + httptest，离线可跑）
go test ./cases/match_detail -v
```

要点：在 config 上调用 `WithMatchDetail()`，随后仍然使用原有的 `Match`、`MatchHTTP` 或 `HTTPMatch`，从返回的 `Framework.MatchDetail` 读取命中的规则和 matcher 信息。

### spray_crawl_finger - 单 URL 爬虫 + 深度指纹探测

演示如何输入一个 URL，交给 `spray` 自动爬取，并通过 spray 内部指纹引擎拿到深度匹配结果和 `MatchDetail`。

```bash
# 跑命令行版
go run ./cases/spray_crawl_finger -target http://127.0.0.1:3000

# 跑测试版（inline finger + httptest，离线可跑）
go test ./cases/spray_crawl_finger -v
```

要点：`spray.NewConfig().WithMatchDetail()` 负责把 matcher 细节带进 `types.Framework`，随后在 `spray.Context` 上打开 `SetCrawlPlugin(true)` 和 `SetFinger(true)` 即可。

---

## 开发说明

完整 SDK 文档: [Chainreactors SDK](https://github.com/chainreactors/sdk)

---

## License

MIT
