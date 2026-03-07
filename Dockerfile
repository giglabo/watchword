FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder
ARG TARGETOS TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /watchword ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /watchword /usr/local/bin/watchword
COPY config.yaml /etc/watchword/config.yaml

EXPOSE 8080

ENTRYPOINT ["watchword"]
CMD ["--config", "/etc/watchword/config.yaml"]
