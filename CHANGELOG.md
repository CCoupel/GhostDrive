# Changelog

All notable changes to GhostDrive will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-04-18

### Added
- Icône systray et menu contextuel natif (Wails v2) — fenêtre cachée au lieu de quittée (#28)
- Page de configuration backends WebDAV et MooseFS avec validation et test de connexion (#29)
- Configuration des points de synchronisation (SyncDir local + RemotePath distant) (#30)
- Vue état de synchronisation en temps réel avec barres de progression par fichier (#31)
- `BackendSyncState` : état de sync individuel par backend (statut, progression, fichier courant, erreurs)
- `BackendManager` : gestionnaire du cycle de vie des backends (Add/Remove/Connect/Disconnect)
- Hook `useSyncStatus` : écoute temps réel des événements `sync:state-changed`, `sync:progress`, `sync:error`
- Hook `useBackends` : gestion CRUD backends + polling statut toutes les 10s
- Utilitaires `formatBytes` et `formatRelative` (formatage taille fichiers et dates relatives)
- 13 tests vitest, 9 tests Go supplémentaires (internal/config, internal/backends)

### Notes techniques
- `main.go` placé à la racine du projet (contrainte Wails v2 — co-localisé avec wails.json)
- API SystemTray absente de Wails v2.12.0 — menu tray implémenté via `options.Menu` + `HideWindowOnClose: true` (icône tray native prévue en v0.3.0)
- `SyncDir` : champ texte libre (`runtime.OpenDirectoryDialog` absent du runtime JS Wails v2)

## [0.1.0] - 2026-04-18

### Added
- Go module initialization with Wails v2 framework
- StorageBackend plugin interface (WebDAV, MooseFS)
- Internal types: SyncState, SyncError, BackendStatus, ProgressEvent, CacheStats
- Configuration management (Load/Save AppConfig, XDG/AppData path resolution)
- WebDAV storage backend with full StorageBackend implementation
- MooseFS storage backend via FUSE mount point
- Local file cache with LRU eviction
- Bidirectional sync engine with watcher, queue, reconciler, and dispatcher
- File watcher with 500ms debounce (fsnotify)
- Sync queue with exponential backoff retry (max 5 attempts)
- Conflict resolution: last-write-wins strategy (V1)
- Placeholder manager interface (Files On-Demand)
- Linux placeholder fallback (.ghost files)
- Wails App bindings: GetConfig, SaveConfig, AddBackend, RemoveBackend, TestBackendConnection
- Wails App bindings: GetSyncState, StartSync, StopSync, PauseSync, ForceSync
- Wails App bindings: ListFiles, DownloadFile, OpenSyncFolder
- Wails App bindings: GetCacheStats, ClearCache, GetBackendStatuses, GetVersion, Quit
- Wails events: sync:state-changed, sync:progress, sync:file-event, sync:error
- Wails events: backend:status-changed, app:ready
- Backend manager for lifecycle management of storage backends
- `internal/sync/upload.go` — bidirectional local→backend upload with progress events (sync:progress) and 100ms throttle (#6)
- `internal/sync/download.go` — backend→local download with atomic write (tmp+rename) and progress events (#7)
- `internal/sync/conflict.go` — last-write-wins conflict resolution with sync:conflict-resolved event and sync.log journal (#8)
- `contracts/wails-events.md` — new event sync:conflict-resolved (path, winner, localModTime, remoteModTime, time) (#8)

### Fixed
- Path traversal vulnerability in Upload/Download (filepath.Clean + prefix check) (#9)
- Dispatcher now routes all downloads through the atomic write wrapper (#7)
- Deadlock in Pause()/Resume() caused by double-lock on mu (#6)
- Naming collision between SyncError constant and SyncErrorInfo struct — struct renamed to SyncErrorInfo (#6)
- Removed dead code resumeCh/pauseCh channels (#6)
- Watcher debounce now uses first-event-wins: `create+write` sequence correctly reports `created` instead of `modified` on Linux/WSL2 (#4)
- Missing `frontend/src/main.tsx` entry point added (Vite build was failing in CI)

### CI/CD
- `ci.yml` — pipeline léger sur chaque push/PR : go vet, go build, go test (seuil 70%), frontend build
- `build.yml` — pipeline complet sur tag `v*` : CI → inject version → cross-compile (windows/amd64 + linux/arm64) → GitHub Release
- Le tag est la source de vérité de version : `config.json` et `frontend/package.json` patchés automatiquement au build
- `ci.yml` réutilisé dans `build.yml` via `workflow_call` (pas de duplication)
- Actions upgradées vers les versions compatibles Node.js 24 : checkout@v6, setup-go@v6, setup-node@v6, upload-artifact@v7, download-artifact@v8

[0.1.0]: https://github.com/CCoupel/GhostDrive/releases/tag/v0.1.0
