package relayx

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"runtime"

	"contrib.go.opencensus.io/exporter/prometheus"
	"github.com/ipfs/go-cid"
	"github.com/ipni/go-indexer-core"
	coremetrics "github.com/ipni/go-indexer-core/metrics"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	promclient "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats/view"
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

func newMetricsHandler(exportProviderMetrics bool) http.Handler {
	if err := view.Register(coremetrics.DefaultViews...); err != nil {
		logger.Warnw("failed to register default metric views", "err", err)
	}

	if err := view.Register(coremetrics.PebbleViews...); err != nil {
		logger.Warnw("failed to register pebble metric views", "err", err)
	}

	if err := view.Register(coremetrics.MeteringViews...); err != nil {
		logger.Warnw("failed to register metering metric views", "err", err)
	}
	if exportProviderMetrics {
		if err := view.Register(coremetrics.MeteringProviderViews...); err != nil {
			logger.Warnw("failed to register per-provider metering metric views", "err", err)
		}
	}

	registry, ok := promclient.DefaultRegisterer.(*promclient.Registry)
	if !ok {
		logger.Warnf("failed to export default prometheus registry; some metrics will be unavailable; unexpected type: %T", promclient.DefaultRegisterer)
	}

	exporter, err := prometheus.NewExporter(prometheus.Options{
		Registry:  registry,
		Namespace: "storetheindex",
	})
	if err != nil {
		logger.Warnw("could not create the prometheus stats exporter", "err", err)
	}

	return exporter
}

func newPprofHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("GET /debug/pprof/gc", func(w http.ResponseWriter, req *http.Request) {
		runtime.GC()
	})

	return mux
}

func (rx *Server) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ipni/v0/relay/find/{multihash}", rx.findGetHandler)
	mux.HandleFunc("PUT /ipni/v0/relay/ingest/{provider_id}", rx.ingestPutHandler)
	mux.HandleFunc("PUT /ipni/v0/relay/ingest/{provider_id}/", rx.ingestPutHandler)
	mux.HandleFunc("PUT /ipni/v0/relay/ingest/{provider_id}/{context_id}", rx.ingestPutHandler)
	mux.HandleFunc("DELETE /ipni/v0/relay/ingest/{provider_id}/{context_id}", rx.ingestDeleteProviderContextHandler)
	mux.HandleFunc("DELETE /ipni/v0/relay/ingest/{provider_id}", rx.ingestDeleteProviderHandler)
	mux.HandleFunc("GET /ipni/v0/relay/metering", rx.meteringStatsHandler)
	mux.HandleFunc("GET /ipni/v0/relay/metering/providers", rx.meteringProvidersHandler)
	mux.HandleFunc("GET /ipni/v0/relay/metering/providers/{provider_id}", rx.meteringProviderHandler)
	mux.HandleFunc("GET /ipni/v0/relay/metering/scan", rx.meteringScanGetHandler)
	mux.HandleFunc("GET /ipni/v0/relay/metering/scan/{provider_id}", rx.meteringProviderScanHandler)
	mux.HandleFunc("POST /ipni/v0/relay/metering/scan", rx.meteringScanPostHandler)
	mux.HandleFunc("DELETE /ipni/v0/relay/metering/scan", rx.meteringScanDeleteHandler)
	mux.Handle("GET /metrics/", newMetricsHandler(rx.exportMeteringProviderMetrics))
	mux.Handle("GET /debug/pprof/", newPprofHandler())
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
	contextID, err := base64.URLEncoding.DecodeString(ctxidPath)
	if err != nil {
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
	logger.Debugw("Handing put request", "provider", providerID, "context", contextID, "count", len(req.Entries))
	if err := rx.delegate.Put(indexer.Value{
		ProviderID:    providerID,
		ContextID:     contextID,
		MetadataBytes: req.Metadata,
	}, req.Entries...); err != nil {
		logger.Errorw("Failed to ingest entries", "provider", providerID, "context", contextID, "count", len(req.Entries), "err", err)
		rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to put entries: %s", err),
		})
		return
	}
	logger.Debugw("successfully ingested entries", "provider", providerID, "context", contextID, "count", len(req.Entries))
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
	contextID, err := base64.URLEncoding.DecodeString(ctxidPath)
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

func (rx *Server) writeMeteringErr(w http.ResponseWriter, err error, what string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, indexer.ErrMeteringNotSupported) {
		rx.writeJson(w, http.StatusNotImplemented, ErrorResponse{Error: err.Error()})
		return true
	}
	rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
		Error: fmt.Sprintf("failed to %s: %s", what, err),
	})
	return true
}

func (rx *Server) meteringStatsHandler(w http.ResponseWriter, r *http.Request) {
	// An empty provider list asks for totals only. The store does not read provider rows.
	report, err := rx.delegate.MeteringAllStats(r.Context(), []peer.ID{})
	if rx.writeMeteringErr(w, err, "get metering stats") {
		return
	}
	if report == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rx.writeJson(w, http.StatusOK, report.CompletedScanStats)
}

func (rx *Server) meteringProvidersHandler(w http.ResponseWriter, r *http.Request) {
	report, err := rx.delegate.MeteringAllStats(r.Context(), nil)
	if rx.writeMeteringErr(w, err, "get provider stats") {
		return
	}
	if report == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rx.writeJson(w, http.StatusOK, report)
}

func (rx *Server) meteringProviderHandler(w http.ResponseWriter, r *http.Request) {
	providerID, err := peer.Decode(r.PathValue("provider_id"))
	if err != nil || providerID.Validate() != nil {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid provider ID",
		})
		return
	}
	report, err := rx.delegate.MeteringAllStats(r.Context(), []peer.ID{providerID})
	if rx.writeMeteringErr(w, err, "get provider stats") {
		return
	}
	if report == nil || len(report.Providers) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rx.writeJson(w, http.StatusOK, report.Providers[0])
}

func (rx *Server) meteringScanGetHandler(w http.ResponseWriter, r *http.Request) {
	status, err := rx.delegate.MeteringScanStatus(r.Context(), nil)
	if rx.writeMeteringErr(w, err, "get scan status") {
		return
	}
	rx.writeMeteringScanStatus(w, status)
}

func (rx *Server) meteringProviderScanHandler(w http.ResponseWriter, r *http.Request) {
	providerID, err := peer.Decode(r.PathValue("provider_id"))
	if err != nil || providerID.Validate() != nil {
		rx.writeJson(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid provider ID",
		})
		return
	}
	status, err := rx.delegate.MeteringScanStatus(r.Context(), []peer.ID{providerID})
	if rx.writeMeteringErr(w, err, "get scan status") {
		return
	}
	rx.writeMeteringScanStatus(w, status)
}

func (rx *Server) meteringScanPostHandler(w http.ResponseWriter, r *http.Request) {
	err := rx.delegate.MeteringTriggerScan(r.Context())
	if err != nil {
		if errors.Is(err, indexer.ErrMeteringNotSupported) {
			rx.writeJson(w, http.StatusNotImplemented, ErrorResponse{Error: err.Error()})
			return
		}
		if errors.Is(err, indexer.ErrScanInProgress) {
			rx.writeJson(w, http.StatusConflict, ErrorResponse{Error: err.Error()})
			return
		}
		rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to trigger scan: %s", err),
		})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (rx *Server) meteringScanDeleteHandler(w http.ResponseWriter, r *http.Request) {
	err := rx.delegate.MeteringCancelScan(r.Context(), r.URL.Query().Get("reason"))
	if err != nil {
		if errors.Is(err, indexer.ErrMeteringNotSupported) {
			rx.writeJson(w, http.StatusNotImplemented, ErrorResponse{Error: err.Error()})
			return
		}
		if errors.Is(err, indexer.ErrScanNotInProgress) {
			rx.writeJson(w, http.StatusConflict, ErrorResponse{Error: err.Error()})
			return
		}
		rx.writeJson(w, http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to cancel scan: %s", err),
		})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// writeMeteringScanStatus encodes a scan that has not been recorded as
// State "none". A running, finished, or failed scan is returned in full,
// including the counters it produced.
func (rx *Server) writeMeteringScanStatus(w http.ResponseWriter, status *indexer.ScanStatus) {
	if status == nil || status.State == "" || status.State == indexer.ScanStateNone {
		rx.writeJson(w, http.StatusOK,
			struct{ State indexer.ScanState }{State: indexer.ScanStateNone})
		return
	}
	rx.writeJson(w, http.StatusOK, status)
}

func (rx *Server) writeJson(w http.ResponseWriter, statusCode int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Errorw("Failed to write JSON", "status", statusCode, "error", err)
	}
}
