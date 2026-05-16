# MailBox Deploy Checklist

Use this checklist before deploying a locally built release to the server.

## Local verification

1. Run `npm run check`.
2. Build the Linux backend binary:
   `cd server && set CGO_ENABLED=0 && set GOOS=linux && set GOARCH=amd64 && go build -o ..\build\mailbox-server-linux-amd64 .`
3. Confirm `dist/index.html` and `build/mailbox-server-linux-amd64` exist.
4. Package `dist`, `build`, `Dockerfile.runtime`, and `docker-compose.yml`.
5. Do not package `.env`, screenshots, logs, or local deployment archives.

## Server verification

1. Upload the runtime package to `/opt/mailbox/app`.
2. Keep the existing `/opt/mailbox/app/.env`; do not overwrite secrets.
3. Ensure `docker-compose.yml` uses `Dockerfile.runtime` for release builds. The full `Dockerfile` is only for source-build images, not the local-build release path.
4. Run `docker compose up -d --build mailbox`.
5. Check health: `curl -fsS http://127.0.0.1:8787/api/health`.
6. Check logs: `docker logs --tail 80 mailbox`.
