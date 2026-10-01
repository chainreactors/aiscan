package node

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	filepb "github.com/chainreactors/cyber/aop/file"
	protobuf "google.golang.org/protobuf/proto"
)

func TestAgentMediaReadIsChunked(t *testing.T) {
	data := bytes.Repeat([]byte("record"), maxFileReadChunk)
	path := filepath.Join(t.TempDir(), "record.mp4")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var received protobuf.Message
	read := func(offset int64, limit int32) *filepb.Result {
		t.Helper()
		handleAgentFileMessage(connectionConfig{}, &aop.Envelope{Id: "read"},
			&filepb.ProtocolMessage{Message: &filepb.ProtocolMessage_ReadRequest{ReadRequest: &filepb.ReadRequest{
				Path: path, Offset: offset, Limit: limit,
			}}}, func(replyTo string, message protobuf.Message) {
				if replyTo != "read" {
					t.Errorf("replyTo = %q", replyTo)
				}
				received = message
			})
		message, ok := received.(*filepb.ProtocolMessage)
		if !ok || message.GetResult() == nil {
			t.Fatalf("file response = %v", received)
		}
		return message.GetResult()
	}
	first := read(0, maxFileReadChunk)
	if len(first.Data) != maxFileReadChunk || first.Size != int64(len(data)) || first.Eof || !bytes.Equal(first.Data, data[:maxFileReadChunk]) {
		t.Fatalf("first chunk = offset %d size %d data %d eof %v", first.Offset, first.Size, len(first.Data), first.Eof)
	}
	last := read(int64(len(data)-7), 100)
	if !last.Eof || last.Offset != int64(len(data)-7) || !bytes.Equal(last.Data, data[len(data)-7:]) {
		t.Fatalf("last chunk = %+v", last)
	}
	if end := read(int64(len(data)), 100); !end.Eof || len(end.Data) != 0 {
		t.Fatalf("EOF chunk = %+v", end)
	}
	if whole := read(0, 0); !whole.Eof || !bytes.Equal(whole.Data, data) {
		t.Fatalf("zero-limit read = size %d eof %v", len(whole.Data), whole.Eof)
	}
}

func TestAgentMediaReadRejectsInvalidRequests(t *testing.T) {
	for _, request := range []*filepb.ReadRequest{
		nil, {}, {Path: "file.mp4", Offset: -1}, {Path: "file.mp4", Limit: -1},
		{Path: "file.mp4", Limit: maxFileReadChunk + 1}, {Path: t.TempDir()},
	} {
		if _, err := readNodeFile(request); err == nil {
			t.Fatalf("accepted invalid read: %+v", request)
		}
	}
}
