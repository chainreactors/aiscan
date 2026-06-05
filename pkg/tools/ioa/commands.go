package ioa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/chainreactors/aiscan/pkg/command"
	ioamodel "github.com/chainreactors/ioa"
	ioaclient "github.com/chainreactors/ioa/client"
)

func NewCommands(client ioaclient.API, nodeName string, meta map[string]any) []command.PseudoCommand {
	var cmds []command.PseudoCommand
	resolvingClient := newSpaceResolvingClient(client, nodeName)
	for _, t := range ioaclient.NewTools(resolvingClient, ioaclient.ToolOptions{NodeName: nodeName, NodeMeta: meta}) {
		cmds = append(cmds, &toolAdapter{tool: t, descOverride: toolDescOverrides[t.Name()]})
	}
	return cmds
}

var toolDescOverrides = map[string]string{
	"ioa_send": `Send a structured IOA message to a space. The --space_id value accepts either the space hash ID or the human-readable space name; --space and --space_name are accepted aliases. The --content value MUST be a valid JSON object (not a string). It MUST include a "content" key. Correct: --content '{"content": "your message here", "meta": {"kind": "finding"}}'. WRONG: --content '"just a string"'. Use --refs '{"nodes": ["<id>"]}' to direct to a specific node.`,
	"ioa_read": `Read messages from an IOA space. The --space_id value accepts either the space hash ID or the human-readable space name; --space and --space_name are accepted aliases. Example: ioa_read --space "my-space-name" --all true --limit 50. Use --after "<message_id>" to paginate.`,
}

type spaceResolver interface {
	ResolveSpace(ctx context.Context, nameOrID string) (ioamodel.SpaceInfo, error)
}

type spaceResolvingClient struct {
	ioaclient.API

	mu                  sync.RWMutex
	cache               map[string]string
	autoJoinDescription string
}

func newSpaceResolvingClient(client ioaclient.API, nodeName string) ioaclient.API {
	desc := "aiscan ioa tool auto-join"
	if strings.TrimSpace(nodeName) != "" {
		desc = "aiscan ioa tool auto-join: " + strings.TrimSpace(nodeName)
	}
	return &spaceResolvingClient{
		API:                 client,
		cache:               make(map[string]string),
		autoJoinDescription: desc,
	}
}

func (c *spaceResolvingClient) Space(ctx context.Context, name, description string, tags ...string) (ioamodel.SpaceInfo, error) {
	info, err := c.API.Space(ctx, name, description, tags...)
	if err != nil {
		return ioamodel.SpaceInfo{}, err
	}
	c.cacheSpace("", info)
	return info, nil
}

func (c *spaceResolvingClient) Send(ctx context.Context, spaceID string, body ioamodel.SendMessage) (ioamodel.Message, error) {
	resolved, err := c.resolveSpaceID(ctx, spaceID)
	if err != nil {
		return ioamodel.Message{}, err
	}
	return c.API.Send(ctx, resolved, body)
}

func (c *spaceResolvingClient) Read(ctx context.Context, spaceID string, opts ioamodel.ReadOptions) ([]ioamodel.Message, error) {
	resolved, err := c.resolveSpaceID(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	return c.API.Read(ctx, resolved, opts)
}

func (c *spaceResolvingClient) resolveSpaceID(ctx context.Context, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("space_id is required")
	}
	if id, ok := c.cachedSpaceID(selector); ok {
		return id, nil
	}
	if resolver, ok := c.API.(spaceResolver); ok {
		info, err := resolver.ResolveSpace(ctx, selector)
		if err == nil {
			c.cacheSpace(selector, info)
			return info.ID, nil
		}
		if !spaceNotFound(err) || looksLikeSpaceID(selector) {
			return "", err
		}
	} else if looksLikeSpaceID(selector) {
		return selector, nil
	}

	info, err := c.Space(ctx, selector, c.autoJoinDescription)
	if err != nil {
		return "", err
	}
	c.cacheSpace(selector, info)
	return info.ID, nil
}

func (c *spaceResolvingClient) cachedSpaceID(selector string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.cache[selector]
	return id, ok
}

func (c *spaceResolvingClient) cacheSpace(selector string, info ioamodel.SpaceInfo) {
	if strings.TrimSpace(info.ID) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[info.ID] = info.ID
	if strings.TrimSpace(info.Name) != "" {
		c.cache[info.Name] = info.ID
	}
	if strings.TrimSpace(selector) != "" {
		c.cache[selector] = info.ID
	}
}

func spaceNotFound(err error) bool {
	var protocolErr *ioamodel.Error
	return errors.As(err, &protocolErr) && protocolErr.Status == http.StatusNotFound
}

func looksLikeSpaceID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

type toolAdapter struct {
	tool         ioaclient.Tool
	descOverride string
}

func (a *toolAdapter) InProcess() {}

func (a *toolAdapter) Name() string { return a.tool.Name() }

func (a *toolAdapter) Usage() string {
	def := a.tool.Definition()
	desc := a.tool.Description()
	if a.descOverride != "" {
		desc = a.descOverride
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s - %s\n", def.Function.Name, desc))

	params := def.Function.Parameters
	props, _ := params["properties"].(map[string]interface{})
	reqRaw, _ := params["required"].([]interface{})
	required := make([]string, 0, len(reqRaw))
	for _, r := range reqRaw {
		if s, ok := r.(string); ok {
			required = append(required, s)
		}
	}
	requiredSet := make(map[string]bool, len(required))
	for _, r := range required {
		requiredSet[r] = true
	}

	if len(props) > 0 {
		sb.WriteString("\nUsage:\n")
		sb.WriteString(fmt.Sprintf("  %s", def.Function.Name))
		for name := range props {
			if requiredSet[name] {
				sb.WriteString(fmt.Sprintf(" --%s <value>", name))
			} else {
				sb.WriteString(fmt.Sprintf(" [--%s <value>]", name))
			}
		}
		sb.WriteString("\n\nOptions:\n")
		for name, schema := range props {
			desc := ""
			if m, ok := schema.(map[string]interface{}); ok {
				desc, _ = m["description"].(string)
			}
			marker := ""
			if requiredSet[name] {
				marker = " (required)"
			}
			sb.WriteString(fmt.Sprintf("  --%-16s %s%s\n", name, desc, marker))
		}
	}
	return sb.String()
}

func (a *toolAdapter) Execute(ctx context.Context, args []string) (string, error) {
	argMap, err := argsToMap(args)
	if err != nil {
		return "", fmt.Errorf("%s: %w\n\n%s", a.tool.Name(), err, a.Usage())
	}
	normalizeSpaceAliases(a.tool.Name(), argMap)
	jsonArgs, err := jsonFromMap(argMap)
	if err != nil {
		return "", err
	}
	return a.tool.Execute(ctx, jsonArgs)
}

func argsToJSON(args []string) (string, error) {
	m, err := argsToMap(args)
	if err != nil {
		return "", err
	}
	return jsonFromMap(m)
}

func argsToMap(args []string) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			m[key] = true
			continue
		}
		i++
		val := args[i]
		if val == "true" {
			m[key] = true
		} else if val == "false" {
			m[key] = false
		} else if n, err := parseInt(val); err == nil {
			m[key] = n
		} else if json.Valid([]byte(val)) && (val[0] == '{' || val[0] == '[') {
			var v interface{}
			if err := json.Unmarshal([]byte(val), &v); err != nil {
				return nil, fmt.Errorf("parse %s JSON: %w", key, err)
			}
			m[key] = v
		} else {
			m[key] = val
		}
	}
	return m, nil
}

func jsonFromMap(m map[string]interface{}) (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func normalizeSpaceAliases(toolName string, args map[string]interface{}) {
	if toolName != "ioa_send" && toolName != "ioa_read" {
		return
	}
	if _, ok := args["space_id"]; ok {
		return
	}
	for _, alias := range []string{"space", "space_name"} {
		if value, ok := args[alias]; ok {
			args["space_id"] = value
			return
		}
	}
}
