package cpamp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL      string
	adminKey     string
	httpClient   *http.Client
	maxBodyBytes int64
}

type UpstreamError struct {
	Status int
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("CPAMP returned HTTP %d", e.Status)
}

func New(baseURL, adminKey string, timeout time.Duration, maxBodyBytes int64) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		adminKey: adminKey,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxBodyBytes: maxBodyBytes,
	}
}

func (c *Client) Health(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/health", nil, nil, nil)
}

func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, target any) error {
	return c.doJSON(ctx, http.MethodGet, path, query, nil, target)
}

func (c *Client) GetJSONWithHeaders(ctx context.Context, path string, query url.Values, target any) (http.Header, error) {
	return c.doJSONWithHeaders(ctx, http.MethodGet, path, query, nil, target)
}

func (c *Client) GetJSONWithBearer(ctx context.Context, path, bearer string, query url.Values, target any) error {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" {
		return errors.New("bearer token is required")
	}
	clone := *c
	clone.adminKey = bearer
	return clone.doJSON(ctx, http.MethodGet, path, query, nil, target)
}

func (c *Client) PostJSON(ctx context.Context, path string, body any, target any) error {
	return c.doJSON(ctx, http.MethodPost, path, nil, body, target)
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body any, target any) error {
	_, err := c.doJSONWithHeaders(ctx, method, path, query, body, target)
	return err
}

func (c *Client) doJSONWithHeaders(ctx context.Context, method, path string, query url.Values, body any, target any) (http.Header, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return nil, errors.New("invalid upstream path")
	}
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, c.maxBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > c.maxBodyBytes {
		return nil, errors.New("CPAMP response exceeded configured size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never retain an upstream management error body. It may contain echoed
		// credentials, provider diagnostics, or other data that must not reach
		// application logs through an error value.
		return nil, &UpstreamError{Status: resp.StatusCode}
	}
	if target == nil || len(data) == 0 {
		return resp.Header.Clone(), nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return nil, fmt.Errorf("decode CPAMP response: %w", err)
	}
	return resp.Header.Clone(), nil
}
