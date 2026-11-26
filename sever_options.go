package relayx

import (
	"errors"

	"github.com/ipni/go-indexer-core"
)

type (
	ServerOption  func(*serverOptions) error
	serverOptions struct {
		listenAddr                    string
		delegate                      indexer.Interface
		exportMeteringProviderMetrics bool
		importListenAddr              string
		importToken                   string
		importPartBytes               uint64
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
	if o.importListenAddr != "" && o.importToken == "" {
		return nil, errors.New("import token is required")
	}
	return o, nil
}

func WithListenAddr(addr string) ServerOption {
	return func(o *serverOptions) error {
		o.listenAddr = addr
		return nil
	}
}

// WithExportMeteringProviderMetrics registers Prometheus views labeled by
// provider. Leave this off unless the provider set is known to be small.
func WithExportMeteringProviderMetrics(enabled bool) ServerOption {
	return func(o *serverOptions) error {
		o.exportMeteringProviderMetrics = enabled
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

// WithImport serves the delegate's kinds (pebble.Kinds) to an indexer that
// imports them, on a separate listener at addr, a "host:port". The delegate
// must be a pebble store. Every request must carry token, which is
// required, and each part that the sender opens holds about partBytes of
// keys and values, the store's default when 0. An empty addr leaves the
// import off, which is the default.
func WithImport(addr, token string, partBytes uint64) ServerOption {
	return func(o *serverOptions) error {
		o.importListenAddr = addr
		o.importToken = token
		o.importPartBytes = partBytes
		return nil
	}
}
