package relayx

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/ipfs/go-log/v2"
)

var (
	logger = log.Logger("relayx")
)

type Server struct {
	*serverOptions
	server *http.Server
}

func NewServer(o ...ServerOption) (*Server, error) {
	opts, err := newServerOptions(o...)
	if err != nil {
		return nil, err
	}
	return &Server{serverOptions: opts}, nil
}

func (rx *Server) Start() error {
	if rx.server != nil {
		return nil
	}
	listen, err := net.Listen("tcp", rx.listenAddr)
	if err != nil {
		return err
	}
	rx.server = &http.Server{
		Handler: rx.ServeMux(),
	}
	go func() {
		if err := rx.server.Serve(listen); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped erroneously", "error", err)
		}
	}()
	return nil
}

// Stop stops accepting new connections and waits for in-flight requests to
// finish. If ctx expires, remaining connections are closed immediately.
// Stop does not close the delegate indexer; the caller must Flush and Close it
// after Stop returns so in-flight requests are not served against a closed DB.
func (rx *Server) Stop(ctx context.Context) error {
	if rx.server == nil {
		return nil
	}
	err := rx.server.Shutdown(ctx)
	rx.server = nil
	return err
}
