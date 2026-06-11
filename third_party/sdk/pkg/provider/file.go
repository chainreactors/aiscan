package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chainreactors/sdk/pkg/types"
	"gopkg.in/yaml.v3"
)

// FileProvider 从本地 YAML 文件或目录加载指纹和 POC 数据。
type FileProvider struct {
	fingersPath string
	pocsPath    string
}

func NewFileProvider(fingersPath, pocsPath string) *FileProvider {
	return &FileProvider{fingersPath: fingersPath, pocsPath: pocsPath}
}

func (p *FileProvider) Fingers(ctx context.Context) (types.Fingers, []*types.Alias, error) {
	if p.fingersPath == "" {
		return nil, nil, nil
	}
	fingers, err := loadFingersFromPath(p.fingersPath)
	if err != nil {
		return nil, nil, err
	}
	return fingers, nil, nil
}

func (p *FileProvider) POCs(ctx context.Context) ([]*types.Template, error) {
	if p.pocsPath == "" {
		return nil, nil
	}
	return loadTemplatesFromPath(p.pocsPath)
}

func loadFingersFromPath(path string) (types.Fingers, error) {
	yamlFiles, err := collectYAMLFiles(path)
	if err != nil {
		return nil, err
	}

	var loaded types.Fingers
	for _, f := range yamlFiles {
		file, err := os.Open(f)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f, err)
		}
		var raw []*types.Finger
		err = yaml.NewDecoder(file).Decode(&raw)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", f, err)
		}
		loaded = append(loaded, raw...)
	}
	if len(loaded) == 0 {
		return nil, fmt.Errorf("no fingers loaded from %s", path)
	}
	return loaded, nil
}

func loadTemplatesFromPath(path string) ([]*types.Template, error) {
	yamlFiles, err := collectYAMLFiles(path)
	if err != nil {
		return nil, err
	}

	var loaded []*types.Template
	for _, f := range yamlFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		var list []*types.Template
		if err := yaml.Unmarshal(content, &list); err == nil && len(list) > 0 {
			loaded = append(loaded, list...)
			continue
		}
		tpl := &types.Template{}
		if err := yaml.Unmarshal(content, tpl); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		loaded = append(loaded, tpl)
	}
	if len(loaded) == 0 {
		return nil, fmt.Errorf("no templates loaded from %s", path)
	}
	return loaded, nil
}

func collectYAMLFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("access %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = filepath.Walk(path, func(p string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		ext := filepath.Ext(p)
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}
