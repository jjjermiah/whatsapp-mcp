# Minimal image for the Go bridge. Build context is this directory:
#   docker build -t whatsapp-bridge whatsapp-bridge
#   docker run -v whatsapp-store:/data whatsapp-bridge
#
# The bridge's REST API binds 127.0.0.1 inside the container and only accepts
# loopback Host headers, so reach it from a sidecar sharing its network
# namespace (e.g. `--network container:<bridge>`), not through `-p`.
#
# modernc.org/sqlite is pure Go, so the binary is static (CGO_ENABLED=0) and
# runs on distroless/static, which still ships CA certificates and tzdata.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /whatsapp-bridge . \
 && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /whatsapp-bridge /whatsapp-bridge
# Session keys, message history, media and the bridge token all live here. The
# nonroot user (65532) must own it for a fresh named volume to be writable.
COPY --from=build --chown=65532:65532 /data /data
ENV WHATSAPP_STORE_DIR=/data
VOLUME /data
ENTRYPOINT ["/whatsapp-bridge"]
