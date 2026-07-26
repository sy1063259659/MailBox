# MailBox Deploy Checklist

Use this checklist before deploying a locally built release to the server.

## Local verification

1. Run `npm run check`.
2. Build the Linux backend binary:
   `cd server && set CGO_ENABLED=0 && set GOOS=linux && set GOARCH=amd64 && go build -o ..\build\gptbox-server-linux-amd64 .`
3. Confirm `dist/index.html` and `build/gptbox-server-linux-amd64` exist.
4. Package `dist`, `build`, `Dockerfile.runtime`, and `docker-compose.yml`.
5. Do not package `.env`, screenshots, logs, or local deployment archives.
6. Optional one-command deployment: `npm run deploy:aliyun`.

## Server verification

1. Upload the runtime package to `/opt/mailbox/app`.
2. Keep the existing `/opt/mailbox/app/.env`; do not overwrite secrets. New deployments should prefer `GPTBOX_*` env vars; existing `MAILBOX_*` names remain supported.
   - The database is SQLite. Set `GPTBOX_SQLITE_PATH=/app/data/mailbox.db` and make sure the compose file mounts `./data:/app/data`. Before first start run `mkdir -p /opt/mailbox/app/data && chown 10001 /opt/mailbox/app/data` so the container user can create the database file. Back up by copying the `data/` directory (include `-wal`/`-shm` files, or stop the container first).
3. Ensure `docker-compose.yml` uses `Dockerfile.runtime` for release builds. The full `Dockerfile` is only for source-build images, not the local-build release path.
4. Run `docker compose up -d --build mailbox`.
5. Check health: `curl -fsS http://127.0.0.1:8787/api/health`.
6. Check logs: `docker logs --tail 80 mailbox`.
7. If the public receive API is served from a different host than the admin UI, set that host in the UI: 隐藏邮箱管理 → 自动补货与取件设置 → 取件 URL 域名. It is stored in the database, not in `.env`; leaving it empty makes receive URLs use whatever origin serves the admin page.
