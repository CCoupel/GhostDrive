# Contrat — État de Synchronisation Temps Réel

> **Version** : 0.2.0
> **Issue** : #31 (vue état synchronisation en temps réel)
> **Règle** : Le backend émet ; le frontend écoute et affiche.

---

## Modèle BackendSyncState (nouveau v0.2.0)

État de synchronisation **par backend**, exposé dans `SyncState`.

```go
type BackendSyncState struct {
    BackendID   string      `json:"backendId"`
    BackendName string      `json:"backendName"`
    Status      SyncStatus  `json:"status"`      // "idle" | "syncing" | "paused" | "error"
    Progress    float64     `json:"progress"`     // 0.0 à 1.0
    CurrentFile string      `json:"currentFile"`
    Pending     int         `json:"pending"`
    Errors      []SyncError `json:"errors"`
    LastSync    time.Time   `json:"lastSync"`
}
```

## Modèle SyncState (étendu v0.2.0)

```go
type SyncState struct {
    // Agrégé global
    Status   SyncStatus `json:"status"`   // status le plus "grave" parmi tous les backends
    Progress float64    `json:"progress"` // moyenne pondérée

    // Par backend
    Backends []BackendSyncState `json:"backends"`

    // Transferts actifs (tous backends confondus)
    ActiveTransfers []ProgressEvent `json:"activeTransfers"`
}
```

**Logique d'agrégation du Status global** :
1. Si au moins un backend est en `error` → global = `error`
2. Sinon si au moins un est en `syncing` → global = `syncing`
3. Sinon si tous sont `paused` → global = `paused`
4. Sinon → global = `idle`

---

## Méthodes Wails — Synchronisation (#31)

### GetSyncState

```
Signature Go : GetSyncState() SyncState
Frontend     : window.go.App.GetSyncState()
Retour       : SyncState complet (tous backends + transfers actifs)
```

Appelé une fois au démarrage du composant pour initialiser l'état.

---

### StartSync

```
Signature Go : StartSync(backendID string) error
Frontend     : window.go.App.StartSync(backendId)
Erreur       : "not found: <id>" | "already running"
```

Démarre le moteur de sync pour un backend. Émet immédiatement `sync:state-changed`.

---

### StopSync

```
Signature Go : StopSync(backendID string) error
Frontend     : window.go.App.StopSync(backendId)
```

Arrête proprement (attend la fin du transfert courant). Émet `sync:state-changed`.

---

### PauseSync

```
Signature Go : PauseSync(backendID string) error
Frontend     : window.go.App.PauseSync(backendId)
```

Met en pause (ne démarre pas de nouveau transfert). Émet `sync:state-changed`.

---

### ForceSync

```
Signature Go : ForceSync(backendID string) error
Frontend     : window.go.App.ForceSync(backendId)
```

Déclenche une synchronisation complète immédiate (ignore les timestamps de dernière sync).

---

## Événements Wails — Temps Réel (#31)

### sync:state-changed

```
Nom     : "sync:state-changed"
Payload : SyncState (complet, tous backends)
Écoute  : EventsOn("sync:state-changed", handler)
```

Émis dès qu'un backend change de statut (idle→syncing, syncing→idle, error, etc.).

**Exemple** :
```json
{
  "status": "syncing",
  "progress": 0.35,
  "backends": [
    {
      "backendId": "uuid-abc",
      "backendName": "Mon NAS",
      "status": "syncing",
      "progress": 0.35,
      "currentFile": "documents/rapport.pdf",
      "pending": 8,
      "errors": [],
      "lastSync": "2026-04-18T10:00:00Z"
    }
  ],
  "activeTransfers": [
    {
      "path": "documents/rapport.pdf",
      "direction": "upload",
      "bytesDone": 512000,
      "bytesTotal": 1024000,
      "percent": 50.0
    }
  ]
}
```

---

### sync:progress

```
Nom     : "sync:progress"
Payload : ProgressEvent
Throttle : Max toutes les 100ms par fichier (côté Go)
```

```json
{
  "path": "videos/film.mkv",
  "direction": "download",
  "bytesDone": 52428800,
  "bytesTotal": 104857600,
  "percent": 50.0
}
```

---

### sync:error

```
Nom     : "sync:error"
Payload : SyncError + backendId
```

```json
{
  "backendId": "uuid-abc",
  "path": "documents/locked.docx",
  "message": "file is locked by another process",
  "time": "2026-04-18T10:31:05Z"
}
```

---

### sync:file-event

```
Nom     : "sync:file-event"
Payload : FileEvent + backendId
```

Émis à chaque fichier créé/modifié/supprimé détecté.

---

### sync:conflict-resolved

```
Nom     : "sync:conflict-resolved"
Payload : ConflictResolvedEvent
```

```json
{
  "backendId": "uuid-abc",
  "path": "documents/rapport.docx",
  "winner": "remote",
  "localModTime": "2026-04-18T09:00:00Z",
  "remoteModTime": "2026-04-18T09:05:00Z",
  "time": "2026-04-18T09:05:01Z"
}
```

---

## Comportement attendu côté Frontend (#31)

Le hook `useSyncStatus` doit :

1. **Au montage** : appeler `GetSyncState()` pour initialiser
2. **Écouter** : `sync:state-changed`, `sync:progress`, `sync:error`, `sync:file-event`
3. **Stocker les transferts actifs** dans un `Map<string, ProgressEvent>` (clé = `path`)
4. **Supprimer** un transfert de la Map quand `percent >= 100` ou que `sync:state-changed` indique `idle`
5. **Limiter l'historique d'erreurs** à 50 entrées maximum (FIFO)

### Affichage requis (SyncStatus.tsx)

| Élément | Condition d'affichage |
|---------|----------------------|
| Indicateur status global | Toujours visible (icône + texte) |
| Barre de progression globale | Quand `status === "syncing"` |
| Liste transferts actifs | Quand `activeTransfers.length > 0` |
| Fichier courant par backend | Quand `status === "syncing"` |
| Erreurs | Quand `errors.length > 0` |
| Dernière sync | Toujours (format relatif : "il y a 2 min") |
| Boutons Start/Stop/Pause/Force | Selon statut du backend |
