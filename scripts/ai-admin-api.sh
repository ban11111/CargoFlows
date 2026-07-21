#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ENV_FILE=${CARGOFLOWS_ENV_FILE:-$ROOT/.env}

usage() {
  cat <<'EOF'
Usage: scripts/ai-admin-api.sh METHOD PATH [JSON_FILE|-]

Authenticate with ADMIN_USER_EMAIL and ADMIN_USER_PASSWORD from the repository
.env, then call a CargoFlows AI, OpenAI settings, or SKU API endpoint.

Examples:
  scripts/ai-admin-api.sh GET /ai-content-templates
  scripts/ai-admin-api.sh PATCH /ai-content-template-versions/UUID update.json
  printf '%s' '{"name":"example"}' | scripts/ai-admin-api.sh POST /ai-content-templates -

Environment overrides:
  CARGOFLOWS_ENV_FILE  Credential file (default: repository .env)
  API_BASE_URL         API origin (default: http://127.0.0.1:8080)
EOF
}

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
  usage >&2
  exit 2
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "credential file not found: $ENV_FILE" >&2
  exit 2
fi

# The local .env is user-owned and ignored by Git. Keep its values in this shell;
# credentials and the resulting bearer token are never logged or exported.
# shellcheck disable=SC1090
. "$ENV_FILE"

: "${ADMIN_USER_EMAIL:?ADMIN_USER_EMAIL is required in $ENV_FILE}"
: "${ADMIN_USER_PASSWORD:?ADMIN_USER_PASSWORD is required in $ENV_FILE}"

API_BASE_URL=${API_BASE_URL:-http://127.0.0.1:8080}
API_BASE_URL=${API_BASE_URL%/}
METHOD=$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')
PATH_ARG=$2
BODY_SOURCE=${3:-}

case "$METHOD" in
  GET|POST|PUT|PATCH|DELETE) ;;
  *)
    echo "unsupported HTTP method: $METHOD" >&2
    exit 2
    ;;
esac

case "$PATH_ARG" in
  /api/v1/*) API_PATH=$PATH_ARG ;;
  /*) API_PATH=/api/v1$PATH_ARG ;;
  *)
    echo "PATH must begin with /" >&2
    exit 2
    ;;
esac

case "$API_PATH" in
  /api/v1/ai-*|/api/v1/settings/openai|/api/v1/settings/openai/*|/api/v1/skus|/api/v1/skus/*) ;;
  *)
    echo "refusing non-AI/SKU API path: $API_PATH" >&2
    exit 2
    ;;
esac

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 2; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 2; }

TOKEN=$(
  {
    printf '%s\n' "$ADMIN_USER_EMAIL"
    printf '%s\n' "$ADMIN_USER_PASSWORD"
  } | jq -Rsc 'split("\n") | {email: .[0], password: .[1]}' \
    | curl --fail-with-body --silent --show-error \
      -H 'Content-Type: application/json' \
      --data-binary @- \
      "$API_BASE_URL/api/v1/auth/login" \
    | jq -er '.token'
)

if [ -z "$BODY_SOURCE" ]; then
  exec curl --fail-with-body --silent --show-error \
    -X "$METHOD" \
    -H "Authorization: Bearer $TOKEN" \
    "$API_BASE_URL$API_PATH"
fi

if [ "$BODY_SOURCE" = - ]; then
  exec curl --fail-with-body --silent --show-error \
    -X "$METHOD" \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "$API_BASE_URL$API_PATH"
fi

if [ ! -f "$BODY_SOURCE" ]; then
  echo "JSON body file not found: $BODY_SOURCE" >&2
  exit 2
fi

exec curl --fail-with-body --silent --show-error \
  -X "$METHOD" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary "@$BODY_SOURCE" \
  "$API_BASE_URL$API_PATH"
