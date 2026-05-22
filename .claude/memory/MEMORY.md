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

Aucune feature en cours au moment de cet audit.

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
| GitHub = source de vérité backlog | MEMORY.md devient stale entre sessions | 2026-04-23 |
| 3 sources de vérité version : config.json + frontend/package.json + tag git | Doivent être synchronisées avant tout tag — aucune n'est seule souveraine | 2026-04-18 |
| workflow_call : ci.yml réutilisé dans build.yml | Pas de duplication de la définition des tests | 2026-04-18 |
| first-event-wins dans watcher debounce | create+write sur Linux/WSL2 reportait "modified" au lieu de "created" | 2026-04-18 |
| Plugins = interfaces Go compilées (pas .dll/gRPC) | Pragmatique pour projet solo | 2026-04-15 |
| Frontend Wails (pas systray manuel) | Meilleure DX, installer Windows intégré | 2026-04-15 |
| Tests backends : serveurs in-memory | Pas d'infra réelle requise en CI | 2026-04-15 |

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
