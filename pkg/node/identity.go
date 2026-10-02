package node

import (
	"fmt"
	"slices"
	"strings"

	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// BuildHello builds the AOP core agent registration message.
func BuildHello(name string, executor coretool.Executor, nodeID string, runtimeInfo *aop.AgentRuntimeInfo) (*aop.AgentHello, error) {
	return BuildHelloWithCapabilities(name, executor, nodeID, runtimeInfo)
}

// BuildHelloWithCapabilities adds profile-owned capability ids to the stable
// transport capabilities. The Hub uses these ids to select optional UI and
// protocol plugins without importing the profile implementation.
func BuildHelloWithCapabilities(name string, executor coretool.Executor, nodeID string, runtimeInfo *aop.AgentRuntimeInfo, profileCapabilities ...string) (*aop.AgentHello, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	if runtimeInfo == nil || runtimeInfo.Os == "" {
		runtimeInfo = DefaultRuntimeInfo()
	}
	capabilities := []string{"repl", "pty", "tmux", "file", "sco"}
	for _, capability := range profileCapabilities {
		capability = strings.TrimSpace(capability)
		if capability != "" && !slices.Contains(capabilities, capability) {
			capabilities = append(capabilities, capability)
		}
	}
	hello := &aop.AgentHello{
		NodeId: nodeID, Name: name,
		Capabilities: capabilities,
		Runtime:      runtimeInfo, Tools: executor.ToolDefinitions(),
	}
	return hello, nil
}
