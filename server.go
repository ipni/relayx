package relayx

import (
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

func (rx *Server) Stop() error {
	if rx.server == nil {
		return nil
	}
	if err := rx.server.Close(); err != nil {
		return err
	}
	rx.server = nil
	return nil
}
