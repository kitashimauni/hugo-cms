#!/usr/bin/env bash
set -euo pipefail

test_env_dir="$(mktemp -d)"
trap 'rm -rf "${test_env_dir}"' EXIT
: >"${test_env_dir}/empty.env"
printf 'HUGO_CMS_REPOS=/data/repos/legacy\n' >"${test_env_dir}/legacy.env"
printf 'HOMECMS_REPOS=/data/repos/canonical\nHUGO_CMS_REPOS=/data/repos/legacy\n' >"${test_env_dir}/canonical.env"

if docker compose --env-file "${test_env_dir}/empty.env" config --quiet; then
  echo "compose config unexpectedly succeeded without a repository allowlist" >&2
  exit 1
fi

legacy_json="$(docker compose --env-file "${test_env_dir}/legacy.env" config --format json)"
for service in hugo-cms tool-bootstrap; do
  jq -e --arg expected "/data/repos/legacy" \
    ".services[\"${service}\"].environment.HOMECMS_REPOS == \$expected and .services[\"${service}\"].environment.MISE_TRUSTED_CONFIG_PATHS == \$expected" \
    <<<"${legacy_json}" >/dev/null
done

canonical_json="$(docker compose --env-file "${test_env_dir}/canonical.env" config --format json)"
for service in hugo-cms tool-bootstrap; do
  jq -e --arg expected "/data/repos/canonical" \
    ".services[\"${service}\"].environment.HOMECMS_REPOS == \$expected and .services[\"${service}\"].environment.MISE_TRUSTED_CONFIG_PATHS == \$expected" \
    <<<"${canonical_json}" >/dev/null
done
