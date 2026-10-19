FROM golang:1.26.8-alpine AS launcher
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /node ./cmd/node

FROM alpine:3.23 AS xray
RUN apk add --no-cache ca-certificates curl unzip
# Fixed stable release and SHA-256 of the official linux/amd64 asset.
# This template intentionally targets linux/amd64 only.
RUN test "$(uname -m)" = x86_64 \
 && curl --fail --show-error --location --retry 3 \
    https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip -o /tmp/xray.zip \
 && echo '23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae  /tmp/xray.zip' | sha256sum -c - \
 && unzip /tmp/xray.zip -d /xray \
 && chmod 0755 /xray/xray

FROM alpine:3.23 AS nezha
RUN apk add --no-cache ca-certificates curl unzip
# Official immutable v2.3.5 release; checksum matches its manifest and GitHub asset digest.
RUN test "$(uname -m)" = x86_64 \
 && curl --fail --show-error --location --retry 3 \
    https://github.com/nezhahq/agent/releases/download/v2.3.5/nezha-agent_linux_amd64.zip -o /tmp/nezha.zip \
 && echo 'cb0ace70db050a8485d6314535922ae7614ad74c89689fdaab76ecfd5a81ba96  /tmp/nezha.zip' | sha256sum -c - \
 && mkdir /nezha \
 && unzip /tmp/nezha.zip -d /nezha \
 && chmod 0755 /nezha/nezha-agent

FROM alpine:3.23
RUN apk add --no-cache ca-certificates \
 && addgroup -g 10001 node \
 && adduser -D -H -u 10001 -G node node
COPY --from=launcher /node /usr/local/bin/node
COPY --from=xray /xray/xray /usr/local/bin/xray
COPY --from=xray /xray/LICENSE /usr/share/licenses/xray/LICENSE
COPY --from=nezha /nezha/nezha-agent /usr/local/bin/nezha-agent
COPY licenses/nezha/LICENSE /usr/share/licenses/nezha-agent/LICENSE
LABEL org.opencontainers.image.source="https://github.com/weikkadd/muudd" \
      org.opencontainers.image.description="VLESS over WebSocket; bundles unmodified Xray v26.3.27 (MPL-2.0), source: https://github.com/XTLS/Xray-core/tree/v26.3.27"
USER 10001:10001
ENV PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
 CMD wget -q -O /dev/null http://127.0.0.1:${PORT}/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/node"]
