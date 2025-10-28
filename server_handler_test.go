package relayx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-indexer-core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ indexer.Interface = (*MockDelegate)(nil)

type MockDelegate struct {
	PutFunc                   func(indexer.Value, ...multihash.Multihash) error
	GetFunc                   func(multihash.Multihash) ([]indexer.Value, bool, error)
	RemoveProviderContextFunc func(peer.ID, []byte) error
	RemoveProviderFunc        func(context.Context, peer.ID) error
}

func (m *MockDelegate) Remove(indexer.Value, ...multihash.Multihash) error {
	panic("unsupported")
}

func (m *MockDelegate) Size() (int64, error) {
	panic("should not have been called")
}

func (m *MockDelegate) Flush() error {
	panic("should not have been called")
}

func (m *MockDelegate) Close() error {
	panic("should not have been called")
}

func (m *MockDelegate) Iter() (indexer.Iterator, error) {
	panic("should not have been called")
}

func (m *MockDelegate) Stats() (*indexer.Stats, error) {
	panic("should not have been called")
}

func (m *MockDelegate) Put(v indexer.Value, mhs ...multihash.Multihash) error {
	if m.PutFunc != nil {
		return m.PutFunc(v, mhs...)
	}
	return nil
}

func (m *MockDelegate) Get(mh multihash.Multihash) ([]indexer.Value, bool, error) {
	if m.GetFunc != nil {
		return m.GetFunc(mh)
	}
	return nil, false, nil
}

func (m *MockDelegate) RemoveProviderContext(pid peer.ID, contextID []byte) error {
	if m.RemoveProviderContextFunc != nil {
		return m.RemoveProviderContextFunc(pid, contextID)
	}
	return nil
}

func (m *MockDelegate) RemoveProvider(ctx context.Context, pid peer.ID) error {
	if m.RemoveProviderFunc != nil {
		return m.RemoveProviderFunc(ctx, pid)
	}
	return nil
}

func TestIngestPutHandler(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	validContextID := []byte("test-context")
	validContextIDEncoded := base64.URLEncoding.EncodeToString(validContextID)

	mh, err := multihash.Sum([]byte("test data"), multihash.SHA2_256, -1)
	require.NoError(t, err)

	tests := []struct {
		name           string
		providerID     string
		contextID      string
		requestBody    any
		mockPutFunc    func(indexer.Value, ...multihash.Multihash) error
		expectedStatus int
		expectedError  string
	}{
		{
			name:       "successful put with context ID",
			providerID: validPeerID.String(),
			contextID:  validContextIDEncoded,
			requestBody: IngestPutRequest{
				Entries:  []multihash.Multihash{mh},
				Metadata: []byte("test metadata"),
			},
			mockPutFunc: func(v indexer.Value, mhs ...multihash.Multihash) error {
				assert.Equal(t, validPeerID, v.ProviderID)
				assert.Equal(t, validContextID, v.ContextID)
				assert.Equal(t, []byte("test metadata"), v.MetadataBytes)
				assert.Len(t, mhs, 1)
				return nil
			},
			expectedStatus: http.StatusAccepted,
		},
		{
			name:       "invalid provider ID",
			providerID: "invalid-peer-id",
			contextID:  validContextIDEncoded,
			requestBody: IngestPutRequest{
				Entries: []multihash.Multihash{mh},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid provider ID",
		},
		{
			name:       "invalid context ID - not base64",
			providerID: validPeerID.String(),
			contextID:  "not-valid-base64!!!",
			requestBody: IngestPutRequest{
				Entries: []multihash.Multihash{mh},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid context ID",
		},
		{
			name:           "invalid request body",
			providerID:     validPeerID.String(),
			contextID:      validContextIDEncoded,
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
		{
			name:       "delegate put fails",
			providerID: validPeerID.String(),
			contextID:  validContextIDEncoded,
			requestBody: IngestPutRequest{
				Entries: []multihash.Multihash{mh},
			},
			mockPutFunc: func(v indexer.Value, mhs ...multihash.Multihash) error {
				return errors.New("delegate error")
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "failed to put entries",
		},
		{
			name:       "empty context",
			providerID: validPeerID.String(),
			requestBody: IngestPutRequest{
				Entries: []multihash.Multihash{mh},
			},
			mockPutFunc: func(v indexer.Value, mhs ...multihash.Multihash) error {
				return nil
			},
			expectedStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDelegate{
				PutFunc: tt.mockPutFunc,
			}
			server := &Server{
				serverOptions: &serverOptions{
					delegate: mock,
				},
				server: nil,
			}

			var body []byte
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			path := fmt.Sprintf("/ipni/v0/relay/ingest/%s/%s", tt.providerID, tt.contextID)
			req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
			req.SetPathValue("provider_id", tt.providerID)
			req.SetPathValue("context_id", tt.contextID)

			w := httptest.NewRecorder()
			server.ingestPutHandler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, tt.expectedError)
			}
		})
	}
}

func TestIngestDeleteProviderContextHandler(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	validContextID := []byte("test-context")
	validContextIDEncoded := base64.URLEncoding.EncodeToString(validContextID)

	tests := []struct {
		name                          string
		providerID                    string
		contextID                     string
		mockRemoveProviderContextFunc func(peer.ID, []byte) error
		expectedStatus                int
		expectedError                 string
	}{
		{
			name:       "successful delete",
			providerID: validPeerID.String(),
			contextID:  validContextIDEncoded,
			mockRemoveProviderContextFunc: func(pid peer.ID, ctx []byte) error {
				assert.Equal(t, validPeerID, pid)
				assert.Equal(t, validContextID, ctx)
				return nil
			},
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "invalid provider ID",
			providerID:     "invalid-peer-id",
			contextID:      validContextIDEncoded,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid provider ID",
		},
		{
			name:           "invalid context ID",
			providerID:     validPeerID.String(),
			contextID:      "not-valid-base64!!!",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid context ID",
		},
		{
			name:       "delegate remove fails",
			providerID: validPeerID.String(),
			contextID:  validContextIDEncoded,
			mockRemoveProviderContextFunc: func(pid peer.ID, ctx []byte) error {
				return errors.New("remove failed")
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "failed to remove provider context ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDelegate{
				RemoveProviderContextFunc: tt.mockRemoveProviderContextFunc,
			}
			server := &Server{serverOptions: &serverOptions{
				delegate: mock,
			}}

			path := fmt.Sprintf("/ipni/v0/relay/ingest/%s/%s", tt.providerID, tt.contextID)
			req := httptest.NewRequest(http.MethodDelete, path, nil)
			req.SetPathValue("provider_id", tt.providerID)
			req.SetPathValue("context_id", tt.contextID)

			w := httptest.NewRecorder()
			server.ingestDeleteProviderContextHandler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, tt.expectedError)
			}
		})
	}
}

func TestIngestDeleteProviderHandler(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	tests := []struct {
		name                   string
		providerID             string
		mockRemoveProviderFunc func(context.Context, peer.ID) error
		expectedStatus         int
		expectedError          string
	}{
		{
			name:       "successful delete",
			providerID: validPeerID.String(),
			mockRemoveProviderFunc: func(ctx context.Context, pid peer.ID) error {
				assert.Equal(t, validPeerID, pid)
				return nil
			},
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "invalid provider ID",
			providerID:     "invalid-peer-id",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid provider ID",
		},
		{
			name:       "delegate remove fails",
			providerID: validPeerID.String(),
			mockRemoveProviderFunc: func(ctx context.Context, pid peer.ID) error {
				return errors.New("remove failed")
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "failed to remove provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDelegate{
				RemoveProviderFunc: tt.mockRemoveProviderFunc,
			}
			server := &Server{serverOptions: &serverOptions{
				delegate: mock,
			}}

			path := fmt.Sprintf("/ipni/v0/relay/ingest/%s", tt.providerID)
			req := httptest.NewRequest(http.MethodDelete, path, nil)
			req.SetPathValue("provider_id", tt.providerID)

			w := httptest.NewRecorder()
			server.ingestDeleteProviderHandler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, tt.expectedError)
			}
		})
	}
}

func TestFindGetHandler(t *testing.T) {
	// Create a valid multihash
	mh, err := multihash.Sum([]byte("test data"), multihash.SHA2_256, -1)
	require.NoError(t, err)
	mhString := mh.B58String()

	// Create a valid CID
	c := cid.NewCidV1(cid.Raw, mh)
	cidString := c.String()

	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	tests := []struct {
		name           string
		multihash      string
		mockGetFunc    func(multihash.Multihash) ([]indexer.Value, bool, error)
		expectedStatus int
		expectedError  string
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful get with multihash",
			multihash: mhString,
			mockGetFunc: func(receivedMh multihash.Multihash) ([]indexer.Value, bool, error) {
				assert.Equal(t, mh, receivedMh)
				return []indexer.Value{
					{
						ProviderID:    validPeerID,
						ContextID:     []byte("context1"),
						MetadataBytes: []byte("metadata1"),
					},
				}, false, nil
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp FindGetResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Providers, 1)
				assert.Equal(t, validPeerID, resp.Providers[0].ProviderID)
			},
		},
		{
			name:      "successful get with CID",
			multihash: cidString,
			mockGetFunc: func(receivedMh multihash.Multihash) ([]indexer.Value, bool, error) {
				assert.Equal(t, mh, receivedMh)
				return []indexer.Value{}, false, nil
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp FindGetResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Empty(t, resp.Providers)
			},
		},
		{
			name:           "invalid multihash and invalid CID",
			multihash:      "invalid-multihash",
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "failed to parse multihash or cid",
		},
		{
			name:      "delegate get fails",
			multihash: mhString,
			mockGetFunc: func(mh multihash.Multihash) ([]indexer.Value, bool, error) {
				return nil, false, errors.New("get failed")
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "failed to get providers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockDelegate{
				GetFunc: tt.mockGetFunc,
			}
			server := &Server{serverOptions: &serverOptions{
				delegate: mock,
			}}

			path := fmt.Sprintf("/ipni/v0/relay/find/%s", tt.multihash)
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.SetPathValue("multihash", tt.multihash)

			w := httptest.NewRecorder()
			server.findGetHandler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, tt.expectedError)
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestServeMux(t *testing.T) {
	server := &Server{serverOptions: &serverOptions{
		delegate: &MockDelegate{},
	}}
	mux := server.ServeMux()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "find route",
			method: http.MethodGet,
			path:   "/ipni/v0/relay/find/test",
		},
		{
			name:   "ingest without context route",
			method: http.MethodPut,
			path:   "/ipni/v0/relay/ingest/test/",
		},
		{
			name:   "ingest with context route",
			method: http.MethodPut,
			path:   "/ipni/v0/relay/ingest/test/context",
		},
		{
			name:   "delete provider context route",
			method: http.MethodDelete,
			path:   "/ipni/v0/relay/ingest/test/context",
		},
		{
			name:   "delete provider route",
			method: http.MethodDelete,
			path:   "/ipni/v0/relay/ingest/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			assert.NotEqual(t, http.StatusNotFound, w.Code, "Route should be registered")
			assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code, "Route should be allowed")
		})
	}
}

func TestWriteJson(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name           string
		statusCode     int
		value          any
		expectedHeader string
	}{
		{
			name:       "write success response",
			statusCode: http.StatusOK,
			value: FindGetResponse{
				Providers: []indexer.Value{},
			},
			expectedHeader: "application/json; charset=utf-8",
		},
		{
			name:       "write error response",
			statusCode: http.StatusBadRequest,
			value: ErrorResponse{
				Error: "test error",
			},
			expectedHeader: "application/json; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			server.writeJson(w, tt.statusCode, tt.value)

			assert.Equal(t, tt.statusCode, w.Code)
			assert.Equal(t, tt.expectedHeader, w.Header().Get("Content-Type"))
			assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))

			var result map[string]any
			err := json.NewDecoder(w.Body).Decode(&result)
			assert.NoError(t, err)
		})
	}
}
