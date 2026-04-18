# Contrats — Modèles Partagés (Go ↔ Frontend)

> **Version** : 0.1.0  
> **Scope** : Types de données exposés via Wails v2 (JSON sérialisés)  
> **Règle** : Le backend PEUT ajouter des champs ; le frontend ne modifie pas ce fichier.

---

## FileInfo

Représente un fichier ou répertoire sur le backend distant ou local.

```go
type FileInfo struct {
    Name         string    `json:"name"`
    Path         string    `json:"path"`         // chemin relatif depuis la racine sync
    Size         int64     `json:"size"`
    IsDir        bool      `json:"isDir"`
    ModTime      time.Time `json:"modTime"`
    ETag         string    `json:"etag"`          // hash/etag backend pour détection de changement
    IsPlaceholder bool     `json:"isPlaceholder"` // true = fichier virtuel non téléchargé
    IsCached     bool      `json:"isCached"`      // true = présent dans le cache local
}
```

---

## FileEvent

Événement de changement détecté sur le backend ou local.

```go
type FileEvent struct {
    Type      FileEventType `json:"type"`      // "created" | "modified" | "deleted" | "renamed"
    Path      string        `json:"path"`
    OldPath   string        `json:"oldPath"`   // peuplé uniquement pour "renamed"
    Timestamp time.Time     `json:"timestamp"`
    Source    string        `json:"source"`    // "local" | "remote"
}

type FileEventType string

const (
    FileEventCreated  FileEventType = "created"
    FileEventModified FileEventType = "modified"
    FileEventDeleted  FileEventType = "deleted"
    FileEventRenamed  FileEventType = "renamed"
)
```

---

## SyncState

État global de synchronisation exposé au frontend.

```go
type SyncState struct {
    Status      SyncStatus `json:"status"`      // "idle" | "syncing" | "paused" | "error"
    Progress    float64    `json:"progress"`     // 0.0 à 1.0
    CurrentFile string     `json:"currentFile"`  // fichier en cours
    Pending     int        `json:"pending"`      // nombre de fichiers en attente
    Errors      []SyncError `json:"errors"`
    LastSync    time.Time  `json:"lastSync"`
}

type SyncStatus string

const (
    SyncIdle    SyncStatus = "idle"
    SyncSyncing SyncStatus = "syncing"
    SyncPaused  SyncStatus = "paused"
    SyncError   SyncStatus = "error"
)

type SyncError struct {
    Path    string `json:"path"`
    Message string `json:"message"`
    Time    time.Time `json:"time"`
}
```

---

## BackendConfig

Configuration d'un backend de stockage.

```go
type BackendConfig struct {
    ID       string            `json:"id"`       // UUID généré
    Name     string            `json:"name"`     // nom affiché (ex: "Mon NAS")
    Type     string            `json:"type"`     // "webdav" | "moosefs"
    Enabled  bool              `json:"enabled"`
    Params   map[string]string `json:"params"`   // paramètres spécifiques au backend
    SyncDir  string            `json:"syncDir"`  // répertoire local à synchroniser
}
```

Paramètres par type :

```
webdav:
  url      : "https://nas.local/dav"
  username : "user"
  password : "***"       // ne jamais logger

moosefs:
  master   : "192.168.1.1"
  port     : "9421"
  mountPath: "/mnt/moosefs"
```

---

## AppConfig

Configuration globale de l'application (stockée dans config.json).

```go
type AppConfig struct {
    Version        string          `json:"version"`
    Backends       []BackendConfig `json:"backends"`
    CacheEnabled   bool            `json:"cacheEnabled"`
    CacheDir       string          `json:"cacheDir"`
    CacheSizeMaxMB int             `json:"cacheSizeMaxMB"`
    StartMinimized bool            `json:"startMinimized"`
    AutoStart      bool            `json:"autoStart"`
}
```

---

## ProgressEvent

Événement de progression d'un transfert fichier.

```go
type ProgressEvent struct {
    Path      string  `json:"path"`
    Direction string  `json:"direction"` // "upload" | "download"
    BytesDone int64   `json:"bytesDone"`
    BytesTotal int64  `json:"bytesTotal"`
    Percent   float64 `json:"percent"`
}
```

---

## BackendStatus

État de connexion d'un backend.

```go
type BackendStatus struct {
    BackendID  string `json:"backendId"`
    Connected  bool   `json:"connected"`
    Error      string `json:"error"`      // vide si connected=true
    FreeSpace  int64  `json:"freeSpace"`  // -1 si non disponible
    TotalSpace int64  `json:"totalSpace"` // -1 si non disponible
}
```
