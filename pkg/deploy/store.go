package deploy

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// DeployStore persists cloud credentials and deployment records on the hub.
// The concrete implementation is file-backed (see FileStore).
type DeployStore interface {
	Load(ctx context.Context) (*State, error)
	Save(ctx context.Context, state *State) error
}

// FileStore is a 0600 YAML-backed DeployStore. It holds the cloud
// AccessKeySecret, which never leaves the hub.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a FileStore persisting deploy state at path.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load(ctx context.Context) (*State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := &State{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, state); err != nil {
		return nil, err
	}
	return state, nil
}

func (s *FileStore) Save(ctx context.Context, state *State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Write atomically: a crash mid-write must not truncate the file, which is the
	// hub's only record of every cloud credential, deploy, and the relay private
	// key — a corrupt file would strand live billing instances as unrecoverable.
	// Temp lives in the same dir so the rename stays on one filesystem.
	tmp, err := os.CreateTemp(dir, ".aiscan-deploy-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}
