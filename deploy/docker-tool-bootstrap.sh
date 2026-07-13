#!/usr/bin/env bash
set -euo pipefail

readonly REPOS_ROOT="/data/repos"
readonly CLEAN_PATH="/data/mise/shims:/usr/local/bin:/usr/bin:/bin"

log() {
  printf '[docker-tool-bootstrap] %s\n' "$*"
}

die() {
  log "ERROR: $*"
  exit 1
}

run_clean() {
  env -i \
    HOME="${HOME}" \
    PATH="${CLEAN_PATH}" \
    CI=true \
    MISE_DATA_DIR="${MISE_DATA_DIR}" \
    MISE_CACHE_DIR="${MISE_CACHE_DIR}" \
    MISE_TRUSTED_CONFIG_PATHS="${HUGO_CMS_REPOS}" \
    "$@"
}

has_mise_config() {
  local repo="$1"
  [ -f "${repo}/mise.toml" ] \
    || [ -f "${repo}/.mise.toml" ] \
    || [ -f "${repo}/.tool-versions" ] \
    || [ -f "${repo}/mise/config.toml" ] \
    || [ -f "${repo}/.mise/config.toml" ] \
    || [ -f "${repo}/.config/mise.toml" ] \
    || [ -f "${repo}/.config/mise/config.toml" ]
}

install_node_dependencies() {
  local repo="$1"
  local lockfile
  local -a lockfiles=()
  local yarn_version
  local yarn_major

  if [ ! -f "${repo}/package.json" ]; then
    return 0
  fi

  for lockfile in package-lock.json npm-shrinkwrap.json pnpm-lock.yaml yarn.lock bun.lock bun.lockb; do
    if [ -f "${repo}/${lockfile}" ]; then
      lockfiles+=("${lockfile}")
    fi
  done

  [ "${#lockfiles[@]}" -gt 0 ] \
    || die "package.json exists but no supported lockfile was found in ${repo}"
  [ "${#lockfiles[@]}" -eq 1 ] \
    || die "multiple lockfiles found in ${repo}: ${lockfiles[*]}"

  case "${lockfiles[0]}" in
    package-lock.json|npm-shrinkwrap.json)
      log "install npm dependencies for ${repo}"
      run_clean mise exec -C "${repo}" -- npm ci
      ;;
    pnpm-lock.yaml)
      log "install pnpm dependencies for ${repo}"
      run_clean mise exec -C "${repo}" -- pnpm install --frozen-lockfile
      ;;
    yarn.lock)
      yarn_version="$(run_clean mise exec -C "${repo}" -- yarn --version)"
      yarn_major="${yarn_version%%.*}"
      case "${yarn_major}" in
        ''|*[!0-9]*) die "unexpected yarn version ${yarn_version@Q} in ${repo}" ;;
      esac
      if [ "${yarn_major}" -ge 2 ]; then
        log "install Yarn ${yarn_version} dependencies for ${repo} with --immutable"
        run_clean mise exec -C "${repo}" -- yarn install --immutable
      else
        log "install Yarn ${yarn_version} dependencies for ${repo} with --frozen-lockfile"
        run_clean mise exec -C "${repo}" -- yarn install --frozen-lockfile
      fi
      ;;
    bun.lock|bun.lockb)
      log "install Bun dependencies for ${repo}"
      run_clean mise exec -C "${repo}" -- bun install --frozen-lockfile
      ;;
  esac
}

bootstrap_repo() {
  local configured_repo="$1"
  local repo

  [ -n "${configured_repo}" ] || die "HUGO_CMS_REPOS contains an empty entry"
  case "${configured_repo}" in
    /*) ;;
    *) die "repository path must be absolute: ${configured_repo}" ;;
  esac
  [ -d "${configured_repo}" ] || die "repository does not exist: ${configured_repo}"

  repo="$(readlink -f -- "${configured_repo}")"
  case "${repo}" in
    "${REPOS_ROOT}"/*) ;;
    *) die "repository resolves outside ${REPOS_ROOT}: ${configured_repo} -> ${repo}" ;;
  esac

  has_mise_config "${repo}" \
    || die "no supported mise config found in ${repo}"

  log "install mise tools for ${repo}"
  (
    cd "${repo}"
    run_clean mise install
  )
  install_node_dependencies "${repo}"
}

: "${HOME:=/home/hugo-cms}"
: "${MISE_DATA_DIR:=/data/mise}"
: "${MISE_CACHE_DIR:=${MISE_DATA_DIR}/cache}"
: "${HUGO_CMS_REPOS:?HUGO_CMS_REPOS must list allowed repositories separated by ':'}"
: "${MISE_TRUSTED_CONFIG_PATHS:?MISE_TRUSTED_CONFIG_PATHS must be set}"

[ "${MISE_TRUSTED_CONFIG_PATHS}" = "${HUGO_CMS_REPOS}" ] \
  || die "MISE_TRUSTED_CONFIG_PATHS must exactly match HUGO_CMS_REPOS"

case "${HUGO_CMS_REPOS}" in
  :*|*:|*::*) die "HUGO_CMS_REPOS contains an empty entry" ;;
esac

IFS=':' read -r -a configured_repos <<< "${HUGO_CMS_REPOS}"
for configured_repo in "${configured_repos[@]}"; do
  bootstrap_repo "${configured_repo}"
done

log "tool bootstrap completed"
