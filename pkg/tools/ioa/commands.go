package ioa

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/chainreactors/aiscan/pkg/command"
	ioamodel "github.com/chainreactors/ioa"
	ioaclient "github.com/chainreactors/ioa/client"
)

// spaceResolver is implemented by ioa/client.Client.
type spaceResolver interface {
	ResolveSpace(ctx context.Context, nameOrID string) (ioamodel.SpaceInfo, error)
}

// spaceLister is implemented by ioa/client.Client.
type spaceLister interface {
	ListSpaces(ctx context.Context) ([]ioamodel.SpaceInfo, error)
}

func NewCommands(client ioaclient.API, nodeName string, meta map[string]any) []command.PseudoCommand {
	rc := &resolvingClient{
		API:   client,
		cache: make(map[string]string),
	}
	var cmds []command.PseudoCommand
	for _, t := range ioaclient.NewTools(rc, ioaclient.ToolOptions{NodeName: nodeName, NodeMeta: meta}) {
		cmds = append(cmds, &toolAdapter{tool: t, descOverride: toolDescOverrides[t.Name()]})
	}
	return cmds
}

var toolDescOverrides = map[string]string{
	"ioa_send": `Send a structured IOA message to a space. The --space_id value accepts either the space hash ID or the human-readable space name; --space and --space_name are accepted aliases. The --content value MUST be a valid JSON object (not a string). It MUST include a "content" key. Correct: --content '{"content": "your message here", "meta": {"kind": "finding"}}'. WRONG: --content '"just a string"'. Use --refs '{"nodes": ["<id>"]}' to direct to a specific node.`,
	"ioa_read": `Read messages from an IOA space. The --space_id value accepts either the space hash ID or the human-readable space name; --space and --space_name are accepted aliases. Example: ioa_read --space "my-space-name" --all true --limit 50. Use --after "<message_id>" to paginate.`,
}

// resolvingClient wraps ioaclient.API to transparently resolve space names to
// IDs. If a name cannot be resolved it returns an error listing available
// spaces — it never auto-creates/joins a space.
type resolvingClient struct {
	ioaclient.API

	mu    sync.RWMutex
	cache map[string]string // selector → spaceID
}

func (c *resolvingClient) Space(ctx context.Context, name, description string, tags ...string) (ioamodel.SpaceInfo, error) {
	info, err := c.API.Space(ctx, name, description, tags...)
	if err != nil {
		return ioamodel.SpaceInfo{}, err
	}
	c.remember("", info)
	return info, nil
}

func (c *resolvingClient) Send(ctx context.Context, spaceID string, body ioamodel.SendMessage) (ioamodel.Message, error) {
	resolved, err := c.resolve(ctx, spaceID)
	if err != nil {
		return ioamodel.Message{}, err
	}
	return c.API.Send(ctx, resolved, body)
}

func (c *resolvingClient) Read(ctx context.Context, spaceID string, opts ioamodel.ReadOptions) ([]ioamodel.Message, error) {
	resolved, err := c.resolve(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	return c.API.Read(ctx, resolved, opts)
}

// resolve maps a selector (hash ID or human name) to a canonical space ID.
// It never creates a space — unknown names produce an actionable error.
func (c *resolvingClient) resolve(ctx context.Context, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("space_id is required")
	}
	// 1. cache hit
	if id, ok := c.cached(selector); ok {
		return id, nil
	}
	// 2. ask server to resolve (works for both ID and name)
	if resolver, ok := c.API.(spaceResolver); ok {
		info, err := resolver.ResolveSpace(ctx, selector)
		if err == nil {
			c.remember(selector, info)
			return info.ID, nil
		}
	} else if looksLikeSpaceID(selector) {
		// no resolver, but looks like a raw ID — pass through
		return selector, nil
	}
	// 3. not found → list available spaces and return a helpful error
	return "", c.notFoundError(ctx, selector)
}

func (c *resolvingClient) notFoundError(ctx context.Context, selector string) error {
	if lister, ok := c.API.(spaceLister); ok {
		spaces, err := lister.ListSpaces(ctx)
		if err == nil && len(spaces) > 0 {
			var names []string
			for _, s := range spaces {
				names = append(names, fmt.Sprintf("  - %s (id: %s)", s.Name, s.ID))
			}
			return fmt.Errorf("space %q not found. Use ioa_space to join first.\nAvailable spaces:\n%s",
				selector, strings.Join(names, "\n"))
		}
	}
	return fmt.Errorf("space %q not found. Use ioa_space to create or join a space first", selector)
}

func (c *resolvingClient) cached(selector string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.cache[selector]
	return id, ok
}

func (c *resolvingClient) remember(selector string, info ioamodel.SpaceInfo) {
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
