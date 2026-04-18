# Contrats — Wails Bindings (Référence Condensée)

> **Version** : 0.1.0  
> **Détail complet** : voir `wails-bindings.md` et `wails-events.md`  
> **Types** : voir `types.go` (Go) et `typescript-types.ts` (TypeScript)

---

## Méthodes exposées (Go → Frontend via `window.go.App.*`)

| Méthode | Signature Go | Retour |
|---------|-------------|--------|
| `GetConfig` | `GetConfig() AppConfig` | Configuration complète |
| `SaveConfig` | `SaveConfig(AppConfig) error` | – |
| `AddBackend` | `AddBackend(BackendConfig) (BackendConfig, error)` | Config avec ID généré |
| `RemoveBackend` | `RemoveBackend(id string) error` | – |
| `TestBackendConnection` | `TestBackendConnection(BackendConfig) (BackendStatus, error)` | Statut connexion |
| `GetSyncState` | `GetSyncState() SyncState` | État sync courant |
| `StartSync` | `StartSync(backendID string) error` | – |
| `StopSync` | `StopSync(backendID string) error` | – |
| `PauseSync` | `PauseSync(backendID string) error` | – |
| `ForceSync` | `ForceSync(backendID string) error` | – |
| `ListFiles` | `ListFiles(backendID, path string) ([]FileInfo, error)` | Liste de fichiers |
| `DownloadFile` | `DownloadFile(backendID, remotePath string) error` | – |
| `OpenSyncFolder` | `OpenSyncFolder(backendID string) error` | – |
| `GetCacheStats` | `GetCacheStats() CacheStats` | Stats cache |
| `ClearCache` | `ClearCache() error` | – |
| `GetBackendStatuses` | `GetBackendStatuses() []BackendStatus` | Statuts tous backends |
| `GetVersion` | `GetVersion() string` | "0.1.0" |
| `Quit` | `Quit()` | – |

---

## Événements (Go → Frontend via `runtime.EventsEmit`)

| Événement | Payload | Déclencheur |
|-----------|---------|-------------|
| `sync:state-changed` | `SyncState` | Changement d'état sync |
| `sync:progress` | `ProgressEvent` | Transfert en cours (max 10/s) |
| `sync:file-event` | `FileEvent` | Changement fichier détecté |
| `sync:error` | `SyncError` | Erreur de sync |
| `backend:status-changed` | `BackendStatus` | Connexion/déconnexion backend |
| `placeholder:hydration-started` | `{path, size}` | Ouverture fichier placeholder |
| `placeholder:hydration-done` | `{path}` | Téléchargement placeholder terminé |
| `app:ready` | `{version, backendsCount}` | App initialisée |

---

## Paramètres Backends

### WebDAV
```
url       : "https://nas.local/dav"
username  : "user"
password  : "***"
```

### MooseFS
```
mountPath : "/mnt/moosefs"   (chemin vers le montage FUSE existant)
```
