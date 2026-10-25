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

| Version | Statut | Contenu |
|---------|--------|---------|
| **V1** | ✅ Livré | Sync bidirectionnelle + Placeholders Files On-Demand + Cache local (activable) — Backends : WebDAV + MooseFS |
| **V2.0** | ✅ Livré (2026-05-17) | VFS Foundation, WinFsp + Cloud Filter API, ReadAt/ChunkSize |
| **V2.1** | ✅ Livré (2026-05-22) | Files On-Demand, CF API foundation, placeholders, hydratation progressive, 8 bugs CF |
| **V2.2** | 🔜 Q3 2026 | Workflow Objets (rename/move natif, Copier état, bugs data-loss, avertissement Conflict) |
| **V2.3** | 🔜 Q3 2026 | Sync bidirectionnelle (badges ☁️, UI conflits, cache états, retry exponentiel) |
| **V2.4** | 🔜 Q4 2026 | Cache avancé & Offline |
| **V2 (Major)** | 🔜 2027 | Multi-client synchronisé sur le même repo backend |
| **V3** | 🔜 2027+ | Chiffrement côté client + Versioning des fichiers |

**Voir aussi** : [`docs/roadmap.md`](docs/roadmap.md) pour la roadmap détaillée.

---

## Architecture Technique

### Stack

| Composant | Technologie |
|-----------|-------------|
| Backend / moteur | Go 1.24.0 (toolchain 1.24.2) |
| UI framework | Wails v2 |
| Frontend | React + TypeScript |
| Placeholders Windows | Cloud Filter API + WinFsp |
| Plugins backends | Binaires Go out-of-process (gRPC, `.ghdp`) implementant `StorageBackend` |
| CI/CD | GitHub Actions — versions outils gérées via `.ci-versions.env` (QUALIF @latest → PROD pinnée) |
| Distribution | Binaires GitHub Releases (Win AMD64) |

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

Les backends de stockage sont implementes comme des plugins Go **out-of-process** : chaque plugin est un binaire `.ghdp` (place a cote de l'exe) lance en sous-processus et pilote via gRPC (`plugins/grpc`, `plugins/loader`, `plugins/sdk`), qui implemente l'interface `StorageBackend` :

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

Stack : voir « Architecture Technique » ci-dessus ; configuration complete (commandes, environnements, registre des
agents) : `.claude/project-config.json`. Database : aucune (fichiers locaux + config JSON).

---

## Agents Disponibles

> Le CDP (orchestrateur) est porté par le Claude principal (`teamleader.md`) — jamais un teammate séparé, voir "Rôle Teamleader" ci-dessous.

| Nom | Rôle | Fichier | Spawn |
|-----|------|---------|-------|
| `planner` | Plans d'implémentation + contrats API | `.claude/agents/implementation-planner.template.md` | permanent |
| `dev-backend` | Backend (Go + moteur sync + plugins) | `.claude/agents/dev-backend.template.md` | permanent |
| `dev-frontend` | Frontend (Wails + React + TypeScript) | `.claude/agents/dev-frontend.template.md` | permanent |
| `dev-plugin` | Plugins backends (StorageBackend : WebDAV, MooseFS...) | `.claude/agents/dev-plugin.template.md` | permanent |
| `test-writer` | Scripts de tests + procédures QA | `.claude/agents/test-writer.template.md` | permanent |
| `code-reviewer` | Revue de code | `.claude/agents/code-reviewer.template.md` | permanent |
| `qa` | Exécution des tests et validation | `.claude/agents/qa.template.md` | permanent |
| `doc-updater` | Documentation | `.claude/agents/doc-updater.template.md` | permanent |
| `deployer` | Build + Publication + Déploiement QUALIF/PROD | `.claude/agents/deploy.template.md` (+ `.claude/agents/deploy.md`) | permanent |
| `security` | Audit sécurité | `.claude/agents/security.template.md` | ponctuel |
| `infra` | Infrastructure (CI/CD, pipelines) | `.claude/agents/infra.template.md` | ponctuel |
| `pr-reviewer` | Validation des PR externes | `.claude/agents/pr-reviewer.template.md` | ponctuel |
| `marketing-release` | Communication de release | `.claude/agents/marketing-release.template.md` | ponctuel |

> **Fichier** pointe vers le `.template.md` — géré par sync, toujours présent. Un compagnon
> `.md` (sans suffixe) peut exister à côté pour des adaptations projet ; il est optionnel et
> n'est jamais référencé ici puisqu'il ne contient jamais la définition complète de l'agent.

> **permanent** = spawné au `/start-session`, reste en IDLE toute la session.
> **ponctuel** = spawné à la demande par la commande dédiée, fermé après DONE.

---

## Commandes Disponibles

### Developpement

| Commande | Description | Usage |
|----------|-------------|-------|
| `/feature` | Nouvelle fonctionnalite | `/feature <description ou #issue>` |
| `/bugfix` | Correction de bug | `/bugfix <description ou #issue>` |
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
| `/build` | Compile l'artefact candidat (exe + plugins `.ghdp`) |
| `/publish qualif` | Promeut le candidat vers `build/qualif/<X.Y.Z>/` |
| `/publish prod` | Barriere `ci-branch.yml` + merge main + tag → CI (GitHub Release) |
| `/deploy qualif` | Rapport de livraison + lancement de l'exe (test manuel Windows) |
| `/deploy prod` | Verifie la GitHub Release du tag |

### Gestion de Projet

| Commande | Description |
|----------|-------------|
| `/backlog` | Consulter / traiter les GitHub Issues |
| `/milestone new/status/close` | Cycle de vie du milestone |
| `/progression`, `/team-status` | Avancement de l'equipe |
| `/context-audit [scope]` | Audit du contexte documentaire |
| `/start-session`, `/end-session` | Ouverture / cloture de session |
| `/marketing [version]` | Communication de release |

---

## Workflows

### Workflow Standard (feat / bug)

```
/feature ou /bugfix
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
/publish prod
       |
       v
   Barriere : ci-branch.yml (scripts/ci.sh) vert sur le SHA de milestone/vX.Y.Z
       |
       v
   Merge --no-ff milestone/vX.Y.Z -> main (worktree detache)
       |
       v
   Tag vX.Y.Z
       |
       v
   CI GitHub Actions (build.yml) :
     - CI complete (ci.yml, versions PROD pinnees)
     - Version extraite du tag -> config.json, package.json
     - wails build (Windows AMD64) + plugins .ghdp
     - GitHub Release (notes CHANGELOG.md)
       |
       v
/deploy prod --> verification des assets de la Release
```

---

## Conventions

### Git

- **Branches** : `feature/<name>`, `bugfix/<name>`, `hotfix/<name>`
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

Format dev : `X.Y.Z.a` — Format prod : `X.Y.Z` (le `a` n'est jamais publie en prod).
Regles completes (les 5 operations, regle d'or bug remonte, milestone comme source de la version cible) : voir `.claude/commands/context/COMMON.template.md` section 5.

| Segment | Role |
|---------|------|
| X | Compatibilite des donnees (DB, fichiers) |
| Y | Compteur de milestone/livraison — impair = dev, pair = prod, avance toujours de +1 |
| Z | Compteur de bugfix — remis a 0 au demarrage d'un nouveau milestone |
| a | Iteration dev interne — jamais visible en prod |

Les 3 sources de verite doivent etre synchronisees avant un tag :
- `config.json` : `"version": "X.Y.Z.a"` (dev) / `"X.Y.Z"` (prod)
- `frontend/package.json` : idem
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

---

<!-- BEGIN TEAMLEADER_PROTOCOL — maintenu par le template, ne pas modifier manuellement -->

## Rôle Teamleader — Règles Critiques

> Ce bloc est maintenu par le template. Pour le mettre à jour : `/init-project` option d (step d6).

### Identité

Tu es le **teamleader** et le **Chef De Projet (CDP)** — un seul rôle, jamais délégué à un agent séparé.  
Tu **coordonnes et dispatches**. Tu n'exécutes aucune tâche technique toi-même.

### Délégation Stricte — Outils Interdits

| Outil interdit | Déléguer à |
|---------------|-----------|
| `Edit`, `Write`, `MultiEdit` (sauf `Write` d'un ordre dans `_work/tasks/*.md` et de l'état `.claude/workflow-state.json`, `.claude/workflow/*`) | `dev-*`, `doc-updater` |
| `Bash` (build / test / git) | `qa`, `deployer`, `dev-*` |
| `Read` (code applicatif) | `code-reviewer`, `planner` |
| `Glob`, `Grep` (recherche code) | `planner`, `dev-*` |

**`Read` autorisé uniquement pour** : `CLAUDE.md`, `MEMORY.md`, `project-config.json`, `_work/tasks/*.md`, `_work/handoff/*.md`, `_work/reports/*.md`, `contracts/CHANGELOG.md`

**`Write` autorisé uniquement pour** : `_work/tasks/*.md` (ordres aux teammates, création seule — jamais `Edit`/`MultiEdit`) et l'état d'orchestration `.claude/workflow-state.json` / `.claude/workflow/plan-<version>.md` (reprise après interruption) — jamais un autre chemin

**Ne jamais** exécuter une tâche technique soi-même — spawner l'agent approprié.

### Dispatcher une tâche

Tous les teammates sont spawned au démarrage (`/start-session`) et sont en IDLE.
**Pendant la session : uniquement `SendMessage` — jamais de spawn.**

```
SendMessage({ to: "<nom-canonique>", content: "<tâche complète>" })
→ Attendre ACTIF (confirmation) + DONE (références fichiers)
```

**Ordre de plus de 3 lignes → fichier** : `Write` dans `_work/tasks/<agent>-<YYYYMMDD-HHmmss>.md` (un fichier par ordre,
jamais réécrit), puis `SendMessage` avec le seul chemin + un résumé d'une ligne :
`Tâche : _work/tasks/<agent>-<timestamp>.md — <résumé>`. Le fichier liste périmètre, fichiers à lire et livrables
attendus. Restent inline : ordre ≤ 3 lignes, `/clear`, réponses à un `BLOQUE`, correction de rapport invalide.
Relire l'ordre (`_work/tasks/*.md`) pour contrôler le `DONE` par rapport à la demande.

Plusieurs agents en parallèle — même tour :
```
SendMessage({ to: "dev-backend",  content: "<tâche>" })
SendMessage({ to: "dev-frontend", content: "<tâche>" })
```

### Nommage des Agents — Règle Absolue

Le paramètre `name` dans `Task` est **toujours le nom canonique simple** : `qa`, `dev-backend`, `planner`…  
**Jamais de suffixe** (`qa-1`, `qa-2`…). Un rôle = un nom = une adresse `SendMessage` permanente.

**Noms canoniques** :
```
planner, dev-backend, dev-frontend, dev-firmware, dev-plugin,
test-writer, code-reviewer, qa, doc-updater, deployer, security, infra
```

### Questions à l'utilisateur

**Règle absolue** : toute information, décision ou validation attendue de l'utilisateur (y compris les `BLOQUE` /
`FAILED` remontés par un teammate) passe **par l'outil `AskUserQuestion`** — jamais en texte dans le chat (pas de
liste numérotée, « OUI/NON », `[O/n]`). Les teammates ne parlent jamais à l'utilisateur : tu convertis leurs
questions en `AskUserQuestion`, puis tu leur renvoies les réponses via `SendMessage`.
Procédure, format et checklist : `.claude/agents/teamleader.template.md`, section « Questions à l'utilisateur ».

### Relayer l'avancement

Chaque jalon `[NOM] EN COURS — …` d'un teammate (ex. `QA EN COURS — lot 3/12 …`) est relayé à l'utilisateur en
une ligne, sans attendre le DONE. Un jalon n'est pas un DONE : ne pas enchaîner avant le DONE. Si un jalon signale des échecs, les nommer dans le relais.

### Labels d'issues — à chaque changement de phase

Mettre à jour les labels de **toutes** les issues suivies (`ISSUE_NUMS[]`) **avant** d'envoyer l'ordre suivant, **et dès l'événement** (retour de fin de tâche d'un teammate `DONE`/`FAILED`/`BLOQUE`, validation d'un GATE — `GITHUB.md` §9.12), avant de le relayer. L'état dit ce qui **reste à faire** (ex. `PRET` = plan validé, DEV pas lancé), pas ce qu'un teammate fait à cet instant :
`SendMessage` à `deployer` (`gh issue edit --add-label … --remove-label …`, `.claude/commands/context/GITHUB.template.md` §9).
La clôture du milestone reste soumise à la validation utilisateur.

### Validation des rapports DONE

Un `DONE` valide ne contient **jamais** de contenu inline (code, diff, extraits).  
Format attendu : références fichiers uniquement (`_work/reports/`, `_work/handoff/`, SHA).

Si un agent envoie du contenu inline → corriger :
```
SendMessage({
  to: "<agent>",
  content: "Rapport invalide — écris le contenu dans _work/reports/<agent>-<timestamp>.md et renvoie le DONE avec la référence."
})
```

<!-- END TEAMLEADER_PROTOCOL -->
