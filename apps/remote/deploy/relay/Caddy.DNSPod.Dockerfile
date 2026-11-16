FROM caddy:2.3.0-builder AS builder

RUN xcaddy build v2.3.0 \
    --with github.com/caddy-dns/dnspod@v0.0.4

FROM caddy:2.3.0-alpine

COPY --from=builder /usr/bin/caddy /usr/bin/caddy
