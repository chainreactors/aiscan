package ioa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/chainreactors/aiscan/pkg/command"
	ioamodel "github.com/chainreactors/ioa"
	ioaclient "github.com/chainreactors/ioa/client"
)

// spaceBinding holds the current space ID shared across all IOA commands.
type spaceBinding struct {
	mu      sync.RWMutex
	spaceID string
}

func (b *spaceBinding) get() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.spaceID
}

func (b *spaceBinding) set(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spaceID = id
}

func NewCommands(client ioaclient.API, nodeName string, meta map[string]any) []command.Command {
	binding := &spaceBinding{}
	return []command.Command{
		&spaceCommand{client: client, binding: binding, nodeName: nodeName, meta: meta},
		&sendCommand{client: client, binding: binding},
		&readCommand{client: client, binding: binding},
	}
}

func ensureNode(ctx context.Context, client ioaclient.API, name string, meta map[string]any) error {
	if client.NodeID() != "" {
		return nil
	}
	if name == "" {
		name = "aiscan-agent"
	}
	if meta == nil {
		meta = map[string]any{}
	}
	_, err := client.RegisterNode(ctx, name, meta)
	return err
}

func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// --- ioa_space ---

type spaceCommand struct {
	client   ioaclient.API
	binding  *spaceBinding
	nodeName string
	meta     map[string]any
}

func (c *spaceCommand) SetDefaultSpace(id string) { c.binding.set(id) }
func (c *spaceCommand) Name() string                { return "ioa_space" }

func (c *spaceCommand) Usage() string {
	return `ioa_space - Manage IOA spaces

Subcommands:
  ioa_space join --name <name> --description <role> [--tags a,b]
  ioa_space list
  ioa_space nodes
  ioa_space topics

join      Join or create a space (sets it as current for ioa_send/ioa_read)
list      List all available spaces on the server
nodes     Show nodes in the current space
topics    Show root messages (conversation starters) in the current space`
}

func (c *spaceCommand) Execute(ctx context.Context, args []string, w io.Writer) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "join", "":
		m, err := argsToMap(args)
		if err != nil {
			return fmt.Errorf("ioa_space: %w\n\n%s", err, c.Usage())
		}
		return c.execJoin(ctx, m, w)
	case "list", "ls":
		return c.execList(ctx, w)
	case "nodes":
		return c.execNodes(ctx, w)
	case "topics":
		return c.execTopics(ctx, w)
	default:
		return fmt.Errorf("ioa_space: unknown subcommand %q\n\n%s", sub, c.Usage())
	}
}

func (c *spaceCommand) execJoin(ctx context.Context, m map[string]interface{}, w io.Writer) error {
	name, _ := m["name"].(string)
	desc, _ := m["description"].(string)
	if name == "" || desc == "" {
		return fmt.Errorf("ioa_space: --name and --description are required\n\n%s", c.Usage())
	}
	var tags []string
	if raw, ok := m["tags"].(string); ok && raw != "" {
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}

	if err := ensureNode(ctx, c.client, c.nodeName, c.meta); err != nil {
		return err
	}
	info, err := c.client.Space(ctx, name, desc, tags...)
	if err != nil {
		return err
	}
	c.binding.set(info.ID)

	allMessages, readErr := c.client.Read(ctx, info.ID, ioamodel.ReadOptions{All: true})
	if readErr != nil {
		return writeJSON(w, info)
	}
	var startMessages []ioamodel.Message
	for _, msg := range allMessages {
		if len(msg.Refs.Messages) == 0 && len(msg.Refs.Nodes) == 0 {
			startMessages = append(startMessages, msg)
		}
	}
	return writeJSON(w, struct {
		ioamodel.SpaceInfo
		StartMessages []ioamodel.Message `json:"start_messages"`
	}{info, startMessages})
}

func (c *spaceCommand) execList(ctx context.Context, w io.Writer) error {
	type lister interface {
		ListSpaces(ctx context.Context) ([]ioamodel.SpaceInfo, error)
	}
	l, ok := c.client.(lister)
	if !ok {
		return fmt.Errorf("ioa_space list: not supported by this client")
	}
	spaces, err := l.ListSpaces(ctx)
	if err != nil {
		return err
	}
	return writeJSON(w, spaces)
}

func (c *spaceCommand) execNodes(ctx context.Context, w io.Writer) error {
	spaceID := c.binding.get()
	if spaceID == "" {
		return fmt.Errorf("no space joined. Use ioa_space join --name <name> --description <role> first")
	}
	type infoGetter interface {
		GetSpaceInfo(ctx context.Context, spaceID string) (ioamodel.SpaceInfo, error)
	}
	g, ok := c.client.(infoGetter)
	if !ok {
		return fmt.Errorf("ioa_space --nodes: not supported by this client")
	}
	info, err := g.GetSpaceInfo(ctx, spaceID)
	if err != nil {
		return err
	}
	return writeJSON(w, info.Nodes)
}

func (c *spaceCommand) execTopics(ctx context.Context, w io.Writer) error {
	spaceID := c.binding.get()
	if spaceID == "" {
		return fmt.Errorf("no space joined. Use ioa_space join --name <name> --description <role> first")
	}
	if err := ensureNode(ctx, c.client, c.nodeName, c.meta); err != nil {
		return err
	}
	messages, err := c.client.Read(ctx, spaceID, ioamodel.ReadOptions{All: true})
	if err != nil {
		return err
	}
	var topics []ioamodel.Message
	for _, msg := range messages {
		if len(msg.Refs.Messages) == 0 && len(msg.Refs.Nodes) == 0 {
			topics = append(topics, msg)
		}
	}
	return writeJSON(w, topics)
}

// --- ioa_send ---

type sendCommand struct {
	client  ioaclient.API
	binding *spaceBinding
}

func (c *sendCommand) Name() string { return "ioa_send" }

func (c *sendCommand) Usage() string {
	return `ioa_send - Send a message to the current IOA space

Subcommands:
  ioa_send --content '{"content": "msg"}'                        Send to space (broadcast)
  ioa_send to --node <node_id> --content '{"content": "msg"}'    Send to a specific node
  ioa_send reply --to <message_id> --content '{"content": "re"}' Reply to a message
  ioa_send checkpoint --kind <kind> --title <title> --content <body> [--target <url>] [--status <status>]

Options:
  --content         Structured message content as JSON object (required, except for checkpoint)
  --node            Target node ID (for "to" subcommand)
  --to              Message ID to reply to (for "reply" subcommand)
  --refs            Raw references JSON: '{"messages": ["id"], "nodes": ["id"]}'
  --kind            Checkpoint kind: verify, sniper, deep
  --title           Short finding title (checkpoint)
  --target          Target host:port or URL (checkpoint)
  --status          Verification status: confirmed, not_confirmed, info, inconclusive (checkpoint)`
}

func (c *sendCommand) Execute(ctx context.Context, args []string, w io.Writer) error {
	spaceID := c.binding.get()
	if spaceID == "" {
		return fmt.Errorf("no space joined. Use ioa_space join first")
	}

	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		sub = args[0]
		args = args[1:]
	}

	m, err := argsToMap(args)
	if err != nil {
		return fmt.Errorf("ioa_send: %w\n\n%s", err, c.Usage())
	}
	content, _ := m["content"].(map[string]interface{})
	if content == nil {
		return fmt.Errorf("ioa_send: --content is required and must be a JSON object\n\n%s", c.Usage())
	}

	body := ioamodel.SendMessage{Content: content}

	switch sub {
	case "checkpoint":
		return c.execCheckpoint(ctx, m, w)
	case "to":
		node, _ := m["node"].(string)
		if node == "" {
			return fmt.Errorf("ioa_send to: --node <node_id> is required")
		}
		body.Refs = &ioamodel.Ref{Nodes: []string{node}}
	case "reply":
		to, _ := m["to"].(string)
		if to == "" {
			return fmt.Errorf("ioa_send reply: --to <message_id> is required")
		}
		body.Refs = &ioamodel.Ref{Messages: []string{to}}
	case "broadcast", "":
		if refs, ok := m["refs"].(map[string]interface{}); ok {
			data, _ := json.Marshal(refs)
			var r ioamodel.Ref
			if json.Unmarshal(data, &r) == nil {
				body.Refs = &r
			}
		}
	default:
		return fmt.Errorf("ioa_send: unknown subcommand %q\n\n%s", sub, c.Usage())
	}

	if err := ensureNode(ctx, c.client, "", nil); err != nil {
		return err
	}
	msg, err := c.client.Send(ctx, spaceID, body)
	if err != nil {
		return err
	}
	return writeJSON(w, msg)
}

func (c *sendCommand) execCheckpoint(ctx context.Context, m map[string]interface{}, w io.Writer) error {
	spaceID := c.binding.get()
	if spaceID == "" {
		return fmt.Errorf("no space joined. Use ioa_space join first")
	}
	kind, _ := m["kind"].(string)
	title, _ := m["title"].(string)
	contentStr, _ := m["content"].(string)
	if kind == "" || title == "" {
		return fmt.Errorf("ioa_send checkpoint: --kind and --title are required\n\n%s", c.Usage())
	}
	checkpointContent := map[string]interface{}{
		"type":    "checkpoint",
		"kind":    kind,
		"title":   title,
		"content": contentStr,
	}
	if target, _ := m["target"].(string); target != "" {
		checkpointContent["target"] = target
	}
	if status, _ := m["status"].(string); status != "" {
		checkpointContent["status"] = status
	}
	if err := ensureNode(ctx, c.client, "", nil); err != nil {
		return err
	}
	msg, err := c.client.Send(ctx, spaceID, ioamodel.SendMessage{
		Content: checkpointContent,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, msg)
}

// --- ioa_read ---

type readCommand struct {
	client  ioaclient.API
	binding *spaceBinding
}

func (c *readCommand) Name() string { return "ioa_read" }

func (c *readCommand) Usage() string {
	return `ioa_read - Read messages from the current IOA space

Subcommands:
  ioa_read                           Read messages addressed to this node
  ioa_read all [--limit 50]          Read all messages in the space
  ioa_read thread --id <message_id>  Read context of a specific message
  ioa_read new [--after <msg_id>]    Read messages after a cursor (pagination)

Options:
  --limit           Maximum number of messages
  --after           Message ID cursor for pagination
  --id              Message ID for thread context`
}

func (c *readCommand) Execute(ctx context.Context, args []string, w io.Writer) error {
	spaceID := c.binding.get()
	if spaceID == "" {
		return fmt.Errorf("no space joined. Use ioa_space join first")
	}

	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		sub = args[0]
		args = args[1:]
	}

	m, err := argsToMap(args)
	if err != nil {
		return fmt.Errorf("ioa_read: %w\n\n%s", err, c.Usage())
	}

	opts := ioamodel.ReadOptions{}
	if v, ok := m["limit"].(int); ok {
		opts.Limit = v
	}
	if v, ok := m["after"].(string); ok {
		opts.After = v
	}

	switch sub {
	case "all":
		opts.All = true
	case "thread":
		id, _ := m["id"].(string)
		if id == "" {
			return fmt.Errorf("ioa_read thread: --id <message_id> is required")
		}
		opts.MessageID = id
	case "new":
		// uses --after from flags above
	case "":
		// default: read messages addressed to this node
	default:
		return fmt.Errorf("ioa_read: unknown subcommand %q\n\n%s", sub, c.Usage())
	}

	if err := ensureNode(ctx, c.client, "", nil); err != nil {
		return err
	}
	messages, err := c.client.Read(ctx, spaceID, opts)
	if err != nil {
		return err
	}
	return writeJSON(w, messages)
}

// --- arg parsing ---

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
		} else if n, err := strconv.Atoi(val); err == nil {
			m[key] = n
		} else if json.Valid([]byte(val)) && len(val) > 0 && (val[0] == '{' || val[0] == '[') {
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
