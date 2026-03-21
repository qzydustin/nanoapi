FROM golang:1.26-alpine AS builder

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o /out/nanoapi ./main.go

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/nanoapi /app/nanoapi
COPY config.example.yaml /app/config.example.yaml

EXPOSE 8080

ENTRYPOINT ["/app/nanoapi", "/app/config.yaml"]
