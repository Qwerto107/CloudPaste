# CloudPaste

[中文](README.md) · [English](README_EN.md)

A temporary text and file sharing site for Docker and regular servers. The backend is Go; the Vue frontend compiles into static assets served by Nginx. Node.js is needed only at build time.

## Features

- Text / Markdown sharing and safe previews, with expiration, passwords and view limits.
- File upload and sharing; native text, image, video and audio previews. Other formats remain downloadable.
- Text and file share management, LOCAL / S3 storage configuration, connection tests and default storage.
- API Keys, permissions, storage ACLs and account management.
- Global, preview and site settings: upload limit, default expiration, preview extensions and size, entry switches, announcements, footer, favicon and sanitized HTML.

New shares expire after 24 hours by default. Expiration and share deletion revoke access without deleting the original stored file. Renaming a share changes its display name only.

Backup/restore, WebDAV, dashboards, complex file browsing, jobs, indexing and Office/PDF/archive previews have been removed. Existing Go data and internal mount/path compatibility remain available for existing shares. New uploads do not require mounts. The Go database is incompatible with the old Node database.

All application assets are served locally. There are no CDN libraries or remote fonts. Markdown loads on demand and blocks external images by default. Offline precaching is disabled; the upgrade worker clears old application caches. Site HTML is sanitized and cannot execute scripts or embed forms/iframes.

The mint interface supports light and dark modes. The first visit follows the system preference, and manual choices are remembered in the browser. Mobile screens use bottom navigation; icons are local SVGs with no external fonts or icon libraries.

## Admin isolation

API Key users sign in at `/login` and manage their shares under `/manage/*`. The login page links to the separate administrator login at `/admin/login`, followed by `/admin/*` for administration. Signed-in ordinary-user management pages contain no admin navigation links.

Text and file sharing show expiry, access password and maximum views directly (0 means unlimited views). Expiry options range from minutes to days, plus no expiry. New API Keys can use a custom value of 24–128 visible ASCII characters without spaces; leaving it blank generates a key. Editing permissions does not replace the key.

Settings are grouped into General, File previews, and Site appearance, with descriptive controls, units, and unsaved-change protection. Advanced HTML is collapsed by default; fixed security policies are read-only.

Text and file creation support custom link names such as `/paste/my-note` and `/file/my-file`. Leave blank for a generated name. Names accept 1–100 ASCII letters, digits, hyphens, or underscores and must be unique within the share type. Batch uploads allow a different name per file.

Admins use `/api/admin/*`, API Keys use ordinary `/api/*`, and visitors use public share endpoints. Apply your IP allowlist or mTLS policy to the complete admin prefix in the external reverse proxy. Admin login is still required; do not expose internal ports that bypass your proxy. See the [API reference](Api-doc.md).

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
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.4.1
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.4.1
LOCAL_FILES_DIR=./files
```

On Linux, grant UID `65532` permission to enter, read and write the file directory using ownership, permissions or ACLs. Avoid `chmod 777`. For browsing only, set `LOCAL_FILES_READ_ONLY=true` and mark the LOCAL storage read-only in the admin UI.

```sh
docker compose --env-file backend/.env config
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

Open `http://127.0.0.1:8081/admin/login` and log in with the credentials you configured. Create a LOCAL storage with `root_path=/files`, or configure S3. Choose the destination directly on the upload page; no mount is required. Create API Keys for ordinary users, who sign in at `/login`.

The default bind address is loopback. Use an HTTPS reverse proxy for public access. Nginx proxies `/api` to the backend on the same origin; the backend has no separate host port. Set `BIND_ADDRESS` for LAN access when needed.

To build from source, remove or comment out both `GO_*_IMAGE` variables and run:

```sh
docker compose --env-file backend/.env up -d --build
```

## Binaries and publishing

[Releases](https://github.com/Qwerto107/CloudPaste/releases) provide Linux `amd64` and `arm64` backend binaries with SHA-256 checksums. Deploy frontend assets or the Nginx image separately.

The **Go backend** workflow is [.github/workflows/backend.yml](.github/workflows/backend.yml). Relevant branch pushes and pull requests run checks and upload binary artifacts. Manual runs publish commit-tagged GHCR images only when `publish_images` is selected. Pushing a `go-vX.Y.Z` tag publishes both images and a GitHub Release. Prerelease tags such as `go-vX.Y.Z-rc.1` create a prerelease and do not update `latest`.

Both images support `linux/amd64` and `linux/arm64`. Publication waits for Go race, static and vulnerability checks, binary builds, Docker smoke tests and browser regression tests. A failed build tag does not mean images or a Release were published. See the [publishing guide](backend/README.md#github-ci-构建与发布) for details.

## Data and upgrades

To upgrade from Go `0.3.1`, retain your data volume, files and environment file, update both image variables to the same new version, then run `pull` and `up -d --no-build` as above. No database reinitialization is needed. Refresh old browser assets if needed. See the [changelog](CHANGELOG.md).

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
VITE_BACKEND_URL=http://127.0.0.1:3000 npm run dev -- --host 127.0.0.1
```

Open `http://127.0.0.1:3000`; Vite forwards same-origin `/api` requests to `http://127.0.0.1:8787`. The override avoids older local settings that connect directly to `localhost:8787` and trigger CORS errors. In PowerShell, set `$env:VITE_BACKEND_URL='http://127.0.0.1:3000'` before running `npm run dev -- --host 127.0.0.1`. `npm run build` generates static assets in `frontend/dist/`. See the [backend guide](backend/README.md) and [API reference](Api-doc.md) for resource limits, security boundaries and supported endpoints.

The default chain-cloud favicon uses `frontend/public/chain-cloud.svg`, with browser color-scheme adaptation, a 16/32/48 px ICO and a 180 px touch icon. A custom favicon URL overrides tab icons; clearing it restores defaults. Touch icons remain local. Run `node scripts/generate-icons.mjs` from `frontend/` to regenerate raster assets using Chrome, Edge or Playwright Chromium.

## License

[Apache License 2.0](LICENSE).
