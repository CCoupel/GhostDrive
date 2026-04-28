# Changelog

All notable changes to GhostDrive will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] — 2026-04-28

### Added

- **Plugin loader dynamique** (go-plugin + gRPC) : chargement de plugins externes compilés depuis `<AppDir>/plugins/*.exe`
- **Bridge gRPC complet** couvrant toute l'interface `StorageBackend` (13 RPCs, streaming Upload/Download/Watch)
- **Registre dynamique** (`DynamicRegistry`) : scan de répertoire, watchdog avec backoff exponentiel (1s→2s→4s, 3 tentatives)
- **SDK Go pour développeurs de plugins** — template de référence + plugin echo fonctionnel
- **Bindings Wails** : `GetLoadedPlugins()`, `ReloadPlugins()`, `GetAvailableBackendTypes()` étendu aux plugins dynamiques
- **Événements Wails** : `plugin:loaded`, `plugin:reloaded`
- Fix `init()` d'auto-registration dans `webdav` et `moosefs`
- Sécurité : cap upload gRPC à 10 GB (`codes.ResourceExhausted`)

### Fixed

- `go.mod` : dépendances go-plugin/go-hclog/grpc/protobuf promues en directes

### Known Issues (v0.6.1)

- `context.Background()` sans timeout sur les appels lifecycle gRPC (MAJEUR-3)
- Watchdog restart n'invalide pas les instances `BackendManager` existantes (MAJEUR-4)
- `GRPCBackend.Version()` retourne `"unknown"` — `GetVersion` RPC prévu v0.6.1

## [0.5.0] — 2026-04-26

### Added

- **Drive virtuel GhD:** monté via WinFsp/cgofuse — les backends connectés apparaissent comme sous-dossiers (`GhD:\NomBackend\`) (#11, #52)
- **Navigation contenu distant** dans l'onglet "GhD:" — listage via `StorageBackend.List()`, breadcrumb, sélecteur de backend (#55)
- **Téléchargement à la demande** depuis GhD: avec progress bar inline — `StorageBackend.Download()` + `sync:progress` (#56)
- **Démontage propre** du drive GhD: à l'arrêt de l'application (#57)
- **Création automatique** du dossier racine `C:\GhostDrive\` au démarrage (#58)
- **Menu systray complet** — 3 nouvelles entrées : "Synchroniser maintenant", "Pause / Reprendre", "Paramètres" (#54)

### Technical

- Package `internal/placeholder/` : interface `VirtualDrive`, `WinFspDrive` (Windows), `NullDrive` (stub cross-platform)
- Dépendance cgofuse v1.6.0 (github.com/winfsp/cgofuse)
- Hook React `useDriveStatus` + composants `FileBrowserPage`, `RemoteFileList`
- `AppConfig.MountPoint` configurable — lettre de lecteur (`G:`) ou chemin répertoire (`C:\GhostDrive\GhD\`, défaut)
- **Prérequis runtime** : WinFsp 2.0+ — https://winfsp.dev/rel/
- Icône GhostDrive embarquée (`//go:embed ghostdrive.ico`) et servie comme fichier virtuel FUSE ; registre `DriveIcons` pour les lettres de lecteur (#71 — en cours, voir issue pour limitations connues)

## [0.4.0] — TBD

### Added

- Stabilisation interface `StorageBackend` — sentinelles partagées (`ErrNotConnected`, `ErrFileNotFound`) et Godoc complets sur tous les types et méthodes (#45)
- Template de plugin vierge (`plugins/template/template.go`) implémentant l'interface `StorageBackend` — base pour les nouveaux backends (#45)
- Guide complet d'implémentation de plugin (`docs/plugin-development.md`) — architecture, interface, conventions, tests, checklist PR (#45)
- Support du backend type `"local"` (v0.4.0) — paramètre `rootPath` documenté dans `contracts/backend-config.md` (#45, implémenté dans #47)
- `plugins/local` : nouveau backend LOCAL — synchronisation vers répertoire local ou monté (NAS, disque réseau) (#47)
- `plugins/registry` : registre dynamique de plugins (`Register`, `Get`, `ListBackends`) — remplace le switch hardcodé (#50)
- `internal/backends/manager` : délégation vers le registry pour `InstantiateBackend` et `AvailableTypes` (#50)
- Binding Wails `GetAvailableBackendTypes()` désormais alimenté dynamiquement par le registry (#50)
- `BackendConfig` enrichi : champ `LocalPath string` — point de synchronisation local configurable (#51)
- `AppConfig` enrichi : champ `GhostDriveRoot string` — racine GhostDrive configurable (défaut `C:\GhostDrive\`) (#51)
- Binding Wails `GetGhostDriveRoot()` — expose la racine au frontend (#51)
- `AddBackend()` mode Auto — `LocalPath` calculé automatiquement si vide (`<racine>\<nom>`) (#51)
- `SyncPointForm` restructuré en 2 zones Local/Remote — champ Nom avec preview temps réel, radio Auto/Manuel (#51)
- Champ `AutoSync bool` dans `BackendConfig` — contrôle le démarrage automatique de la synchronisation (default: false, opt-in) (#53)
- Bindings Wails `SetBackendEnabled(id, enabled)` et `SetAutoSync(id, autoSync)` — activation/désactivation persistée (#53)
- Boutons toggle Enabled (`Power`/`PowerOff`) et AutoSync (`RefreshCw`) sur chaque BackendConfigCard (#53)
- Indicateur 3 états sur les cards (gris: désactivé, vert: connecté, rouge: erreur) (#53)
- Badges "Désactivé" et "Manuel" sur les cards backend (#53)
- Navigation simplifiée — 2 vues uniquement : "Configuration" et "À propos" (#53)
- Page "À propos" avec version, préférences (autoStart, startMinimized) et gestion du cache (#53)

### Changed

- Navigation restructurée en 3 onglets — "Backends" (liste des backends + ajout), "Configuration" (démarrage + cache), "À propos" (version + vérification mises à jour)
- Page "À propos" avec vérification des mises à jour via GitHub Releases API

### Fixed

- Sécurité : protection contre le path traversal dans `plugins/local` — `absPath()` vérifie le containment avec `strings.HasPrefix` + `filepath.Clean` (#47)
- Race condition : capture atomique de `connected` + `rootPath` sous un seul `RLock` dans `plugins/local` (#47)
- `plugins/local` : erreurs fsnotify dans `Watch` désormais loggées au lieu d'être silencieusement ignorées (#47)
- Path traversal via `Name=".."` bloqué dans `validateBackendConfig` + containment check dans `AddBackend` (#51)
- `os.MkdirAll` déplacé après `validateBackendConfig` — plus de répertoires orphelins sur erreur (#51)
- `os.Stat(SyncDir)` tolère `ErrNotExist` en mode Auto — régression corrigée (#51)
- Boucle de rendu infinie dans `SyncPointForm` lors de la saisie d'un nom dupliqué — freeze complet de l'application (menus React + systray) résolu par guards anti-boucle et stabilisation des dépendances useEffect
- `useMemo` sur `existingNames` dans `SettingsPage` pour éviter les re-renders en cascade
- `useCallback` sur `onOpenSettings` dans `App.tsx` pour stabiliser l'écouteur Wails
- Libération de `e.mu` avant `Emit("sync:error")` dans `engine.go` — élimination d'un deadlock potentiel
- Initialisation des slices nil à `[]` dans les payloads Emit du sync engine — empêche la sérialisation JSON `null` qui causait un crash React au lancement de la synchronisation
- Normalisation défensive des payloads `sync:state-changed` dans `useSyncStatus` (`errors ?? []`, `backends ?? []`, `activeTransfers ?? []`)
- Ajout d'un `ErrorBoundary` global dans `main.tsx` — affiche un message d'erreur récupérable au lieu d'un écran vide en cas d'exception React non catchée
- `SetBackendEnabled` — `config.Save` déplacé après le succès de `manager.Add` (prévention incohérence disque/mémoire sur échec de reconnexion) (#53)
- Gestion d'erreur ajoutée sur les toggles Enabled/AutoSync — feedback inline `role="alert"` au lieu d'absorption silencieuse (#53)
- Suppression de la barre de menu Windows native de la fenêtre Wails (`buildTrayMenu` retiré de `main.go`)
- Systray simplifié — "Ouvrir GhostDrive" et "Quitter" uniquement (suppression des items Synchroniser / Pause / Paramètres)

### Tests

- 34 tests unitaires pour `plugins/local` — coverage 78.1% (#48)
- 7 tests pour `plugins/registry` — coverage 100% (#50)
- 14 tests unitaires pour `validateBackendConfig` et `AddBackend` auto-mode (`internal/app/app_test.go`) (#51)

---

## [0.3.0] - 2026-04-19

### Added

- CI : pipeline de tests frontend avec vitest (issue #32)
- CI : build Windows AMD64 via cross-compilation MinGW + wails build (issue #33)
- CI : publication automatique GitHub Release au push d'un tag vX.Y.Z (issue #34)
- CI : version Wails épinglée à v2.12.0 dans les workflows

### Fixed

- CI : suppression des dépendances GTK inutiles dans le step Windows AMD64
- CI : correction du mismatch merge-multiple/glob dans le job release (artifacts n'étaient pas attachés)

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

### Fixed
- Prevent multiple GhostDrive instances from launching simultaneously via named mutex `Local\GhostDrive` (Windows) and flock (Unix)
- Systray menu items Ouvrir, Paramètres, Quitter now functional — call `WindowShow`/`Quit` directly instead of relying on unhandled frontend events
- `App.Quit()` — add mutex RLock + nil check (consistent with `Emit()` and `Context()`)
- Remove dead `EventsOn("tray:open-window")` handler in `Startup` (Go-side listener never triggered by Go-emitted events)

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

[0.6.0]: https://github.com/CCoupel/GhostDrive/releases/tag/v0.6.0
[0.5.0]: https://github.com/CCoupel/GhostDrive/releases/tag/v0.5.0
[0.4.0]: https://github.com/CCoupel/GhostDrive/releases/tag/v0.4.0
[0.3.0]: https://github.com/CCoupel/GhostDrive/releases/tag/v0.3.0
[0.2.0]: https://github.com/CCoupel/GhostDrive/releases/tag/v0.2.0
[0.1.0]: https://github.com/CCoupel/GhostDrive/releases/tag/v0.1.0
