# CLAUDE.md — GhostDrive

> **Version** : 0.1.0
> **Initialise le** : 2026-04-15
> **Config** : `.claude/project-config.json`
> **GitHub** : https://github.com/CCoupel/GhostDrive

---

## Vision Projet

**GhostDrive** est un client de synchronisation Windows equivalent a OneDrive, mais vers des backends de stockage prives (WebDAV, MooseFS, S3, CephFS...).

**Probleme resolu** : Aujourd'hui, un utilisateur Windows est limite a Google Drive ou iDrive pour etendre son stockage vers le cloud. Ces solutions sont bridees, limitees en espace et impliquent un vendor locking — alors qu'il existe des solutions de stockage maison (NAS) qui ne sont pas exploitables simplement depuis Windows.

**Differenciateur** : Solution libre (GitHub), architecture plugin extensible, experience OneDrive-like (Files On-Demand, sync bidirectionnelle, cache local), distribution binaire simple.

---

## Roadmap

| Version | Fonctionnalites |
|---------|-----------------|
| **V1** | Sync bidirectionnelle + Placeholders Files On-Demand + Cache local (activable) — Backends : WebDAV + MooseFS |
| **V2** | Multi-client synchronise sur le meme repo backend |
| **V3** | Chiffrement cote client + Versioning des fichiers |

---

## Architecture Technique

### Stack

| Composant | Technologie |
|-----------|-------------|
| Backend / moteur | Go 1.21 |
| UI framework | Wails v2 |
| Frontend | React + TypeScript |
| Placeholders Windows | Cloud Filter API + WinFsp |
| Plugins backends | Interface Go (compile) |
| CI/CD | GitHub Actions |
| Distribution | Binaires cross-compiles (Win AMD64, Linux ARM64) |

### Structure Projet

```
ghostdrive/
├── cmd/
│   └── ghostdrive/
│       └── main.go           # Point d'entree Wails
├── internal/
│   ├── sync/                 # Moteur de synchronisation
│   ├── placeholder/          # Cloud Filter API / WinFsp (Files On-Demand)
│   ├── cache/                # Gestion du cache local
│   └── config/               # Configuration application
├── plugins/
│   ├── plugin.go             # Interface StorageBackend (contrat plugin)
│   ├── webdav/               # Plugin WebDAV (V1)
│   └── moosefs/              # Plugin MooseFS (V1)
├── frontend/                 # React frontend (Wails)
│   ├── src/
│   │   ├── components/       # Composants UI
│   │   │   ├── tray/         # Icone systray et menu
│   │   │   ├── settings/     # Configuration backends et sync
│   │   │   └── status/       # Etat de synchronisation
│   │   ├── hooks/
│   │   ├── services/         # Appels Wails runtime
│   │   └── App.tsx
│   └── package.json
├── contracts/                # Contrats API Wails (Go <-> Frontend)
├── docs/                     # Documentation technique
├── tests/                    # Tests d'integration
├── CLAUDE.md                 # Ce fichier
├── CHANGELOG.md
└── README.md
```

### Architecture Plugin

Les backends de stockage sont implementes comme des plugins Go (interfaces compilees) :

```go
type StorageBackend interface {
    Name() string
    Connect(config BackendConfig) error
    Disconnect() error
    Upload(ctx context.Context, local, remote string, progress ProgressCallback) error
    Download(ctx context.Context, remote, local string, progress ProgressCallback) error
    Delete(ctx context.Context, remote string) error
    List(ctx context.Context, path string) ([]FileInfo, error)
    Stat(ctx context.Context, path string) (*FileInfo, error)
    Watch(ctx context.Context, path string) (<-chan FileEvent, error)
    CreateDir(ctx context.Context, path string) error
}
```

**Regles plugin** :
- Chaque plugin dans son propre package `plugins/<nom>/`
- Template et documentation dans `docs/plugin-development.md`
- Tests unitaires obligatoires avec mock ou serveur in-memory

### États de synchronisation et Icônes

Definis dans `contracts/sync-icons.md`. Deux niveaux :

**États fichier** (10 états) : `synced`, `pending_upload`, `pending_download`, `uploading`, `downloading`, `conflict`, `error`, `placeholder` (v1.2.0), `excluded`, `offline`

**Icônes UI GhostDrive** : librairie Lucide React uniquement — couleurs standardisees (vert #22c55e, bleu #3b82f6, ambre #f59e0b, rouge #ef4444, gris #94a3b8)

**Icônes Explorateur Windows** (v1.2.0) : Cloud Filter API overlay icons — voir `contracts/sync-icons.md` section 3

**Priorite d'agregation dossier** : `error > conflict > uploading|downloading > pending_* > synced > placeholder > excluded`

### Concept Backend + Point de Sync

La configuration d'un backend implique **deux notions distinctes** :

| Notion | Definition | Exemple |
|--------|-----------|---------|
| **Backend** | Source des donnees (ou vivent les fichiers) | Dossier local, serveur WebDAV, MooseFS |
| **Point de sync** | Destination locale (ou apparaissent les fichiers sur le PC) | `C:\GhostDrive\MonNAS\` |

Le formulaire de configuration d'un backend est divise en **2 zones distinctes** :

**Zone 1 — Local (Point de sync)**
Ou GhostDrive cree la copie synchronisee sur le PC de l'utilisateur (et dans `GhD:`).
- Nom du backend : unique, insensible a la casse, chars Windows valides (pas `\ / : * ? " < > |`)
- Mode Auto : sous-dossier `<RacineGhostDrive>\<nom-backend>\` (ex: `C:\GhostDrive\MonNAS\`)
- Mode Manuel : chemin libre via bouton "Parcourir" (SelectDirectory)
- Racine GhostDrive configurable dans les preferences globales (defaut : `C:\GhostDrive\`)
- Stocke dans `BackendConfig.LocalPath` + `BackendConfig.Name`

**Zone 2 — Remote (Configuration plugin)**
La source des donnees — varie selon le plugin :
- `local` : `rootPath` (dossier source, bouton Parcourir)
- `webdav` : URL + credentials
- `moosefs` : adresse master + port + chemin

> Distinction cle : pour le plugin LOCAL, Zone 1 = destination sur le PC, Zone 2 = source a synchroniser. Les deux sont des chemins locaux mais ont des roles opposes.

**Regles de validation (`validateBackendConfig`) :**
1. **Nom unique** (insensible a la casse, chars Windows valides) — erreur bloquante
2. **LocalPath unique** parmi tous les backends — erreur bloquante (evite melange fichiers dans `GhD:`)
3. **rootPath Remote identique** a un backend existant — warning non bloquant (cas valide : meme source, destinations differentes)

---

## Configuration Projet

| Parametre | Valeur |
|-----------|--------|
| Nom | GhostDrive |
| Backend | Go 1.21 + Wails v2 |
| Frontend | React + TypeScript |
| Database | Aucune (fichiers locaux + config JSON) |
| CI/CD | GitHub Actions |
| Deploiement | Binaires GitHub Releases |
| Backends V1 | WebDAV + MooseFS |

---

## Architecture des Agents

### Agents de Workflow

| Agent | Role | Fichier |
|-------|------|---------|
| **CDP** | Orchestrateur | `.claude/agents/cdp.md` |
| **Planner** | Plans d'implementation + contrats API | `.claude/agents/implementation-planner.md` |
| **Reviewer** | Revue de code | `.claude/agents/code-reviewer.md` |
| **QA** | Tests et validation | `.claude/agents/qa.md` |
| **Security** | Audit securite | `.claude/agents/security.md` |
| **Doc** | Documentation | `.claude/agents/doc-updater.md` |
| **Deploy** | Deploiement binaires | `.claude/agents/deploy.md` |
| **Infra** | CI/CD + pipelines | `.claude/agents/infra.md` |
| **PR Reviewer** | Validation PR externes | `.claude/agents/pr-reviewer.md` |
| **Marketing** | Communication de release | `.claude/agents/marketing-release.md` |

### Agents de Developpement

| Agent | Role | Fichier |
|-------|------|---------|
| **dev-backend** | Go + moteur sync + plugins | `.claude/agents/dev-backend.md` |
| **dev-frontend** | Wails + React + TypeScript | `.claude/agents/dev-frontend.md` |

---

## Commandes Disponibles

### Developpement

| Commande | Description | Usage |
|----------|-------------|-------|
| `/feat` | Nouvelle fonctionnalite | `/feat <description>` |
| `/bug` | Correction de bug | `/bug <description>` |
| `/hotfix` | Correction urgente prod | `/hotfix <description>` |
| `/refactor` | Refactoring | `/refactor <description>` |

### Validation

| Commande | Description |
|----------|-------------|
| `/review` | Revue de code |
| `/qa` | Tests complets |
| `/secu [scope]` | Audit de securite |

### Deploiement

| Commande | Description |
|----------|-------------|
| `/deploy qualif` | Build + smoke test local |
| `/deploy prod` | Merge main + tag + CI (binaires GitHub Release) |

### Gestion de Projet

| Commande | Description |
|----------|-------------|
| `/backlog` | Consulter / traiter les GitHub Issues |
| `/pr <numero>` | Valider une PR externe |
| `/marketing [version]` | Communication de release |

---

## Workflows

### Workflow Standard (feat / bug)

```
/feat ou /bug
       |
       v
   [PLAN] --> Plan + contrats Wails (contracts/)
       |
       v
   [DEV] --> Backend Go et/ou Frontend React
       |
       v
   [REVIEW] --> Revue de code
       |
       v
   [QA] --> go test + vitest + build Wails
       |
       v
   [DOC] --> CHANGELOG + docs
       |
       v
   [DEPLOY] --> Binaire release
```

### Deploiement PROD

```
/deploy prod
       |
       v
   Merge feat/* -> main (squash)
       |
       v
   Tag vX.Y.Z
       |
       v
   CI GitHub Actions:
     - Verify versions (config.json, package.json, tag)
     - npm ci && npm run build (Wails frontend)
     - wails build (binaire Windows + Linux)
     - gh release create
```

---

## Conventions

### Git

- **Branches** : `feat/<name>`, `bug/<name>`, `hotfix/<name>`
- **Commits** : Conventional Commits `type(scope): message`
  - Types : `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `plugin`
- **Tags** : `v<major>.<minor>.<patch>` (ex: `v1.0.0`)
- **Jamais de push direct sur main**

### Issues GitHub

- **Epics** : Issues parentes avec checklist de sous-issues
- **Milestones** : V1, V2, V3
- **Workflow** : `open` → assigne + branche liee → PR → `closed`
- Les agents maintiennent les issues a jour (statut, labels, commentaires)

### Versioning

Les 3 sources de verite doivent etre synchronisees avant un tag :
- `config.json` : `"version": "X.Y.Z"`
- `frontend/package.json` : `"version": "X.Y.Z"`
- Git tag : `vX.Y.Z`

### Tests

- Tests unitaires : `go test ./... -v -cover` (seuil : 70%)
- Tests integration backends : serveur WebDAV in-memory (`golang.org/x/net/webdav`)
- Tests sync engine : filesystem temporaire (`os.MkdirTemp`)
- Frontend : `vitest`

### Securite

- Jamais de credentials dans le code (config.json chiffre ou variables env)
- Validation des chemins de fichiers (path traversal)
- Audit `/secu` avant chaque release majeure

---

## Memoire Projet

Le fichier `.claude/memory/MEMORY.md` est la **source de verite pour demarrer une session**.
Il est mis a jour par `/end-session` et contient :
- Version courante par environnement
- Travail en cours (phase, branche, issues actives)
- Decisions techniques importantes
- Regles critiques du projet

**Au demarrage de chaque session** : lire `.claude/memory/MEMORY.md` avant toute action.

---

## Notes pour Claude

### Regles Critiques

1. **Lire `.claude/memory/MEMORY.md`** au debut de chaque session
2. **Contract-first** : creer les contrats Wails dans `contracts/` AVANT le code
3. **Plugin d'abord** : toute interaction backend passe par l'interface `StorageBackend`
4. **WinFsp / Cloud Filter API** : les placeholders sont la fonctionnalite cle de V1
5. **Commits conventionnels** : `feat(sync): ...`, `fix(webdav): ...`, `plugin(moosefs): ...`
6. **Maintenir les issues GitHub** : commenter l'avancement, fermer quand done
7. **Ne jamais push sur main** sans validation QA
8. **Fermer la session avec `/end-session`**

### Detection du Contexte

- Lire `project-config.json` pour la stack
- Backend Go = `internal/` pour la logique, `plugins/` pour les backends
- Frontend = Wails bindings dans `contracts/`, composants dans `frontend/src/`
- Plugin = implemente `StorageBackend` dans `plugins/<nom>/`
