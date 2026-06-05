package command

import "strings"

type ToolResult struct {
	Content   []ContentBlock
	IsError   bool
	Terminate bool
	// SkipTruncation marks results that have already applied their own output
	// budgeting and must be delivered to the model unchanged.
	SkipTruncation bool
}

// Text returns all text content blocks concatenated, for backward
// compatibility with code that expects a plain string.
func (r ToolResult) Text() string {
	var sb strings.Builder
	for _, block := range r.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

func TextResult(s string) ToolResult {
	return ToolResult{Content: []ContentBlock{TextBlock(s)}}
}

// ManagedTextResult returns text that should bypass the agent's generic
// result truncation because the producing tool has already bounded it.
func ManagedTextResult(s string) ToolResult {
	return ToolResult{Content: []ContentBlock{TextBlock(s)}, SkipTruncation: true}
}

func ErrorResult(msg string) ToolResult {
	return ToolResult{Content: []ContentBlock{TextBlock(msg)}, IsError: true}
}

func TerminateResult(s string) ToolResult {
	return ToolResult{Content: []ContentBlock{TextBlock(s)}, Terminate: true}
}

func (r ToolResult) HasImages() bool {
	for _, block := range r.Content {
		if block.Type == "image" {
			return true
		}
	}
	return false
}
