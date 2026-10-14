<div align="center">

<img src="../frontend/public/chain-cloud.svg" width="76" height="76" alt="CloudPaste">

# CloudPaste

**A small place for things you want to share.**

Self-hosted text and file sharing, with custom links, expiration, passwords and view limits.

[中文](../README.md) · [Documentation](README.md) · [API](api.md) · [Releases](https://github.com/Qwerto107/CloudPaste/releases)

**Go + Vue**　 / 　**LOCAL · S3**　 / 　**Docker · amd64 · arm64**

</div>

---

## A focused sharing experience

![CloudPaste product showcase: Markdown editing, mobile file collections and device transfers](assets/product-showcase.png)

*Illustrative product showcase featuring Markdown editing, mobile file collections and device transfers. Actual application screenshots are shown below.*

<details>
<summary><strong>View full screenshots: editing, uploads, transfers, management and settings</strong></summary>

![Text sharing: Markdown editor and access controls in light mode](assets/share-light.png)

<table>
  <tr><th width="70%">Dark mode</th><th width="30%">Mobile file collections</th></tr>
  <tr><td valign="top"><img src="assets/share-dark.png" alt="Markdown editing and sharing in dark mode"></td><td valign="top"><img src="assets/upload-mobile.png" alt="Upload multiple files on mobile and share them with one collection link"></td></tr>
</table>

![Device transfers: exchange text, images and files between connected devices](assets/transfer.png)

![Share management: validity status and consistent actions](assets/management.png)

![System settings: grouped options with clear descriptions](assets/settings.png)

*All screenshots use demonstration content.*

</details>

| Feature | What it offers |
| --- | --- |
| Text sharing | Plain text and Markdown, with editing and preview modes |
| File sharing | Drag and drop, batch uploads, individual link names and retrying remaining files |
| Link controls | Custom names, passwords, view limits and expiration from minutes to days |
| Previews | Text, images, browser-native video and audio; download other formats |
| Storage | Local directories and S3, connection tests, defaults and access controls |
| API Keys | Generated or custom values, separate sharing and management permissions |
| Administration | Share records, storage, keys, admin account and site settings |
| Device transfers | Pairing codes, text and pasted images, WebRTC file transfers and streaming relay downloads through the website port |

New shares expire after 24 hours by default. Expiring or deleting a share revokes access without deleting the stored file. Editing a filename changes its display name only.

Open `/transfer` to connect two devices. Relay downloads support the browser and external download tools such as AB Download Manager through the same link. Both pages must remain online; the server does not cache files. WebRTC downloads are available when the receiving browser supports saving directly to disk.

## Docker quick start

The recommended **single-container deployment** includes both the web interface and API. With Docker and Compose installed, create a deployment directory and download the Compose file:

```sh
mkdir -p cloudpaste/data cloudpaste/files
cd cloudpaste
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.yml -o docker-compose.yml
```

Create `.env` in the `cloudpaste` directory and set your own administrator password of at least 12 bytes:

```dotenv
ADMIN_PASSWORD='replace-with-your-own-strong-password'
```

On Linux, grant UID/GID `65532` access to both directories. For newly created dedicated directories, use `sudo chown 65532:65532 data files`.

```sh
docker compose up -d --pull always
```

Open `http://127.0.0.1:8081/admin/login` and sign in with `admin` and your password. Configure LOCAL storage with `/files` as its root, or add S3 storage. Create API Keys for other users, who sign in at `/login`.

For a **separate frontend and backend**, also save the alternative configuration as `docker-compose.yml`. If the single-container deployment is running, execute `docker compose down` before replacing its configuration:

```sh
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.split.yml -o docker-compose.yml
docker compose up -d --pull always
```

The single-container `cloudpaste` image supports Linux amd64, arm64, armv6, armv5 and loong64. The separate `cloudpaste-frontend` and `cloudpaste-backend` images support Linux amd64 and arm64. Both modes use the same entry point; stop the active mode before switching. Access is limited to the local machine by default. See the [deployment guide](deployment.md) for server access, switching modes and upgrades. [Linux, Windows and macOS binaries](binary.md) include the complete web interface in one executable; separate backend binaries are available for Linux amd64 and arm64.

## Local assets, clear boundaries

The Vue frontend builds into static pages. Scripts, styles and icons are served by the site; Markdown loads on demand. The backend streams uploads through private staging before writing to storage.

API Key management pages use `/manage/*`; administrator pages use `/admin/*`. Admin APIs use `/api/admin/*` and can be protected by an external reverse proxy with an IP allowlist or mTLS, in addition to account authentication.

Markdown blocks external images by default. Custom site HTML is sanitized. The mint interface supports light and dark modes, remembered theme choices and mobile navigation. The chain-cloud favicon adapts to the browser color scheme.

## Documentation

| Topic | Guide |
| --- | --- |
| Sharing, permissions and settings | [Usage](usage.md) |
| Docker, data and upgrades | [Deployment](deployment.md) |
| Single executable or API-only backend | [Binaries](binary.md) |
| Guest, API Key and administrator endpoints | [API reference](api.md) |
| Local development and checks | [Development](development.md) |
| Multi-platform images and release assets | [Publishing](releasing.md) |
| Version changes | [Changelog](changelog.md) |

Detailed guides are currently in Chinese.

---

Built with Go, SQLite and Vue · [Apache License 2.0](../LICENSE)
