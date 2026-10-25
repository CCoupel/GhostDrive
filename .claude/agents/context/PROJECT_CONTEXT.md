# PROJECT_CONTEXT.md — GhostDrive

> Contexte technique du projet GhostDrive pour les agents Claude Code.
> Source de vérité : `CLAUDE.md` (architecture complète) et `config.json` (version).

---

## Stack Technique

| Composant | Technologie | Version |
|-----------|-------------|---------|
| Backend | Go | 1.24.0 (toolchain 1.24.2) |
| UI Framework | Wails v2 | — |
| Frontend | React + TypeScript | Vite 5 |
| Placeholders Windows | Cloud Filter API + WinFsp | — |
| Plugins (V1) | WebDAV + MooseFS (interfaces Go compilées) | — |
| CI/CD | GitHub Actions | versions outils via `.ci-versions.env` (QUALIF @latest → PROD pinnée) |
| Distribution | Binaires GitHub Releases (Win AMD64) | Linux ARM64 retiré en v0.3.0 |

---

## Structure Principale

```
ghostdrive/
├── cmd/ghostdrive/main.go    # Point d'entrée Wails
├── internal/
│   ├── sync/                 # Moteur de synchronisation
│   ├── placeholder/          # Cloud Filter API / WinFsp (Files On-Demand)
│   ├── cache/                # Cache local
│   └── config/               # Configuration application
├── plugins/
│   ├── plugin.go             # Interface StorageBackend (contrat plugin)
│   ├── webdav/               # Plugin WebDAV (V1)
│   └── moosefs/              # Plugin MooseFS (V1)
├── frontend/src/             # React frontend (Wails)
│   └── components/tray/      # Icône systray et menu
├── contracts/                # Contrats API Wails (Go <-> Frontend)
├── config.json               # Config runtime + version (source de vérité)
└── frontend/package.json     # Version frontend (synchronisée avec config.json)
```

---

## Commandes Clés

| Opération | Commande |
|-----------|---------|
| Build complet | `wails build` |
| Build + vérification | `wails build && ls -la ghostdrive.exe` |
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
| `frontend/package.json` | Version frontend (doit être synchronisée avec config.json) |
| `plugins/plugin.go` | Interface `StorageBackend` — contrat de tous les plugins |
| `CHANGELOG.md` | Historique — source des release notes GitHub |
| `.github/workflows/ci.yml` | CI légère push/PR (appel par workflow_call) |
| `.github/workflows/build.yml` | Pipeline complet sur tag `v*` (binaires + release) |
| `contracts/` | Contrats API Wails Go↔Frontend — à créer AVANT le code |

---

## Conventions de Branches

| Type | Format | Exemple |
|------|--------|---------|
| Feature | `feature/<nom>` | `feature/v1.5.x` |
| Bugfix | `bugfix/<nom>` | `bugfix/webdav-timeout` |
| Hotfix | `hotfix/<nom>` | `hotfix/auth-crash` |

Production : `main` — jamais de push direct sans QA validée.

---

## Architecture Plugin (StorageBackend)

```go
type StorageBackend interface {
    Name() string
    Connect(config BackendConfig) error
    Upload(ctx context.Context, local, remote string, ...) error
    Download(ctx context.Context, remote, local string, ...) error
    Delete(ctx context.Context, remote string) error
    List(ctx context.Context, path string) ([]FileInfo, error)
    Stat(ctx context.Context, path string) (*FileInfo, error)
    Watch(ctx context.Context, path string) (<-chan FileEvent, error)
    CreateDir(ctx context.Context, path string) error
}
```

Chaque plugin dans `plugins/<nom>/` — tests unitaires obligatoires avec serveur in-memory.

---

## Règles Critiques

- **Contract-first** : créer les contrats dans `contracts/` AVANT le code
- **Plugin-first** : toute interaction backend passe par `StorageBackend`
- **Jamais de push sur main** sans validation QA
- **Version** : `config.json` + `frontend/package.json` + tag git DOIVENT être synchronisés
- **Tests** : seuil minimum 70% de couverture
- **`frontend/package-lock.json`** : doit être commité (reproductibilité CI)
- **Commits** : Conventional Commits `type(scope): message`
