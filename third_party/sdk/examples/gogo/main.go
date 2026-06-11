package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/pkg/provider"
	"github.com/chainreactors/sdk/pkg/types"
)

var (
	// 本地指纹配置
	fingersPath = flag.String("fingers-path", "", "Local fingers directory path")
	loadFingers = flag.Bool("fingers", true, "Load fingerprints")

	// 扫描配置
	target       = flag.String("target", "", "Target IP or CIDR (required)")
	ports        = flag.String("ports", "80,443,8080,8443", "Ports to scan (comma separated)")
	threads      = flag.Int("threads", 1000, "Number of threads")
	versionLevel = flag.Int("version", 0, "Version detection level (0-3)")
	exploit      = flag.String("exploit", "none", "Exploit mode (none/all/known)")
	timeout      = flag.Int("timeout", 5, "Request timeout in seconds")

	// 输出选项
	jsonOut = flag.Bool("json", false, "Output as JSON")
	verbose = flag.Bool("v", false, "Verbose output")
)

func main() {
	flag.Parse()

	// 验证参数
	if *target == "" {
		fmt.Println("Usage: gogo -target <ip/cidr> [-ports <ports>] [options]")
		fmt.Println("\nBasic scan:")
		fmt.Println("  gogo -target 127.0.0.1 -ports 80,443")
		fmt.Println("\nWith local fingerprints:")
		fmt.Println("  gogo -fingers -fingers-path /path/to/fingers -target 127.0.0.1")
		fmt.Println("\nAdvanced:")
		fmt.Println("  gogo -target 192.168.1.0/24 -ports 80,443,8080 -threads 2000 -version 2")
		os.Exit(1)
	}

	// 判断 target 是否为域名
	isDomain := !strings.Contains(*target, "/") && net.ParseIP(*target) == nil

	// 1. 加载 Fingers (可选)
	var fEngine *fingers.Engine

	// 加载 Fingers
	if *loadFingers {
		if !*jsonOut {
			fmt.Printf("Loading fingerprints from local path (%s)...\n", *fingersPath)
		}

		fingersConfig := fingers.NewConfig()
		fingersConfig.WithProvider(provider.NewFileProvider(*fingersPath, ""))

		fingersEng, err := fingers.NewEngine(fingersConfig)
		if err != nil {
			fmt.Printf("Error creating fingers engine: %v\n", err)
			os.Exit(1)
		}

		impl, err := fingersEng.GetFingersEngine()
		if err != nil {
			fmt.Printf("Error getting fingers engine: %v\n", err)
			os.Exit(1)
		}
		if impl != nil {
			fEngine = fingersEng
			if !*jsonOut {
				fmt.Printf("✅ Loaded %d HTTP fingerprints, %d Socket fingerprints\n",
					len(impl.HTTPFingers), len(impl.SocketFingers))
			}
		}
	}

	// 2. 创建 GoGo 引擎
	if !*jsonOut {
		fmt.Println("\nInitializing GoGo engine...")
	}

	gogoConfig := gogo.NewConfig()
	if fEngine != nil {
		gogoConfig.WithFingersEngine(fEngine)
	}
	gogoEngine := gogo.NewEngine(gogoConfig)

	if err := gogoEngine.Init(); err != nil {
		fmt.Printf("Error initializing gogo: %v\n", err)
		os.Exit(1)
	}

	if !*jsonOut {
		fmt.Println("✅ GoGo engine initialized")
		fmt.Println()
	}

	// 3. 配置扫描参数
	gogoCtx := gogo.NewContext().
		SetThreads(*threads).
		SetVersionLevel(*versionLevel).
		SetExploit(*exploit).
		SetDelay(*timeout)

	// 4. 执行扫描
	if !*jsonOut {
		fmt.Printf("🔍 Scanning %s (ports: %s)\n", *target, *ports)
		fmt.Printf("   Threads: %d | Version Level: %d | Timeout: %ds\n\n", *threads, *versionLevel, *timeout)
	}

	scanTask := gogo.NewScanTask(*target, *ports)
	resultCh, err := gogoEngine.Execute(gogoCtx, scanTask)
	if err != nil {
		fmt.Printf("Error executing scan: %v\n", err)
		os.Exit(1)
	}

	// 5. 处理结果
	results := []map[string]interface{}{}
	aliveCount := 0

	for result := range resultCh {
		if !result.Success() {
			continue
		}

		gogoResult, ok := types.ResultData[*types.GOGOResult](result)
		if !ok || gogoResult == nil {
			continue
		}

		aliveCount++

		// 转换端口为整数
		portNum := 0
		fmt.Sscanf(gogoResult.Port, "%d", &portNum)

		// 如果输入是域名，使用域名作为 host
		hostValue := gogoResult.Ip
		if isDomain {
			hostValue = *target
		}

		resultMap := map[string]interface{}{
			"host":     hostValue,
			"port":     portNum,
			"protocol": gogoResult.Protocol,
			"status":   gogoResult.Status,
		}

		if len(gogoResult.Frameworks) > 0 {
			frameworks := []string{}
			for _, fw := range gogoResult.Frameworks {
				frameworks = append(frameworks, fw.Name)
			}
			resultMap["frameworks"] = frameworks
		}

		if gogoResult.Title != "" {
			resultMap["title"] = gogoResult.Title
		}

		results = append(results, resultMap)

		// 实时输出
		if !*jsonOut {
			output := fmt.Sprintf("✓ %s:%d - %s", gogoResult.Ip, portNum, gogoResult.Status)
			if len(gogoResult.Frameworks) > 0 {
				fwNames := []string{}
				for _, fw := range gogoResult.Frameworks {
					fwNames = append(fwNames, fw.Name)
				}
				output += fmt.Sprintf(" [%s]", strings.Join(fwNames, ", "))
			}
			if gogoResult.Title != "" {
				output += fmt.Sprintf(" (%s)", gogoResult.Title)
			}
			fmt.Println(output)
		}
	}

	// 6. 输出汇总
	if *jsonOut {
		output := map[string]interface{}{
			"results": results,
		}
		jsonData, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(jsonData))
	} else {
		fmt.Println("\n========================================")
		fmt.Printf("📊 Scan completed\n")
		fmt.Printf("   Alive hosts: %d\n", aliveCount)
		fmt.Println("========================================")
	}
}
