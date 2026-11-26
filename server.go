package relayx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/ipfs/go-log/v2"
	"github.com/ipni/go-indexer-core/importer"
	"github.com/ipni/go-indexer-core/importer/importhttp"
	"github.com/ipni/go-indexer-core/store/pebble"
)

var (
	logger = log.Logger("relayx")
)

type Server struct {
	*serverOptions
	server *http.Server

	// importHandler serves the import, nil when the import is off
	importHandler http.Handler
	// importServer is the running import listener, nil when it is not
	// running
	importServer *importhttp.Server
}

func NewServer(o ...ServerOption) (*Server, error) {
	opts, err := newServerOptions(o...)
	if err != nil {
		return nil, err
	}
	rx := &Server{serverOptions: opts}
	if opts.importListenAddr != "" {
		if rx.importHandler, err = newImportHandler(opts); err != nil {
			return nil, err
		}
	}
	return rx, nil
}

// newImportHandler returns the sender of the delegate's kinds
func newImportHandler(opts *serverOptions) (http.Handler, error) {
	src, ok := opts.delegate.(importer.Source)
	if !ok {
		return nil, fmt.Errorf("the import needs a pebble delegate, not %T", opts.delegate)
	}
	sources := make(map[importer.Kind]importer.Source, len(pebble.Kinds))
	for _, kind := range pebble.Kinds {
		sources[kind] = src
	}
	return importhttp.NewSender(sources, opts.importToken, importhttp.WithPartBytes(opts.importPartBytes))
}

// Start starts the HTTP server, and the import listener when the import is
// on. The import listener serves only the import, behind its token.
func (rx *Server) Start() error {
	if rx.server != nil {
		return nil
	}
	listen, err := net.Listen("tcp", rx.listenAddr)
	if err != nil {
		return err
	}
	// The goroutines serve the servers they start with: Stop clears the
	// fields, possibly before a goroutine runs
	if rx.importHandler != nil {
		importServer, err := importhttp.Listen(rx.importListenAddr, rx.importHandler)
		if err != nil {
			_ = listen.Close()
			return fmt.Errorf("failed to listen for import: %w", err)
		}
		rx.importServer = importServer
		go func() {
			if err := importServer.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Errorw("import server stopped erroneously", "error", err)
			}
		}()
	}
	server := &http.Server{
		Handler: rx.ServeMux(),
	}
	rx.server = server
	go func() {
		if err := server.Serve(listen); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Errorw("server stopped erroneously", "error", err)
		}
	}()
	return nil
}

// ImportAddr returns the address of the running import listener, or nil
// when the import is off or the server is not running
func (rx *Server) ImportAddr() net.Addr {
	if rx.importServer == nil {
		return nil
	}
	return rx.importServer.Addr()
}

// Stop immediately closes the HTTP server without waiting for in-flight
// requests. Prefer StopContext when callers can wait for a graceful shutdown.
// The import listener stops as in StopContext. Stop does not close the
// delegate indexer.
func (rx *Server) Stop() error {
	if rx.server == nil {
		return nil
	}
	err := errors.Join(rx.server.Close(), rx.stopImport())
	if err != nil {
		return err
	}
	rx.server = nil
	return nil
}

// StopContext stops accepting new connections and waits for in-flight requests
// to finish. If ctx expires, remaining connections are closed immediately.
// The import listener's requests, which stream for as long as a part takes,
// are cancelled at once, and StopContext returns only once they have
// returned (see importhttp.Server.Close).
//
// StopContext does not close the delegate indexer; the caller must Flush and
// Close it after StopContext returns so in-flight requests are not served
// against a closed DB.
func (rx *Server) StopContext(ctx context.Context) error {
	if rx.server == nil {
		return nil
	}
	err := errors.Join(rx.stopImport(), rx.server.Shutdown(ctx))
	rx.server = nil
	return err
}

// stopImport closes the import listener, when it runs
func (rx *Server) stopImport() error {
	if rx.importServer == nil {
		return nil
	}
	err := rx.importServer.Close()
	rx.importServer = nil
	return err
}
