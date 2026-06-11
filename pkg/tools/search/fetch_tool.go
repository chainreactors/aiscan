package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/chainreactors/aiscan/pkg/command"
)

type FetchTool struct {
	fetch *FetchCommand
}

type fetchToolArgs struct {
	URL     string `json:"url"              jsonschema:"description=URL to fetch. HTTPS assumed if no scheme provided."`
	Extract string `json:"extract,omitempty" jsonschema:"description=Optional focus hint to return only matching sections (e.g. CVSS score)"`
}

func NewFetchTool() *FetchTool {
	return &FetchTool{fetch: NewFetchCommand()}
}

func (t *FetchTool) Name() string { return "fetch" }

func (t *FetchTool) Description() string {
	return "Fetch a URL and return its content as readable text. HTML is auto-converted to Markdown. Use the extract parameter to focus on specific sections."
}

func (t *FetchTool) Definition() command.ToolDefinition {
	return command.ToolDef("fetch", t.Description(), fetchToolArgs{})
}

func (t *FetchTool) ClearCache() { t.fetch.ClearCache() }

func (t *FetchTool) Execute(ctx context.Context, arguments string) (command.ToolResult, error) {
	args, err := command.ParseStrictArgs[fetchToolArgs](arguments)
	if err != nil {
		return command.ToolResult{}, err
	}
	args.URL = strings.TrimSpace(args.URL)
	args.Extract = strings.TrimSpace(args.Extract)
	if args.URL == "" {
		return command.ToolResult{}, fmt.Errorf("url is required")
	}

	normalizedURL, err := normalizeURL(args.URL)
	if err != nil {
		return command.ToolResult{}, err
	}
	if err := validateURL(normalizedURL); err != nil {
		return command.ToolResult{}, err
	}

	if cached, ok := t.fetch.cache.Get(normalizedURL); ok {
		if cached.binary {
			return command.TextResult(formatBinaryCacheOutput(normalizedURL, cached)), nil
		}
		return command.TextResult(formatFetchOutput(normalizedURL, cached, args.Extract)), nil
	}

	result, redir, err := t.fetch.fetchWithRedirects(ctx, normalizedURL, 0)
	if err != nil {
		return command.ToolResult{}, err
	}

	if redir != nil {
		return command.TextResult(formatRedirectMessage(redir)), nil
	}

	if isBinaryContentType(result.contentType) {
		entry := &cacheEntry{
			contentType: result.contentType,
			binary:      true,
			bytes:       result.bytes,
			code:        result.code,
			codeText:    result.codeText,
			size:        binaryCacheEntrySize(result),
			fetchedAt:   timeNow(),
		}
		t.fetch.cache.Set(normalizedURL, entry)
		return command.TextResult(formatBinaryCacheOutput(normalizedURL, entry)), nil
	}

	content := result.body
	if isHTMLContentType(result.contentType) {
		content = htmlToMarkdown(content)
	}

	entry := &cacheEntry{
		content:     content,
		contentType: result.contentType,
		bytes:       result.bytes,
		code:        result.code,
		codeText:    result.codeText,
		size:        len(content),
		fetchedAt:   timeNow(),
	}
	t.fetch.cache.Set(normalizedURL, entry)

	return command.TextResult(formatFetchOutput(normalizedURL, entry, args.Extract)), nil
}
