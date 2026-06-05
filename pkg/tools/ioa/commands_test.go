package ioa

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/command"
	ioamodel "github.com/chainreactors/ioa"
)

const knownSpaceID = "a34763e95c29179802a4451597446c35"

func TestSendResolvesSpaceNameToID(t *testing.T) {
	client := newFakeIOAClient(ioamodel.SpaceInfo{ID: knownSpaceID, Name: "sgcc-v2-06011528"})
	cmd := findIOACommand(t, client, "ioa_send")

	if _, err := cmd.Execute(context.Background(), []string{
		"--space_id", "sgcc-v2-06011528",
		"--content", `{"content":"hello"}`,
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := onlyValue(t, client.sentSpaceIDs, "sent space ids"); got != knownSpaceID {
		t.Fatalf("Send space id = %q, want %q", got, knownSpaceID)
	}
}

func TestReadAcceptsSpaceAlias(t *testing.T) {
	client := newFakeIOAClient(ioamodel.SpaceInfo{ID: knownSpaceID, Name: "sgcc-v2-06011528"})
	cmd := findIOACommand(t, client, "ioa_read")

	if _, err := cmd.Execute(context.Background(), []string{
		"--space", "sgcc-v2-06011528",
		"--all", "true",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := onlyValue(t, client.readSpaceIDs, "read space ids"); got != knownSpaceID {
		t.Fatalf("Read space id = %q, want %q", got, knownSpaceID)
	}
}

func TestUnknownNameListsAvailableSpaces(t *testing.T) {
	client := newFakeIOAClient(ioamodel.SpaceInfo{ID: knownSpaceID, Name: "existing-space"})
	cmd := findIOACommand(t, client, "ioa_send")

	_, err := cmd.Execute(context.Background(), []string{
		"--space_id", "no-such-space",
		"--content", `{"content":"hello"}`,
	})
	if err == nil {
		t.Fatal("expected error for unknown space name")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "not found") {
		t.Fatalf("error should mention 'not found', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "existing-space") {
		t.Fatalf("error should list available spaces, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "ioa_space") {
		t.Fatalf("error should suggest ioa_space, got: %s", errMsg)
	}
	if len(client.createdNames) != 0 {
		t.Fatalf("should NOT auto-create, but created: %v", client.createdNames)
	}
}

func TestUnknownHashIDDoesNotAutoCreate(t *testing.T) {
	client := newFakeIOAClient()
	cmd := findIOACommand(t, client, "ioa_send")
	unknownID := strings.Repeat("a", 32)

	_, err := cmd.Execute(context.Background(), []string{
		"--space_id", unknownID,
		"--content", `{"content":"hello"}`,
	})
	if err == nil {
		t.Fatal("expected error for unknown hash ID")
	}
	if len(client.createdNames) != 0 {
		t.Fatalf("should NOT auto-create, but created: %v", client.createdNames)
	}
}

func findIOACommand(t *testing.T, client *fakeIOAClient, name string) command.PseudoCommand {
	t.Helper()
	for _, cmd := range NewCommands(client, "tester", nil) {
		if cmd.Name() == name {
			return cmd
		}
	}
	t.Fatalf("command %q not found", name)
	return nil
}

func onlyValue(t *testing.T, values []string, label string) string {
	t.Helper()
	if len(values) != 1 {
		t.Fatalf("%s = %v, want one value", label, values)
	}
	return values[0]
}

type fakeIOAClient struct {
	nodeID string

	spacesByID   map[string]ioamodel.SpaceInfo
	spacesByName map[string]ioamodel.SpaceInfo

	resolveCalls []string
	createdNames []string
	sentSpaceIDs []string
	readSpaceIDs []string
}

func newFakeIOAClient(spaces ...ioamodel.SpaceInfo) *fakeIOAClient {
	client := &fakeIOAClient{
		spacesByID:   make(map[string]ioamodel.SpaceInfo),
		spacesByName: make(map[string]ioamodel.SpaceInfo),
	}
	for _, space := range spaces {
		client.addSpace(space)
	}
	return client
}

func (c *fakeIOAClient) NodeID() string {
	return c.nodeID
}

func (c *fakeIOAClient) RegisterNode(_ context.Context, name string, meta map[string]interface{}) (ioamodel.Node, error) {
	if c.nodeID == "" {
		c.nodeID = "node-1"
	}
	return ioamodel.Node{ID: c.nodeID, Name: name, Meta: meta}, nil
}

func (c *fakeIOAClient) Space(_ context.Context, name, _ string, _ ...string) (ioamodel.SpaceInfo, error) {
	if space, ok := c.spacesByName[name]; ok {
		return space, nil
	}
	space := ioamodel.SpaceInfo{ID: "created-" + name, Name: name}
	c.createdNames = append(c.createdNames, name)
	c.addSpace(space)
	return space, nil
}

func (c *fakeIOAClient) ListSpaces(_ context.Context) ([]ioamodel.SpaceInfo, error) {
	var out []ioamodel.SpaceInfo
	for _, s := range c.spacesByID {
		out = append(out, s)
	}
	return out, nil
}

func (c *fakeIOAClient) ResolveSpace(_ context.Context, nameOrID string) (ioamodel.SpaceInfo, error) {
	c.resolveCalls = append(c.resolveCalls, nameOrID)
	if space, ok := c.spacesByID[nameOrID]; ok {
		return space, nil
	}
	if space, ok := c.spacesByName[nameOrID]; ok {
		return space, nil
	}
	return ioamodel.SpaceInfo{}, ioamodel.ProtocolError(http.StatusNotFound, "space %q not found", nameOrID)
}

func (c *fakeIOAClient) Send(_ context.Context, spaceID string, body ioamodel.SendMessage) (ioamodel.Message, error) {
	if body.Content == nil {
		return ioamodel.Message{}, fmt.Errorf("content is required")
	}
	c.sentSpaceIDs = append(c.sentSpaceIDs, spaceID)
	return ioamodel.Message{
		ID:      "msg-1",
		Sender:  c.nodeID,
		Content: body.Content,
	}, nil
}

func (c *fakeIOAClient) Read(_ context.Context, spaceID string, _ ioamodel.ReadOptions) ([]ioamodel.Message, error) {
	c.readSpaceIDs = append(c.readSpaceIDs, spaceID)
	return []ioamodel.Message{{ID: "msg-1", Sender: c.nodeID}}, nil
}

func (c *fakeIOAClient) addSpace(space ioamodel.SpaceInfo) {
	c.spacesByID[space.ID] = space
	c.spacesByName[space.Name] = space
}
