FROM node:20-bookworm AS frontend
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build

FROM golang:1.25-bookworm AS backend
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/mailbox-server .

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --home-dir /app mailbox
WORKDIR /app
COPY --from=backend /out/mailbox-server /app/mailbox-server
COPY --from=frontend /app/dist /app/dist
ENV MAILBOX_SERVER_ADDR=0.0.0.0:8787
ENV MAILBOX_STATIC_DIR=/app/dist
EXPOSE 8787
USER mailbox
ENTRYPOINT ["/app/mailbox-server"]
