# CloudPaste

[中文](README.md) · [English](README_EN.md)

A file browsing, upload and text sharing service for Docker and regular servers. The backend is written in Go. The Vue / Vite frontend builds into static assets served by Nginx. Node.js is only needed to build the frontend.

The old Node backend and its Cloudflare / Docker Hub deployment workflows have been removed. The Go backend now lives in `backend/`. Its database is incompatible with the old Node database, and no legacy data importer is available.

## Features and limits

- Text / Markdown sharing with passwords, expiration and view limits.
- LOCAL, S3-compatible and WebDAV storage, mounts, streaming downloads and Range requests.
- Streaming and form uploads, upload-to-share and resumable uploads through mounts. Existing files are never overwritten.
- Admin sessions, API Key permissions, path scopes, storage ACLs, directory passwords and basic site settings.
- Read-only WebDAV access through `/dav`.

Other storage drivers, remote URL imports, direct browser uploads to upstream storage, public WebDAV writes, file moves / copies / renames / deletion, background jobs and full-drive search are not implemented. Some advanced frontend screens are inherited and do not imply backend support. See the [backend guide](backend/README.md) for details.

## Admin API isolation

Admin requests use `/api/admin/*`; API Key users use ordinary `/api/*` routes, and visitors use public sharing endpoints. Admin WebDAV is `/api/admin/dav`; API Key WebDAV remains `/dav`. Admin download and multipart tickets are bound to the admin namespace. Public site settings are available at `/api/public/settings`.

Configure your IP allowlist or mTLS policy in the external Nginx proxy. Protect both `/api/admin` and every subpath, and prevent direct access to the internal services. Go does not enforce this proxy policy, and admin login is still required. See the [API reference](Api-doc.md) for routes and compatibility changes.
## Docker deployment

Run from the repository root:

```sh
cp backend/.env.example backend/.env
mkdir -p files
```

Set an admin password of at least 12 bytes in `backend/.env`. To use released images, set both image variables to the same version:

```dotenv
ADMIN_USERNAME=admin
ADMIN_PASSWORD=replace-with-your-own-strong-password
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.2.0
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.2.0
LOCAL_FILES_DIR=./files
```

On Linux, grant UID `65532` permission to enter, read and write the file directory using ownership, permissions or ACLs. Avoid `chmod 777`. For browsing only, set `LOCAL_FILES_READ_ONLY=true` and mark the LOCAL storage read-only in the admin UI.

```sh
docker compose --env-file backend/.env config
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

Open `http://127.0.0.1:8081` and log in with the credentials you configured. Create a LOCAL storage with `root_path=/files`, then a mount such as `mount_path=/local`, `root_path=/`. S3 and WebDAV can be configured in the admin UI.

The default bind address is loopback. Use an HTTPS reverse proxy for public access. Nginx proxies `/api` and `/dav` to the backend on the same origin; the backend has no separate host port. Set `BIND_ADDRESS` for LAN access when needed.

To build from source, remove or comment out both `GO_*_IMAGE` variables and run:

```sh
docker compose --env-file backend/.env up -d --build
```

## Binaries and publishing

[Releases](https://github.com/Qwerto107/CloudPaste/releases) provide Linux `amd64` and `arm64` backend binaries with SHA-256 checksums. Deploy frontend assets or the Nginx image separately.

The **Go backend** workflow is [.github/workflows/backend.yml](.github/workflows/backend.yml). Relevant branch pushes and pull requests run checks and upload binary artifacts. Manual runs publish commit-tagged GHCR images only when `publish_images` is selected. Pushing a `go-vX.Y.Z` tag publishes both images and a GitHub Release. Prerelease tags such as `go-vX.Y.Z-rc.1` create a prerelease and do not update `latest`.

Both images support `linux/amd64` and `linux/arm64`. Publication waits for Go checks, binary builds and Docker upload / sharing smoke tests. See the [publishing guide](backend/README.md#github-ci-构建与发布) for details.

## Data and upgrades

The `go-data` volume contains `cloudpaste-go.db`, SQLite WAL files, `encryption.key` and upload staging data. Stop the service and back up the entire volume. A database-only backup cannot decrypt stored credentials. `docker compose down` preserves the volume; `down -v` deletes it.

This directory cleanup preserves the Compose project name `cloudpaste-go`, volume name `go-data`, database filename and both `GO_*_IMAGE` variables. Existing Go deployments should move their environment file from the former Go directory into `backend/.env`, use the default `docker-compose.yml`, and run Compose from the same project directory to keep using the existing volume.

The Node database, password formats, credentials, sessions, shares, mounts and API Keys cannot be reused directly. Removing legacy code does not migrate data. A separate validated importer is required before switching existing Node data.

## Development

Use Go `1.27.2` with pure Go SQLite; no C compiler is required:

```sh
cd backend
go test ./... -count=1
go vet ./...
go build -o bin/cloudpaste ./cmd/cloudpaste
ADMIN_PASSWORD='replace-with-a-strong-password' DATA_DIR=./data go run ./cmd/cloudpaste
```

In another terminal, use Node.js 22 and npm for frontend development:

```sh
cd frontend
npm ci
npm run test:go-upload
npm run dev
```

The development proxy defaults to `http://localhost:8787`. `npm run build` generates static assets in `frontend/dist/`. See the [backend guide](backend/README.md) and [API reference](Api-doc.md) for resource limits, security boundaries and supported endpoints.

## License

[Apache License 2.0](LICENSE).
