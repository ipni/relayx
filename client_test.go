package relayx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ipni/go-indexer-core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		options     []ClientOption
		expectError bool
	}{
		{
			name:        "create client with default options",
			options:     []ClientOption{},
			expectError: false,
		},
		{
			name: "create client with custom server address",
			options: []ClientOption{
				WithServerAddr("http://custom-server.com"),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.options...)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
			}
		})
	}
}

func TestClientGet(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	mh, err := multihash.Sum([]byte("test data"), multihash.SHA2_256, -1)
	require.NoError(t, err)

	tests := []struct {
		name           string
		multihash      multihash.Multihash
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectValues   []indexer.Value
		expectFound    bool
		expectError    bool
		errorContains  string
	}{
		{
			name:      "successful get with providers",
			multihash: mh,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "find")
				assert.Contains(t, r.URL.Path, mh.B58String())

				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(FindGetResponse{
					Providers: []indexer.Value{
						{
							ProviderID:    validPeerID,
							ContextID:     []byte("context1"),
							MetadataBytes: []byte("metadata1"),
						},
					},
				})
			},
			expectValues: []indexer.Value{
				{
					ProviderID:    validPeerID,
					ContextID:     []byte("context1"),
					MetadataBytes: []byte("metadata1"),
				},
			},
			expectFound: true,
			expectError: false,
		},
		{
			name:      "get returns empty providers",
			multihash: mh,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(FindGetResponse{
					Providers: []indexer.Value{},
				})
			},
			expectValues: nil,
			expectFound:  false,
			expectError:  false,
		},
		{
			name:      "get returns not found",
			multihash: mh,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectValues: nil,
			expectFound:  false,
			expectError:  false,
		},
		{
			name:      "get returns error response",
			multihash: mh,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(ErrorResponse{
					Error: "internal server error",
				})
			},
			expectValues:  nil,
			expectFound:   false,
			expectError:   true,
			errorContains: "unsuccessful response 500",
		},
		{
			name:      "get returns invalid json on success",
			multihash: mh,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("invalid json"))
			},
			expectValues: nil,
			expectFound:  false,
			expectError:  true,
		},
		{
			name:      "get returns invalid json on error",
			multihash: mh,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("invalid json"))
			},
			expectValues:  nil,
			expectFound:   false,
			expectError:   true,
			errorContains: "failed to recode unsuccessful response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client, err := NewClient(WithServerAddr(server.URL))
			require.NoError(t, err)

			values, found, err := client.Get(tt.multihash)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectFound, found)
			if tt.expectValues != nil {
				require.Len(t, values, len(tt.expectValues))
				for i := range tt.expectValues {
					assert.Equal(t, tt.expectValues[i].ProviderID, values[i].ProviderID)
					assert.Equal(t, tt.expectValues[i].ContextID, values[i].ContextID)
					assert.Equal(t, tt.expectValues[i].MetadataBytes, values[i].MetadataBytes)
				}
			} else {
				assert.Nil(t, values)
			}
		})
	}
}

func TestClientGetNetworkError(t *testing.T) {
	mh, err := multihash.Sum([]byte("test data"), multihash.SHA2_256, -1)
	require.NoError(t, err)
	client, err := NewClient(WithServerAddr("http://invalid-server-that-does-not-exist:99999"))
	require.NoError(t, err)

	_, _, err = client.Get(mh)
	assert.Error(t, err)
}

func TestClientPut(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	mh, err := multihash.Sum([]byte("test data"), multihash.SHA2_256, -1)
	require.NoError(t, err)

	contextID := []byte("test-context")

	tests := []struct {
		name           string
		value          indexer.Value
		entries        []multihash.Multihash
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		errorContains  string
	}{
		{
			name: "successful put",
			value: indexer.Value{
				ProviderID:    validPeerID,
				ContextID:     contextID,
				MetadataBytes: []byte("metadata"),
			},
			entries: []multihash.Multihash{mh},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, r.URL.Path, "/ingest/"+validPeerID.String()+"/"+base64.URLEncoding.EncodeToString(contextID))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var req IngestPutRequest
				err := json.NewDecoder(r.Body).Decode(&req)
				assert.NoError(t, err)
				assert.Len(t, req.Entries, 1)
				assert.Equal(t, mh, req.Entries[0])
				assert.Equal(t, []byte("metadata"), req.Metadata)

				w.WriteHeader(http.StatusAccepted)
			},
			expectError: false,
		},
		{
			name: "successful put with empty context ID",
			value: indexer.Value{
				ProviderID:    validPeerID,
				MetadataBytes: []byte("metadata"),
			},
			entries: []multihash.Multihash{mh},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, r.URL.Path, "/ingest/"+validPeerID.String())
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var req IngestPutRequest
				err := json.NewDecoder(r.Body).Decode(&req)
				assert.NoError(t, err)
				assert.Len(t, req.Entries, 1)
				assert.Equal(t, mh, req.Entries[0])
				assert.Equal(t, []byte("metadata"), req.Metadata)

				w.WriteHeader(http.StatusAccepted)
			},
			expectError: false,
		},
		{
			name: "put returns error response",
			value: indexer.Value{
				ProviderID: validPeerID,
				ContextID:  contextID,
			},
			entries: []multihash.Multihash{mh},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(ErrorResponse{
					Error: "bad request",
				})
			},
			expectError:   true,
			errorContains: "unsuccessful response 400",
		},
		{
			name: "put returns invalid json on error",
			value: indexer.Value{
				ProviderID: validPeerID,
				ContextID:  contextID,
			},
			entries: []multihash.Multihash{mh},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("invalid json"))
			},
			expectError:   true,
			errorContains: "failed to recode unsuccessful response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client, err := NewClient(WithServerAddr(server.URL))
			require.NoError(t, err)

			err = client.Put(tt.value, tt.entries...)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClientPutNetworkError(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	mh, err := multihash.Sum([]byte("test data"), multihash.SHA2_256, -1)
	require.NoError(t, err)

	client, err := NewClient(WithServerAddr("http://invalid-server-that-does-not-exist:99999"))
	require.NoError(t, err)

	err = client.Put(indexer.Value{
		ProviderID: validPeerID,
		ContextID:  []byte("context"),
	}, mh)
	assert.Error(t, err)
}

func TestClientRemove(t *testing.T) {
	client, err := NewClient()
	require.NoError(t, err)

	err = client.Remove(indexer.Value{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestClientRemoveProvider(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	tests := []struct {
		name           string
		providerID     peer.ID
		ctx            context.Context
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		errorContains  string
	}{
		{
			name:       "successful remove provider",
			providerID: validPeerID,
			ctx:        context.Background(),
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Contains(t, r.URL.Path, "ingest")
				assert.Contains(t, r.URL.Path, validPeerID.String())
				// Should not contain context ID
				assert.NotContains(t, r.URL.Path, base64.URLEncoding.EncodeToString([]byte("context")))

				w.WriteHeader(http.StatusAccepted)
			},
			expectError: false,
		},
		{
			name:       "remove provider returns error",
			providerID: validPeerID,
			ctx:        context.Background(),
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(ErrorResponse{
					Error: "internal error",
				})
			},
			expectError:   true,
			errorContains: "unsuccessful response 500",
		},
		{
			name:       "remove provider with cancelled context",
			providerID: validPeerID,
			ctx:        func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(),
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("should not reach server")
			},
			expectError: true,
		},
		{
			name:       "remove provider returns invalid json on error",
			providerID: validPeerID,
			ctx:        context.Background(),
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("invalid json"))
			},
			expectError:   true,
			errorContains: "failed to recode unsuccessful response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client, err := NewClient(WithServerAddr(server.URL))
			require.NoError(t, err)

			err = client.RemoveProvider(tt.ctx, tt.providerID)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClientRemoveProviderContext(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	contextID := []byte("test-context")

	tests := []struct {
		name           string
		providerID     peer.ID
		contextID      []byte
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		errorContains  string
	}{
		{
			name:       "successful remove provider context",
			providerID: validPeerID,
			contextID:  contextID,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Contains(t, r.URL.Path, "ingest")
				assert.Contains(t, r.URL.Path, validPeerID.String())
				assert.Contains(t, r.URL.Path, base64.URLEncoding.EncodeToString(contextID))

				w.WriteHeader(http.StatusAccepted)
			},
			expectError: false,
		},
		{
			name:       "remove provider context returns error",
			providerID: validPeerID,
			contextID:  contextID,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(ErrorResponse{
					Error: "context not found",
				})
			},
			expectError:   true,
			errorContains: "unsuccessful response 404",
		},
		{
			name:       "remove provider context returns invalid json on error",
			providerID: validPeerID,
			contextID:  contextID,
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("invalid json"))
			},
			expectError:   true,
			errorContains: "failed to recode unsuccessful response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client, err := NewClient(WithServerAddr(server.URL))
			require.NoError(t, err)

			err = client.RemoveProviderContext(tt.providerID, tt.contextID)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClientRemoveProviderContextNetworkError(t *testing.T) {
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	client, err := NewClient(WithServerAddr("http://invalid-server-that-does-not-exist:99999"))
	require.NoError(t, err)

	err = client.RemoveProviderContext(validPeerID, []byte("context"))
	assert.Error(t, err)
}

func TestClientSize(t *testing.T) {
	client, err := NewClient()
	require.NoError(t, err)

	size, err := client.Size()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestClientFlush(t *testing.T) {
	client, err := NewClient()
	require.NoError(t, err)

	err = client.Flush()
	assert.NoError(t, err)
}

func TestClientClose(t *testing.T) {
	client, err := NewClient()
	require.NoError(t, err)

	err = client.Close()
	assert.NoError(t, err)
}

func TestClientIter(t *testing.T) {
	client, err := NewClient()
	require.NoError(t, err)

	iter, err := client.Iter()
	assert.NoError(t, err)
	assert.Nil(t, iter)
}

func TestClientStats(t *testing.T) {
	client, err := NewClient()
	require.NoError(t, err)

	stats, err := client.Stats()
	assert.NoError(t, err)
	assert.Nil(t, stats)
}

func TestClientImplementsInterface(t *testing.T) {
	client, err := NewClient()
	require.NoError(t, err)

	// Verify that Client implements indexer.Interface
	var _ indexer.Interface = client
}

func TestClientMultipleOperations(t *testing.T) {
	// Integration-style test that performs multiple operations
	validPeerID, err := peer.Decode("12D3KooWBhL7RVxcJdwQU9aVvA8QJjLvTjq5GfJvjCL3bDQNZJXM")
	require.NoError(t, err)

	mh, err := multihash.Sum([]byte("test data"), multihash.SHA2_256, -1)
	require.NoError(t, err)

	contextID := []byte("test-context")

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		switch {
		case r.Method == http.MethodPut && callCount == 1:
			// First call: Put
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && callCount == 2:
			// Second call: Get
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(FindGetResponse{
				Providers: []indexer.Value{
					{
						ProviderID:    validPeerID,
						ContextID:     contextID,
						MetadataBytes: []byte("metadata"),
					},
				},
			})
		case r.Method == http.MethodDelete && callCount == 3:
			// Third call: RemoveProviderContext
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("Unexpected call: %s %s (call %d)", r.Method, r.URL.Path, callCount)
		}
	}))
	defer server.Close()

	client, err := NewClient(WithServerAddr(server.URL))
	require.NoError(t, err)

	// Perform Put
	err = client.Put(indexer.Value{
		ProviderID:    validPeerID,
		ContextID:     contextID,
		MetadataBytes: []byte("metadata"),
	}, mh)
	require.NoError(t, err)

	// Perform Get
	values, found, err := client.Get(mh)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Len(t, values, 1)

	// Perform RemoveProviderContext
	err = client.RemoveProviderContext(validPeerID, contextID)
	require.NoError(t, err)

	// Close client
	err = client.Close()
	require.NoError(t, err)

	assert.Equal(t, 3, callCount)
}
