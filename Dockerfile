# syntax=docker/dockerfile:1
# Vitrine: one Go binary (with the web app built in) plus ffmpeg (video,
# thumbnails) and libvips (images).

# 1. The web app (Svelte), built into internal/server/web/app.
FROM node:24-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# (the texts: the web app imports ../locales, shared with the server; and the changelog it shows)
COPY locales/ /src/locales/
COPY CHANGELOG.md /src/CHANGELOG.md
RUN npm run build

# 2. The server, embedding the web app.
FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/server/web/app ./internal/server/web/app
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/vitrine ./cmd/vitrine

FROM debian:trixie-slim
# The GPU drivers for decoding videos (media.HWAccel; used when a GPU is passed in - compose.example.yaml): AMD's
# (mesa) everywhere, Intel's on x86 (iHD for 2014 and later, i965 before). Nvidia's comes from the host's driver
# through the NVIDIA Container Toolkit (the NVIDIA_ variables say what to bring: video = its decoder).
ARG TARGETARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg libvips-tools ca-certificates tzdata mesa-va-drivers \
    && if [ "$TARGETARCH" = "amd64" ]; then apt-get install -y --no-install-recommends intel-media-va-driver i965-va-driver; fi \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/vitrine /usr/local/bin/vitrine
# Everything lives under /data (databases, thumbnails, secrets.toml, plugins/,
# and the originals in /data/files unless VITRINE_FILES_DIR points elsewhere).
ENV VITRINE_DATA_DIR=/data \
    VITRINE_LISTEN=:8080 \
    HOME=/data \
    NVIDIA_VISIBLE_DEVICES=all \
    NVIDIA_DRIVER_CAPABILITIES=compute,video,utility
RUN mkdir /data && chown 1000:1000 /data
USER 1000:1000
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=2m CMD ["vitrine", "healthcheck"]
# SIGTERM (docker stop) finishes the current request and stops downloads
# between files.
STOPSIGNAL SIGTERM
ENTRYPOINT ["vitrine"]
CMD ["serve"]
