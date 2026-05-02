#!/usr/bin/env bash
# scripts/qualif.sh — Local QUALIF build, mirrors CI build.yml exactly.
#
# Usage:
#   bash scripts/qualif.sh [--platform windows/amd64|linux/amd64]
#
# Outputs to build/qualif/<version>/
#   ghostdrive-v<version>-<platform>.exe      — main Wails app
#   ghostdrive-<plugin>-v<version>-<platform>.exe — each plugin binary
#
# Requirements:
#   - wails.exe (Windows side via WSL) or wails (native Linux)
#   - go in PATH

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────

PLATFORM="${1:-windows/amd64}"
GOOS="${PLATFORM%%/*}"
GOARCH="${PLATFORM##*/}"
SUFFIX="${GOOS}-${GOARCH}"
[ "$GOOS" = "windows" ] && SUFFIX="${SUFFIX}.exe"

# Read version from config.json (source of truth, same as CI)
VERSION=$(grep '"version"' config.json | head -1 | sed 's/.*"version": *"\([^"]*\)".*/\1/')
echo "▶ GhostDrive v${VERSION} — ${PLATFORM}"

OUT="build/qualif/${VERSION}"
PLUGINS_OUT="$OUT/plugins"
mkdir -p "$OUT" "$PLUGINS_OUT"

# ── Step 1 : Tests ────────────────────────────────────────────────────────────
echo ""
echo "── Tests ──────────────────────────────────────────────────────────────────"
export PATH="$PATH:/usr/local/go/bin"
go vet ./...
go test -race ./... -coverprofile=build/qualif/coverage.out -covermode=atomic \
    -coverpkg=./internal/backends/...,./internal/config/...,./internal/sync/...,./plugins/grpc/...,./plugins/loader/...
COVERAGE=$(go tool cover -func=build/qualif/coverage.out | grep total | awk '{print $3}' | tr -d '%')
echo "Coverage: ${COVERAGE}% (seuil: 70%)"
awk "BEGIN {exit !($COVERAGE < 70)}" && echo "❌ Coverage < 70%" && exit 1 || true
echo "✅ Tests OK"

# ── Step 2 : Frontend ─────────────────────────────────────────────────────────
echo ""
echo "── Frontend ───────────────────────────────────────────────────────────────"
(cd frontend && npm install --silent && npm run build)
echo "✅ Frontend OK"

# ── Step 3 : Wails main app ───────────────────────────────────────────────────
echo ""
echo "── Wails build ────────────────────────────────────────────────────────────"
MAIN_OUT="ghostdrive-v${VERSION}-${SUFFIX}"
WAILS="wails.exe"
command -v wails.exe &>/dev/null || WAILS="wails"
"$WAILS" build -platform "$PLATFORM" -o "$MAIN_OUT" -skipbindings
# wails outputs to build/bin/ when -o has no path separator
SRC="build/bin/${MAIN_OUT}"
[ -f "$SRC" ] || SRC="$MAIN_OUT"
cp "$SRC" "$OUT/"
echo "✅ Main app → $OUT/$MAIN_OUT"

# ── Step 4 : Plugins (auto-discovery, mirrors CI) ─────────────────────────────
echo ""
echo "── Plugins ────────────────────────────────────────────────────────────────"
built=0
for cmd_dir in plugins/*/cmd; do
    [ -d "$cmd_dir" ] || continue
    plugin_name=$(basename "$(dirname "$cmd_dir")")
    pkg=$(GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go list "./$cmd_dir/" 2>/dev/null) || continue
    [ -z "$pkg" ] && continue
    PLUGIN_OUT="${PLUGINS_OUT}/ghostdrive-${plugin_name}-v${VERSION}-${SUFFIX}"
    GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 \
        go build -ldflags="-s -w" -o "$PLUGIN_OUT" "./$cmd_dir/"
    echo "✅ ${plugin_name} → $PLUGIN_OUT"
    built=$((built + 1))
done
echo "Total plugins: $built"

# ── Résumé ────────────────────────────────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo "QUALIF OK — artefacts dans $OUT/"
ls -lh "$OUT/"
echo "═══════════════════════════════════════════════════════════════════════════"
