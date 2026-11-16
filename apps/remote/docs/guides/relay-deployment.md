# Relay Deployment Guide

This guide describes how to deploy the public MobileVC relay and connect a local MobileVC node through it.

The relay is a public transport broker, not the local MobileVC backend. It only accepts relay agent/client websocket connections and forwards encrypted relay envelopes. It does not need `AUTH_TOKEN`, does not run Codex or Claude, and does not store trusted device identities. Device trust, file authorization, runner state, and decrypted MobileVC traffic remain on the local node.

## What Runs Where

There are two separate runtime roles:

| Machine | Run this | Why |
| --- | --- | --- |
| VPS / cloud server | `deploy/relay/docker-compose.yml` | Hosts the public relay endpoint such as `wss://relay.example.com:9443` |
| Local development computer | `mobilevc public --relay wss://relay.example.com:9443` | Starts the local MobileVC backend, connects it to the public relay, and prints the phone pairing QR |
| Phone | MobileVC app | Scans/imports the relay pairing link and connects through the relay |

The `mobilevc` command is the Node.js launcher from the npm package:

```bash
npm install -g @justprove/mobilevc
```

Do not run the Node launcher on the VPS unless that VPS is also the computer that owns the Codex/Claude workspace. The public relay Docker service does not need Node, `AUTH_TOKEN`, Codex, Claude, or project files.

## Architecture

```text
Phone app
  | wss://relay.example.com:9443/relay/client
  | E2EE relay tunnel
Public relay
  | wss://relay.example.com:9443/relay/agent
  | E2EE relay tunnel
Local MobileVC backend on your Mac
  | loopback or LAN
Codex / Claude / workspace files
```

Public relay routes:

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Health check |
| `GET /version` | Relay binary version |
| `GET /relay/agent` | Local node websocket |
| `GET /relay/client` | Phone websocket |

`/ws` and `/download` are selected-route policy names for the encrypted tunnel. They are not plaintext public HTTP handlers on the relay.

## Docker Compose Deployment

Use the provided template:

```bash
cd deploy/relay
cp .env.example .env
```

Set at least:

```dotenv
RELAY_PUBLIC_URL=wss://relay.example.com:9443
RELAY_DOMAIN=relay.example.com
CADDY_HTTPS_PORT=9443
```

Run the relay with the bundled Caddy reverse proxy:

```bash
docker compose up -d --build
docker compose logs -f relay
docker compose logs -f caddy
```

Caddy is built locally with DNS provider plugins. The default image supports Cloudflare and Alibaba Cloud AliDNS on current Caddy; DNSPod uses a separate legacy Caddy Dockerfile because its plugin does not build against current libdns. It uses ACME DNS-01, terminates HTTPS on port `9443` by default, and forwards websocket upgrades to the relay service on the Docker network. The public phone/local-node URL is the scheme, host, and port from `RELAY_PUBLIC_URL`; do not append `/relay/client`, `/relay/agent`, or query parameters. The default public URL is `wss://relay.example.com:9443`.

If you already have Nginx, Caddy, Traefik, or another edge proxy, run only the relay service:

```bash
docker compose up -d --build relay
docker compose logs -f relay
```

The default compose file does not publish relay port `9000` on the host. Add a local override if an existing host-level reverse proxy must reach the relay through `127.0.0.1:9000`.

Example for an existing public mapping on a different HTTPS port:

```dotenv
RELAY_PUBLIC_URL=wss://relay.example.com:9443
```

Then configure the edge proxy for that public host and port to forward to the relay service.

## Relay Environment

Common relay settings:

| Variable | Default | Notes |
| --- | --- | --- |
| `RELAY_ADDR` | `:9000` | Container listen address |
| `RELAY_PUBLIC_URL` | required in compose | Public `ws://` or `wss://` URL used in pairing links |
| `RELAY_STATE_PATH` | `/data/public_relay_state.json` | Runtime session state persisted across relay restarts |
| `RELAY_REQUIRE_E2EE` | `true` | Production should keep this enabled |
| `RELAY_PLAINTEXT_TEST_MODE` | `false` | Local testing only; do not enable for public relay |
| `RELAY_TRUSTED_PROXY_CIDRS` | `172.16.0.0/12` | Reverse proxy private CIDRs allowed to supply forwarded client IPs |
| `RELAY_MAX_AGENT_CONNS` | `1000` | Agent websocket cap |
| `RELAY_MAX_CLIENT_CONNS` | `2000` | Client websocket cap |
| `RELAY_MAX_CONNS_PER_IP` | `20` | Per-IP websocket cap |
| `RELAY_FORWARD_QUEUE_SIZE` | `64` | Bounded forward queue per peer |
| `RELAY_MAX_PAYLOAD_BYTES` | `33554432` | Max decoded relay payload size |
| `CADDY_DOCKERFILE` | `Caddy.DNS.Dockerfile` | Caddy build template; use `Caddy.DNSPod.Dockerfile` for DNSPod |
| `CADDYFILE_PATH` | `./Caddyfile.cloudflare` | DNS-01 provider template: Cloudflare, AliDNS, or DNSPod |
| `CF_API_TOKEN` | empty | Cloudflare scoped token when using `Caddyfile.cloudflare` |
| `ALIYUN_ACCESS_KEY_ID` / `ALIYUN_ACCESS_KEY_SECRET` | empty | AliDNS credentials when using `Caddyfile.alidns` |
| `DNSPOD_TOKEN` | empty | DNSPod `APP_ID,APP_TOKEN` when using `Caddyfile.dnspod` |

`RELAY_HTTP_ALLOWLIST` and `RELAY_WS_ALLOWLIST` are selected-route policy metadata. The defaults allow encrypted `file.download` and `mobilevc.ws` tunnel streams after E2EE:

```dotenv
RELAY_HTTP_ALLOWLIST=GET:/healthz,GET:/version,GET:/download
RELAY_WS_ALLOWLIST=GET:/ws
```

## Connect a Local Node

Recommended launcher command:

```bash
mobilevc public --relay wss://relay.example.com:9443
```

This keeps LAN direct access enabled and also prints a relay pairing QR. To expose only through relay while the local backend listens on loopback:

```bash
mobilevc public --relay wss://relay.example.com:9443 --network-exposure-mode relay-only
```

Source-build equivalent:

```bash
AUTH_TOKEN="replace-with-local-token" go run ./cmd/server \
  --relay-mode=true \
  --relay-url wss://relay.example.com:9443 \
  --relay-pairing-event-path /tmp/mobilevc-relay-pairing.json \
  --network-exposure-mode lan
```

The local backend writes an owner-only pairing event file. The launcher converts it into a `mobilevc://relay/v1?...` URI and terminal QR, then removes the event file. The pairing URI contains a one-time secret and a node fingerprint; do not paste it into public logs or docs.

## Security Checklist

- Use `wss://` for any public relay URL.
- Keep `RELAY_REQUIRE_E2EE=true` and `RELAY_PLAINTEXT_TEST_MODE=false`.
- Persist `/data` so runtime relay session state survives container restarts.
- Set `RELAY_TRUSTED_PROXY_CIDRS` only to the private subnet of your reverse proxy. Do not use `0.0.0.0/0`.
- Keep relay pairing links private and regenerate them when expired or consumed.
- Device trust and revocation are stored on the local node, not on the public relay.
- File downloads remain limited by the local node download roots. E2EE does not grant broader filesystem access.

## Troubleshooting

`pairing_rejected`

The pairing link is expired, already consumed, or does not match the active relay session. Restart the local MobileVC node or run `mobilevc public --relay ...` again to print a new QR.

`device_unknown`

The phone has credentials for an old local node identity, or the local node rotated identity/trust. Re-pair the phone with a fresh relay link.

`e2ee_handshake_failed` or `e2ee_decrypt_failed`

The phone app, local backend, and relay may be built from incompatible code, or the imported link may have an old node fingerprint. Rebuild/restart the changed components and generate a fresh pairing link.

`payload_too_large`

The encrypted relay payload exceeded `RELAY_MAX_PAYLOAD_BYTES`. The default is `32 MiB`. File downloads should use the encrypted chunked download stream instead of sending a whole file in one frame.

Public `ws://` is rejected

Public relay URLs must use `wss://`. Plain `ws://` is accepted only for loopback or private LAN testing.
