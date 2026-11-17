#!/bin/bash
# Builds the app, seeds a throwaway store, and runs the browser suites against
# it. Everything it writes stays in a temporary directory or artifacts/.
#
#   bash test/browser/run.sh              both suites
#   bash test/browser/run.sh --ui         Playwright's UI mode, Lighthouse skipped
#   SKIP_LIGHTHOUSE=1 bash test/browser/run.sh
#
# The browser build is Playwright's own pinned download, not a browser already
# installed on the machine. Run `npm ci` in this directory once, then
# `npx playwright install chromium`.
set -u

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
PORT=${PORT:-18090}
BASE="http://127.0.0.1:$PORT"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/bittrawler-browser.XXXXXX")
DATA="$WORK/data"
BINARY="$WORK/bittrawler"

mkdir -p "$HERE/artifacts"

cd "$REPO" || exit 1

go build -o "$BINARY" . || exit 1
go run ./test/browser/fixture -data "$DATA" || exit 1

# The crawl is disabled: this exercises the interface, never the network.
BITTRAWLER_CRAWLER_ENABLED=false "$BINARY" run \
  -data "$DATA" \
  -http "127.0.0.1:$PORT" \
  -dht "127.0.0.1:0" \
  -log-level warn > "$HERE/artifacts/server.log" 2>&1 &
PID=$!
trap 'kill -TERM "$PID" 2>/dev/null' EXIT

for _ in $(seq 1 150); do
  if curl -sf -o /dev/null "$BASE/healthz"; then
    break
  fi
  sleep 0.1
done

cd "$HERE" || exit 1
export BASE_URL="$BASE"

STATUS=0
npx playwright test "$@" || STATUS=$?

if [ -z "${SKIP_LIGHTHOUSE:-}" ]; then
  node lighthouse.mjs || STATUS=$?
fi

kill -TERM "$PID" 2>/dev/null
wait "$PID" 2>/dev/null

exit "$STATUS"
