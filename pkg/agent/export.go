package agent

import "github.com/chainreactors/aiscan/pkg/agent/provider"

// Type aliases re-export provider types through the agent package,
// so external consumers only need to import agent.

type ChatMessage = provider.ChatMessage
type ChatMessageDelta = provider.ChatMessageDelta
type ToolCall = provider.ToolCall
type ToolCallDelta = provider.ToolCallDelta
type FunctionCall = provider.FunctionCall
type FunctionCallDelta = provider.FunctionCallDelta
type ToolDefinition = provider.ToolDefinition
type FunctionDefinition = provider.FunctionDefinition
type ContentPart = provider.ContentPart
type ImageURL = provider.ImageURL
type ChatCompletionRequest = provider.ChatCompletionRequest
type ChatCompletionResponse = provider.ChatCompletionResponse
type ChatCompletionStreamEvent = provider.ChatCompletionStreamEvent
type Choice = provider.Choice
type Usage = provider.Usage
type APIError = provider.APIError
type ResponseFormat = provider.ResponseFormat
type JSONSchemaSpec = provider.JSONSchemaSpec
type CacheRetention = provider.CacheRetention
type Provider = provider.Provider
type StreamingProvider = provider.StreamingProvider
type ProviderConfig = provider.ProviderConfig

const (
	CacheNone  = provider.CacheNone
	CacheShort = provider.CacheShort
	CacheLong  = provider.CacheLong
)

var (
	NewTextMessage       = provider.NewTextMessage
	NewToolResultMessage = provider.NewToolResultMessage
	NewMultimodalMessage = provider.NewMultimodalMessage
	TextPart             = provider.TextPart
	ImagePart            = provider.ImagePart
	ParseDataURI         = provider.ParseDataURI

	NewProvider             = provider.NewProvider
	NewProviderFromResolved = provider.NewProviderFromResolved
	ResolveProvider         = provider.Resolve
	InferProviderFromBaseURL = provider.InferFromBaseURL
	KnownProviders          = provider.KnownProviders
	APIKeyEnvName           = provider.APIKeyEnvName
)
