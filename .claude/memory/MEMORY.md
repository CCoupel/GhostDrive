# MEMORY.md — Memoire Projet

> **Usage** : Ce fichier est la source de verite pour demarrer une nouvelle session.
> Mis a jour par `/end-session`.

---

## Projet

| Parametre | Valeur |
|-----------|--------|
| Nom | `GhostDrive` |
| Repository | `https://github.com/CCoupel/GhostDrive` |
| Version actuelle | `0.1.0` |
| Derniere mise a jour | `2026-04-18` |

---

## Versions

| Environnement | Version | Date deploy |
|---------------|---------|-------------|
| Production (GitHub Release) | `0.1.0` | `2026-04-18` |
| En developpement | `0.2.0` | — |

---

## Travail en Cours

### Phase Actuelle

**Phase** : IDLE — v0.1.0 livre, pret pour v0.2.0
**Branche** : `main` (feat/sync-engine-v0.1.0 mergee et close)
**Description** : Milestone v0.1.0 complete — moteur de sync Go livré en prod

### Issues GitHub Actives (v0.2.0)

| # | Titre | Labels |
|---|-------|--------|
| #28 | Icône systray + menu contextuel (Wails) | frontend |
| #29 | Page configuration backend (WebDAV / MooseFS) | frontend |
| #30 | Page configuration points de synchronisation | frontend |
| #31 | Vue état synchronisation en temps réel | frontend |
| #46 | ci: upgrade softprops/action-gh-release (Node.js 24) | infra |

### Prochaines Etapes

1. Creer branche `feat/ui-v0.2.0`
2. Implémenter systray + pages config (issues #28-#31)
3. Corriger `softprops/action-gh-release@v1` → v2 (#46)

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
| `config.json` | Config runtime (version patchée par CI au tag) |
| `frontend/package.json` | Version frontend (patchée par CI au tag) |
| `CHANGELOG.md` | Historique — source des release notes GitHub |
| `.github/workflows/ci.yml` | CI légère push/PR |
| `.github/workflows/build.yml` | Pipeline complet sur tag v* |

---

## Decisions Techniques

| Decision | Raison | Date |
|----------|--------|------|
| Tag = source de vérité version | config.json + package.json patchés par CI au build | 2026-04-18 |
| workflow_call : ci.yml réutilisé dans build.yml | Pas de duplication de la définition des tests | 2026-04-18 |
| first-event-wins dans watcher debounce | create+write sur Linux/WSL2 reportait "modified" au lieu de "created" | 2026-04-18 |
| Plugins = interfaces Go compilées (pas .dll/gRPC) | Pragmatique pour projet solo | 2026-04-15 |
| Frontend Wails (pas systray manuel) | Meilleure DX, installer Windows intégré | 2026-04-15 |
| Tests backends : serveurs in-memory | Pas d'infra réelle requise en CI | 2026-04-15 |

---

## Regles Critiques du Projet

- Contract-first : créer les contrats dans `contracts/` AVANT le code
- Plugin-first : toute interaction backend passe par `StorageBackend`
- Jamais de push sur main sans CI verte
- Le tag git est la seule source de vérité pour la version
- `frontend/package-lock.json` commité (reproductibilité CI)
- `coverage.out` et `.remember/` ne pas commiter

---

## Points d'Attention

- `softprops/action-gh-release@v1` dépréciée Node.js 20 → corriger en v0.2.0 (#46)
- `cmd/ghostdrive/main.go` non implémenté → build Wails pas encore actif (TODO dans build.yml)
- v0.2.0 = premier milestone avec UI Wails → tester le build Wails en CI (libgtk-3-dev requis)
