package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	filepb "github.com/chainreactors/cyber/aop/file"
	types "github.com/chainreactors/cyber/core/types"
	"github.com/gorilla/websocket"
	protobuf "google.golang.org/protobuf/proto"
)

func TestRecordMediaHTTPPlaybackAndDownload(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"chat", "other-chat"} {
		if err := store.CreateSession(t.Context(), &types.SessionRecord{Session: &aop.Session{Id: id, NodeId: "node-record", State: SessionStateOpen}, CreatedAt: nowProto(), UpdatedAt: nowProto()}); err != nil {
			t.Fatal(err)
		}
	}
	video := bytes.Repeat([]byte("video-data"), 80000)
	files := map[string][]byte{"/runner/task/capture.mp4": video, "/runner/task/shot.png": []byte("original-png"), "invalid.mp4": []byte("bad")}
	appendResult := func(id, name string, contents ...*aop.Content) {
		t.Helper()
		_, _, err := store.AppendAOPEvent(t.Context(), "chat", &aop.Event{Id: id, SessionId: "child-session", TurnId: "turn", Payload: &aop.Event_ToolResult{
			ToolResult: &aop.ToolResult{CallId: "call", Name: name, Output: contents},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	appendResult("video", "record", aop.Text(`{"output":"/runner/task/capture.mp4"}`), aop.MediaURI("video", "video/mp4", "capture.mp4", ".cyber/record/capture.mp4"))
	appendResult("shot", "record", aop.Text(`{"action":"screenshot","output":"/runner/task/shot.png"}`), aop.MediaData("image", "image/jpeg", "shot.png", []byte("preview-jpeg")))
	appendResult("html", "record", aop.MediaData("image", "text/html", "bad.html", []byte("html")))
	appendResult("other-tool", "bash", aop.MediaData("image", "image/png", "shot.png", []byte("png")))
	appendResult("invalid", "record", aop.MediaURI("video", "video/mp4", "invalid.mp4", "invalid.mp4"))
	svc := NewService(ServiceConfig{Store: store, AccessKey: "record-token"})
	defer svc.Close(context.Background())
	pool := NewAgentPool(svc.Hub(), nil)
	svc.SetAgentPool(pool)
	server := httptest.NewServer(newHandler(svc, nil))
	defer server.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+NodeWebSocketPath, http.Header{"Authorization": {"Bearer record-token"}})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	writeAgentEnvelope(t, conn, aop.MustWrap("hello", "", &aop.ProtocolMessage{Message: &aop.ProtocolMessage_AgentHello{AgentHello: &aop.AgentHello{NodeId: "node-record", Name: "record"}}}))
	if accepted := unwrapEnvelope(t, readHubEnvelope(t, conn)).(*aop.ProtocolMessage).GetAgentAccepted(); accepted == nil {
		t.Fatal("node was not accepted")
	}
	var mu sync.Mutex
	var reads []*filepb.ReadRequest
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			envelope := new(aop.Envelope)
			if protobuf.Unmarshal(raw, envelope) != nil {
				return
			}
			message, err := aop.Unwrap(envelope)
			file, ok := message.(*filepb.ProtocolMessage)
			if err != nil || !ok || file.GetReadRequest() == nil {
				continue
			}
			request := file.GetReadRequest()
			mu.Lock()
			reads = append(reads, request)
			mu.Unlock()
			data, ok := files[request.Path]
			if !ok {
				_ = conn.WriteMessage(websocket.BinaryMessage, mustMarshalMediaReply(envelope.Id, aop.NewProtocolError("NOT_FOUND", "missing media")))
				continue
			}
			end := min(request.Offset+int64(request.Limit), int64(len(data)))
			result := &filepb.Result{Path: request.Path, Size: int64(len(data)), Offset: request.Offset, Data: data[request.Offset:end], Eof: end == int64(len(data))}
			if request.Path == "invalid.mp4" {
				result.Offset++
			}
			if conn.WriteMessage(websocket.BinaryMessage, mustMarshalMediaReply(envelope.Id, &filepb.ProtocolMessage{Message: &filepb.ProtocolMessage_Result{Result: result}})) != nil {
				return
			}
		}
	}()
	defer func() { conn.Close(); <-done }()
	get := func(method, url, span string, auth bool) (*http.Response, []byte) {
		t.Helper()
		request, _ := http.NewRequest(method, server.URL+url, nil)
		if auth {
			request.Header.Set("Authorization", "Bearer record-token")
		}
		if span != "" {
			request.Header.Set("Range", span)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}
	url := "/api/sessions/chat/media/video/1"
	res, data := get("GET", url, "bytes=300000-300099", true)
	if res.StatusCode != 206 || res.Header.Get("Content-Range") != fmt.Sprintf("bytes 300000-300099/%d", len(video)) || !bytes.Equal(data, video[300000:300100]) {
		t.Fatalf("range playback = %d, %v, %q", res.StatusCode, res.Header, data)
	}
	res, data = get("GET", url+"?download=1", "", true)
	if res.StatusCode != 200 || !bytes.Equal(data, video) || res.Header.Get("Content-Type") != "video/mp4" || !strings.Contains(res.Header.Get("Content-Disposition"), "capture.mp4") {
		t.Fatalf("video download = %d, %v, bytes %d", res.StatusCode, res.Header, len(data))
	}
	res, data = get("HEAD", url, "", true)
	if res.StatusCode != 200 || len(data) != 0 || res.Header.Get("Content-Length") != fmt.Sprint(len(video)) {
		t.Fatalf("HEAD = %d, %v", res.StatusCode, res.Header)
	}
	res, data = get("GET", "/api/sessions/chat/media/shot/1", "", true)
	if res.StatusCode != 200 || string(data) != "preview-jpeg" || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("image preview = %d, %q", res.StatusCode, data)
	}
	res, data = get("GET", "/api/sessions/chat/media/shot/1?download=1", "", true)
	if res.StatusCode != 200 || string(data) != "original-png" || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("original screenshot = %d, %q", res.StatusCode, data)
	}
	for _, test := range []struct {
		url    string
		auth   bool
		status int
	}{
		{url, false, 401}, {"/api/sessions/other-chat/media/video/1", true, 404},
		{"/api/sessions/chat/media/video/0", true, 404}, {"/api/sessions/chat/media/video/99", true, 404},
		{"/api/sessions/chat/media/video/-1", true, 404}, {"/api/sessions/chat/media/video/bad", true, 404},
		{"/api/sessions/chat/media/missing/1", true, 404}, {"/api/sessions/chat/media/html/0", true, 404},
		{"/api/sessions/chat/media/other-tool/0", true, 404}, {"/api/sessions/chat/media/invalid/0", true, 502},
	} {
		res, _ := get("GET", test.url, "", test.auth)
		if res.StatusCode != test.status {
			t.Errorf("%s (auth %v) = %d, want %d", test.url, test.auth, res.StatusCode, test.status)
		}
	}
	mu.Lock()
	for _, read := range reads {
		if read.Limit <= 0 || read.Limit > mediaReadChunk || read.Path == ".cyber/record/capture.mp4" {
			t.Errorf("unbounded or unresolved read: %+v", read)
		}
	}
	mu.Unlock()
	conn.Close()
	<-done
	waitAgents(t, pool, 0)
	res, _ = get("GET", url, "", true)
	if res.StatusCode != 502 {
		t.Fatalf("disconnected node status = %d", res.StatusCode)
	}
}

func mustMarshalMediaReply(replyTo string, message protobuf.Message) []byte {
	raw, err := protobuf.Marshal(aop.Reply(replyTo, message))
	if err != nil {
		panic(err)
	}
	return raw
}
