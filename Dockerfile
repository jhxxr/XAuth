# syntax=docker/dockerfile:1.7

# ---- web: build the React app that gets embedded in the Go binary ----
FROM node:22-alpine AS web
WORKDIR /src/web

# Install from the lockfile first so source edits do not bust this layer.
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY web/ ./
# Vite writes to ../internal/webui/dist, which is the path Go embeds.
RUN npm run build

# ---- go: build the single static binary ----
FROM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Module download is cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
# The frontend must already exist before Go compiles, because it is embedded.
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/xauth ./cmd/xauth \
 && mkdir -p /out/data

# ---- runtime: no shell, no Go toolchain, no frontend sources ----
FROM gcr.io/distroless/static-debian12:nonroot

# SQLite lives here. The directory is owned by the runtime user, and Docker
# copies that ownership into a fresh named volume on first mount.
WORKDIR /data
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build /out/xauth /usr/local/bin/xauth

# XAUTH_LISTEN must be 0.0.0.0 inside a container.
ENV XAUTH_LISTEN=0.0.0.0:8080 \
    XAUTH_DB=/data/xauth.db \
    TZ=UTC
EXPOSE 8080
VOLUME ["/data"]

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/xauth"]
CMD ["serve"]
