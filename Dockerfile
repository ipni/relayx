FROM golang:1.25-bookworm AS build

WORKDIR /go/src/relayx

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /go/bin/relayx ./cmd/relayx

FROM gcr.io/distroless/base
COPY --from=build /go/bin/relayx /usr/bin/

ENTRYPOINT ["/usr/bin/relayx"]
