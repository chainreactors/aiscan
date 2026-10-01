package host

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	cfg "github.com/chainreactors/cyber/pkg/config"
	webext "github.com/chainreactors/cyber/pkg/exts/web"
	"github.com/chainreactors/cyber/pkg/rpc"
	"github.com/chainreactors/cyber/pkg/web"
	managementapi "github.com/chainreactors/cyber/pkg/web/api"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

func TestHubAcceptsIndependentProfilesWithoutLocalProfile(t *testing.T) {
	codec := SharedConfigCodec()
	store := &FileConfigStore{Explicit: filepath.Join(t.TempDir(), "cyber.yaml"), Codec: codec,
		Runtime: &cfg.Option{LLMOptions: cfg.LLMOptions{Provider: "openai", Model: "hub-model", APIKey: "runtime-key"}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Config{Addr: "127.0.0.1:0", Ready: func(addr string) { ready <- addr },
			Management: webext.Config{Database: filepath.Join(t.TempDir(), "web.db"), AccessKey: "hub-key",
				ConfigStore: store, ConfigAPI: managementapi.ConfigOptions{Sections: codec.Sections}, RuntimeLLM: store.RuntimeLLM,
			},
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Hub did not close its node streams")
		}
	})
	var addr string
	select {
	case addr = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("Hub did not start")
	}
	for _, name := range []string{"scan", "audit", "custom"} {
		headers := http.Header{"Authorization": {"Bearer hub-key"}}
		conn, response, err := websocket.DefaultDialer.Dial("ws://"+addr+web.NodeWebSocketPath, headers)
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		envelope, err := aop.Wrap("hello-"+name, "", &aop.ProtocolMessage{Message: &aop.ProtocolMessage_AgentHello{
			AgentHello: &aop.AgentHello{NodeId: name, Name: name, Capabilities: []string{"aop"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := proto.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, raw); err != nil {
			t.Fatal(err)
		}
		accepted, ok := readNodeMessage(t, conn).(*aop.ProtocolMessage)
		if !ok || accepted.GetAgentAccepted().GetNodeId() != name {
			t.Fatalf("node %s was not accepted", name)
		}
		reload, ok := readNodeMessage(t, conn).(*types.ReloadProtocolMessage)
		if !ok {
			t.Fatalf("node %s did not receive shared configuration", name)
		}
		active := cfg.ActiveLLMProvider(reload.GetRequest().GetConfig().GetLlm())
		if active.GetModel() != "hub-model" || active.GetApiKey() != "runtime-key" {
			t.Fatal("effective Hub settings were not distributed")
		}
	}
	client := rpc.NewAgentServiceClient(http.DefaultClient, "http://"+addr)
	deadline := time.Now().Add(5 * time.Second)
	for {
		request := connect.NewRequest(&types.ListAgentsRequest{})
		request.Header().Set("Authorization", "Bearer hub-key")
		response, err := client.ListAgents(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Msg.Agents) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registered nodes = %d", len(response.Msg.Agents))
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := client.ListAgents(t.Context(), connect.NewRequest(&types.ListAgentsRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous request: %v", err)
	}
}

func readNodeMessage(t *testing.T, conn *websocket.Conn) proto.Message {
	t.Helper()
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope := new(aop.Envelope)
	if err := proto.Unmarshal(raw, envelope); err != nil {
		t.Fatal(err)
	}
	message, err := aop.Unwrap(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return message
}
