#!/usr/bin/env bash
# Sign and notarize a SAW Mach-O using a dedicated Developer ID keychain.
# Does not mutate the user keychain search list. Does not re-sign on notary wait timeouts.
#
# Required environment (set by CI secrets; do not commit values):
#   MACOS_KEYCHAIN_PASSWORD
#   MACOS_KEYCHAIN_PATH
#   MACOS_SIGN_IDENTITY
#   MACOS_NOTARY_KEY_ID
#   MACOS_NOTARY_ISSUER_ID
#   MACOS_NOTARY_KEY_PATH
#
# Optional:
#   MACOS_BUNDLE_ID       (default: org.axlabs.saw)
#   NOTARY_WAIT_TIMEOUT   (default: 75m)
#
# Usage:
#   ./scripts/sign-macos-saw.sh /path/to/saw
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/saw" >&2
  exit 2
fi

require_env() {
  local name=$1
  if [[ -z "${!name:-}" ]]; then
    echo "error: set ${name}" >&2
    exit 1
  fi
}

require_env MACOS_KEYCHAIN_PASSWORD
require_env MACOS_KEYCHAIN_PATH
require_env MACOS_SIGN_IDENTITY
require_env MACOS_NOTARY_KEY_ID
require_env MACOS_NOTARY_ISSUER_ID
require_env MACOS_NOTARY_KEY_PATH

MACOS_BUNDLE_ID="${MACOS_BUNDLE_ID:-org.axlabs.saw}"
# notarytool accepts one integer + unit (75m), not "1h15m".
NOTARY_WAIT_TIMEOUT="${NOTARY_WAIT_TIMEOUT:-75m}"

BINARY=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
if [[ ! -f "$BINARY" ]]; then
  echo "error: not a file: $BINARY" >&2
  exit 1
fi
if [[ ! -f "$MACOS_KEYCHAIN_PATH" ]]; then
  echo "error: keychain not found" >&2
  exit 1
fi
if [[ ! -f "$MACOS_NOTARY_KEY_PATH" ]]; then
  echo "error: notary API key file not found" >&2
  exit 1
fi

notary_auth=(
  --key "$MACOS_NOTARY_KEY_PATH"
  --key-id "$MACOS_NOTARY_KEY_ID"
  --issuer "$MACOS_NOTARY_ISSUER_ID"
)

json_field() {
  python3 -c '
import json, sys
raw = sys.stdin.read()
start = raw.find("{")
if start < 0:
    raise SystemExit("no JSON object in notarytool output")
print(json.loads(raw[start:])[sys.argv[1]])
' "$1"
}

notary_status() {
  local id=$1
  xcrun notarytool info "$id" --output-format json "${notary_auth[@]}" | json_field status
}

WORKDIR=$(mktemp -d)
cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

security unlock-keychain -p "$MACOS_KEYCHAIN_PASSWORD" "$MACOS_KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$MACOS_KEYCHAIN_PATH"

echo "==> codesigning identity"
security find-identity -v -p codesigning "$MACOS_KEYCHAIN_PATH"

echo "==> signing $BINARY"
codesign --force \
  --options runtime \
  --timestamp \
  --sign "$MACOS_SIGN_IDENTITY" \
  --keychain "$MACOS_KEYCHAIN_PATH" \
  --identifier "$MACOS_BUNDLE_ID" \
  --verbose=2 \
  "$BINARY"

echo "==> verifying signature"
codesign --verify --strict --verbose=2 "$BINARY"
codesign -dv --verbose=4 "$BINARY"

BIN_DIR=$(dirname "$BINARY")
BIN_NAME=$(basename "$BINARY")
ZIP="$WORKDIR/${BIN_NAME}.zip"
(
  cd "$BIN_DIR"
  ditto -c -k "$BIN_NAME" "$ZIP"
)

echo "==> submitting to notary service"
SUBMIT_JSON=$(xcrun notarytool submit "$ZIP" --output-format json "${notary_auth[@]}")
echo "$SUBMIT_JSON"
SUBMISSION_ID=$(printf '%s\n' "$SUBMIT_JSON" | json_field id)
if [[ -z "$SUBMISSION_ID" ]]; then
  echo "error: could not parse notary submission id" >&2
  exit 1
fi
echo "==> submission id $SUBMISSION_ID"

wait_ok=0
for attempt in 1 2 3; do
  echo "==> waiting for notarization (attempt ${attempt}/3, timeout ${NOTARY_WAIT_TIMEOUT})"
  if xcrun notarytool wait "$SUBMISSION_ID" --timeout "$NOTARY_WAIT_TIMEOUT" "${notary_auth[@]}"; then
    wait_ok=1
    break
  fi
  echo "==> notary wait failed (likely HTTP timeout); checking status without re-signing"
done

STATUS=$(notary_status "$SUBMISSION_ID")
echo "==> notary status: $STATUS"
if [[ "$STATUS" != "Accepted" ]]; then
  xcrun notarytool log "$SUBMISSION_ID" "${notary_auth[@]}" || true
  echo "error: notarization not accepted (status=${STATUS})" >&2
  exit 1
fi

if [[ "$wait_ok" -eq 0 ]]; then
  echo "==> wait polling failed but submission is Accepted"
fi

xcrun notarytool log "$SUBMISSION_ID" "${notary_auth[@]}" || true
echo "==> signed and notarized: $BINARY"
