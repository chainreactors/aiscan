package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chainreactors/sdk/pkg/types"
	"gopkg.in/yaml.v3"
)

// URLProvider 从远程 URL 下载 YAML 并加载指纹和 POC 数据。
type URLProvider struct {
	fingersURL string
	pocsURL    string
	client     *http.Client
}

func NewURLProvider(fingersURL, pocsURL string) *URLProvider {
	return &URLProvider{
		fingersURL: fingersURL,
		pocsURL:    pocsURL,
		// per-instance 客户端，不共享 http.DefaultClient 全局。
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *URLProvider) WithClient(c *http.Client) *URLProvider {
	p.client = c
	return p
}

func (p *URLProvider) Fingers(ctx context.Context) (types.Fingers, []*types.Alias, error) {
	if p.fingersURL == "" {
		return nil, nil, nil
	}
	data, err := p.fetch(ctx, p.fingersURL)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch fingers: %w", err)
	}
	var fingers types.Fingers
	if err := yaml.Unmarshal(data, &fingers); err != nil {
		return nil, nil, fmt.Errorf("parse fingers: %w", err)
	}
	return fingers, nil, nil
}

func (p *URLProvider) POCs(ctx context.Context) ([]*types.Template, error) {
	if p.pocsURL == "" {
		return nil, nil
	}
	data, err := p.fetch(ctx, p.pocsURL)
	if err != nil {
		return nil, fmt.Errorf("fetch pocs: %w", err)
	}
	var tpls []*types.Template
	if err := yaml.Unmarshal(data, &tpls); err != nil {
		return nil, fmt.Errorf("parse pocs: %w", err)
	}
	return tpls, nil
}

func (p *URLProvider) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
