package accessguard

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"cpamp-viewer/server/internal/cpamp"
)

const bindingsPath = "/v0/management/plugins/access-guard/native-key-bindings"

// ErrUnavailable deliberately carries no upstream response or URL details.
var ErrUnavailable = errors.New("Access Guard is unavailable")

// Client exposes only the fixed, read-only binding-list operation. It uses an
// a configured CPA connection or CPAMP's authenticated plugin proxy. The
// underlying transport blocks redirects in both modes.
type Client struct {
	transport *cpamp.Client
}

func New(baseURL, managementKey string, timeout time.Duration, maxBodyBytes int64) *Client {
	return &Client{transport: cpamp.New(baseURL, managementKey, timeout, maxBodyBytes)}
}

func (c *Client) NativeKeyBindings(ctx context.Context) ([]json.RawMessage, error) {
	var envelope struct {
		Bindings *[]json.RawMessage `json:"bindings"`
	}
	if err := c.transport.GetJSON(ctx, bindingsPath, nil, &envelope); err != nil || envelope.Bindings == nil {
		return nil, ErrUnavailable
	}
	return *envelope.Bindings, nil
}
