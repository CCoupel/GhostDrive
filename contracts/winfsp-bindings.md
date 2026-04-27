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
