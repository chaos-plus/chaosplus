# NATS deployment boundary

`apps/server-ai/cmd/nats` is a local-development fallback only. It refuses to
start when `CONTROL_ENV=production`. When Docker is available in development,
use the official NATS image through `start-dev.sh`. Production must use
`compose.production.yaml` (or an equivalent deployment of the official image).

## Local development

```sh
./deploy/nats/start-dev.sh
```

The script uses Docker Compose when the Docker daemon is available. Only when
Docker is unavailable does it run `go run ./cmd/nats`. JetStream data in the
Docker path is kept in the `nats-dev-data` volume.

## Production

Create a TLS directory containing `ca.crt`, `server.crt`, and `server.key`.
The server certificate must contain the DNS name used by `CONTROL_NATS_URL`.
Then start the pinned official image:

```sh
export NATS_AUTH_TOKEN='replace-with-a-random-secret'
export NATS_TLS_DIR=/absolute/path/to/nats-tls
docker compose -f deploy/nats/compose.production.yaml pull
docker compose -f deploy/nats/compose.production.yaml up -d --wait
```

Configure server-ai with:

```text
CONTROL_ENV=production
CONTROL_NATS_DEPLOYMENT=official-image
CONTROL_NATS_URL=tls://nats.example.internal:4222
CONTROL_NATS_TOKEN=<same token>
CONTROL_NATS_TLS_CA=/absolute/path/to/ca.crt
```

Set both `CONTROL_NATS_TLS_CERT` and `CONTROL_NATS_TLS_KEY` only when the NATS
deployment is changed to require mutual TLS. Do not put a token in the URL.

The monitoring endpoint binds to loopback by default. The client port also
defaults to loopback; set `NATS_BIND_ADDRESS` only on a private network protected
by firewall rules. Back up the named JetStream volume and test restore before a
production release. For HA, deploy an odd-sized official NATS cluster with
replicated JetStream streams; this single-node compose file is the minimum
production-safe topology, not an HA topology.
