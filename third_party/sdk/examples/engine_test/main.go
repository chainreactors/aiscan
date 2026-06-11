package main

import (
	"fmt"
	"log"

	"github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/provider"
)

func main() {
	fmt.Println("=== 测试 Fingers 引擎功能 ===")
	fmt.Println()

	// 创建配置（使用本地测试数据）
	config := fingers.NewConfig().WithProvider(provider.NewFileProvider("test_fingers.yaml", ""))

	fmt.Println("正在加载指纹数据...")

	// 创建引擎
	engine, err := fingers.NewEngine(config)
	if err != nil {
		log.Fatalf("❌ 创建引擎失败: %v\n", err)
	}
	defer engine.Close()

	fmt.Printf("✅ 引擎创建成功，加载了 %d 个指纹\n\n", engine.Count())

	// 测试1: 被动内容匹配
	testMatch(engine)

	// 测试2: HTTP主动探测
	testHTTPMatch(engine)
}

func testMatch(engine *fingers.Engine) {
	fmt.Println("1. 测试被动内容匹配")
	fmt.Println("----------------------------------------")

	// 创建测试数据（模拟HTTP响应）
	httpData := []byte(`HTTP/1.1 200 OK
Server: nginx/1.18.0
Content-Type: text/html

<!DOCTYPE html>
<html>
<head><title>Test</title></head>
<body>Powered by nginx</body>
</html>`)

	// 执行匹配
	frameworks, err := engine.Match(httpData)
	if err != nil {
		log.Printf("❌ 匹配失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 匹配成功，识别到 %d 个指纹\n", len(frameworks))
	count := 0
	for _, fw := range frameworks {
		if count < 5 { // 只显示前5个
			fmt.Printf("   - %s (版本: %s)\n", fw.Name, fw.Version)
			count++
		}
	}
	if len(frameworks) > 5 {
		fmt.Printf("   ... 还有 %d 个指纹\n", len(frameworks)-5)
	}
	fmt.Println()
}

func testHTTPMatch(engine *fingers.Engine) {
	fmt.Println("2. 测试 HTTP 主动探测")
	fmt.Println("----------------------------------------")

	// 创建 Context
	ctx := fingers.NewContext().WithTimeout(10).WithLevel(1)

	// 测试URL列表
	testURLs := []string{
		"http://httpbin.org",
		"http://example.com",
	}

	fmt.Println("开始扫描...")

	// 执行 HTTP 匹配
	results, err := engine.HTTPMatch(ctx, testURLs)
	if err != nil {
		log.Printf("❌ 扫描失败: %v\n", err)
		return
	}

	// 显示结果
	for _, result := range results {
		fmt.Printf("\n目标: %s\n", result.Target)
		if result.Success() {
			if len(result.Results) > 0 {
				fmt.Printf("✅ 识别到 %d 个指纹\n", len(result.Results))
				for i, sr := range result.Results {
					if i < 3 { // 只显示前3个
						fmt.Printf("   - %s (版本: %s)\n",
							sr.Framework.Name, sr.Framework.Version)
					}
				}
				if len(result.Results) > 3 {
					fmt.Printf("   ... 还有 %d 个指纹\n", len(result.Results)-3)
				}
			} else {
				fmt.Println("⚠️  未识别到指纹")
			}
		} else {
			fmt.Printf("❌ 扫描失败: %v\n", result.Err)
		}
	}
}
