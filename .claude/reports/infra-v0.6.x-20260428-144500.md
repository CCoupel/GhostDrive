# Validation Infra — QUALIF v0.6.0

**Branche** : `feat/v0.6.x-plugin-loader` — SHA `40a6643`
**Date** : 2026-04-28

---

## Verdict : VALIDATED (avec 1 avertissement non-bloquant)

---

## Cohérences vérifiées

- [x] **CI protoc — position** : étape `Install protoc + plugins` insérée après `setup-go`, avant `go vet` → ordre correct
- [x] **CI protoc — version pinnée** : `PB_REL="3.25.1"` — pas de drift possible
- [x] **CI protoc — source** : génère depuis `plugins/proto/storage.proto` avec `--go_opt=paths=source_relative` → stubs produits dans `plugins/proto/` comme attendu
- [x] **`.pb.go` non trackés** : `git ls-files plugins/proto/` retourne uniquement `storage.proto` — les fichiers `*.pb.go` sont bien gitignorés (`plugins/proto/*.pb.go` dans `.gitignore`)
- [x] **`.pb.go` disponibles en CI** : les stubs sont générés par `protoc` avant `go vet` / `go build` / `go test` → chaîne cohérente
- [x] **go.mod — go-plugin** : `github.com/hashicorp/go-plugin v1.7.0` (direct) ✅
- [x] **go.mod — grpc** : `google.golang.org/grpc v1.80.0` (direct) ✅
- [x] **go.mod — protobuf** : `google.golang.org/protobuf v1.36.11` (direct) ✅
- [x] **go.mod — genproto/rpc** : `google.golang.org/genproto/googleapis/rpc` (indirect) ✅
- [x] **`go_package` proto** : `github.com/CCoupel/GhostDrive/plugins/proto;storagepb` — cohérent avec `source_relative` et les imports dans `plugins/grpc/`
- [x] **`plugins/sdk/go/main.go`** : porte le tag `//go:build ignore` → exclu de `go build ./...` ✅

---

## Écart détecté

- [ ] **`plugins/sdk/go/echo/main.go` — absence de build tag** — `plugins/sdk/go/echo/main.go:1`

  `echo/main.go` est `package main` sans `//go:build ignore`. À la différence du template `sdk/go/main.go` qui est correctement exclu, le plugin echo **est compilé** par `go build ./...` en CI.

  **Impact** : non bloquant — toutes les dépendances sont satisfaites dans `go.mod` (`go-plugin`, `grpc`), le build réussit. Mais l'echo plugin SDK est compilé dans le scan du module principal, ce qui n'est pas l'intention déclarée.

  **Correction recommandée** : ajouter en ligne 1 de `plugins/sdk/go/echo/main.go` :
  ```go
  //go:build ignore
  ```

---

## Recommandations

1. **(Non-bloquant pour QUALIF)** Ajouter `//go:build ignore` à `plugins/sdk/go/echo/main.go` pour aligner son comportement sur `sdk/go/main.go`. Peut être fait en même temps que la correction du build CI ou en ticket séparé.

2. **(Info)** Les fichiers `.pb.go` existent sur le disque local du développeur (générés manuellement) mais ne sont pas trackés — comportement normal et attendu. En CI ils seront toujours régénérés à partir de `storage.proto`.
