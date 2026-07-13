#!/usr/bin/env bash
set -euo pipefail

APP_USER="${HUGO_CMS_USER:-hugo-cms}"
APP_GROUP="${HUGO_CMS_GROUP:-hugo-cms}"

export HOME="${HOME:-/home/${APP_USER}}"
export MISE_DATA_DIR="${MISE_DATA_DIR:-/data/mise}"
export MISE_CACHE_DIR="${MISE_CACHE_DIR:-${MISE_DATA_DIR}/cache}"
export MISE_TRUSTED_CONFIG_PATHS="${MISE_TRUSTED_CONFIG_PATHS:-/data/repos}"
export PATH="${MISE_DATA_DIR}/shims:/usr/local/bin:/usr/bin:/bin:${PATH:-}"

if [ "$(id -u)" = "0" ]; then
  mkdir -p /data/repos "${MISE_DATA_DIR}" "${MISE_CACHE_DIR}" "${HOME}"
  chown -R "${APP_USER}:${APP_GROUP}" /data "${HOME}"
  exec gosu "${APP_USER}:${APP_GROUP}" "$0" "$@"
fi

log() {
  printf '[hugo-cms-entrypoint] %s\n' "$*"
}

has_mise_config() {
  local repo="$1"
  [ -f "${repo}/mise.toml" ] || [ -f "${repo}/.mise.toml" ] || [ -f "${repo}/.tool-versions" ]
}

install_repo_tools() {
  local repo="$1"
  if [ ! -d "${repo}" ]; then
    return 0
  fi
  if ! has_mise_config "${repo}"; then
    log "skip mise install for ${repo}: no mise.toml, .mise.toml, or .tool-versions"
    return 0
  fi

  log "install mise tools for ${repo}"
  (
    cd "${repo}"
    mise install
  )
}

append_repo() {
  local repo="$1"
  if [ -n "${repo}" ] && [ -d "${repo}" ]; then
    printf '%s\n' "${repo}"
  fi
}

if [ "${HUGO_CMS_BOOTSTRAP_MISE:-1}" != "0" ]; then
  repos_file="$(mktemp)"
  trap 'rm -f "${repos_file}"' EXIT

  append_repo "${REPO_PATH:-}" >> "${repos_file}"

  if [ -n "${HUGO_CMS_BOOTSTRAP_REPOS:-}" ]; then
    normalized_repos="${HUGO_CMS_BOOTSTRAP_REPOS//,/ }"
    for repo in ${normalized_repos}; do
      append_repo "${repo}" >> "${repos_file}"
    done
  fi

  if [ -d /data/repos ]; then
    for repo in /data/repos/*; do
      append_repo "${repo}" >> "${repos_file}"
    done
  fi

  if [ -s "${repos_file}" ]; then
    sort -u "${repos_file}" | while IFS= read -r repo; do
      install_repo_tools "${repo}"
    done
  else
    log "no repositories found for mise bootstrap"
  fi
fi

exec "$@"
