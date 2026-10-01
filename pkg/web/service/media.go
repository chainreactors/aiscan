package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	filepb "github.com/chainreactors/cyber/aop/file"
)

const mediaReadChunk = 256 << 10

// SessionMediaHandler exposes only media referenced by a persisted record
// result. The browser supplies event identity, never a filesystem path or node.
func (s *Service) SessionMediaHandler() http.Handler {
	if s == nil || s.store == nil {
		return nil
	}
	return http.HandlerFunc(s.serveSessionMedia)
}

func (s *Service) serveSessionMedia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		http.NotFound(w, r)
		return
	}
	var stored aopEventModel
	err = s.store.orm.NewSelect().Model(&stored).Column("event_proto").
		Where("session_id = ? AND event_id = ?", r.PathValue("sessionID"), r.PathValue("eventID")).Scan(r.Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "could not load media result", http.StatusInternalServerError)
		}
		return
	}
	event, err := eventFromProto(stored.EventProto)
	if err != nil {
		http.Error(w, "could not decode media result", http.StatusInternalServerError)
		return
	}
	result := event.GetToolResult()
	if result == nil || result.Name != "record" || result.IsError || index >= len(result.Output) {
		http.NotFound(w, r)
		return
	}
	media := result.Output[index].GetMedia()
	if media == nil || media.Resource == nil {
		http.NotFound(w, r)
		return
	}
	resource := media.Resource
	mediaType := resource.MediaType
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp", "video/mp4":
	default:
		http.NotFound(w, r)
		return
	}
	filename := path.Base(strings.ReplaceAll(resource.Filename, "\\", "/"))
	if filename == "." || filename == "" {
		filename = "capture"
	}
	filePath := resource.GetUri()
	// record's metadata preserves the absolute output path even when the media
	// URI is task-relative. This also lets image downloads retain the full PNG
	// while the conversation displays the bounded inline preview.
	output := recordOutputPath(result)
	if output != "" && (resource.Filename == "" || path.Base(strings.ReplaceAll(output, "\\", "/")) == filename) {
		if filePath != "" || r.URL.Query().Get("download") == "1" {
			filePath = output
			if media.Kind == "image" {
				mediaType = "image/png"
			}
		}
	}
	var content io.ReadSeeker
	if filePath == "" {
		if _, ok := resource.Source.(*aop.Resource_Data); !ok {
			http.NotFound(w, r)
			return
		}
		content = bytes.NewReader(resource.GetData())
	} else {
		if strings.Contains(filePath, "://") || strings.ContainsRune(filePath, '\x00') {
			http.NotFound(w, r)
			return
		}
		session, err := s.store.GetSession(r.Context(), r.PathValue("sessionID"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		remote := &remoteMediaFile{ctx: r.Context(), service: s, nodeID: session.GetSession().GetNodeId(), path: filePath}
		if err := remote.load(0); err != nil {
			http.Error(w, "media unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		content = remote
	}
	w.Header().Set("Content-Type", mediaType)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	}
	http.ServeContent(w, r, filename, time.Time{}, content)
}

func recordOutputPath(result *aop.ToolResult) string {
	for _, part := range result.Output {
		if part.GetText() == nil {
			continue
		}
		var meta struct {
			Output string `json:"output"`
		}
		if json.Unmarshal([]byte(part.GetText().Text), &meta) == nil && meta.Output != "" {
			return meta.Output
		}
	}
	return ""
}

func (s *Service) readMediaChunk(ctx context.Context, nodeID, filePath string, offset int64) (*filepb.Result, error) {
	if s.agents == nil || nodeID == "" {
		return nil, fmt.Errorf("no assigned node available")
	}
	ctx, cancel := context.WithTimeout(ctx, agentControlTimeout)
	defer cancel()
	id := generateID()
	replies, err := s.agents.dispatchMessage(nodeID, id, &filepb.ProtocolMessage{Message: &filepb.ProtocolMessage_ReadRequest{
		ReadRequest: &filepb.ReadRequest{Path: filePath, Offset: offset, Limit: mediaReadChunk},
	}})
	if err != nil {
		return nil, err
	}
	defer s.agents.CancelTask(nodeID, id, "")
	select {
	case reply, ok := <-replies:
		if !ok {
			return nil, fmt.Errorf("node disconnected during media read")
		}
		if failure := taskError(reply); failure != nil {
			return nil, fmt.Errorf("%s", failure.Message)
		}
		result, ok := reply.(*filepb.Result)
		if !ok || result.Offset != offset || result.Size < 0 || len(result.Data) > mediaReadChunk ||
			offset > result.Size || int64(len(result.Data)) > result.Size-offset || (len(result.Data) == 0 && offset < result.Size) {
			return nil, fmt.Errorf("node returned an invalid media chunk")
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ServeContent supplies HTTP range/HEAD handling and video seeking. One cached
// AOP chunk keeps memory bounded independently of the recording's total size.
type remoteMediaFile struct {
	ctx                       context.Context
	service                   *Service
	nodeID, path              string
	size, offset, chunkOffset int64
	chunk                     []byte
	initialized               bool
}

func (f *remoteMediaFile) load(offset int64) error {
	result, err := f.service.readMediaChunk(f.ctx, f.nodeID, f.path, offset)
	if err != nil {
		return err
	}
	if f.initialized && f.size != result.Size {
		return fmt.Errorf("media changed during read")
	}
	f.size, f.chunkOffset, f.chunk, f.initialized = result.Size, offset, result.Data, true
	return nil
}

func (f *remoteMediaFile) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if f.offset >= f.size {
		return 0, io.EOF
	}
	if f.offset < f.chunkOffset || f.offset >= f.chunkOffset+int64(len(f.chunk)) {
		if err := f.load(f.offset); err != nil {
			return 0, err
		}
	}
	n := copy(p, f.chunk[f.offset-f.chunkOffset:])
	f.offset += int64(n)
	return n, nil
}

func (f *remoteMediaFile) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = f.offset + offset
	case io.SeekEnd:
		next = f.size + offset
	default:
		return 0, fmt.Errorf("invalid media seek")
	}
	if next < 0 {
		return 0, fmt.Errorf("negative media offset")
	}
	f.offset = next
	return next, nil
}
