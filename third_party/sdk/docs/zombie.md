# Zombie - 弱口令检测

Zombie 引擎用于弱口令爆破和未授权访问检测，支持 SSH、MySQL、Redis、SMB、RDP 等 26+ 种服务协议。

## 创建引擎

```go
engine := zombie.NewEngine(nil)

// 带容量限制
engine := zombie.NewEngine(zombie.NewConfig().WithCapacity(500))
```

> 源码：[`zombie/engine.go`](../zombie/engine.go)

## 三种攻击模式

### Brute（ClusterBomb）

笛卡尔积：每个用户名 × 每个密码。

```go
targets := []zombie.Target{
    {IP: "192.168.1.1", Port: "22", Service: "ssh"},
    {IP: "192.168.1.2", Port: "3306", Service: "mysql"},
}

ctx := zombie.NewContext().SetThreads(50).SetTimeout(5)

// 同步
results, err := engine.Brute(ctx, targets,
    []string{"root", "admin"},       // 用户名
    []string{"123456", "password"},  // 密码
)

// 流式
ch, err := engine.BruteStream(ctx, targets, users, passwords)
for r := range ch {
    fmt.Printf("[+] %s:%s %s/%s\n", r.IP, r.Port, r.Username, r.Password)
}
```

用户名和密码都可以传 nil，引擎会自动使用服务对应的内置默认字典。

### Pitchfork

一对一配对：第 N 个 auth 对应第 N 个目标。

```go
auths := []zombie.Auth{
    {Username: "root", Password: "toor"},
    {Username: "admin", Password: "admin123"},
}

results, err := engine.Pitchfork(ctx, targets, auths)

// 流式
ch, err := engine.PitchforkStream(ctx, targets, auths)
```

### Sniper

单发模式：每个目标使用自身携带的用户名/密码。

```go
targets := []zombie.Target{
    {IP: "192.168.1.1", Port: "22", Service: "ssh", Username: "root", Password: "toor"},
    {IP: "192.168.1.2", Port: "3306", Service: "mysql", Username: "root", Password: "mysql123"},
}

results, err := engine.Sniper(ctx, targets)

// 流式
ch, err := engine.SniperStream(ctx, targets)
```

## Context 配置

```go
ctx := zombie.NewContext().
    SetThreads(100).        // 并发线程数（默认 100）
    SetTimeout(5).          // 单次超时（秒，默认 5）
    SetTop(10).             // 使用 top N 默认字典（0=全部）
    SetFirstOnly(true).     // 每目标首次成功即停止（默认 true）
    SetNoUnauth(false)      // 是否跳过未授权检测（默认 false）
```

### 通过 ZombieOption 整体配置

```go
opt := &types.ZombieOption{
    Threads:   200,
    Timeout:   10,
    Mod:       types.ZombieModeBomb,
    FirstOnly: false,
    NoUnAuth:  true,
}

ctx := zombie.NewContext().SetOption(opt)
```

> 源码：[`zombie/types.go`](../zombie/types.go)

## SSH 私钥认证

密码字段支持 `pk:` 前缀传递私钥：

```go
// 文件路径（CLI 场景）
zombie.Target{
    IP: "192.168.1.1", Port: "22", Service: "ssh",
    Username: "root", Password: "pk:/path/to/id_rsa",
}

// base64 编码的 PEM 内容（SDK 场景）
keyB64 := base64.StdEncoding.EncodeToString(pemKeyBytes)
zombie.Target{
    IP: "192.168.1.1", Port: "22", Service: "ssh",
    Username: "root", Password: "pk:" + keyB64,
}
```

## 支持的服务

SSH, FTP, SMB, RDP, Telnet, MySQL, PostgreSQL, MSSQL, Oracle, MongoDB, Redis, VNC, LDAP, SNMP, SOCKS5, HTTP/HTTPS, POP3, RSYNC, Zookeeper, AMQP, MQTT, Memcached 等 26+ 种。

## 扫描结果

`*types.ZombieResult` 包含：

```go
r.IP          // IP 地址
r.Port        // 端口
r.Service     // 服务名
r.Username    // 成功的用户名
r.Password    // 成功的密码
r.Mod         // 攻击模式
```

## 通过统一接口使用

```go
task := zombie.NewBruteTask(targets)
task.Users = []string{"root"}
task.Passwords = []string{"123456"}

resultCh, err := engine.Execute(ctx, task)
for result := range resultCh {
    if data, ok := types.ResultData[*types.ZombieResult](result); ok {
        fmt.Println(data.IP, data.Username, data.Password)
    }
}
```

## 统计回调

```go
ctx := zombie.NewContext().
    SetStatsHandler(func(s types.Stats) {
        fmt.Printf("targets=%d tasks=%d results=%d duration=%v\n",
            s.Targets, s.Tasks, s.Results, s.Duration)
    })
```
