#!/usr/bin/env bash
set -euo pipefail

if env -u HOMECMS_REPOS -u HUGO_CMS_REPOS docker compose --env-file /dev/null config --quiet; then
  echo "compose config unexpectedly succeeded without a repository allowlist" >&2
  exit 1
fi

legacy_json="$(env -u HOMECMS_REPOS HUGO_CMS_REPOS=/data/repos/legacy docker compose --env-file /dev/null config --format json)"
for service in hugo-cms tool-bootstrap; do
  jq -e --arg expected "/data/repos/legacy" \
    ".services[\"${service}\"].environment.HOMECMS_REPOS == \$expected and .services[\"${service}\"].environment.MISE_TRUSTED_CONFIG_PATHS == \$expected" \
    <<<"${legacy_json}" >/dev/null
done

canonical_json="$(env HOMECMS_REPOS=/data/repos/canonical HUGO_CMS_REPOS=/data/repos/legacy docker compose --env-file /dev/null config --format json)"
for service in hugo-cms tool-bootstrap; do
  jq -e --arg expected "/data/repos/canonical" \
    ".services[\"${service}\"].environment.HOMECMS_REPOS == \$expected and .services[\"${service}\"].environment.MISE_TRUSTED_CONFIG_PATHS == \$expected" \
    <<<"${canonical_json}" >/dev/null
done
