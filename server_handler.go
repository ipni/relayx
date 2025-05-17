package relayx

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-indexer-core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
)

type (
	IngestPutRequest struct {
		Entries  []multihash.Multihash `json:"entries"`
		Metadata []byte                `json:"metadata"`
	}
	FindGetResponse struct {
		Providers []indexer.Value `json:"providers"`
	}
	ErrorResponse struct {
		Error string `json:"error"`
	}
)

func (rx *Server) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ipni/v0/relay/find/{multihash}", rx.findGetHandler)
	mux.HandleFunc("PUT /ipni/v0/relay/ingest/{provider_id}/{context_id}", rx.ingestPutHandler)
	mux.HandleFunc("DELETE /ipni/v0/relay/ingest/{provider_id}/{context_id}", rx.ingestDeleteProviderContextHandler)
	mux.HandleFunc("DELETE /ipni/v0/relay/ingest/{provider_id}", rx.ingestDeleteProviderHandler)
	return mux
}

func (rx *Server) ingestPutHandler(w http.ResponseWriter, r *http.Request) {
	pidPath := r.PathValue("provider_id")
	providerID, err := peer.Decode(pidPath)
	if err != nil || providerID.Validate() != nil {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid provider ID",
		})
		return
	}
	ctxidPath := r.PathValue("context_id")
	contextID, err := base64.StdEncoding.DecodeString(ctxidPath)
	if err != nil || len(contextID) == 0 {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid context ID",
		})
		return
	}

	var req IngestPutRequest
	defer func() { _ = r.Body.Close() }()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return
	}
	if err := rx.delegate.Put(indexer.Value{
		ProviderID:    providerID,
		ContextID:     contextID,
		MetadataBytes: req.Metadata,
	}, req.Entries...); err != nil {
		rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to put entries: %s", err),
		})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (rx *Server) ingestDeleteProviderContextHandler(w http.ResponseWriter, r *http.Request) {
	pidPath := r.PathValue("provider_id")
	providerID, err := peer.Decode(pidPath)
	if err != nil || providerID.Validate() != nil {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid provider ID",
		})
		return
	}
	ctxidPath := r.PathValue("context_id")
	contextID, err := base64.StdEncoding.DecodeString(ctxidPath)
	if err != nil || len(contextID) == 0 {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid context ID",
		})
		return
	}
	if err := rx.delegate.RemoveProviderContext(providerID, contextID); err != nil {
		rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to remove provider context ID: %s", err),
		})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (rx *Server) ingestDeleteProviderHandler(w http.ResponseWriter, r *http.Request) {
	pidPath := r.PathValue("provider_id")
	providerID, err := peer.Decode(pidPath)
	if err != nil || providerID.Validate() != nil {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid provider ID",
		})
		return
	}
	if err := rx.delegate.RemoveProvider(r.Context(), providerID); err != nil {
		rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to remove provider: %s", err),
		})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (rx *Server) findGetHandler(w http.ResponseWriter, r *http.Request) {
	mhPath := r.PathValue("multihash")
	mh, err := multihash.FromB58String(mhPath)
	if err != nil {
		// Be nice and try to decode it as a CID.
		c, cerr := cid.Decode(mhPath)
		if cerr != nil {
			rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
				Error: fmt.Sprintf("failed to parse multihash or cid: %s, %s", err, cerr),
			})
			return
		}
		mh = c.Hash()
	}
	values, _, err := rx.delegate.Get(mh)
	if err != nil {
		rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to get providers: %s", err),
		})
		return
	}
	rx.writeJson(w, http.StatusOK, FindGetResponse{
		Providers: values,
	})
}

func (rx *Server) writeJson(w http.ResponseWriter, statusCode int, v any) {
	w.WriteHeader(statusCode)
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Errorw("Failed to write JSON", "status", statusCode, "error", err)
	}
}
