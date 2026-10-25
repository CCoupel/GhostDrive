# PROJECT_CONTEXT.md — GhostDrive

> Contexte technique du projet GhostDrive pour les commandes Claude Code.
> Source de vérité : `CLAUDE.md` (architecture complète) et `config.json` (version).
> **Version détaillée** (avec interface StorageBackend, fichiers clés complets) : `agents/context/PROJECT_CONTEXT.md`.

---

## Stack Technique

| Composant | Technologie |
|-----------|-------------|
| Backend | Go 1.24.0 (toolchain 1.24.2) + Wails v2 |
| Frontend | React + TypeScript (Vite 5) |
| Plugins V1 | WebDAV + MooseFS (interfaces Go compilées) |
| CI/CD | GitHub Actions |
| Distribution | Binaires GitHub Releases |

---

## Commandes Clés

| Opération | Commande |
|-----------|---------|
| Build | `wails build` |
| Tests | `go test ./... -v -cover` |
| Couverture | `go test ./... -v -coverprofile=coverage.out && go tool cover -html=coverage.out` |
| Lint | `golangci-lint run` |
| Audit sécurité | `govulncheck ./...` |
| Typecheck frontend | `cd frontend && npm run typecheck` |
| Version actuelle | `jq -r '.version' config.json` |

---

## Fichiers Clés

| Fichier | Rôle |
|---------|------|
| `config.json` | Config runtime + version (source de vérité absolue) |
| `frontend/package.json` | Version frontend (synchronisée avec config.json) |
| `plugins/plugin.go` | Interface `StorageBackend` |
| `contracts/` | Contrats API Wails Go↔Frontend |
| `CHANGELOG.md` | Historique des versions |

---

## Dispatch DEV

| Fichiers impactés | Agent |
|-------------------|-------|
| `internal/`, `plugins/`, `cmd/` | `dev-backend` |
| `frontend/src/` | `dev-frontend` |
| Mixte avec nouvelle API | Backend → Frontend (séquentiel) |
| Modifications isolées | Backend ‖ Frontend (parallèle) |

---

## Règles Critiques

- **Contract-first** : `contracts/` AVANT le code
- **Plugin-first** : toute interaction backend via `StorageBackend`
- **Jamais de push sur main** sans QA validée
- **Version** : `config.json` + `frontend/package.json` + tag git synchronisés
- **Tests** : seuil minimum 70% de couverture
