package relayx

import (
	"errors"

	"github.com/ipni/go-indexer-core"
)

type (
	ServerOption  func(*serverOptions) error
	serverOptions struct {
		listenAddr string
		delegate   indexer.Interface
	}
)

func newServerOptions(opts ...ServerOption) (*serverOptions, error) {
	o := &serverOptions{
		listenAddr: ":8080",
	}
	for _, apply := range opts {
		if err := apply(o); err != nil {
			return nil, err
		}
	}
	if o.delegate == nil {
		return nil, errors.New("delegate indexer is required")
	}
	return o, nil
}

func WithListenAddr(addr string) ServerOption {
	return func(o *serverOptions) error {
		o.listenAddr = addr
		return nil
	}
}

func WithDelegateIndexer(delegate indexer.Interface) ServerOption {
	return func(o *serverOptions) error {
		if delegate == nil {
			return errors.New("delegate indexer cannot be nil")
		}
		o.delegate = delegate
		return nil
	}
}
