package relayx

import (
	"errors"
	"net/http"
)

type (
	ClientOption  func(*clientOptions) error
	clientOptions struct {
		serverAddr string
		client     *http.Client
	}
)

func newClientOptions(opts ...ClientOption) (*clientOptions, error) {
	o := &clientOptions{
		serverAddr: "http://localhost:8080",
		client:     http.DefaultClient,
	}
	for _, apply := range opts {
		if err := apply(o); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func WithServerAddr(addr string) ClientOption {
	return func(o *clientOptions) error {
		if addr == "" {
			return errors.New("server address cannot be empty")
		}
		o.serverAddr = addr
		return nil
	}
}

func WithHttpClient(client *http.Client) ClientOption {
	return func(o *clientOptions) error {
		if client == nil {
			return errors.New("http client cannot be nil")
		}
		o.client = client
		return nil
	}
}
