# Contrats — Wails Bindings (Go → Frontend)

> **Version** : 0.1.0  
> **Framework** : Wails v2  
> **Usage frontend** : `window.go.App.<MethodName>(args)` → retourne une Promise  
> **Règle** : Le backend implémente ; le frontend consomme sans modifier ce fichier.

---

## Structure App (Go)

```go
type App struct {
    ctx context.Context
}
```

Toutes les méthodes ci-dessous sont sur `*App` et exposées via `wails:expose`.

---

## Configuration

### GetConfig

```
Signature : GetConfig() AppConfig
Frontend  : window.go.App.GetConfig()
Retour    : AppConfig (voir models.md)
Erreur    : –
```

Retourne la configuration complète de l'application.

---

### SaveConfig

```
Signature : SaveConfig(config AppConfig) error
Frontend  : window.go.App.SaveConfig(config)
Retour    : null en cas de succès
Erreur    : string décrivant l'erreur
```

Sauvegarde la configuration et redémarre les backends si nécessaire.

---

### AddBackend

```
Signature : AddBackend(config BackendConfig) (BackendConfig, error)
Frontend  : window.go.App.AddBackend(config)
Retour    : BackendConfig avec ID généré
Erreur    : string (validation, connexion impossible)
```

---

### RemoveBackend

```
Signature : RemoveBackend(backendID string) error
Frontend  : window.go.App.RemoveBackend(backendId)
```

---

### TestBackendConnection

```
Signature : TestBackendConnection(config BackendConfig) (BackendStatus, error)
Frontend  : window.go.App.TestBackendConnection(config)
Retour    : BackendStatus
```

Teste la connexion sans sauvegarder la config.

---

## Synchronisation

### GetSyncState

```
Signature : GetSyncState() SyncState
Frontend  : window.go.App.GetSyncState()
Retour    : SyncState (voir models.md)
```

---

### StartSync

```
Signature : StartSync(backendID string) error
Frontend  : window.go.App.StartSync(backendId)
```

Démarre la synchronisation pour un backend donné.

---

### StopSync

```
Signature : StopSync(backendID string) error
Frontend  : window.go.App.StopSync(backendId)
```

---

### PauseSync

```
Signature : PauseSync(backendID string) error
Frontend  : window.go.App.PauseSync(backendId)
```

---

### ForceSync

```
Signature : ForceSync(backendID string) error
Frontend  : window.go.App.ForceSync(backendId)
```

Déclenche une synchronisation complète immédiate (ignore les timestamps).

---

## Fichiers

### ListFiles

```
Signature : ListFiles(backendID string, path string) ([]FileInfo, error)
Frontend  : window.go.App.ListFiles(backendId, path)
Retour    : []FileInfo
```

Liste les fichiers sur le backend distant pour un chemin donné.

---

### DownloadFile

```
Signature : DownloadFile(backendID string, remotePath string) error
Frontend  : window.go.App.DownloadFile(backendId, remotePath)
```

Télécharge un fichier (hydrate un placeholder). Émet des événements `sync:progress`.

---

### OpenSyncFolder

```
Signature : OpenSyncFolder(backendID string) error
Frontend  : window.go.App.OpenSyncFolder(backendId)
```

Ouvre le dossier de synchronisation dans l'explorateur Windows.

---

## Cache

### GetCacheStats

```
Signature : GetCacheStats() CacheStats
Frontend  : window.go.App.GetCacheStats()
```

```go
type CacheStats struct {
    SizeMB    float64 `json:"sizeMB"`
    FileCount int     `json:"fileCount"`
    MaxSizeMB int     `json:"maxSizeMB"`
}
```

---

### ClearCache

```
Signature : ClearCache() error
Frontend  : window.go.App.ClearCache()
```

---

## Système

### GetAvailableBackendTypes

```
Signature : GetAvailableBackendTypes() []string
Frontend  : window.go.App.GetAvailableBackendTypes()
Retour    : []string — noms triés des plugins compilés (ex: ["local", "webdav"])
Erreur    : –
```

Retourne la liste des types de backends disponibles dans le binaire courant.
Utilisé par le frontend pour alimenter le sélecteur "Type" du formulaire d'ajout de backend.
La liste est dynamique : tout plugin qui appelle `plugins.Register` dans son `init()` apparaît ici.

---

### GetBackendStatuses

```
Signature : GetBackendStatuses() []BackendStatus
Frontend  : window.go.App.GetBackendStatuses()
Retour    : []BackendStatus
```

---

### SetBackendEnabled

```
Signature : SetBackendEnabled(id string, enabled bool) error
Frontend  : window.go.App.SetBackendEnabled(id, enabled)
Retour    : null en cas de succès
Erreur    : "not found: <id>"
```

Comportement :
- `enabled=false` : StopSync(id) → manager.Remove(id) → cfg.Backends[i].Enabled=false → Save
- `enabled=true`  : cfg.Backends[i].Enabled=true → Save → manager.Add(bc)
                    → si `AutoSync=true` : StartSync(id)
- Émet `backend:status-changed`

---

### SetAutoSync

```
Signature : SetAutoSync(id string, autoSync bool) error
Frontend  : window.go.App.SetAutoSync(id, autoSync)
Retour    : null en cas de succès
Erreur    : "not found: <id>"
```

Comportement :
- `autoSync=false` : cfg.Backends[i].AutoSync=false → Save → StopSync(id) si engine actif
- `autoSync=true`  : cfg.Backends[i].AutoSync=true → Save → StartSync(id) si backend connecté
- Émet `sync:state-changed`

---

### GetVersion

```
Signature : GetVersion() string
Frontend  : window.go.App.GetVersion()
Retour    : "0.1.0"
```

---

### Quit

```
Signature : Quit()
Frontend  : window.go.App.Quit()
```

Quitte l'application proprement.

---

## Drive Virtuel

> Détail complet dans `contracts/winfsp-bindings.md`.

### MountDrive

```
Signature : MountDrive() error
Frontend  : window.go.App.MountDrive()
```

Monte la lettre de lecteur `GhD:` via WinFsp. No-op si déjà monté.
Émet `drive:mounted` avec payload `DriveStatus` en cas de succès.

---

### UnmountDrive

```
Signature : UnmountDrive() error
Frontend  : window.go.App.UnmountDrive()
```

Démonte `GhD:` proprement. No-op si non monté.
Émet `drive:unmounted` après démontage réussi.

---

### GetDriveStatus

```
Signature : GetDriveStatus() DriveStatus
Frontend  : window.go.App.GetDriveStatus()
Retour    : DriveStatus (voir models.md)
```

Retourne l'état courant du drive virtuel.
Voir `contracts/models.md` pour la définition de `DriveStatus`.

---

### GetAvailableDriveLetters

```
Signature : GetAvailableDriveLetters() []string
Frontend  : window.go.App.GetAvailableDriveLetters()
Retour    : []string — lettres libres au format "X:" (ex: ["D:", "G:", "H:"])
```

Retourne la liste des lettres de lecteur Windows non utilisées (A–Z).
Utilise `GetLogicalDrives()` (syscall Windows) pour déterminer les lettres occupées.
Sur plateforme non-Windows, retourne `null`.

Utilisation typique : alimenter le sélecteur de lettre de lecteur dans la page Paramètres.
