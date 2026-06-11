package main

import (
	"fmt"

	"github.com/chainreactors/sdk/client"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/spray"
	"github.com/chainreactors/sdk/zombie"
)

// 本示例演示 SDK 的三层代理配置机制，优先级：Context > Config > Client。
// 支持 proxyclient 的全部协议：socks5 / http / https / trojan / vless /
// hysteria2 / shadowsocks / anytls / clash 订阅，以及多级代理链。
func main() {
	// ============================================================
	// 1) Client 级别：全局默认代理，下沉到 gogo / spray / zombie 所有引擎
	// ============================================================
	c := client.New(
		client.WithProxy("socks5://127.0.0.1:1080"),
	)
	defer c.Close()

	// 不做任何额外配置时，三个引擎都会使用上面的全局代理。
	gogoEngine, err := c.Gogo()
	if err != nil {
		panic(err)
	}
	// 端口扫描将通过 socks5://127.0.0.1:1080
	_, _ = gogoEngine.Scan(gogo.NewContext(), "127.0.0.1", "80,443")

	// ============================================================
	// 2) Engine Config 级别：覆盖某个引擎的默认代理
	// ============================================================
	c2 := client.New(
		client.WithProxy("socks5://127.0.0.1:1080"), // 全局默认
		client.WithGogoConfig(
			gogo.NewConfig().WithProxy("http://10.0.0.1:8080"), // gogo 单独覆盖
		),
		client.WithZombieConfig(
			zombie.NewConfig().WithProxy("trojan://pass@example.com:443"),
		),
	)
	defer c2.Close()

	// ============================================================
	// 3) Context 级别：单次任务细粒度覆盖（优先级最高）
	// ============================================================
	sprayEngine, err := c2.Spray()
	if err != nil {
		panic(err)
	}

	// 这次 check 使用专门的代理，不影响该引擎其它执行
	ctx := spray.NewContext().SetProxy("socks5://special-proxy:1080")
	results, err := sprayEngine.Check(ctx, []string{"https://target.example.com"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("spray check via dedicated proxy: %d results\n", len(results))

	// 同一引擎的另一次执行，使用 Config / Client 级代理（未设置 Context 代理）
	results2, _ := sprayEngine.Check(spray.NewContext(), []string{"https://other.example.com"})
	fmt.Printf("spray check via inherited proxy: %d results\n", len(results2))

	// ============================================================
	// 4) 多级代理链：按顺序串联（client -> A -> B -> target）
	// ============================================================
	c3 := client.New(
		client.WithProxy("http://hop-a:8080", "socks5://hop-b:1080"),
	)
	defer c3.Close()

	zombieEngine, err := c3.Zombie()
	if err != nil {
		panic(err)
	}
	// 注意：zombie 仅对原生 TCP / 可注入拨号器的插件生效
	// （ssh / smb / vnc / ftp / rsync / redis 等）。
	_ = zombieEngine
	fmt.Println("done")
}
