package main

import (
	"fmt"
	"log"
	"os"

	"github.com/chainreactors/sdk/client"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/spray"
)

func main() {
	fmt.Println("=== SDK Client 使用示例 ===")
	fmt.Println()

	// 方式一：无参创建（使用本地默认数据）
	c := client.New()
	defer c.Close()

	// 方式二：通过 Provider 共享数据源 + 开启关联索引
	// provider := cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key")
	// c := client.New(
	//     client.WithProvider(provider),
	//     client.WithIndex(nil),
	// )
	// defer c.Close()

	// 方式三：从环境变量自动配置
	if os.Getenv("CYBERHUB_URL") != "" {
		provider := cyberhub.NewProvider(
			os.Getenv("CYBERHUB_URL"),
			os.Getenv("CYBERHUB_KEY"),
		)
		c = client.New(
			client.WithProvider(provider),
			client.WithIndex(nil),
			client.WithGogoConfig(gogo.NewConfig().WithCapacity(5000)),
		)
		defer c.Close()
		fmt.Printf("Using Cyberhub: %s\n\n", os.Getenv("CYBERHUB_URL"))
	}

	testFingers(c)
	fmt.Println()
	testGogo(c)
	fmt.Println()
	testSpray(c)
	fmt.Println()
	testLookup(c)
}

func testFingers(c *client.Client) {
	fmt.Println("1. Fingers 指纹识别")
	fmt.Println("----------------------------------------")

	fingersEngine, err := c.Fingers()
	if err != nil {
		log.Printf("获取 Fingers 引擎失败: %v\n", err)
		return
	}

	testData := []byte("HTTP/1.1 200 OK\r\nServer: nginx/1.18.0\r\n\r\n")
	frameworks, err := fingersEngine.Match(testData)
	if err != nil {
		log.Printf("匹配失败: %v\n", err)
		return
	}

	fmt.Printf("识别到 %d 个指纹\n", len(frameworks))
	count := 0
	for _, fw := range frameworks {
		if count < 3 {
			fmt.Printf("   - %s\n", fw.Name)
			count++
		}
	}
}

func testGogo(c *client.Client) {
	fmt.Println("2. Gogo 端口扫描")
	fmt.Println("----------------------------------------")

	gogoEngine, err := c.Gogo()
	if err != nil {
		log.Printf("获取 Gogo 引擎失败: %v\n", err)
		return
	}

	ctx := gogo.NewContext()
	results, err := gogoEngine.Scan(ctx, "127.0.0.1", "80,443")
	if err != nil {
		log.Printf("扫描失败: %v\n", err)
		return
	}

	fmt.Printf("扫描完成，发现 %d 个开放端口\n", len(results))
}

func testSpray(c *client.Client) {
	fmt.Println("3. Spray URL 检测")
	fmt.Println("----------------------------------------")

	sprayEngine, err := c.Spray()
	if err != nil {
		log.Printf("获取 Spray 引擎失败: %v\n", err)
		return
	}

	ctx := spray.NewContext()
	urls := []string{
		"http://httpbin.org",
		"http://example.com",
	}

	results, err := sprayEngine.Check(ctx, urls)
	if err != nil {
		log.Printf("检测失败: %v\n", err)
		return
	}

	fmt.Printf("检测完成，收到 %d 个响应\n", len(results))
}

func testLookup(c *client.Client) {
	fmt.Println("4. 关联查询")
	fmt.Println("----------------------------------------")

	result, err := c.LookupByFinger("nginx")
	if err != nil {
		fmt.Println("关联索引未开启（使用 WithIndex 开启）")
		return
	}

	fmt.Printf("nginx 关联: %d 个别名, %d 个 POC\n",
		len(result.Aliases), len(result.Templates))
	for _, t := range result.Templates {
		fmt.Printf("   - %s [%s]\n", t.Id, t.Info.Severity)
	}
}
