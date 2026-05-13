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
| Backend / moteur | Go 1.24.0 (toolchain 1.24.2) |
| UI framework | Wails v2 |
| Frontend | React + TypeScript |
| Placeholders Windows | Cloud Filter API + WinFsp |
| Plugins backends | Interface Go (compile) |
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
| `Edit`, `Write`, `MultiEdit` | `dev-*`, `doc-updater` |
| `Bash` (build / test / git) | `qa`, `deployer`, `dev-*` |
| `Read` (code applicatif) | `code-reviewer`, `planner` |
| `Glob`, `Grep` (recherche code) | `planner`, `dev-*` |

**`Read` autorisé uniquement pour** : `CLAUDE.md`, `MEMORY.md`, `project-config.json`, `workflow-state.json`, `_work/handoff/*.md`, `_work/reports/*.md`, `contracts/CHANGELOG.md`

Si un agent ne répond pas au PING → la boucle de supervision spawne via `Task` au cycle suivant (≤ 60s). **Ne jamais** exécuter la tâche soi-même.

### Protocole PING — Activation sans ScheduleWakeup

**Aucune attente, aucun ScheduleWakeup ad-hoc.** La boucle de supervision (60s) ramasse les non-répondants.

```
Étape 1 — Envoyer tous les PINGs dans le même bloc de réponse :
  SendMessage({to: "<agent1>", content: "PING"})
  SendMessage({to: "<agent2>", content: "PING"})
  … (tous les agents à activer)

Étape 2 — Écrire immédiatement dans workflow-state.json pour chaque agent pingé :
  status = "ping_pending", ping_sent_at = <ISO>, pending_order = "<ordre à dispatcher après activation>"

Étape 3 — Sur chaque réponse "<NOM> ACTIF" reçue :
  → workflow-state.json : status = "idle", ping_sent_at = null, pending_order = null
  → Dispatcher l'ordre via SendMessage immédiatement

(Pas de ScheduleWakeup — la boucle de supervision gère les ping_pending expirés à chaque cycle)
```

### Nommage des Agents — Règle Absolue

Le paramètre `name` dans `Task` est **toujours le nom canonique simple** : `qa`, `dev-backend`, `planner`…  
**Jamais de suffixe** (`qa-1`, `qa-2`…). Un rôle = un nom = une adresse `SendMessage` permanente.

Si le système impose un suffixe → l'agent précédent tourne encore → envoyer PING au nom simple ; au timeout s'il ne répond pas → c'est qu'il est bloqué, forcer via `TaskStop` puis re-spawn.

### Restauration après compactage de contexte

Après un compactage, un hook `UserPromptSubmit` ré-injecte automatiquement `workflow-state.json`. **À réception de ce bloc, re-vérifier tous les agents actifs via PING** :

**Étape 1** — Pour chaque agent listé, écrire `ping_sent_at: <ISO>`, `status: "ping_pending"` et envoyer le PING dans le même bloc :
```
Pour chaque agent présent dans workflow-state.json :
  → workflow-state.json : status = "ping_pending", ping_sent_at = <ISO>
     (conserver pending_order = null si l'agent était idle, ou le dernier ordre connu si working)
  → SendMessage({to: "<agent>", content: "PING"})
```

**Étape 2** — Sur chaque réponse `<NOM> ACTIF` reçue : `status = "idle"`, `ping_sent_at = null`. Agent confirmé vivant.

**Étape 3** — La boucle de supervision (déjà active) traitera les non-répondants à son prochain cycle (≤ 60s) : agents toujours `ping_pending` avec `ping_sent_at` expiré → spawn ou retrait.

### Workflow-state.json — Source de Vérité

Écrire **immédiatement sur disque** à chaque événement (jamais en mémoire) :

| Événement | Mise à jour |
|-----------|-------------|
| Envoi PING | `status: "ping_pending"`, `ping_sent_at: <ISO>`, `pending_order: "<ordre>"` |
| Réception ACTIF (réponse PING) | `status: "idle"`, `ping_sent_at: null`, `pending_order: null` |
| Expiration PING (boucle, ≥ 60s) | spawn via Task + dispatch `pending_order`, `status: "working"`, `ping_sent_at: null` |
| Dispatch (SendMessage de travail) | `status: "working"`, `last_order_sent_at: <ISO>`, `idle_since: null` |
| Réception DONE | `status: "idle"`, `idle_since: <ISO>` |
| Réception `PONG(WORKING\|IDLE)` | `status` correspondant, `last_pong_at: <ISO>` |
| Envoi `shutdown_request` | `status: "pending_delete"` |
| Réception `shutdown_response` | supprimer l'entrée agent |
| `TaskStop` (boucle, non-répondant PING-STATUS) | supprimer l'entrée agent |

Format minimal :
```json
{
  "watchdog_active": false,
  "ping_status_sent_at": null,
  "agents": {
    "<nom>": { "status": "working|idle|ping_pending|pending_delete", "last_order_sent_at": "<ISO>", "idle_since": null, "ping_sent_at": null, "pending_order": null, "last_pong_at": null }
  }
}
```

### Boucle de Supervision — Singleton

- Prérequis : `project-config.json` absent → skip (pas de team)
- Une seule boucle (`watchdog_active` = garde), **60s par cycle**
- Gère en un seul endroit : expirations PING, TTL idle, pending_delete, liveness PING-STATUS

### Activation des Agents (démarrage de workflow)

**Temps 1** — Activer `planner` (PING → ping_pending dans JSON | ACTIF → dispatch immédiat | expiration → boucle spawne)  
**Temps 2** — Après rapport planner, activer en parallèle les agents du scope détecté

Scope → agents dev concernés + `test-writer` + `code-reviewer` + `qa` + `doc-updater` + `deployer`  
Exception HOTFIX : pas de planner, activer directement dev-* + deployer  
Exception SECU : uniquement `security`

**Prompt obligatoire pour tout `Task` de spawn** (première activation ou re-spawn) :
```
"Lis .claude/agents/context/TEAMMATES_PROTOCOL.md puis .claude/agents/<nom>.template.md,
 puis .claude/agents/<nom>.md si ce fichier existe (adaptations projet).
 Tu fais partie de {TEAM_NAME} sur {PROJECT_NAME}.
 Reste en mode IDLE et attends mes ordres."
```
Un agent spawné sans cette ligne ne connaît pas le protocole et répondra en inline.

### Validation des rapports DONE

Un `DONE` valide ne contient **jamais** de contenu inline (code, diff, extraits).  
Format attendu : références fichiers uniquement (`_work/reports/`, `_work/handoff/`, SHA).

Si un agent envoie du contenu inline → refuser et corriger :
```
SendMessage({
  to: "<agent>",
  content: "Rapport invalide — aucun contenu inline autorisé. Écris le contenu dans _work/reports/<agent>-<timestamp>.md et renvoie le DONE avec la référence uniquement."
})
```
Ne jamais accepter un DONE inline comme valide — relancer jusqu'au format correct.

<!-- END TEAMLEADER_PROTOCOL -->
