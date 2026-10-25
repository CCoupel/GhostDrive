#!/usr/bin/env bash
# CI_CMD (commands.ci) — script unique de la CI GhostDrive.
# Appele par les workflows (release + pre-validation), par qa et deploy en local.
# Prerequis outillage (installe par le workflow ou le poste) : go, node/npm, protoc,
# protoc-gen-go, protoc-gen-go-grpc (versions de .ci-versions.env), gcc (detecteur de course), govulncheck.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== Frontend : build (requis pour go:embed) + tests"
( cd frontend && npm install && npm run build && npm run test )

echo "== Stubs protobuf"
protoc --go_out=. --go-grpc_out=. --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
  plugins/proto/storage.proto

echo "== Go : vet + build"
go vet ./...
go build ./...

echo "== Plugin mock statique (CGO_ENABLED=0 limite a ce build)"
MOCK="${TMPDIR:-/tmp}/ghostdrive-mock-plugin"
CGO_ENABLED=0 go build -o "$MOCK" ./plugins/testdata/mock-plugin/

echo "== Tests Go (detecteur de course + couverture)"
GHOSTDRIVE_MOCK_PLUGIN="$MOCK" go test -race ./... -coverprofile=coverage.out -covermode=atomic \
  -coverpkg=./internal/backends/...,./internal/config/...,./internal/sync/...,./plugins/grpc/...,./plugins/loader/...

echo "== Seuil de couverture (70 %, packages metier)"
COVERAGE=$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%","",$3); print $3}')
echo "Coverage: ${COVERAGE}%"
awk "BEGIN {exit !($COVERAGE < 70)}" && { echo "❌ Below 70%"; exit 1; } || echo "✅ OK"

echo "== Audit des dependances (commands.audit)"
govulncheck ./...

echo "✅ CI_CMD OK"
