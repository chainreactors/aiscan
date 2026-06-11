//go:build hostspray

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/sdk/spray"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: host_spray_sdk <target_ip> <hosts_file> [limit]")
		fmt.Println("Example: host_spray_sdk 110.75.231.10 domain.txt 10000")
		os.Exit(1)
	}

	targetIP := os.Args[1]
	hostsFile := os.Args[2]
	limit := 10000

	if len(os.Args) > 3 {
		fmt.Sscanf(os.Args[3], "%d", &limit)
	}

	// 读取 hosts 字典
	fmt.Printf("📖 Reading hosts from: %s\n", hostsFile)
	hosts, err := readHosts(hostsFile, limit)
	if err != nil {
		fmt.Printf("❌ Error reading hosts: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Loaded %d hosts\n\n", len(hosts))

	// 创建 Spray 引擎
	fmt.Println("🔧 Initializing Spray engine...")
	engine := spray.NewEngine(nil)
	if err := engine.Init(); err != nil {
		fmt.Printf("❌ Error initializing engine: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Engine initialized\n")

	// 配置上下文 - 关键：设置 Mod 为 "host"
	fmt.Println("⚙️  Configuring spray context...")
	ctx := spray.NewContext().
		SetThreads(100). // 并发线程数
		SetTimeout(5).   // 超时时间
		SetMod("host")   // 设置为 host 模式（关键！）

	fmt.Printf("   Mode: host\n")
	fmt.Printf("   Threads: 100\n")
	fmt.Printf("   Timeout: 5s\n\n")

	// 创建暴力破解任务
	fmt.Printf("🎯 Starting host collision attack on %s\n", targetIP)
	fmt.Printf("   Testing %d hosts...\n\n", len(hosts))

	baseURL := "http://" + targetIP
	task := spray.NewBruteTask(baseURL, hosts)

	// 执行任务
	resultCh, err := engine.Execute(ctx, task)
	if err != nil {
		fmt.Printf("❌ Error executing task: %v\n", err)
		os.Exit(1)
	}

	// 处理结果
	fmt.Println("📊 Results:")
	fmt.Println("─────────────────────────────────────────────────────────────")

	count := 0
	validCount := 0

	for result := range resultCh {
		count++

		if !result.Success() {
			continue
		}

		sprayResult, ok := types.ResultData[*types.SprayResult](result)
		if !ok || sprayResult == nil {
			continue
		}

		// 过滤掉 502 错误（通常是默认响应）
		if sprayResult.Status == 502 {
			continue
		}

		validCount++

		// 输出有效结果
		fmt.Printf("[%d] %s\n", validCount, sprayResult.UrlString)
		fmt.Printf("    Status: %d | Length: %d",
			sprayResult.Status, sprayResult.BodyLength)

		if sprayResult.Title != "" {
			fmt.Printf(" | Title: %s", sprayResult.Title)
		}
		fmt.Println()
	}

	// 输出统计
	fmt.Println("─────────────────────────────────────────────────────────────")
	fmt.Printf("\n✓ Scan completed\n")
	fmt.Printf("   Total processed: %d\n", count)
	fmt.Printf("   Valid hosts found: %d\n", validCount)
}

func readHosts(filename string, limit int) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var hosts []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		hosts = append(hosts, line)

		// 达到限制后停止
		if limit > 0 && len(hosts) >= limit {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return hosts, nil
}
