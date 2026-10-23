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

func (c *Client) MeteringAllStats(ctx context.Context, providerIDs []peer.ID) (*indexer.AllStatsReport, error) {
	switch {
	case providerIDs != nil && len(providerIDs) == 0:
		// Totals only — matches storetheindex GET /metering.
		stats, err := c.getCompletedScanStats(ctx)
		if err != nil || stats == nil {
			return nil, err
		}
		return &indexer.AllStatsReport{CompletedScanStats: *stats}, nil
	case len(providerIDs) == 1:
		// One provider — matches storetheindex GET /metering/providers/{id},
		// with whole-store totals from GET /metering.
		stats, err := c.getCompletedScanStats(ctx)
		if err != nil {
			return nil, err
		}
		if stats == nil {
			return nil, nil
		}
		ps, found, err := c.getProviderStats(ctx, providerIDs[0])
		if err != nil {
			return nil, err
		}
		report := &indexer.AllStatsReport{CompletedScanStats: *stats}
		if found {
			report.Providers = []indexer.ProviderStats{*ps}
		}
		return report, nil
	default:
		// nil (all providers) or more than one ID: fetch the full report.
		report, err := c.getAllProvidersReport(ctx)
		if err != nil || report == nil {
			return nil, err
		}
		if len(providerIDs) > 1 {
			wanted := make(map[peer.ID]struct{}, len(providerIDs))
			for _, id := range providerIDs {
				wanted[id] = struct{}{}
			}
			filtered := report.Providers[:0]
			for _, ps := range report.Providers {
				if _, ok := wanted[ps.ProviderID]; ok {
					filtered = append(filtered, ps)
				}
			}
			report.Providers = filtered
		}
		return report, nil
	}
}

func (c *Client) getCompletedScanStats(ctx context.Context) (*indexer.CompletedScanStats, error) {
	endpoint, err := url.JoinPath(c.serverAddr, "metering")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var stats indexer.CompletedScanStats
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			return nil, err
		}
		return &stats, nil
	case http.StatusNoContent:
		return nil, nil
	case http.StatusNotImplemented:
		return nil, indexer.ErrMeteringNotSupported
	default:
		return nil, c.decodeError(resp)
	}
}

func (c *Client) getProviderStats(ctx context.Context, providerID peer.ID) (*indexer.ProviderStats, bool, error) {
	endpoint, err := url.JoinPath(c.serverAddr, "metering", "providers", providerID.String())
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var ps indexer.ProviderStats
		if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
			return nil, false, err
		}
		return &ps, true, nil
	case http.StatusNoContent:
		return nil, false, nil
	case http.StatusNotImplemented:
		return nil, false, indexer.ErrMeteringNotSupported
	default:
		return nil, false, c.decodeError(resp)
	}
}

func (c *Client) getAllProvidersReport(ctx context.Context) (*indexer.AllStatsReport, error) {
	endpoint, err := url.JoinPath(c.serverAddr, "metering", "providers")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var report indexer.AllStatsReport
		if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
			return nil, err
		}
		return &report, nil
	case http.StatusNoContent:
		return nil, nil
	case http.StatusNotImplemented:
		return nil, indexer.ErrMeteringNotSupported
	default:
		return nil, c.decodeError(resp)
	}
}

func (c *Client) MeteringScanStatus(ctx context.Context, providerIDs []peer.ID) (*indexer.ScanStatus, error) {
	var endpoint string
	var err error
	switch {
	case providerIDs != nil && len(providerIDs) == 0:
		endpoint, err = url.JoinPath(c.serverAddr, "metering", "scan")
	case len(providerIDs) == 1:
		endpoint, err = url.JoinPath(c.serverAddr, "metering", "scan", providerIDs[0].String())
	default:
		endpoint, err = url.JoinPath(c.serverAddr, "metering", "scan")
	}
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var status indexer.ScanStatus
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return nil, err
		}
		// Empty selection asks for totals only in Current.
		if providerIDs != nil && len(providerIDs) == 0 {
			status.Current.Providers = nil
		}
		return &status, nil
	case http.StatusNotImplemented:
		return nil, indexer.ErrMeteringNotSupported
	default:
		return nil, c.decodeError(resp)
	}
}

func (c *Client) MeteringTriggerScan(ctx context.Context) error {
	endpoint, err := url.JoinPath(c.serverAddr, "metering", "scan")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
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
	case http.StatusConflict:
		return indexer.ErrScanInProgress
	case http.StatusNotImplemented:
		return indexer.ErrMeteringNotSupported
	default:
		return c.decodeError(resp)
	}
}

func (c *Client) MeteringCancelScan(ctx context.Context, reason string) error {
	endpoint, err := url.JoinPath(c.serverAddr, "metering", "scan")
	if err != nil {
		return err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if reason != "" {
		q := u.Query()
		q.Set("reason", reason)
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
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
	case http.StatusConflict:
		return indexer.ErrScanNotInProgress
	case http.StatusNotImplemented:
		return indexer.ErrMeteringNotSupported
	default:
		return c.decodeError(resp)
	}
}

func (c *Client) decodeError(resp *http.Response) error {
	var errResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return fmt.Errorf("unsuccessful response %d: %w", resp.StatusCode, err)
	}
	return fmt.Errorf("unsuccessful response %d: %s", resp.StatusCode, errResp.Error)
}

var _ indexer.StatsMeter = (*Client)(nil)
