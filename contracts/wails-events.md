# Contrats — Wails Events (Go → Frontend)

> **Version** : 0.1.0  
> **Framework** : Wails v2  
> **Émission Go** : `runtime.EventsEmit(ctx, "event:name", payload)`  
> **Écoute Frontend** : `EventsOn("event:name", (payload) => { ... })`  
> **Règle** : Le backend émet ; le frontend écoute. Ne pas modifier ce fichier côté frontend.

---

## sync:state-changed

Émis quand l'état global de synchronisation change.

```
Nom     : "sync:state-changed"
Payload : SyncState (voir models.md)
Fréquence : Sur changement d'état (idle→syncing, erreur, etc.)
```

**Exemple payload :**
```json
{
  "status": "syncing",
  "progress": 0.42,
  "currentFile": "documents/rapport.pdf",
  "pending": 12,
  "errors": [],
  "lastSync": "2026-04-16T10:30:00Z"
}
```

---

## sync:progress

Émis pendant le transfert d'un fichier (upload ou download).

```
Nom     : "sync:progress"
Payload : ProgressEvent (voir models.md)
Fréquence : Maximum toutes les 100ms par fichier (throttlé côté Go)
```

**Exemple payload :**
```json
{
  "path": "videos/film.mkv",
  "direction": "upload",
  "bytesDone": 52428800,
  "bytesTotal": 104857600,
  "percent": 50.0
}
```

---

## sync:file-event

Émis quand un changement de fichier est détecté (local ou distant).

```
Nom     : "sync:file-event"
Payload : FileEvent (voir models.md)
```

---

## sync:error

Émis quand une erreur de synchronisation se produit.

```
Nom     : "sync:error"
Payload : SyncError
```

**Exemple payload :**
```json
{
  "path": "documents/locked.docx",
  "message": "file is locked by another process",
  "time": "2026-04-16T10:31:05Z"
}
```

---

## backend:status-changed

Émis quand l'état de connexion d'un backend change.

```
Nom     : "backend:status-changed"
Payload : BackendStatus (voir models.md)
```

**Exemple payload :**
```json
{
  "backendId": "uuid-abc-123",
  "connected": false,
  "error": "connection refused",
  "freeSpace": -1,
  "totalSpace": -1
}
```

---

## placeholder:hydration-started

Émis quand l'utilisateur ouvre un fichier placeholder (déclenchement de téléchargement).

```
Nom     : "placeholder:hydration-started"
Payload : { "path": string, "size": number }
```

---

## placeholder:hydration-done

Émis quand un fichier placeholder est entièrement téléchargé.

```
Nom     : "placeholder:hydration-done"
Payload : { "path": string }
```

---

## app:ready

Émis une fois que l'application est initialisée et prête (backends connectés, moteur sync démarré).

```
Nom     : "app:ready"
Payload : { "version": string, "backendsCount": number }
```

---

## sync:conflict-resolved

Émis quand un conflit est détecté et résolu automatiquement (stratégie last-write-wins).

```
Nom     : "sync:conflict-resolved"
Payload : ConflictResolvedEvent
Fréquence : Sur chaque conflit résolu
```

**Exemple payload :**
```json
{
  "path": "documents/rapport.docx",
  "winner": "remote",
  "localModTime": "2026-04-18T09:00:00Z",
  "remoteModTime": "2026-04-18T09:05:00Z",
  "time": "2026-04-18T09:05:01Z"
}
```

---

## meta:updated

Émis par la goroutine `watchLoop` de `GhostFileSystem` à chaque `FileEvent` reçu de `Watch()`.
Indique qu'un chemin distant a changé et que le cache métadonnées VFS a été invalidé.

```
Nom     : "meta:updated"
Payload : MetaUpdatedEvent
Fréquence : Dépend du backend — polling 30s (MooseFS/WebDAV) ou push natif (futur)
```

**Payload** :
```json
{
  "backendID": "backend-uuid",
  "path":      "/documents/rapport.pdf",
  "eventType": "modified"
}
```

**Champs** :
| Champ | Type | Description |
|-------|------|-------------|
| `backendID` | `string` | Identifiant du backend qui a émis l'événement |
| `path` | `string` | Chemin distant affecté par le changement |
| `eventType` | `string` | `"created"`, `"modified"`, `"deleted"`, `"renamed"` |

**Usage frontend** : écouter `"meta:updated"` pour rafraîchir la vue du répertoire `path.dirname(path)`.
N'émet que si `Watch()` est disponible sur le backend — la dégradation gracieuse (TTL seul) ne produit pas d'événement.

---

## sync:conflict

Émis quand un conflit est détecté entre la version locale et distante d'un fichier.
Le conflit est auto-résolu (last-write-wins V1) — l'événement est informatif, non-bloquant.

```
Nom     : "sync:conflict"
Payload : ConflictEvent
Fréquence : Sur chaque conflit détecté lors de la réconciliation
```

**Payload** :
```json
{
  "backendID":     "uuid-abc",
  "path":          "documents/rapport.docx",
  "localModTime":  "2026-05-23T10:00:00Z",
  "remoteModTime": "2026-05-23T10:05:00Z",
  "resolution":    "local-wins"
}
```

**Champs** :
| Champ | Type | Description |
|-------|------|-------------|
| `backendID` | `string` | UUID du backend concerné |
| `path` | `string` | Chemin relatif du fichier en conflit |
| `localModTime` | `string` | ISO-8601 — date de modification locale |
| `remoteModTime` | `string` | ISO-8601 — date de modification distante |
| `resolution` | `string` | `"local-wins"` ou `"remote-wins"` |

**Usage frontend** : afficher un toast non-bloquant (auto-dismiss 5s) indiquant le conflit résolu.
**Issue** : #144

---

## sync:file-state-changed

Émis quand l'état d'un fichier change dans le cache `Engine.fileStates` (optionnel v2.2).

```
Nom     : "sync:file-state-changed"
Payload : FileStateChangedEvent
Fréquence : Sur chaque transition d'état (P→U→S, U→E, etc.)
```

**Payload** :
```json
{
  "backendID": "uuid-abc",
  "localPath": "C:\\GhostDrive\\MonNAS\\documents\\rapport.docx",
  "state":     "U"
}
```

**Champs** :
| Champ | Type | Description |
|-------|------|-------------|
| `backendID` | `string` | UUID du backend |
| `localPath` | `string` | Chemin local absolu du fichier |
| `state` | `string` | Nouvel état — voir `GetFileState` pour les valeurs |

**Issue** : #136 (optionnel v2.2 — implémentation du toast uniquement si temps disponible)

---

## Tableau Récapitulatif

| Événement | Émetteur | Consommateur | Fréquence |
|-----------|----------|--------------|-----------|
| `sync:state-changed` | SyncEngine | StatusPanel, TrayIcon | Sur changement |
| `sync:progress` | SyncEngine | StatusPanel | Max 10/s |
| `sync:file-event` | SyncEngine / Watcher | StatusPanel | Sur événement |
| `sync:error` | SyncEngine | StatusPanel, TrayIcon | Sur erreur |
| `sync:conflict` | SyncEngine (Reconciler) | StatusPanel, Toast | Sur conflit auto-résolu |
| `sync:file-state-changed` | SyncEngine | Badges UI | Sur transition état fichier |
| `backend:status-changed` | BackendManager | SettingsPanel, TrayIcon | Sur changement |
| `placeholder:hydration-started` | PlaceholderManager | StatusPanel | Sur ouverture |
| `placeholder:hydration-done` | PlaceholderManager | StatusPanel | Sur complétion |
| `sync:conflict-resolved` | SyncEngine | StatusPanel | Sur conflit résolu |
| `app:ready` | App.startup | Tous | 1x au démarrage |
| `meta:updated` | GhostFileSystem.watchLoop | FileListView | Sur événement Watch() |
