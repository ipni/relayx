package relayx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ipni/go-indexer-core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
)

var _ indexer.Interface = (*Client)(nil)

type Client struct {
	*clientOptions
}

func NewClient(o ...ClientOption) (*Client, error) {
	opts, err := newClientOptions(o...)
	if err != nil {
		return nil, err
	}
	return &Client{clientOptions: opts}, nil
}

func (c *Client) Get(multihash multihash.Multihash) ([]indexer.Value, bool, error) {
	endpoint, err := url.JoinPath(c.serverAddr, "find", multihash.B58String())
	if err != nil {
		return nil, false, err
	}
	resp, err := c.client.Get(endpoint)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var findResp FindGetResponse
		if err := json.NewDecoder(resp.Body).Decode(&findResp); err != nil {
			return nil, false, err
		}
		if len(findResp.Providers) == 0 {
			return nil, false, nil
		}
		return findResp.Providers, true, nil
	case http.StatusNotFound:
		return nil, false, nil
	default:
		var errResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, false, fmt.Errorf("failed to recode unsuccessful response %d:%w", resp.StatusCode, err)
		}
		return nil, false, fmt.Errorf("unsuccessful response %d: %s", resp.StatusCode, errResp.Error)
	}
}

func (c *Client) Put(iv indexer.Value, entries ...multihash.Multihash) error {
	endpoint, err := url.JoinPath(c.serverAddr, "ingest", iv.ProviderID.String(), base64.URLEncoding.EncodeToString(iv.ContextID))
	if err != nil {
		return err
	}
	body, err := json.Marshal(IngestPutRequest{
		Entries:  entries,
		Metadata: iv.MetadataBytes,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	default:
		var errResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return fmt.Errorf("failed to recode unsuccessful response %d:%w", resp.StatusCode, err)
		}
		return fmt.Errorf("unsuccessful response %d: %s", resp.StatusCode, errResp.Error)
	}
}

func (c *Client) Remove(indexer.Value, ...multihash.Multihash) error {
	return errors.New("not supported")
}

func (c *Client) RemoveProvider(ctx context.Context, id peer.ID) error {
	endpoint, err := url.JoinPath(c.serverAddr, "ingest", id.String())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	default:
		var errResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return fmt.Errorf("failed to recode unsuccessful response %d:%w", resp.StatusCode, err)
		}
		return fmt.Errorf("unsuccessful response %d: %s", resp.StatusCode, errResp.Error)
	}
}

func (c *Client) RemoveProviderContext(providerID peer.ID, contextID []byte) error {
	endpoint, err := url.JoinPath(c.serverAddr, "ingest", providerID.String(), base64.URLEncoding.EncodeToString(contextID))
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	default:
		var errResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return fmt.Errorf("failed to recode unsuccessful response %d:%w", resp.StatusCode, err)
		}
		return fmt.Errorf("unsuccessful response %d: %s", resp.StatusCode, errResp.Error)
	}
}

func (c *Client) Size() (int64, error) {
	return 0, nil
}

func (c *Client) Flush() error {
	return nil
}

func (c *Client) Close() error {
	c.client.CloseIdleConnections()
	return nil
}

func (c *Client) Stats() (*indexer.Stats, error) {
	return nil, nil
}
