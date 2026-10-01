# `relayx`

> :satellite: Relays indexing over http

[![Go](https://github.com/ipni/relayx/actions/workflows/build.yaml/badge.svg)](https://github.com/ipni/relayx/actions/workflows/build.yaml)

relayx is a relay server designed for the InterPlanetary Network Indexing (IPNI). It acts as an intermediary, delegating
requests to an underlying indexer implementation, such as Pebble, for efficient indexing and querying of data decoupled
from the ingestion pipeline.

See [RelayX OpenAPI specification](openapi.yaml).

## Features

- **HTTP API**: Provides a simple HTTP API for querying and indexing data.
- **Client SDK**: Includes a client SDK for easy integration with existing indexer implementations.
- **Indexing**: Supports indexing of data using the Pebble indexer.
- **Per-provider metering**: Optional background scan that counts multihashes
  and slots per provider. Exposed at `/ipni/v0/relay/metering`,
  `/ipni/v0/relay/metering/providers`, `/ipni/v0/relay/metering/providers/{provider_id}`,
  `/ipni/v0/relay/metering/scan`, and `/ipni/v0/relay/metering/scan/{provider_id}`.
  `POST /ipni/v0/relay/metering/scan` starts a scan; `DELETE` stops the current
  scan and records `user cancelled` on scan status. Pass `?reason=` to add a
  note after that text.
  Configure batch size, interval, and related options on the relayx process
  (CLI flags), not on storetheindex when it uses relayx as the value-store backend.
- **Decoupled Architecture**: Separates the indexing logic from the ingestion pipeline, allowing for more flexible and
  scalable data processing.
- **Extensible**: Easily extendable to support different indexers or data sources.
- **OpenAPI Specification**: Provides an OpenAPI specification for easy integration with other services and tools.

## Getting Started

RelayX is usable as a standalone relay server or as a library in your own application.

### Embedded RelayX

To use RelayX as an embedded library, you can import the `relayx` module and use the `RelayX` class to create a relay
server
instance. You can then configure the server to use a specific indexer implementation, such as Pebble, and start the
server to handle incoming requests.

```bash
go get github.com/ipni/relayx@latest
```

Example:

```go
package main
import (
    "log"
    "net/http"

    "github.com/yourusername/relayx"
)

func main() {
    // Replace with your indexer implementation
    var delegate indexer.Interface 
    // Create a new RelayX server
    server, err := relayx.NewServer(
        relayx.WithListenAddr(":8080"),
        relayx.WithDelegateIndexer(delegate))
    if err != nil {
        panic(err)
    }
    if err := server.Start(); err != nil {
        return err
    }
    ...
    // Interact with the server using the client SDK
    client, err := relayx.NewClient(relayx.WithServerAddr("localhost:8080/ipni/v0/relay"))
    if err != nil {
        panic(err)
    }
    mh, err := multihash.FromB58String("QmQTw94j68Dgakgtfd45bG3TZG6CAfc427UVRH4mugg4q4") 
    ...
    values, err := client.Get(mh)
    ...
}
```

### Standalone RelayX

To run RelayX as a standalone relay server, you can use the `relayx` command-line tool. This allows you to start a
RelayX server with a specific indexer implementation and configuration options. The only supported option is currently
`pebble`.

```bash
go install github.com/ipni/relayx/cmd/relayx@latest

relayx serve --delegate pebble

# With per-provider metering enabled:
relayx serve --delegate pebble --meteringEnabled \
  --meteringBatchSize 1000000 \
  --meteringInterval 24h \
  --meteringTimeFill 0.1
```

Metering flags (pebble delegate only):

| Flag | Default | Meaning |
| --- | --- | --- |
| `--meteringEnabled` | false | Enable the background scanner |
| `--meteringBatchSize` | 1000000 | Keys read per batch |
| `--meteringInterval` | 0 | Wait before the next automatic scan. A manual scan restarts this wait. 0 = manual only |
| `--meteringTimeFill` | 0.1 | Fraction of time spent reading, in (0, 1]. After a batch that took T, the scan sleeps T*(1-fill)/fill. 1 runs batches back to back. |
| `--meteringExportProviderMetrics` | false | Export per-provider gauges to Prometheus. One series per provider; leave off unless the provider set is known to be small. Totals are always exported. |

When storetheindex uses `ValueStoreType: "relayx"`, enable and tune metering with
these relayx flags. storetheindex's admin `/metering` API forwards to relayx;
`Indexer.Metering` in the storetheindex config applies only to a local pebble
value store.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)