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
```

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)