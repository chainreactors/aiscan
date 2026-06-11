package main

import (
	"fmt"
	"log"

	"github.com/chainreactors/sdk/client"
)

func main() {
	fmt.Println("=== 测试所有引擎 ===")
	fmt.Println()

	// 创建 SDK 客户端
	c := client.New()
	defer c.Close()

	// 测试 Fingers 引擎
	fmt.Println("1. Fingers 引擎")
	fmt.Println("----------------------------------------")
	fingersEngine, err := c.Fingers()
	if err != nil {
		log.Printf("❌ 获取 Fingers 引擎失败: %v\n", err)
	} else {
		fmt.Printf("✅ Fingers 引擎初始化成功，指纹数量: %d\n", fingersEngine.Count())
	}
	fmt.Println()

	// 测试 Gogo 引擎
	fmt.Println("2. Gogo 引擎")
	fmt.Println("----------------------------------------")
	_, err = c.Gogo()
	if err != nil {
		log.Printf("❌ 获取 Gogo 引擎失败: %v\n", err)
	} else {
		fmt.Printf("✅ Gogo 引擎初始化成功\n")
	}
	fmt.Println()

	// 测试 Spray 引擎
	fmt.Println("3. Spray 引擎")
	fmt.Println("----------------------------------------")
	_, err = c.Spray()
	if err != nil {
		log.Printf("❌ 获取 Spray 引擎失败: %v\n", err)
	} else {
		fmt.Printf("✅ Spray 引擎初始化成功\n")
	}
	fmt.Println()

	// 测试 Neutron 引擎
	fmt.Println("4. Neutron 引擎")
	fmt.Println("----------------------------------------")
	neutronEngine, err := c.Neutron()
	if err != nil {
		log.Printf("❌ 获取 Neutron 引擎失败: %v\n", err)
	} else {
		fmt.Printf("✅ Neutron 引擎初始化成功，模板数量: %d\n", neutronEngine.Count())
	}
	fmt.Println()

	fmt.Println("=== 测试完成 ===")
}
