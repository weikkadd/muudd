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

FROM golang:1.26.8-alpine AS nezha
RUN apk add --no-cache ca-certificates curl patch git
WORKDIR /src
# Official v2.3.5 commit; source archive hash verified before the local TCP-only DNS patch.
RUN test "$(uname -m)" = x86_64 \
 && curl --fail --show-error --location --retry 3 \
    https://codeload.github.com/nezhahq/agent/tar.gz/4979248b05875638534934938623ea5b32edfeca -o /tmp/nezha-source.tar.gz \
 && echo '90760f2941eea8bdad3d9073634053f5d5af503d3df6f2fde40a3c41cdc69b39  /tmp/nezha-source.tar.gz' | sha256sum -c - \
 && tar -xzf /tmp/nezha-source.tar.gz --strip-components=1
COPY patches/nezha-dns-tcp.patch /tmp/nezha-dns-tcp.patch
RUN patch --batch --fuzz=0 -p1 < /tmp/nezha-dns-tcp.patch \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOSUMDB=sum.golang.org \
    go build -mod=readonly -trimpath \
    -ldflags="-s -w -X github.com/nezhahq/agent/pkg/monitor.Version=2.3.5-dns-tcp1 -X main.arch=amd64" \
    -o /nezha-agent ./cmd/agent

FROM alpine:3.23
RUN apk add --no-cache ca-certificates \
 && addgroup -g 10001 node \
 && adduser -D -H -u 10001 -G node node
COPY --from=launcher /node /usr/local/bin/node
COPY --from=xray /xray/xray /usr/local/bin/xray
COPY --from=xray /xray/LICENSE /usr/share/licenses/xray/LICENSE
COPY --from=nezha /nezha-agent /usr/local/bin/nezha-agent
COPY licenses/nezha/LICENSE /usr/share/licenses/nezha-agent/LICENSE
LABEL org.opencontainers.image.source="https://github.com/weikkadd/muudd" \
      org.opencontainers.image.description="VLESS over WebSocket; bundles unmodified Xray v26.3.27 (MPL-2.0), source: https://github.com/XTLS/Xray-core/tree/v26.3.27"
USER 10001:10001
ENV PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
 CMD wget -q -O /dev/null http://127.0.0.1:${PORT}/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/node"]
