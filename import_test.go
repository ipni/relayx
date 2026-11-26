package relayx

import (
	"context"
	"crypto/rand"
	"iter"
	"testing"
	"time"

	ppebble "github.com/cockroachdb/pebble/v2"
	"github.com/ipni/go-indexer-core"
	"github.com/ipni/go-indexer-core/importer"
	"github.com/ipni/go-indexer-core/importer/importhttp"
	"github.com/ipni/go-indexer-core/store/pebble"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

const testImportToken = "test-token"

// testPebble opens a pebble store in a new directory, with n multihashes of
// one provider and context. The test closes it.
func testPebble(t *testing.T, n int) indexer.Interface {
	t.Helper()
	opts := &ppebble.Options{}
	opts.EnsureDefaults()
	store, err := pebble.New(t.TempDir(), opts)
	require.NoError(t, err)
	pid, err := peer.Decode("12D3KooWD3eckifWpRn9wQpMG9R9hX3sD158z7EqHWmweQAJU5SA")
	require.NoError(t, err)
	mhs := make([]multihash.Multihash, n)
	for i := range mhs {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		mhs[i], err = multihash.Sum(b, multihash.SHA2_256, -1)
		require.NoError(t, err)
	}
	require.NoError(t, store.Put(indexer.Value{ProviderID: pid, ContextID: []byte("ctx"), MetadataBytes: []byte("md")}, mhs...))
	require.NoError(t, store.Flush())
	return store
}

// testImportServer starts a server of delegate with the import on
func testImportServer(t *testing.T, delegate indexer.Interface) (*Server, importer.Source) {
	t.Helper()
	server, err := NewServer(
		WithListenAddr("127.0.0.1:0"),
		WithDelegateIndexer(delegate),
		WithImport("127.0.0.1:0", testImportToken, 0))
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Stop() })
	require.NotNil(t, server.ImportAddr())
	c, err := importhttp.NewClient("http://"+server.ImportAddr().String(), testImportToken)
	require.NoError(t, err)
	return server, c
}

func TestImportServesPebble(t *testing.T) {
	delegate := testPebble(t, 100)
	t.Cleanup(func() { require.NoError(t, delegate.Close()) })
	_, c := testImportServer(t, delegate)

	want := map[importer.Kind]int{importer.KindRecords: 1, importer.KindContextMap: 1, importer.KindMultihashes: 100}
	for _, kind := range pebble.Kinds {
		_, entries, err := c.OpenStream(t.Context(), kind, nil, nil, 0)
		require.NoError(t, err)
		var n int
		for _, err := range entries {
			require.NoError(t, err)
			n++
		}
		require.Equal(t, want[kind], n, kind)
	}

	// The delegate's other kinds are not served
	_, _, err := c.OpenStream(t.Context(), importer.KindSync, nil, nil, 0)
	require.ErrorContains(t, err, "404")
}

// TestImportStopsBeforeDelegate checks that StopContext stops the import
// listener with a part in flight, so the delegate closes cleanly after it.
// importhttp's tests cover how a part in flight stops.
func TestImportStopsBeforeDelegate(t *testing.T) {
	delegate := testPebble(t, 20000)
	server, c := testImportServer(t, delegate)

	_, entries, err := c.OpenStream(t.Context(), importer.KindMultihashes, nil, []byte{0xff}, 0)
	require.NoError(t, err)
	next, stop := iter.Pull2(entries)
	defer stop()
	_, err, ok := next()
	require.True(t, ok)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, server.StopContext(ctx))
	require.Nil(t, server.ImportAddr())
	require.NoError(t, delegate.Close())
}

func TestImportOptions(t *testing.T) {
	_, err := NewServer(WithDelegateIndexer(&MockDelegate{}), WithImport("127.0.0.1:0", "", 0))
	require.ErrorContains(t, err, "token")
	_, err = NewServer(WithDelegateIndexer(&MockDelegate{}), WithImport("127.0.0.1:0", testImportToken, 0))
	require.ErrorContains(t, err, "pebble")

	// The import is off by default
	server, err := NewServer(WithListenAddr("127.0.0.1:0"), WithDelegateIndexer(&MockDelegate{}))
	require.NoError(t, err)
	require.NoError(t, server.Start())
	require.Nil(t, server.ImportAddr())
	require.NoError(t, server.Stop())
}
