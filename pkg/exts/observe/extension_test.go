package observe_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	filepb "github.com/chainreactors/cyber/aop/file"
	operationpb "github.com/chainreactors/cyber/aop/operation"
	trafficpb "github.com/chainreactors/cyber/aop/traffic"
	coreevents "github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	"github.com/chainreactors/cyber/core/telemetry"
	coretool "github.com/chainreactors/cyber/core/tool"
	fileext "github.com/chainreactors/cyber/pkg/exts/files"
	observe "github.com/chainreactors/cyber/pkg/exts/observe"
	"github.com/chainreactors/cyber/tools/files"
	"github.com/chainreactors/cyber/tools/proxy"
	"github.com/chainreactors/cyber/tools/terminal"
	"google.golang.org/protobuf/proto"
)

func TestCapturedHTTPPublishesNativeSessionEvent(t *testing.T) {
	registry := hooks.New()
	stream := coreevents.New()
	observer, err := observe.New(observe.Options{Kinds: []observe.Kind{observe.HTTP}})
	if err != nil {
		t.Fatal(err)
	}
	set, err := extension.New(extension.Provided[*hooks.Registry](registry), extension.Provided[*coreevents.Stream](stream), extension.Provided[telemetry.Logger](telemetry.NopLogger()), observer)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })
	captured := make(chan *aop.Event, 4)
	sub := stream.Observe(func(event *aop.Event) { captured <- event })
	defer sub.Cancel()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Add("X-Captured", "one")
		w.Header().Add("X-Captured", "two")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(append([]byte("response:"), body...))
	}))
	defer target.Close()
	hub, err := proxy.NewHub(t.TempDir(), "", true, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close(context.Background()) })
	ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: "s1", TurnID: "t1", CallID: "http-call", Emitter: "node-1"})
	ctx, finish := operation.Begin(ctx, "test", "request")
	defer finish(nil)
	proxyURL, _, release := hub.ProxyHub.Egress(ctx)
	defer release()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Post(target.URL+"/api?q=1", "text/plain", strings.NewReader("请求"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "response:请求" {
		t.Fatalf("response = %q, %v", body, err)
	}
	select {
	case event := <-captured:
		// This is the same binary event persisted and delivered by the WebUI.
		data, err := proto.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		decoded := new(aop.Event)
		if err := proto.Unmarshal(data, decoded); err != nil {
			t.Fatal(err)
		}
		flow := new(trafficpb.Flow)
		if err := decoded.GetExtension().UnmarshalTo(flow); err != nil {
			t.Fatal(err)
		}
		if decoded.SessionId != "s1" || decoded.TurnId != "t1" || decoded.Emitter != "node-1" {
			t.Fatalf("invocation = %v", decoded)
		}
		ref := new(operationpb.Ref)
		if ok, err := aop.FindTypedExtension(decoded, ref); !ok || err != nil || ref.CallId != "http-call" {
			t.Fatalf("operation = %v, %v", ref, err)
		}
		if !flow.Complete || flow.Request.Url != target.URL+"/api?q=1" || string(flow.Request.Body) != "请求" || flow.Response.StatusCode != 201 || string(flow.Response.Body) != "response:请求" {
			t.Fatalf("flow = %v", flow)
		}
		var values []string
		for _, header := range flow.Response.Headers {
			if strings.EqualFold(header.Name, "X-Captured") {
				values = append(values, header.Value)
			}
		}
		if len(values) != 2 || values[0] != "one" || values[1] != "two" {
			t.Fatalf("headers = %v", values)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("captured HTTP was not published to the session stream")
	}
	if err := hub.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-captured:
		t.Fatalf("duplicate observation = %v", event)
	default:
	}
}

func TestObservePublishesOneCorrelatedAOPStream(t *testing.T) {
	hookRegistry := hooks.New()
	stream := coreevents.New()
	observer, err := observe.New(observe.Options{Kinds: []observe.Kind{observe.Tools, observe.Files}})
	if err != nil {
		t.Fatal(err)
	}
	registry := coretool.NewToolRegistry()
	fileTools := fileext.New(files.Config{Directory: t.TempDir()})
	set, err := extension.New(
		extension.Provided[*hooks.Registry](hookRegistry),
		extension.Provided[*coreevents.Stream](stream),
		extension.Provided[telemetry.Logger](telemetry.NopLogger()),
		registry,
		observer,
		fileTools,
	)
	if err != nil {
		t.Fatal(err)
	}
	var events []*aop.Event
	sub := stream.Observe(func(event *aop.Event) { events = append(events, event) })
	defer sub.Cancel()
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{
		CallID: "call-1", SessionID: "session-1", TurnID: "turn-1", Emitter: "test",
	})
	if _, err := registry.ExecuteTool(ctx, "write", `{"path":"note","content":"committed"}`); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want started, access, completed", len(events))
	}

	refs := make([]*operationpb.Ref, len(events))
	for i, event := range events {
		ref := new(operationpb.Ref)
		if ok, err := aop.FindTypedExtension(event, ref); err != nil || !ok {
			t.Fatalf("event %d operation: ok=%v err=%v", i, ok, err)
		}
		if ref.GetCallId() != "call-1" || ref.GetOperationId() == "" {
			t.Fatalf("event %d operation = %v", i, ref)
		}
		if event.GetSessionId() != "session-1" || event.GetTurnId() != "turn-1" || event.GetEmitter() != "test" {
			t.Fatalf("event %d invocation metadata = %v", i, event)
		}
		refs[i] = ref
	}
	if refs[0].GetOperationId() != refs[2].GetOperationId() {
		t.Fatalf("tool lifecycle operation changed: %v", refs)
	}
	if refs[1].GetOperationId() == refs[0].GetOperationId() || refs[1].GetParentOperationId() != refs[0].GetOperationId() {
		t.Fatalf("file access is not a child of the tool operation: %v", refs)
	}

	started := new(operationpb.Started)
	access := new(filepb.Access)
	completed := new(operationpb.Completed)
	if err := events[0].GetExtension().UnmarshalTo(started); err != nil || started.GetKind() != "tool" || started.GetName() != "write" {
		t.Fatalf("started = %v, %v", started, err)
	}
	if err := events[1].GetExtension().UnmarshalTo(access); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("committed"))
	if access.GetOp() != filepb.AccessOp_ACCESS_OP_CREATE || access.GetDigest() != hex.EncodeToString(digest[:]) {
		t.Fatalf("access = %v", access)
	}
	if err := events[2].GetExtension().UnmarshalTo(completed); err != nil || completed.GetKind() != "tool" || completed.GetStartedAt() == nil || completed.GetFailure() != nil {
		t.Fatalf("completed = %v, %v", completed, err)
	}
}

func TestObserveRejectsInvalidSelection(t *testing.T) {
	if _, err := observe.New(observe.Options{Kinds: []observe.Kind{"unknown"}}); err == nil {
		t.Fatal("accepted unknown observation kind")
	}
	if _, err := observe.New(observe.Options{Kinds: []observe.Kind{observe.Files, observe.Files}}); err == nil {
		t.Fatal("accepted duplicate observation kind")
	}
	if err := (*observe.Extension)(nil).Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestObservePublishesNativeCommandAndProcessLifecycle(t *testing.T) {
	hookRegistry := hooks.New()
	stream := coreevents.New()
	observer, err := observe.New(observe.Options{Kinds: []observe.Kind{observe.Commands, observe.Processes}})
	if err != nil {
		t.Fatal(err)
	}
	commands := coretool.NewCommandRegistry()
	_, err = commands.Add(coretool.Command{Name: "observe_test", Run: func(context.Context, *coretool.Execution) (any, error) { return "done", nil }})
	if err != nil {
		t.Fatal(err)
	}
	set, err := extension.New(extension.Provided[*hooks.Registry](hookRegistry), extension.Provided[*coreevents.Stream](stream), extension.Provided[telemetry.Logger](telemetry.NopLogger()), commands, observer)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })
	observed := make(chan *aop.Event, 8)
	sub := stream.Observe(func(event *aop.Event) { observed <- event })
	defer sub.Cancel()
	bash := terminal.NewBashTool(t.TempDir(), 30, hookRegistry)
	bash.SetCommandRegistry(commands)
	defer bash.Close()
	ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: "session", TurnID: "turn", CallID: "bash-call", Emitter: "node"})
	execution, err := bash.Start(ctx, "observe_test", terminal.BashExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for range 4 {
		select {
		case event := <-observed:
			if event.SessionId != "session" || event.TurnId != "turn" || event.Emitter != "node" {
				t.Fatalf("lost invocation: %v", event)
			}
			ref := new(operationpb.Ref)
			if ok, err := aop.FindTypedExtension(event, ref); !ok || err != nil || ref.CallId != "bash-call" || ref.OperationId == "" {
				t.Fatalf("operation: %v, %v", ref, err)
			}
			started, completed := new(operationpb.Started), new(operationpb.Completed)
			if event.GetExtension().MessageIs(started) {
				if err := event.GetExtension().UnmarshalTo(started); err != nil {
					t.Fatal(err)
				}
				counts[started.Kind+".started"]++
			} else {
				if err := event.GetExtension().UnmarshalTo(completed); err != nil {
					t.Fatal(err)
				}
				if completed.Failure != nil {
					t.Fatalf("unexpected failure: %v", completed)
				}
				counts[completed.Kind+".completed"]++
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing command/process observation")
		}
	}
	for _, phase := range []string{"command.started", "command.completed", "process.started", "process.completed"} {
		if counts[phase] != 1 {
			t.Fatalf("lifecycle: %v", counts)
		}
	}
}
