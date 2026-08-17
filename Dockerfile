# syntax=docker/dockerfile:1

# --- stage 1: build both Vite frontends -------------------------------------
FROM node:22-alpine AS web

WORKDIR /src

# Install deps first so edits to the app sources don't invalidate the layer.
COPY web/visitor/package.json web/visitor/package-lock.json ./web/visitor/
COPY web/inbox/package.json web/inbox/package-lock.json ./web/inbox/
RUN npm ci --prefix web/visitor && npm ci --prefix web/inbox

COPY web/ ./web/
RUN npm run build --prefix web/visitor && npm run build --prefix web/inbox

# --- stage 2: compile the static binary with the assets embedded ------------
FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
COPY --from=web /src/web/visitor/dist ./web/visitor/dist
COPY --from=web /src/web/inbox/dist ./web/inbox/dist

# CGO off: modernc.org/sqlite is pure Go, so the result is fully static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/freesupp ./cmd/freesupp

# --- stage 3: runtime -------------------------------------------------------
# Alpine rather than distroless: busybox wget gives us a HEALTHCHECK without
# baking a probe into the binary, at the cost of ~8 MB.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S freesupp && adduser -S -G freesupp -u 10001 freesupp \
    && mkdir -p /data && chown freesupp:freesupp /data

COPY --from=build /out/freesupp /usr/local/bin/freesupp

USER freesupp
WORKDIR /data
VOLUME /data
EXPOSE 8080

ENV LISTEN=:8080 \
    DB_PATH=/data/freesupp.db

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/ping || exit 1

ENTRYPOINT ["/usr/local/bin/freesupp"]
