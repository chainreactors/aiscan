package node

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	filepb "github.com/chainreactors/cyber/aop/file"
)

const maxFileReadChunk = 256 << 10

// Media callers pass an explicit limit to keep recordings out of a single
// frame. A zero limit retains the file protocol's whole-file read semantics.
func readNodeFile(request *filepb.ReadRequest) (*filepb.Result, error) {
	if request == nil || strings.TrimSpace(request.Path) == "" {
		return nil, fmt.Errorf("file path is required")
	}
	if request.Offset < 0 || request.Limit < 0 || request.Limit > maxFileReadChunk {
		return nil, fmt.Errorf("invalid file read offset or limit (maximum %d)", maxFileReadChunk)
	}
	limit := int64(request.Limit)
	file, err := os.Open(request.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("media path is not a regular file")
	}
	remaining := info.Size() - request.Offset
	if remaining < 0 {
		remaining = 0
	}
	if limit == 0 || remaining < limit {
		limit = remaining
	}
	data := make([]byte, limit)
	n, err := file.ReadAt(data, request.Offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return &filepb.Result{
		Path: request.Path, Filename: info.Name(), Size: info.Size(),
		Data: data[:n], MediaType: mime.TypeByExtension(filepath.Ext(info.Name())),
		Offset: request.Offset, Eof: request.Offset+int64(n) >= info.Size(),
	}, nil
}
