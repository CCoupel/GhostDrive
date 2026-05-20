# Changelog des Contrats API

Ce fichier documente tous les changements de contrats API (Wails bindings, événements, modèles).
Les changements **BREAKING** doivent être validés par le CDP avant implémentation.

---

## [20260520] — v2.0 Plugin Events (#130 #131)

- **[NEW]** `FileInfo.Version string` — jeton de version opaque fourni par le backend (WebDAV=ETag, MooseFS=CTime décimal, local="") ; zero-value valide → aucun BREAKING CHANGE
- **[NEW]** `FileEvent.ModTime time.Time` — modtime courant du fichier au moment de la détection (évite un Stat() post-Watch)
- **[NEW]** `FileEvent.PreviousModTime time.Time` — modtime précédent (snapshot Watch) pour détection sans re-stat
- **[NEW]** `FileEvent.MetadataOnly bool` — true quand seules les métadonnées ont changé (ETag/mtime) sans changement de contenu
- **[NEW]** `FileEventMetadataChanged FileEventType = "metadata_changed"` — nouveau type d'événement Watch
- **[NEW]** `FileInfoProto.version` (proto field 20) — version opaque dans le transport gRPC
- **[NEW]** `FileEventProto.mod_time_unix` (proto field 6), `previous_mod_time_unix` (field 7), `metadata_only` (field 8)
- **[NEW]** Contrat `contracts/v2.0-plugin-events.md` — spécification complète

---

## [20260517] — v2.0 VFS Foundation (#120 #121)

- **[BREAKING]** `StorageBackend` interface — ajout de `ReadAt(ctx, remote, offset, length) ([]byte, error)` : tout plugin tiers doit l'implémenter pour compiler
- **[BREAKING]** `StorageBackend` interface — ajout de `ChunkSize() int64` : tout plugin tiers doit l'implémenter pour compiler
- **[BREAKING]** Drive WinFsp — architecture "un drive par backend" supprimée ; remplacée par un drive unique `GhD:` (configurable via `AppConfig.MountPoint`) avec un sous-dossier par backend
- **[BREAKING]** `GetDriveStatuses()` — retourne désormais une seule entrée `"unified"` au lieu d'une entrée par backendID
- **[NEW]** `rpc ReadAt (ReadAtRequest) returns (ReadAtResponse)` — RPC gRPC pour les range reads
- **[NEW]** `rpc ChunkSize (ChunkSizeRequest) returns (ChunkSizeResponse)` — RPC gRPC retournant la granularité naturelle du backend
- **[NEW]** `DriveManager.MountUnified(mountPoint, backends)` — monte le drive unifié GhD:
- **[NEW]** `DriveManager.UpdateBackends(backends)` — met à jour la liste de backends sans remonter le drive
- **[NEW]** `DriveStatus.BackendPaths` — mappe chaque backendID vers son chemin sous GhD: (ex: `"G:\\MonNAS\\"`)
- **[CHANGED]** `BackendConfig.MountPoint` — déprécié ; ignoré par la couche VFS v2.0 (conservé pour compatibilité JSON)
- **[CHANGED]** `AppConfig.MountPoint` — promu au rôle de point de montage global du drive unifié ; défaut `"G:"`
- **[NEW]** Contrat `contracts/v2.0-vfs-foundation.md` — spécification complète

---

## [20260516] — v1.8.0 / #114 EC4+1 MooseFS Go

- **[CHANGED]** `ChunkInfo.ECParts int` — nouveau champ interne (non-breaking, rétrocompatible)
  Positionné par `parseChunkInfo` quand proto=3 ; 0 pour chunks normaux
- **[CHANGED]** `parseChunkInfo` proto=3 — ne retourne plus d'erreur ; set `ECParts=4` ou `ECParts=8`
  (rétrocompatible : les chunks normaux proto=0/1/2 sont inchangés)
- **[NEW]** `ECPhysicalChunkID(logicalID uint64, partIdx int) uint64` — fonction interne mfsclient
- **[NEW]** `(c *Client) readEC4At(...)` — lecture shard-granulaire EC4+1 (interne)
- **[INTERNAL]** Constantes `EC4ECIDStart`, `EC4ECIDStep`, `EC8ECIDStart` ajoutées dans protocol.go

---

## [20260513] — v1.7.0 / #108 #109 #110

- **[BREAKING]** `GetDriveStatus()` — binding Wails supprimé (remplacé par `GetDriveStatuses()` depuis v1.1.x) ; le tray migre vers `GetDriveStatuses()` avec agrégation `LastError`
- **[NEW]** `meta:updated` — événement Wails émis par la goroutine `watchLoop` de `GhostFileSystem` à chaque `FileEvent` reçu de `Watch()` ; payload `MetaUpdatedEvent{backendID, path, eventType}`
- **[INTERNAL]** Cache LRU métadonnées `Stat`/`List` dans la couche VFS (`GhostFileSystem`) ; invalidation push via `Watch()` ; TTL 5 min fallback ; borné à 1 000 entrées
- **[BUGFIX]** `isCacheFresh()` : fichier 0-octet en cache local systématiquement considéré périmé (re-download forcé)

---

## [20260503] — v1.5.x MooseFS plugin (#26 #27 #92 #93)

- **[NEW]** Plugin `moosefs` — backend StorageBackend natif TCP ; binaire `ghostdrive-moosefs[.exe]` (issues #26 #27)
- **[CHANGED]** `volname` WinFsp — dynamique = `backends[0].Name` (fallback `"GhostDrive"` si vide) (issue #92)

---

## [20260502] — #90/#91 plugin extension + placement

- **[BREAKING]** Extension des plugins : `.exe` → `.ghdp` (GhostDrive Plugin)
- **[BREAKING]** Emplacement des plugins : sous-dossier `plugins/` supprimé — les plugins sont désormais placés à côté du binaire GhostDrive

---

## [20260502] — v1.1.x drive-par-backend (#85 #88 #89)

- **[BREAKING]** `AddBackend` — `enabled` forcé à `false` à la création (ignoré si `true` côté frontend)
- **[BREAKING]** `SetBackendEnabled` — gère désormais le mount/unmount du drive virtuel associé au backend
- **[BREAKING]** Drive GhD: global supprimé — remplacé par un drive virtuel indépendant par backend (#88)
- **[BREAKING]** `MountDrive()` et `UnmountDrive()` — bindings Wails supprimés ; utiliser `SetBackendEnabled()` à la place
- **[NEW]** `BackendConfig.mountPoint string` — lettre de lecteur (ex. `E:`) ou chemin absolu Windows ; auto-assigné si absent
- **[NEW]** `DriveStatus.backendID string` — identifiant du backend propriétaire du drive
- **[NEW]** `DriveStatus.backendName string` — nom lisible du backend (pour affichage UI)
- **[NEW]** `GetDriveStatuses() map[string]DriveStatus` — binding Wails, retourne le statut de chaque drive par `backendID`
- **[CHANGED]** `GetDriveStatus()` — marqué deprecated, conservé pour compatibilité v1.1.x ; retourne un DriveStatus vide
- **[CHANGED]** Events `drive:mounted`, `drive:unmounted`, `drive:error` — payload étendu avec `backendID`/`backendName` (non-breaking, nouveaux champs)
- **[CHANGED]** `GetQuota` erreur → `FreeSpace = -1` au lieu de `0` dans `ListStatuses()` et `TestBackendConnection()` (#89)
- **[NEW]** Contrat `contracts/PLAN_v1.1.x.md` — spécification complète v1.1.x

---

## [20260430] — v1.1.0 plugin-describe (#78 #79 #80)

- **[BREAKING]** `plugins.StorageBackend` — méthode `Describe() PluginDescriptor` ajoutée :
  tout plugin tiers doit l'implémenter pour compiler
- **[BREAKING]** `plugins/local` — suppression de `init()`/`Register()` :
  l'enregistrement est désormais explicite via `ServeInProcess` dans `app.Startup()`
- **[NEW]** `plugins.ParamType`, `plugins.ParamSpec`, `plugins.PluginDescriptor` — types Go
- **[NEW]** `rpc Describe (DescribeRequest) returns (DescribeResponse)` — RPC gRPC
- **[NEW]** `func ServeInProcess(impl StorageBackend) (*GRPCBackend, func(), error)` —
  transport in-process via bufconn pour les plugins statiques
- **[NEW]** `GetPluginDescriptors() []plugins.PluginDescriptor` — binding Wails
- **[NEW]** `ParamType`, `ParamSpec`, `PluginDescriptor` — types TypeScript dans `ghostdrive.ts`
- **[CHANGED]** `SyncPointForm.tsx` — Zone 2 (Remote) générée dynamiquement depuis les `ParamSpec`
  du plugin sélectionné (rétrocompatible fonctionnellement pour `local`)
- **[NEW]** Contrat `contracts/plugin-describe.md` — spécification complète

---

## [20260430] — v0.8.0 plugin-webdav

- **[CHANGED]** `contracts/backend-config.md` — params WebDAV étendus (rétro-compatible) :
  ajout `token`, `authType`, `pollInterval` (ms), `tlsSkipVerify` ; défauts preservés
- **[NEW]** Plugin dynamique gRPC `ghostdrive-webdav` — implémentation complète `StorageBackend`
  WebDAV : Basic Auth, Bearer Token, PROPFIND/PUT/GET/DELETE/MOVE/MKCOL, Watch polling, quota
- **[CHANGED]** `plugins/loader` — `loadPlugin` passe en mode factory : chaque `plugins.Get()`
  spawne un nouveau subprocess, permettant plusieurs backends WebDAV avec des configs différentes

---

## [20260428] — v0.6.0 plugin-loader

- **[NEW]** `GetLoadedPlugins() []PluginInfo` — liste des plugins dynamiques chargés depuis `<AppDir>/plugins/*.exe`
- **[NEW]** `ReloadPlugins() error` — rescan du dossier plugins sans redémarrage de l'app
- **[NEW]** Événements : `plugin:loaded`, `plugin:failed`, `plugin:restarting`, `plugin:reloaded`
- **[NEW]** Type `PluginInfo` — voir `contracts/plugin-loader-bindings.md`
- **[CHANGED]** `GetAvailableBackendTypes()` — inclut désormais les plugins dynamiques en plus des statiques (rétrocompatible, aucune modification frontend requise)
- **[NEW]** Contrat `contracts/plugin-loader-bindings.md` — spécification complète des bindings Wails plugin-loader
- **[NEW]** Plan `contracts/PLAN_v0.6.x.md` — plan d'implémentation v0.6.x plugin-loader
