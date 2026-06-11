package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/sdk/spray"
)

var (
	// 输入选项
	targetURL  = flag.String("u", "", "Single target URL")
	targetFile = flag.String("f", "", "File containing target URLs (one per line)")

	// 扫描配置
	threads    = flag.Int("threads", 50, "Number of threads")
	timeout    = flag.Int("timeout", 10, "Request timeout in seconds")
	maxRetries = flag.Int("retries", 0, "Maximum number of retries")
	userAgent  = flag.String("ua", "", "Custom User-Agent")

	// HTTP 选项
	method      = flag.String("method", "GET", "HTTP method (GET/POST/HEAD)")
	headers     = flag.String("headers", "", "Custom headers (format: 'Key1:Value1,Key2:Value2')")
	proxy       = flag.String("proxy", "", "Proxy URL (e.g., http://127.0.0.1:8080)")
	followRedir = flag.Bool("follow", true, "Follow redirects")

	// 过滤选项
	filterStatus = flag.String("fc", "", "Filter by status codes (comma separated)")
	matchStatus  = flag.String("mc", "", "Match status codes (comma separated)")
	filterSize   = flag.String("fs", "", "Filter by size (comma separated)")
	matchSize    = flag.String("ms", "", "Match size (comma separated)")

	// 输出选项
	jsonOut    = flag.Bool("json", false, "Output as JSON")
	verbose    = flag.Bool("v", false, "Verbose output")
	quiet      = flag.Bool("q", false, "Quiet mode (only show matched URLs)")
	outputFile = flag.String("o", "", "Output file")
)

func main() {
	flag.Parse()

	// 验证参数
	if *targetURL == "" && *targetFile == "" {
		fmt.Println("Usage: spray [-u <url> | -f <file>] [options]")
		fmt.Println("\nSingle URL:")
		fmt.Println("  spray -u http://example.com")
		fmt.Println("\nMultiple URLs from file:")
		fmt.Println("  spray -f urls.txt")
		fmt.Println("\nWith options:")
		fmt.Println("  spray -f urls.txt -threads 100 -timeout 5 -mc 200,301,302")
		fmt.Println("\nCustom headers:")
		fmt.Println("  spray -u http://example.com -headers 'Authorization:Bearer token,X-Custom:value'")
		fmt.Println("\nWith proxy:")
		fmt.Println("  spray -u http://example.com -proxy http://127.0.0.1:8080")
		fmt.Println("\nOutput to file:")
		fmt.Println("  spray -f urls.txt -o results.txt")
		os.Exit(1)
	}

	// 1. 读取目标 URLs
	var urls []string

	if *targetURL != "" {
		urls = append(urls, *targetURL)
	}

	if *targetFile != "" {
		fileUrls, err := readURLsFromFile(*targetFile)
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			os.Exit(1)
		}
		urls = append(urls, fileUrls...)
	}

	if len(urls) == 0 {
		fmt.Println("Error: No URLs to scan")
		os.Exit(1)
	}

	if !*quiet && !*jsonOut {
		fmt.Printf("Loaded %d URL(s)\n\n", len(urls))
	}

	// 2. 创建 Spray 引擎
	sprayEngine := spray.NewEngine(nil)
	if err := sprayEngine.Init(); err != nil {
		fmt.Printf("Error initializing spray: %v\n", err)
		os.Exit(1)
	}

	// 3. 配置扫描参数
	sprayCtx := spray.NewContext().
		SetThreads(*threads).
		SetTimeout(*timeout).
		SetMethod(*method)
	if *headers != "" {
		sprayCtx.SetHeaders(strings.Split(*headers, ","))
	}

	// 4. 执行扫描
	if !*quiet && !*jsonOut {
		fmt.Printf("🔍 Scanning %d URL(s)\n", len(urls))
		fmt.Printf("   Threads: %d | Timeout: %ds | Method: %s\n\n", *threads, *timeout, *method)
	}

	checkTask := spray.NewCheckTask(urls)
	resultCh, err := sprayEngine.Execute(sprayCtx, checkTask)
	if err != nil {
		fmt.Printf("Error executing scan: %v\n", err)
		os.Exit(1)
	}

	// 5. 处理结果
	results := []map[string]interface{}{}
	totalCount := 0
	matchedCount := 0

	// 解析过滤器
	var filterStatusCodes, matchStatusCodes []string
	if *filterStatus != "" {
		filterStatusCodes = strings.Split(*filterStatus, ",")
	}
	if *matchStatus != "" {
		matchStatusCodes = strings.Split(*matchStatus, ",")
	}

	// 准备输出文件
	var outputWriter *bufio.Writer
	if *outputFile != "" {
		f, err := os.Create(*outputFile)
		if err != nil {
			fmt.Printf("Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		outputWriter = bufio.NewWriter(f)
		defer outputWriter.Flush()
	}

	for result := range resultCh {
		totalCount++

		if !result.Success() {
			if *verbose && !*jsonOut {
				fmt.Printf("✗ Error processing result\n")
			}
			continue
		}

		sprayResult, ok := types.ResultData[*types.SprayResult](result)
		if !ok || sprayResult == nil {
			continue
		}

		// 应用过滤器
		statusStr := fmt.Sprintf("%d", sprayResult.Status)

		// Filter status codes
		if len(filterStatusCodes) > 0 {
			skip := false
			for _, fc := range filterStatusCodes {
				if strings.TrimSpace(fc) == statusStr {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}

		// Match status codes
		if len(matchStatusCodes) > 0 {
			matched := false
			for _, mc := range matchStatusCodes {
				if strings.TrimSpace(mc) == statusStr {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		matchedCount++

		resultMap := map[string]interface{}{
			"url":    sprayResult.UrlString,
			"status": sprayResult.Status,
		}

		if sprayResult.Title != "" {
			resultMap["title"] = sprayResult.Title
		}

		results = append(results, resultMap)

		// 实时输出
		if !*jsonOut {
			if *quiet {
				output := sprayResult.UrlString
				fmt.Println(output)
				if outputWriter != nil {
					outputWriter.WriteString(output + "\n")
				}
			} else {
				output := fmt.Sprintf("✓ [%d] %s", sprayResult.Status, sprayResult.UrlString)
				if sprayResult.Title != "" {
					output += fmt.Sprintf(" - %s", sprayResult.Title)
				}
				fmt.Println(output)
				if outputWriter != nil {
					outputWriter.WriteString(output + "\n")
				}
			}
		}
	}

	// 6. 输出汇总
	if *jsonOut {
		output := map[string]interface{}{
			"total_urls":    len(urls),
			"processed":     totalCount,
			"matched_count": matchedCount,
			"results":       results,
		}
		jsonData, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(jsonData))
		if outputWriter != nil {
			outputWriter.WriteString(string(jsonData) + "\n")
		}
	} else if !*quiet {
		fmt.Println("\n========================================")
		fmt.Printf("📊 Scan completed\n")
		fmt.Printf("   Total: %d | Processed: %d | Matched: %d\n", len(urls), totalCount, matchedCount)
		if *outputFile != "" {
			fmt.Printf("   Output saved to: %s\n", *outputFile)
		}
		fmt.Println("========================================")
	}
}

func readURLsFromFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return urls, nil
}
