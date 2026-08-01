# syntax=docker/dockerfile:1

FROM golang:1.24-bookworm@sha256:1a6d4452c65dea36aac2e2d606b01b4a029ec90cc1ae53890540ce6173ea77ac AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -ldflags="-s -w" -o /out/hugo-cms .

FROM debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df

ARG HUGO_CMS_UID=10001
ARG HUGO_CMS_GID=10001
RUN case "${HUGO_CMS_UID}" in \
        ""|*[!0-9]*) echo "HUGO_CMS_UID must be a positive integer" >&2; exit 1 ;; \
        *[1-9]*) : ;; \
        *) echo "HUGO_CMS_UID must be a positive integer" >&2; exit 1 ;; \
    esac \
    && case "${HUGO_CMS_GID}" in \
        ""|*[!0-9]*) echo "HUGO_CMS_GID must be a positive integer" >&2; exit 1 ;; \
        *[1-9]*) : ;; \
        *) echo "HUGO_CMS_GID must be a positive integer" >&2; exit 1 ;; \
    esac

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        gzip \
        tini \
        tar \
        unzip \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

ARG MISE_VERSION=v2026.7.5
RUN curl -fsSL https://mise.run \
    | MISE_VERSION="${MISE_VERSION}" MISE_INSTALL_PATH=/usr/local/bin/mise sh \
    && chmod 0755 /usr/local/bin/mise

RUN if ! getent group "${HUGO_CMS_GID}" >/dev/null; then \
        groupadd --gid "${HUGO_CMS_GID}" hugo-cms; \
    fi \
    && if ! getent passwd "${HUGO_CMS_UID}" >/dev/null; then \
        useradd --uid "${HUGO_CMS_UID}" --gid "${HUGO_CMS_GID}" \
            --home-dir /home/hugo-cms --create-home --shell /usr/sbin/nologin hugo-cms; \
    fi

WORKDIR /app
COPY --from=builder /out/hugo-cms /app/hugo-cms
COPY static /app/static
COPY templates /app/templates
COPY deploy/docker-tool-bootstrap.sh /usr/local/bin/docker-tool-bootstrap

RUN chmod 0755 /app/hugo-cms /usr/local/bin/docker-tool-bootstrap \
    && mkdir -p /data/repos /data/mise /data/homecms /home/hugo-cms \
    && chown -R "${HUGO_CMS_UID}:${HUGO_CMS_GID}" /data/mise /data/homecms /home/hugo-cms

ENV HOME=/home/hugo-cms \
    MISE_DATA_DIR=/data/mise \
    MISE_CACHE_DIR=/data/mise/cache \
    PREVIEW_STATE_DIR=/data/homecms/preview-deployments \
    PATH=/data/mise/shims:/usr/local/bin:/usr/bin:/bin \
    GIN_MODE=release \
    PORT=8080 \
    GENERATOR_RUNTIME=mise

EXPOSE 8080

USER ${HUGO_CMS_UID}:${HUGO_CMS_GID}
ENTRYPOINT ["tini", "--"]
CMD ["/app/hugo-cms"]
