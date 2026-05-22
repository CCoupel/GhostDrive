# MEMORY.md — Memoire Projet GhostDrive

> **Usage** : Source de vérité pour démarrer une nouvelle session.
> Mis à jour par `/end-session`.
> **IMPORTANT** : Pour l'état du backlog (issues, milestone, assignees), la source de vérité est **GitHub** — toujours interroger l'API au démarrage (`gh issue list` + `gh api milestones`), ne pas se fier à ce fichier.

---

## Projet

| Paramètre | Valeur |
|-----------|--------|
| Nom | `GhostDrive` |
| Repository | `https://github.com/CCoupel/GhostDrive` |
| Version actuelle (PROD) | `2.1.0` |
| Dernière mise à jour | `2026-05-22` |

---

## Versions

| Environnement | Version | Date deploy |
|---------------|---------|-------------|
| Production (GitHub Release) | `2.1.0` | `2026-05-22` |
| En développement | — | — |

---

## Travail en Cours

**Milestone actif** : `v2.2` — Workflow Objets

Issues v2.2 (ouvertes) :
- `#129` — feat(vfs): overlay badges ☁️ (sparse MSIX package identity requis)
- `#139` — feat(vfs): rename/move natif CF API
- `#140` — feat(vfs): CfSetPinState (Pin/Unpin)
- `#141` — fix(vfs): conflit CF — deux clients écrivent simultanément
- `#142` — feat(vfs): état Copier (CopyFrom CF API)
- `#143` — feat(vfs): avertissement conflit avant écrasement
- `#144` — fix(vfs): sync dossier récursif manquant

> **Backlog** : interroger GitHub au démarrage (`gh issue list` + `gh api milestones`) — ne pas se fier à ce fichier pour les issues ouvertes.

---

## Architecture

### Stack Technique

| Composant | Technologie | Version |
|-----------|-------------|---------|
| Backend | Go + Wails v2 | 1.24.0 (toolchain 1.24.2) |
| Frontend | React + TypeScript | Vite 5 |
| Plugins V1 | WebDAV + MooseFS | — |
| CI/CD | GitHub Actions | versions via `.ci-versions.env` |

### Fichiers Clés

| Fichier | Rôle |
|---------|------|
| `config.json` | Config runtime + **version** (source de vérité absolue) |
| `frontend/package.json` | Version frontend (synchronisée avec config.json) |
| `CHANGELOG.md` | Historique — source des release notes GitHub |
| `.github/workflows/ci.yml` | CI légère push/PR |
| `.github/workflows/build.yml` | Pipeline complet sur tag v* |
| `plugins/plugin.go` | Interface `StorageBackend` + sentinelles d'erreur |
| `docs/plugin-development.md` | Guide développeur plugin |
| `.ci-versions.env` | Versions outils pinnées (QUALIF @latest → PROD pinnée) |

---

## Décisions Techniques

| Décision | Raison | Date |
|----------|--------|------|
| Badges overlay ☁️ déférés à v2.2 avec sparse MSIX | `StorageProviderSyncRootManager::Register` requiert package identity — APPMODEL_ERROR_NO_PACKAGE sans MSIX | 2026-05-22 |
| libcldapi.a généré via cldapi.def bundlé + dlltool + ole32 en CI | Runner Linux n'a pas cldapi.dll — 31 exports définis statiquement dans cldapi.def | 2026-05-22 |
| Convention milestones `vX.Y` (sans Z) | Z = compteur de build, milestone = groupe d'issues pour une release Y | 2026-05-22 |
| Matrice états×actions documentée dans docs/sync-states-actions.md | 9 états × 10 actions — 60% couverture v2.1, gaps tracés v2.2/v2.3 | 2026-05-22 |
| GitHub = source de vérité backlog | MEMORY.md devient stale entre sessions | 2026-04-23 |
| 3 sources de vérité version : config.json + frontend/package.json + tag git | Doivent être synchronisées avant tout tag — aucune n'est seule souveraine | 2026-04-18 |
| workflow_call : ci.yml réutilisé dans build.yml | Pas de duplication de la définition des tests | 2026-04-18 |
| first-event-wins dans watcher debounce | create+write sur Linux/WSL2 reportait "modified" au lieu de "created" | 2026-04-18 |
| Plugins = interfaces Go compilées (pas .dll/gRPC) | Pragmatique pour projet solo | 2026-04-15 |

---

## Règles Critiques du Projet

- **GitHub est la source de vérité pour le backlog** : interroger `gh issue list` + `gh api milestones` au démarrage
- **Contract-first** : créer les contrats dans `contracts/` AVANT le code
- **Plugin-first** : toute interaction backend passe par `StorageBackend`
- **Jamais de push sur main** sans CI verte et QA validée
- **Version** : `config.json` + `frontend/package.json` + tag git doivent être synchronisés avant un tag
- `frontend/package-lock.json` commité (reproductibilité CI)
- `coverage.out` et `.remember/` ne pas commiter
- **QUALIF** : binaire wails build (≥10 MB frontend inclus) + tous les plugins `.ghdp` à rebuilder
- **Plugins** : `.ci-versions.env` maintenu à chaque QUALIF (QUALIF @latest → PROD pinnée)
- **CF API cross-compile** : `CGO_LDFLAGS=-lcldapi -lole32` + `libcldapi.a` via `cldapi.def` bundlé (31 exports) + `sudo dlltool` en CI
- **IID_IAsyncInfo** : ne pas utiliser `-lruntimeobject` de MinGW — définir `static const GUID IID_GHD_IAsyncInfo` dans le .c
- **Badges v2.2** : sparse MSIX package (`Microsoft.Windows.AppSDK`) pour `StorageProviderSyncRootManager::Register` — sans MSIX → APPMODEL_ERROR_NO_PACKAGE
