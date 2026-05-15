# WinFsp Bindings — GhostDrive

> **Version** : 0.5.0
> **Scope** : Drive virtuel GhD: via WinFsp — montage/démontage + état
> **Règle** : Le backend implémente ; le frontend consomme sans modifier ce fichier.

---

## MountDrive

```
Signature : MountDrive() error
Frontend  : window.go.App.MountDrive()
```

Monte le point de montage GhostDrive via WinFsp avec tous les backends connectés.
No-op si déjà monté (retourne nil).

Erreurs possibles :
- `"winfsp: driver not found"` — WinFsp non installé sur la machine
- `"winfsp: no connected backend"` — aucun backend connecté au moment du mount
- `"winfsp: create mount point <path>: ..."` — impossible de créer le répertoire de montage

---

## UnmountDrive

```
Signature : UnmountDrive() error
Frontend  : window.go.App.UnmountDrive()
```

Démonte `GhD:` proprement.
No-op si non monté (retourne nil).

---

## GetMountPoint

```
Signature : GetMountPoint() string
Frontend  : window.go.App.GetMountPoint()
Retour    : string — point de montage configuré (ex: `C:\GhostDrive\GhD\`)
```

Retourne le point de montage WinFsp configuré dans AppConfig.MountPoint.
Défaut : `C:\GhostDrive\GhD\` sous Windows.

---

## GetDriveStatus

```
Signature : GetDriveStatus() DriveStatus
Frontend  : window.go.App.GetDriveStatus()
Retour    : DriveStatus (voir contracts/models.md)
```

Retourne l'état courant du drive virtuel : monté ou non, point de montage,
mapping backendID → chemin sous le drive, et dernière erreur éventuelle.

---

## Événements Wails

| Événement       | Payload             | Déclencheur                        |
|-----------------|---------------------|------------------------------------|
| `drive:mounted`   | `DriveStatus`       | Après un mount réussi              |
| `drive:unmounted` | `{}`                | Après un unmount réussi            |
| `drive:error`     | `DriveStatus`        | Erreur lors d'un mount ou unmount (LastError renseigné) |

---

## Notes d'implémentation

- Le point de montage par défaut est `C:\GhostDrive\GhD\` (configurable via `AppConfig.MountPoint`)
- `MountPoint` peut être une lettre de lecteur (`G:`) ou un répertoire (`C:\GhostDrive\GhD\`)
- Si `MountPoint` est un chemin répertoire, WinFsp le crée via `os.MkdirAll` avant de monter
- WinFsp est requis au runtime : https://winfsp.dev/rel/ (version minimale 2.0)
- Sur les plateformes non-Windows, `MountDrive()` et `UnmountDrive()` retournent
  `"winfsp: not supported on this platform"` (dégradation gracieuse)

---

## Cache Métadonnées VFS — v1.7.0 (#108)

### Getattr (Stat)

`GhostFileSystem.Getattr()` consulte le cache LRU avant d'appeler `backend.Stat()` :

1. **Cache hit** : retourne le `FileInfo` mis en cache — aucun appel réseau.
2. **Cache miss** : appelle `backend.Stat()`, stocke le résultat dans le cache, retourne la valeur.
3. **Invalidation** : le cache est invalidé immédiatement lors de toute écriture locale (Release après upload, Unlink, Rename, Mkdir, Create) et par les événements `Watch()` reçus via `watchLoop`.

### Readdir (List)

`GhostFileSystem.Readdir()` consulte le cache LRU avant d'appeler `backend.List()` :

1. **Cache hit** : retourne la liste mise en cache — aucun appel réseau.
2. **Cache miss** : appelle `backend.List()`, stocke le résultat dans le cache, retourne la liste.
3. **Invalidation** : même stratégie que Getattr ; le répertoire parent est invalidé après chaque modification de son contenu.

### Paramètres de configuration

| Paramètre | Type | Valeur par défaut | Description |
|-----------|------|-------------------|-------------|
| `BackendConfig.Params["metaCacheTTL"]` | `string` (entier secondes) | `"300"` (5 min) | TTL fallback du cache métadonnées |

### Comportement watchLoop

- Une goroutine `watchLoop` est démarrée par backend au moment du `Mount()`.
- La goroutine écoute le canal retourné par `backend.Watch(ctx, "/")`.
- À chaque `FileEvent`, elle invalide les entrées cache correspondantes et émet un événement Wails `"meta:updated"`.
- Si `Watch()` retourne une erreur ou un canal nil, la goroutine se termine immédiatement : le cache fonctionne alors en mode TTL seul (dégradation gracieuse).
- La goroutine est annulée proprement lors de `Unmount()` via un `context.WithCancel`.

### Limites du cache

| Paramètre | Valeur |
|-----------|--------|
| Entrées max | 1 000 (LRU éviction au-delà) |
| TTL fallback | 300 s (configurable par `metaCacheTTL`) |
| Invalidation distante (Watch) | ≤ 30 s (délai polling MooseFS/WebDAV) |
| Invalidation locale (écriture) | Immédiate |
