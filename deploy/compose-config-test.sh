#!/usr/bin/env bash
set -euo pipefail

test_env_dir="$(mktemp -d)"
trap 'rm -rf "${test_env_dir}"' EXIT
: >"${test_env_dir}/empty.env"
printf 'HUGO_CMS_REPOS=/data/repos/legacy\n' >"${test_env_dir}/legacy.env"
printf 'HOMECMS_REPOS=/data/repos/canonical\nHUGO_CMS_REPOS=/data/repos/legacy\n' >"${test_env_dir}/canonical.env"

if docker compose --profile tools --env-file "${test_env_dir}/empty.env" config --quiet >/dev/null 2>&1; then
  echo "compose config unexpectedly succeeded without a repository allowlist" >&2
  exit 1
fi

assert_repository_settings() {
  local config_json="$1"
  local expected="$2"
  local service key actual

  for service in hugo-cms tool-bootstrap; do
    for key in HOMECMS_REPOS MISE_TRUSTED_CONFIG_PATHS; do
      actual="$(jq -r --arg service "${service}" --arg key "${key}" '.services[$service].environment[$key]' <<<"${config_json}")"
      if [[ "${actual}" != "${expected}" ]]; then
        echo "${service}.${key} = ${actual@Q}, want ${expected@Q}" >&2
        return 1
      fi
    done
  done
}

legacy_json="$(docker compose --profile tools --env-file "${test_env_dir}/legacy.env" config --format json)"
assert_repository_settings "${legacy_json}" "/data/repos/legacy"

canonical_json="$(docker compose --profile tools --env-file "${test_env_dir}/canonical.env" config --format json)"
assert_repository_settings "${canonical_json}" "/data/repos/canonical"
