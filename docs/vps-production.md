# VPS production operations

CargoFlows production runs on the existing Ubuntu 24.04 host reached through
the `vps` SSH alias. Application state lives in the external Docker volumes
`cargoflows-prod-mysql` and `cargoflows-prod-minio`; operational files live at
`/opt/cargoflows`. The only host binding is the Tunnel origin at
`127.0.0.1:4015`.

The canonical public URL is `https://www.cargoflows.cc`. Cloudflare permanently
redirects `https://cargoflows.cc/*` to the canonical host while retaining the
path and query string.

## Release images

Pushing a semantic version tag runs `.github/workflows/release-images.yml`. It
tests the Go and Web applications, validates the production Compose file, and
publishes private linux/amd64 images:

- `ghcr.io/ban11111/cargoflows-api`
- `ghcr.io/ban11111/cargoflows-web`

The VPS pulls a requested tag once and records immutable digests in
`/opt/cargoflows/release.env`. It never builds application source.

## Production credentials

Generate production-only database, object-store, and JWT credentials while
copying the existing encryption master key without printing it:

```bash
ops/vps/create-production-env .env.production
ops/vps/install-production-env .env.production
```

Keep the resulting mode-0600 file in a password manager. It contains the key
required to decrypt the OpenAI credential stored in the migrated database, but
never contains the OpenAI API key itself.

## One-time data migration

The export briefly starts MySQL and MinIO when they are not already running,
creates a consistent logical database dump, exports both private buckets,
records an object count, and adds checksums. Application and worker processes
must remain stopped throughout the export.

```bash
ops/vps/export-local-data
ops/vps/import-vps migration-artifacts/<timestamp>-production-seed.tar.gz
```

The remote importer accepts exactly one import, validates archive paths and
checksums, and refuses a non-empty production database or object store. It does
not transfer `.env` or any credential.

The first deployment runs an idempotent production cutover before the worker
starts. Queued/running job items, non-terminal executions, and unfinished image
turns become cancelled; completed results and accounting history remain intact.

## Deploy and rollback

After the release workflow has published `v0.1.0`:

```bash
ops/vps/deploy-vps v0.1.0
ops/vps/verify-vps
```

Every deployment stops the API, worker, and Web tier before taking a logical
MySQL dump and a MinIO volume snapshot. It then migrates the schema, starts the
API and waits for health, and only then starts the worker and Web origin. A
failed migration or unhealthy API leaves the public tier stopped.

Rollback restores the prior release metadata and the pre-deployment database
and object snapshot:

```bash
ops/vps/rollback-vps
```

Rollback discards all writes made after that snapshot. Backups under
`/opt/cargoflows/rollback` are on the same VPS and are not offsite disaster
recovery.

## Cloudflare Tunnel

Create a remotely managed Tunnel named `cargoflows-prod-sg` with this public
hostname:

```text
www.cargoflows.cc -> http://localhost:4015
```

Create a Cloudflare redirect rule from the root hostname to
`https://www.cargoflows.cc${uri}` with a permanent status while preserving its
query string. Do not reuse the development or Spy the Game Tunnel.

Install the new Tunnel token interactively so it never appears in command
history or logs:

```bash
ops/vps/configure-tunnel-vps
```

The token is stored at `/etc/cloudflared/cargoflows-prod.token` with mode 0600,
and the dedicated `cloudflared-cargoflows.service` starts automatically after a
reboot.

## Acceptance

Run `ops/vps/verify-vps`, then verify login, representative SKU data, a source
image upload and authenticated download, historical AI results, and the
cancelled state of every migrated unfinished AI task. Reboot the VPS and repeat
the verification. Confirm externally that the VPS IP exposes only SSH and that
the root domain redirects to the canonical `www` hostname.
