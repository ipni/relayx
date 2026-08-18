FROM golang:1.26-bookworm AS build

WORKDIR /go/src/relayx

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /go/bin/relayx ./cmd/relayx

FROM debian:bookworm-slim AS jemalloc
RUN apt-get update \
    && apt-get install -y --no-install-recommends libjemalloc2 \
    && rm -rf /var/lib/apt/lists/*

FROM gcr.io/distroless/base
COPY --from=build /go/bin/relayx /usr/bin/
COPY --from=jemalloc \
    /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 \
    /usr/lib/x86_64-linux-gnu/libstdc++.so.6 \
    /lib/x86_64-linux-gnu/libgcc_s.so.1 \
    /usr/lib/x86_64-linux-gnu/
ENV LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libjemalloc.so.2
# Optional: return dirty pages to the OS on a few-second delay (CRDB-style).
ENV MALLOC_CONF=background_thread:true,dirty_decay_ms:5000,muzzy_decay_ms:5000

ENTRYPOINT ["/usr/bin/relayx"]
