package cyberhub

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func newClient(baseURL, apiKey string, timeout time.Duration) *client {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/api/v1") {
		baseURL = baseURL + "/api/v1"
	}
	return &client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *client) exportFingers(ctx context.Context, filter *ExportFilter) ([]FingerprintExport, error) {
	params := url.Values{}
	params.Set("with_fingerprint", "true")
	applyFilterParams(params, filter)

	endpoint := fmt.Sprintf("%s/fingerprints/export?%s", c.baseURL, params.Encode())

	var response fingerprintListResponse
	if err := c.doRequest(ctx, "GET", endpoint, nil, &response); err != nil {
		return nil, fmt.Errorf("export fingers failed: %w", err)
	}
	return response.Fingerprints, nil
}

func (c *client) exportPOCs(ctx context.Context, filter *ExportFilter) ([]pocResponse, error) {
	params := url.Values{}
	applyFilterParams(params, filter)
	applyDefaultPOCStatus(params)

	endpoint := fmt.Sprintf("%s/pocs/export?%s", c.baseURL, params.Encode())

	var response pocListResponse
	if err := c.doRequest(ctx, "GET", endpoint, nil, &response); err != nil {
		return nil, fmt.Errorf("export pocs failed: %w", err)
	}
	return response.POCs, nil
}

// applyFilterParams 将筛选条件应用到 URL 参数
func applyFilterParams(params url.Values, filter *ExportFilter) {
	if filter == nil {
		return
	}

	addDedup := func(params url.Values, key string, values []string) {
		existing := make(map[string]struct{})
		for _, v := range params[key] {
			if v != "" {
				existing[v] = struct{}{}
			}
		}
		for _, v := range values {
			if v == "" {
				continue
			}
			if _, exists := existing[v]; exists {
				continue
			}
			params.Add(key, v)
			existing[v] = struct{}{}
		}
	}

	addDedup(params, "names", filter.Names)
	addDedup(params, "tags", filter.Tags)
	addDedup(params, "sources", filter.Sources)
	addDedup(params, "severities", filter.Severities)
	addDedup(params, "engines", filter.Engines)
	addDedup(params, "statuses", filter.Statuses)

	if filter.POCType != "" {
		params.Set("type", filter.POCType)
	}

	if filter.ReviewStatus != "" {
		params.Set("review_status", filter.ReviewStatus)
	}

	if filter.Draft {
		params.Set("with_draft", "true")
	}

	if filter.CreatedAfter != nil {
		params.Set("created_after", filter.CreatedAfter.Format(time.RFC3339))
	}

	if filter.CreatedBefore != nil {
		params.Set("created_before", filter.CreatedBefore.Format(time.RFC3339))
	}

	if filter.UpdatedAfter != nil {
		params.Set("updated_after", filter.UpdatedAfter.Format(time.RFC3339))
	}

	if filter.UpdatedBefore != nil {
		params.Set("updated_before", filter.UpdatedBefore.Format(time.RFC3339))
	}

	if filter.Limit > 0 {
		params.Set("page", "1")
		params.Set("page_size", strconv.Itoa(filter.Limit))
	}
}

// applyDefaultPOCStatus 在没有显式指定状态时默认注入 status=active
func applyDefaultPOCStatus(params url.Values) {
	if len(params["statuses"]) > 0 || params.Get("review_status") != "" {
		return
	}
	params.Set("status", "active")
}

type requestBodyProvider struct {
	data    []byte
	hasBody bool
}

func newRequestBodyProvider(body io.Reader) (*requestBodyProvider, error) {
	if body == nil {
		return &requestBodyProvider{hasBody: false}, nil
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read request body failed: %w", err)
	}
	return &requestBodyProvider{data: data, hasBody: true}, nil
}

func (p *requestBodyProvider) Reader() io.Reader {
	if p == nil || !p.hasBody {
		return nil
	}
	return bytes.NewReader(p.data)
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	return io.ReadAll(reader)
}

func (c *client) doRequest(ctx context.Context, method, endpoint string, body io.Reader, result interface{}) error {
	bodyProvider, err := newRequestBodyProvider(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyProvider.Reader())
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	// 设置请求头
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// 后端要求显式声明 gzip 才会返回压缩数据
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}

	bodyBytes, err := readResponseBody(resp)
	if err != nil {
		return fmt.Errorf("read response failed: %w", err)
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 解析标准响应格式: { code, message, data }
	var apiResp apiResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return fmt.Errorf("parse response failed: %w", err)
	}

	// 检查业务状态码
	if apiResp.Code != 0 {
		return fmt.Errorf("api error: code=%d, message=%s", apiResp.Code, apiResp.Message)
	}

	// 解析 data 字段到目标结构
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return fmt.Errorf("marshal data failed: %w", err)
	}

	if err := json.Unmarshal(dataBytes, result); err != nil {
		return fmt.Errorf("unmarshal data failed: %w", err)
	}

	return nil
}
