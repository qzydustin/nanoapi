FROM golang:1.26-bookworm AS builder

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o /out/nanoapi ./main.go

FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home nanoapi \
    && mkdir -p /app/data \
    && chown -R nanoapi:nanoapi /app

COPY --from=builder /out/nanoapi /app/nanoapi
COPY config.example.yaml /app/config.example.yaml

USER nanoapi

EXPOSE 8080

CMD ["/app/nanoapi", "/app/config.yaml"]
