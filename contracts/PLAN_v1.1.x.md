# Contrat API — v1.1.x (issues #85, #88, #89)

> **Version** : 1.1.x  
> **Date** : 2026-05-02 (révisé après relecture CDP)  
> **Issues** : #89 (GetQuota bug), #85 (enabled/disabled → drive virtuel), #88 (drive par backend)  
> **Règle** : Le backend implémente ; le frontend consomme sans modifier ce fichier.

---

## 1. Modèles modifiés

### BackendConfig (plugins/plugin.go)

Ajout du champ `MountPoint` :

```go
type BackendConfig struct {
    ID         string            `json:"id"`
    Name       string            `json:"name"`
    Type       string            `json:"type"`
    Enabled    bool              `json:"enabled"`
    LocalPath  string            `json:"localPath"`
    MountPoint string            `json:"mountPoint"` // ← NOUVEAU : lettre "E:" ou chemin "C:\GhostDrive\MonNAS\"
    Params     map[string]string `json:"params"`
}
```

**Règle** : Si `MountPoint` est vide à la création (`AddBackend`) ou au démarrage (migration), `app.go` appelle `DriveManager.AssignAvailableLetter()` et persiste la valeur auto-assignée.

### DriveStatus (internal/placeholder/placeholder.go)

Ajout des champs `BackendID` et `BackendName` :

```go
type DriveStatus struct {
    Mounted      bool              `json:"mounted"`
    MountPoint   string            `json:"mountPoint"`
    BackendID    string            `json:"backendID"`   // ← NOUVEAU
    BackendName  string            `json:"backendName"` // ← NOUVEAU (pour affichage UI)
    BackendPaths map[string]string `json:"backendPaths"`
    LastError    string            `json:"lastError"`
}
```

### TypeScript — ghostdrive.ts

```typescript
export interface BackendConfig {
  id: string;
  name: string;
  type: string;
  enabled: boolean;
  localPath: string;
  mountPoint: string;  // ← NOUVEAU
  params: Record<string, string>;
}

export interface DriveStatus {
  mounted: boolean;
  mountPoint: string;
  backendID: string;    // ← NOUVEAU
  backendName: string;  // ← NOUVEAU
  backendPaths: Record<string, string>;
  lastError: string;
}
```

---

## 2. Nouveaux Wails Bindings

### GetDriveStatuses

```
Signature : GetDriveStatuses() map[string]placeholder.DriveStatus
Frontend  : window.go.App.GetDriveStatuses()
Retour    : map[backendID → DriveStatus]
Erreur    : –
```

Retourne l'état de montage de chaque drive virtuel par `backendID`.  
Remplace `GetDriveStatus()` (unique) qui est désormais **DEPRECATED**.

### GetDriveStatus (deprecated)

```
Signature : GetDriveStatus() placeholder.DriveStatus   ← DEPRECATED v1.1.x
```

Conservé pour compatibilité pendant la migration. Retourne un `DriveStatus` vide. Le frontend NE DOIT PAS utiliser ce binding pour les nouvelles features.

---

## 3. Bindings modifiés

### AddBackend

Comportement modifié :
- `enabled` est forcé à `false` à la création (ignoré si `true` côté frontend)
- `mountPoint` auto-assigné si vide (première lettre libre ≥ E:, via `DriveManager.AssignAvailableLetter()`)
- Retourne une erreur bloquante si `mountPoint` est déjà utilisé par un autre backend (activé **ou désactivé**)

### UpdateBackend

Comportement modifié :
- Si `mountPoint` change : démontage de l'ancien drive, montage sur le nouveau point
- Conflit de `mountPoint` (tous backends) → erreur bloquante retournée

### SetBackendEnabled

Comportement étendu :
- `enabled = true` → `Connect()` + `driveManager.Mount(backendID, name, mountPoint, paths)` ; émet `drive:mounted`
- `enabled = false` → `driveManager.Unmount(backendID)` + `Disconnect()` ; émet `drive:unmounted`
- Atomique : si `Connect()` échoue → drive non monté, `enabled` reste `false`, émet `drive:error`

### MountDrive() / UnmountDrive() — SUPPRIMÉS

Ces bindings Wails sont **supprimés** en v1.1.x. Le cycle de vie du drive est entièrement piloté par `SetBackendEnabled()`. Le frontend ne doit plus appeler ces méthodes.

### ValidateBackendConfig (nouvelle règle)

Règle supplémentaire ajoutée à la validation existante :
- **MountPoint unique** parmi **tous** les backends (activés et désactivés) — erreur bloquante
- Implémenté par itération de `a.cfg.Backends` dans `validateBackendConfig()`, **sans appel à DriveManager**
- Format autorisé : `X:` (lettre simple) ou chemin absolu Windows

---

## 4. Événements Wails modifiés

### drive:mounted

```typescript
// Avant
{ mountPoint: string; backendPaths: Record<string, string> }

// Après (v1.1.x)
{
  backendID: string;     // ← NOUVEAU
  backendName: string;   // ← NOUVEAU
  mountPoint: string;
  backendPaths: Record<string, string>;
}
```

### drive:unmounted

```typescript
// Avant
{ mountPoint: string }

// Après (v1.1.x)
{
  backendID: string;   // ← NOUVEAU
  backendName: string; // ← NOUVEAU
  mountPoint: string;
}
```

### drive:error

```typescript
// Avant
{ error: string }

// Après (v1.1.x)
{
  backendID: string;   // ← NOUVEAU (vide si erreur globale)
  backendName: string; // ← NOUVEAU
  error: string;
}
```

> **Note** : Les events sont émis par `app.go` après les appels au `DriveManager`. Le package `placeholder` n'a aucune dépendance sur le système d'events Wails.

---

## 5. Interface interne — DriveManager

> **Note** : Interface interne Go, non exposée en Wails. Documentée ici pour cohérence architecture.

```go
// internal/placeholder/manager.go
type DriveManager struct {
    drives map[string]VirtualDrive  // keyed by backendID
    mu     sync.RWMutex
}

// Constructeur sans context ni EventEmitter.
// Les events sont émis par app.go après les appels au DriveManager.
func NewDriveManager() *DriveManager

func (m *DriveManager) Mount(backendID, backendName, mountPoint string, paths []MountedBackend) error
func (m *DriveManager) Unmount(backendID string) error
func (m *DriveManager) GetStatus(backendID string) (DriveStatus, bool)
func (m *DriveManager) GetAllStatuses() map[string]DriveStatus
func (m *DriveManager) AssignAvailableLetter() (string, error)
func (m *DriveManager) UnmountAll() error
```

**Absent de l'interface** : `IsMountPointInUse()` — la détection de conflit est effectuée dans `validateBackendConfig()` par itération de `a.cfg.Backends`, pas par le DriveManager.

**Cross-platform** : `placeholder.New()` retourne déjà un `NullDrive` sur non-Windows. Aucun fichier `manager_other.go` nécessaire.

L'interface `VirtualDrive` existante est **inchangée** (`Mount`, `Unmount`, `IsMounted`, `Status`).

---

## 6. Correction GetQuota (#89)

Comportement corrigé dans `manager.go:ListStatuses()` et `app.go:TestBackendConnection()` :

```go
// Avant (bug)
if err != nil {
    // FreeSpace reste à 0 (zero value)
}

// Après (fix)
if err != nil {
    status.FreeSpace = -1  // -1 = quota inconnu / non supporté
}
```

TypeScript — convention documentée :
- `freeSpace < 0` → "Quota indisponible" (**déjà implémenté** dans `BackendConfigCard.tsx:162-168`, fix #87, commit 653fb7f)
- `freeSpace === 0` → "0 octet libre" (valeur réelle)

---

## 7. Compatibilité et Migration

| Changement | Type | Impact frontend |
|-----------|------|-----------------|
| `BackendConfig.mountPoint` ajouté | **NON-BREAKING** | Champ optionnel, auto-assigné |
| `DriveStatus.backendID/backendName` ajoutés | **NON-BREAKING** | Nouveaux champs |
| Events `drive:*` — payload étendu | **NON-BREAKING** | Nouveaux champs ignorés par ancien code |
| `GetDriveStatuses()` ajouté | **NON-BREAKING** | Nouveau binding |
| `GetDriveStatus()` déprécié | **NON-BREAKING** | Toujours fonctionnel v1.1.x |
| `AddBackend` — `enabled` forcé à `false` | **BREAKING** | Comportement changé |
| `SetBackendEnabled` — gère le drive | **BREAKING** | Comportement étendu |
| Drive GhD: global supprimé | **BREAKING** | UI à mettre à jour |
| `MountDrive()` / `UnmountDrive()` supprimés | **BREAKING** | Supprimer tout appel frontend |
