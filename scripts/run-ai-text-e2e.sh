#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OFFSET=$(( $$ % 1000 ))
PROJECT=cargoflows-ai-text-e2e-$$
MASTER_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
MYSQL_PORT=$((20000 + OFFSET))
MINIO_PORT=$((21000 + OFFSET))
MINIO_CONSOLE_PORT=$((22000 + OFFSET))
API_HOST_PORT=$((23000 + OFFSET))
FAKE_OPENAI_PORT=$((24000 + OFFSET))
WEB_PORT=$((25000 + OFFSET))

cleanup() {
  docker compose --project-name "$PROJECT" --project-directory "$ROOT" --profile ai-e2e down --volumes --remove-orphans
}
trap cleanup EXIT INT TERM

cleanup
CARGOFLOWS_SECRETS_MASTER_KEY="$MASTER_KEY" \
OPENAI_BASE_URL=http://fake-openai:8099/v1 \
AI_WORKER_DRY_RUN=false \
MYSQL_PORT="$MYSQL_PORT" MINIO_PORT="$MINIO_PORT" MINIO_CONSOLE_PORT="$MINIO_CONSOLE_PORT" API_HOST_PORT="$API_HOST_PORT" FAKE_OPENAI_PORT="$FAKE_OPENAI_PORT" \
docker compose --project-name "$PROJECT" --project-directory "$ROOT" --profile ai-e2e up --detach --build mysql minio migrate fake-openai api worker

attempt=0
until curl --fail --silent "http://127.0.0.1:$API_HOST_PORT/healthz" >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    docker compose --project-name "$PROJECT" --project-directory "$ROOT" logs api worker fake-openai
    exit 1
  fi
  sleep 1
done

cd "$ROOT/web"
if ! API_BASE_URL="http://127.0.0.1:$API_HOST_PORT" FAKE_OPENAI_TEST_URL="http://127.0.0.1:$FAKE_OPENAI_PORT" PLAYWRIGHT_WEB_PORT="$WEB_PORT" PLAYWRIGHT_REUSE_SERVER=false ./node_modules/.bin/playwright test tests/e2e/ai-text-generation.spec.ts --reporter=line; then
  docker compose --project-name "$PROJECT" --project-directory "$ROOT" logs api worker fake-openai
  exit 1
fi
