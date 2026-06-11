package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/utils/httputils"
)

var (
	// Cyberhub 配置
	cyberhubURL = flag.String("url", "", "Cyberhub URL (e.g., http://127.0.0.1:8080)")
	apiKey      = flag.String("key", "", "Cyberhub API Key")
	source      = flag.String("source", "", "Filter by source (optional)")

	// 本地配置
	localEngines = flag.String("engines", "", "Enable specific engines (comma separated, optional)")

	// 匹配选项
	target   = flag.String("target", "", "Target URL to match")
	jsonOut  = flag.Bool("json", false, "Output as JSON")
	showInfo = flag.Bool("info", false, "Show fingerprint details")
)

func main() {
	flag.Parse()

	// 验证参数
	if *target == "" {
		fmt.Println("Usage: fingers [-url <cyberhub_url> -key <api_key>] [-engines <engines>] -target <url>")
		fmt.Println("\nLoad from Cyberhub:")
		fmt.Println("  fingers -url http://127.0.0.1:8080 -key your_key -target http://example.com")
		fmt.Println("\nLoad from local:")
		fmt.Println("  fingers -target http://example.com")
		fmt.Println("\nFilter by source:")
		fmt.Println("  fingers -url http://127.0.0.1:8080 -key your_key -source github -target http://example.com")
		os.Exit(1)
	}

	// 1. 加载 Fingers 引擎
	var engine *fingers.Engine
	var err error

	config := fingers.NewConfig()

	if *cyberhubURL != "" {
		if *apiKey == "" {
			fmt.Println("Error: -key is required when using -url")
			os.Exit(1)
		}
		provider := cyberhub.NewProvider(*cyberhubURL, *apiKey)
		if *source != "" {
			provider.WithFilter(types.NewExportFilter().WithSources(*source))
		}
		config.WithProvider(provider)
		fmt.Printf("Loading fingerprints from Cyberhub (%s)...\n", *cyberhubURL)
	} else {
		if *localEngines != "" {
			// TODO: parse comma separated engines
			fmt.Println("Loading fingerprints from local...")
		} else {
			fmt.Println("Loading fingerprints from local (default engines)...")
		}
	}

	engine, err = fingers.NewEngine(config)
	if err != nil {
		fmt.Printf("Error creating engine: %v\n", err)
		os.Exit(1)
	}

	libEngine := engine.Get()
	if libEngine == nil {
		fmt.Printf("Error getting fingers engine\n")
		os.Exit(1)
	}

	fmt.Println("✅ Fingerprints loaded successfully")

	// 2. 发起 HTTP 请求
	fmt.Printf("Fetching target: %s\n", *target)
	resp, err := http.Get(*target)
	if err != nil {
		fmt.Printf("Error fetching target: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	data := httputils.ReadRaw(resp)

	// 3. 匹配指纹
	frameworks, err := libEngine.DetectContent(data)
	if err != nil {
		fmt.Printf("Error detecting fingerprints: %v\n", err)
		os.Exit(1)
	}

	// 4. 输出结果
	if len(frameworks) == 0 {
		fmt.Println("\n❌ No fingerprints matched")
		os.Exit(0)
	}

	fmt.Printf("\n✅ Matched %d fingerprint(s):\n\n", len(frameworks))

	if *jsonOut {
		// JSON 输出
		output, _ := json.MarshalIndent(frameworks, "", "  ")
		fmt.Println(string(output))
	} else {
		// 人类可读输出
		idx := 1
		for _, fw := range frameworks {
			fmt.Printf("[%d] %s\n", idx, fw.Name)
			if *showInfo {
				if fw.Version != "" {
					fmt.Printf("    Version: %s\n", fw.Version)
				}
				if cpe := fw.CPE(); cpe != "" {
					fmt.Printf("    CPE: %s\n", cpe)
				}
			}
			idx++
		}
	}

	// 5. 通过 Alias 查找关联 POC（纯内存查找，无额外 API 调用）
	//
	// 完整链路:
	//   DetectContent(data) 返回 Frameworks (map[name]*Framework)
	//     → libEngine.Aliases.FindFramework(fw) 根据 fw.Name 查找 Alias
	//       → Alias.Pocs 就是关联的 POC ID 列表 (如 ["CVE-2021-29441", ...])
	//
	// 数据来源: CyberHub 导出指纹时，Alias 及其 Pocs 字段一起加载到内存。
	// 查找方式: FindFramework 通过 framework.From (引擎名) + framework.Name
	//           在 Aliases.Map 中做精确匹配，时间复杂度 O(1)。
	fmt.Println("\n--- Associated POCs ---")
	if libEngine.Aliases == nil {
		fmt.Println("(No alias data loaded)")
	} else {
		for _, fw := range frameworks {
			if a, ok := libEngine.Aliases.FindFramework(fw); ok && len(a.Pocs) > 0 {
				fmt.Printf("%s -> POCs: %v\n", fw.Name, a.Pocs)
			}
		}
	}
}
