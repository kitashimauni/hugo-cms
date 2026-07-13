# syntax=docker/dockerfile:1

FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -ldflags="-s -w" -o /out/hugo-cms .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        gosu \
        gzip \
        tini \
        tar \
        unzip \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://mise.run | sh \
    && mv /root/.local/bin/mise /usr/local/bin/mise \
    && chmod +x /usr/local/bin/mise

RUN groupadd --system --gid 10001 hugo-cms \
    && useradd --system --uid 10001 --gid hugo-cms \
        --home-dir /home/hugo-cms --create-home --shell /usr/sbin/nologin hugo-cms

WORKDIR /app
COPY --from=builder /out/hugo-cms /app/hugo-cms
COPY static /app/static
COPY templates /app/templates
COPY deploy/docker-entrypoint.sh /usr/local/bin/hugo-cms-entrypoint

RUN chmod +x /app/hugo-cms /usr/local/bin/hugo-cms-entrypoint \
    && mkdir -p /data/repos /data/mise /home/hugo-cms \
    && chown -R hugo-cms:hugo-cms /app /data /home/hugo-cms

ENV HOME=/home/hugo-cms \
    MISE_DATA_DIR=/data/mise \
    MISE_CACHE_DIR=/data/mise/cache \
    MISE_TRUSTED_CONFIG_PATHS=/data/repos \
    PATH=/data/mise/shims:/usr/local/bin:/usr/bin:/bin \
    GIN_MODE=release \
    PORT=8080 \
    GENERATOR_RUNTIME=mise

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["tini", "--", "hugo-cms-entrypoint"]
CMD ["/app/hugo-cms"]
