# MEMORY.md — Memoire Projet

> **Usage** : Ce fichier est la source de verite pour demarrer une nouvelle session.
> Mis a jour par `/end-session`.
> **IMPORTANT** : Pour l'etat du backlog (issues, milestone, assignees), la source de verite est **GitHub** — toujours interroger l'API au demarrage, ne pas se fier a ce fichier pour les issues.

---

## Projet

| Parametre | Valeur |
|-----------|--------|
| Nom | `GhostDrive` |
| Repository | `https://github.com/CCoupel/GhostDrive` |
| Version actuelle | `0.3.0` |
| Derniere mise a jour | `2026-04-23` |

---

## Versions

| Environnement | Version | Date deploy |
|---------------|---------|-------------|
| Production (GitHub Release) | `0.1.0` | `2026-04-18` |
| En developpement | `0.4.0` | — |

---

## Travail en Cours

### Phase Actuelle

**Phase** : DEV — v0.4.0 plugin LOCAL en cours
**Branche** : `feat/v0.4.0-plugin-local`
**Description** : Interface StorageBackend + template + docs livres (#45 ferme) — reste implementation LOCAL, tests et registry

> **Backlog** : interroger GitHub au demarrage (`gh issue list` + `gh api milestones`) — ne pas se fier a ce fichier pour les issues ouvertes.

---

## Architecture

### Stack Technique

| Composant | Technologie | Version |
|-----------|-------------|---------|
| Backend | Go + Wails v2 | 1.21 |
| Frontend | React + TypeScript | Vite 5 |
| Plugins V1 | WebDAV + MooseFS | — |
| CI/CD | GitHub Actions | — |

### Fichiers Cles

| Fichier | Role |
|---------|------|
| `config.json` | Config runtime (version patchee par CI au tag) |
| `frontend/package.json` | Version frontend (patchee par CI au tag) |
| `CHANGELOG.md` | Historique — source des release notes GitHub |
| `.github/workflows/ci.yml` | CI legere push/PR |
| `.github/workflows/build.yml` | Pipeline complet sur tag v* |
| `plugins/plugin.go` | Interface StorageBackend + sentinelles d'erreur |
| `plugins/template.go` | Template plugin commenté |
| `docs/plugin-development.md` | Guide developpeur plugin |

---

## Decisions Techniques

| Decision | Raison | Date |
|----------|--------|------|
| GitHub = source de verite backlog | MEMORY.md devient stale entre sessions — toujours lire l'API GitHub au demarrage | 2026-04-23 |
| Tag = source de verite version | config.json + package.json patches par CI au build | 2026-04-18 |
| workflow_call : ci.yml reutilise dans build.yml | Pas de duplication de la definition des tests | 2026-04-18 |
| first-event-wins dans watcher debounce | create+write sur Linux/WSL2 reportait "modified" au lieu de "created" | 2026-04-18 |
| Plugins = interfaces Go compilees (pas .dll/gRPC) | Pragmatique pour projet solo | 2026-04-15 |
| Frontend Wails (pas systray manuel) | Meilleure DX, installer Windows integre | 2026-04-15 |
| Tests backends : serveurs in-memory | Pas d'infra reelle requise en CI | 2026-04-15 |

---

## Regles Critiques du Projet

- **GitHub est la source de verite pour le backlog** : interroger `gh issue list` + `gh api milestones` au demarrage
- Contract-first : creer les contrats dans `contracts/` AVANT le code
- Plugin-first : toute interaction backend passe par `StorageBackend`
- Jamais de push sur main sans CI verte
- Le tag git est la seule source de verite pour la version
- `frontend/package-lock.json` commite (reproductibilite CI)
- `coverage.out` et `.remember/` ne pas commiter

---

## Points d'Attention

- `softprops/action-gh-release@v1` deprecie Node.js 20 → corriger en v0.4.0 ou apres (#46)
- v0.4.0 = milestone plugin LOCAL — issues #47, #48, #50 ouvertes (verifier sur GitHub)
- Registry (#50) requis avant binding Wails `ListBackends`
