package fingers

import (
	"net/http"
	"testing"
	"time"
)

// TestContextConfigurationCrossEffect 测试配置交叉生效
// 验证无论调用顺序如何，配置都能正确应用
func TestContextConfigurationCrossEffect(t *testing.T) {
	// 测试 1: WithTimeout -> WithProxy
	ctx1 := NewContext().WithTimeout(5).WithProxy("socks5://127.0.0.1:1080")
	client1 := ctx1.GetClient()

	if client1.Timeout != 5*time.Second {
		t.Errorf("Test 1 failed: expected timeout 5s, got %v", client1.Timeout)
	}

	// 测试 2: WithProxy -> WithTimeout (反向顺序)
	ctx2 := NewContext().WithProxy("socks5://127.0.0.1:1080").WithTimeout(5)
	client2 := ctx2.GetClient()

	if client2.Timeout != 5*time.Second {
		t.Errorf("Test 2 failed: expected timeout 5s, got %v", client2.Timeout)
	}

	// 测试 3: 多次调用 WithTimeout
	ctx3 := NewContext().WithTimeout(5).WithTimeout(10)
	client3 := ctx3.GetClient()

	if client3.Timeout != 10*time.Second {
		t.Errorf("Test 3 failed: expected timeout 10s, got %v", client3.Timeout)
	}

	// 测试 4: WithLevel 不应影响 client
	ctx4 := NewContext().WithTimeout(5).WithLevel(2)
	client4 := ctx4.GetClient()

	if client4.Timeout != 5*time.Second {
		t.Errorf("Test 4 failed: expected timeout 5s, got %v", client4.Timeout)
	}

	// 测试 5: 验证 defaultClient 被缓存
	ctx5 := NewContext().WithTimeout(5)
	client5a := ctx5.GetClient()
	client5b := ctx5.GetClient()

	if client5a != client5b {
		t.Error("Test 5 failed: GetClient() should return the same cached defaultClient")
	}

	// 测试 6: 验证配置更改后 defaultClient 被重建
	ctx6 := NewContext().WithTimeout(5)
	client6a := ctx6.GetClient()
	ctx6.WithTimeout(10)
	client6b := ctx6.GetClient()

	if client6a == client6b {
		t.Error("Test 6 failed: defaultClient should be rebuilt after configuration change")
	}

	if client6b.Timeout != 10*time.Second {
		t.Errorf("Test 6 failed: expected timeout 10s after update, got %v", client6b.Timeout)
	}
}

// TestContextWithClient 测试用户自定义客户端优先级
func TestContextWithClient(t *testing.T) {
	// 创建自定义客户端
	customClient := &http.Client{
		Timeout: 20 * time.Second,
	}

	// 测试 1: WithClient 应该优先于默认配置
	ctx1 := NewContext().WithTimeout(5).WithClient(customClient)
	client1 := ctx1.GetClient()

	if client1 != customClient {
		t.Error("Test 1 failed: GetClient() should return custom client")
	}

	if client1.Timeout != 20*time.Second {
		t.Errorf("Test 1 failed: expected custom timeout 20s, got %v", client1.Timeout)
	}

	// 测试 2: WithClient 在 WithTimeout 之前
	ctx2 := NewContext().WithClient(customClient).WithTimeout(5)
	client2 := ctx2.GetClient()

	if client2 != customClient {
		t.Error("Test 2 failed: GetClient() should return custom client regardless of order")
	}
}

// TestContextDefaultValues 测试默认值
func TestContextDefaultValues(t *testing.T) {
	ctx := NewContext()

	// 测试默认 timeout
	if ctx.GetTimeout() != 10 {
		t.Errorf("Expected default timeout 10, got %d", ctx.GetTimeout())
	}

	// 测试默认 level
	if ctx.GetLevel() != 1 {
		t.Errorf("Expected default level 1, got %d", ctx.GetLevel())
	}

	// 测试默认客户端
	client := ctx.GetClient()
	if client.Timeout != 10*time.Second {
		t.Errorf("Expected default client timeout 10s, got %v", client.Timeout)
	}
}
