# Contrat — Configuration Backend et Points de Synchronisation

> **Version** : 0.2.0
> **Issues** : #29 (config backend), #30 (sync points)
> **Règle** : Le backend implémente ; le frontend consomme ce contrat sans le modifier.

---

## Modèle BackendConfig (étendu v0.2.0)

```go
type BackendConfig struct {
    ID         string            `json:"id"`         // UUID généré par AddBackend
    Name       string            `json:"name"`       // Nom affiché (ex: "Mon NAS")
    Type       string            `json:"type"`       // "webdav" | "moosefs" | "local" (v0.4.0)
    Enabled    bool              `json:"enabled"`
    AutoSync   bool              `json:"autoSync"`   // true = sync démarre automatiquement (défaut: false)
    Params     map[string]string `json:"params"`     // Paramètres spécifiques au type
    SyncDir    string            `json:"syncDir"`    // Dossier local à synchroniser
    RemotePath string            `json:"remotePath"` // Chemin distant racine (ex: "/GhostDrive")
    LocalPath  string            `json:"localPath"`  // Point de sync local — v0.4.0
}
```

### Champ `AutoSync` — v0.4.0

| Champ | Type | Défaut | Description |
|-------|------|--------|-------------|
| `AutoSync` | bool | `false` | `true` = la sync démarre automatiquement à la connexion du backend ; `false` = mode manuel (ForceSync requis) |

**Comportement au démarrage** : Si `AutoSync=true`, l'engine de synchronisation est démarré automatiquement après reconnexion du backend.
**Backward compatibility** : Les backends existants en config.json sans le champ `autoSync` lisent la zero-value Go (`false`) → comportement inchangé (sync manuelle).

### Paramètres WebDAV (`type: "webdav"`) — v0.8.0

| Clé | Type | Requis | Défaut | Description |
|-----|------|--------|--------|-------------|
| `url` | string | ✓ | — | URL racine WebDAV (ex: `https://nas.local/dav`) |
| `username` | string | cond. | — | Requis si `authType=basic` (défaut) |
| `password` | string | cond. | — | Requis si `authType=basic` ; jamais loggué |
| `token` | string | cond. | — | Requis si `authType=bearer` ; jamais loggué |
| `authType` | string | non | `"basic"` | Schéma d'auth : `"basic"` (RFC 7617) \| `"bearer"` (RFC 6750) |
| `pollInterval` | string | non | `"30"` | Intervalle de polling Watch en **millisecondes** |
| `tlsSkipVerify` | string | non | `"false"` | `"true"` pour accepter les certificats auto-signés (NAS domestiques) |

> **Rétro-compatibilité v0.8.0** : Les configs existantes sans `token`, `authType`,
> `pollInterval` ou `tlsSkipVerify` utilisent les valeurs par défaut ci-dessus.
> Les configs v0.x avec `insecure` doivent migrer vers `tlsSkipVerify`.

### Paramètres MooseFS (`type: "moosefs"`)

| Clé | Type | Requis | Description |
|-----|------|--------|-------------|
| `master` | string | ✓ | IP ou hostname du MooseFS Master (ex: `192.168.1.1`) |
| `port` | string | — | Port (défaut: `9421`) |
| `mountPath` | string | ✓ | Chemin du mount FUSE (ex: `/mnt/moosefs`) |

### Paramètres Local (`type: "local"`) — v0.4.0

| Clé | Type | Requis | Description |
|-----|------|--------|-------------|
| `rootPath` | string | ✓ | Chemin absolu du répertoire local à synchroniser (ex: `D:\MyFolder` ou `/mnt/local/folder`) |

> **Note** : Le backend LOCAL (issue #47) permet de synchroniser un dossier local comme source/destination de sync. Utile pour les tests, les montages réseau (SMB, NFS), ou les stockages locaux temporaires.

---

## Méthodes Wails — Gestion Backends (#29)

### AddBackend

```
Signature Go : AddBackend(config BackendConfig) (BackendConfig, error)
Frontend     : window.go.App.AddBackend(config)
Retour       : BackendConfig avec ID UUID généré
Erreur       : "validation: <champ> requis" | "connection: <message>"
```

**Comportement** :
1. Valider les champs requis (Name, Type, Params, SyncDir, RemotePath)
2. Générer un UUID pour `ID`
3. Tester la connexion via `backend.Connect()`
4. Sauvegarder dans AppConfig.Backends
5. Démarrer la surveillance (Watch)
6. Émettre `backend:status-changed`

**Validation côté Go** (le frontend doit aussi valider pour UX) :
- `Name` : non vide, max 64 chars
- `Type` : "webdav" | "moosefs" | "local"
- `SyncDir` : chemin absolu valide, doit exister
- `RemotePath` : commence par "/"
- Params spécifiques selon Type

---

### RemoveBackend

```
Signature Go : RemoveBackend(backendID string) error
Frontend     : window.go.App.RemoveBackend(backendId)
Erreur       : "not found: <id>" | "sync active, stop first"
```

**Comportement** :
1. Arrêter la synchronisation en cours pour ce backend
2. Retirer du BackendManager
3. Supprimer de AppConfig.Backends et sauvegarder
4. Émettre `backend:status-changed` avec `connected: false`

---

### TestBackendConnection

```
Signature Go : TestBackendConnection(config BackendConfig) (BackendStatus, error)
Frontend     : window.go.App.TestBackendConnection(config)
Retour       : BackendStatus (voir models.md)
```

**Comportement** :
- Ne sauvegarde pas la config
- Instancie un backend temporaire, appelle `Connect()` puis `Stat("/")`
- Retourne `FreeSpace` et `TotalSpace` si disponibles

---

### GetBackendStatuses

```
Signature Go : GetBackendStatuses() []BackendStatus
Frontend     : window.go.App.GetBackendStatuses()
Retour       : []BackendStatus triés par ID
```

---

## Méthodes Wails — Points de Synchronisation (#30)

### GetConfig / SaveConfig

Le frontend récupère `AppConfig.Backends[]` via `GetConfig()` pour afficher la liste des points de sync.

```
Signature Go : GetConfig() AppConfig
Frontend     : window.go.App.GetConfig()
```

```
Signature Go : SaveConfig(config AppConfig) error
Frontend     : window.go.App.SaveConfig(config)
```

**Champs de SyncDir et RemotePath** :
- `SyncDir` : répertoire Windows local (ex: `C:\Users\User\GhostDrive\MonNAS`)
- `RemotePath` : répertoire racine sur le backend (ex: `/GhostDrive`)

### OpenSyncFolder

```
Signature Go : OpenSyncFolder(backendID string) error
Frontend     : window.go.App.OpenSyncFolder(backendId)
```

Ouvre le `SyncDir` du backend dans l'Explorateur Windows (`explorer.exe`).

---

---

## PluginDescriptor et ParamSpec (v1.1.0 — #79 / #80)

Voir `contracts/plugin-describe.md` pour la spécification complète.

Chaque plugin expose un `PluginDescriptor` via la méthode `Describe()` qui décrit les champs de
configuration Zone 2 (Remote) nécessaires. La validation de `AddBackend` utilise `ParamSpec.Required`
pour vérifier que les champs obligatoires sont présents dans `BackendConfig.Params`.

```go
type PluginDescriptor struct {
    Type        string      // == Name()
    DisplayName string      // libellé UI dans le sélecteur
    Description string      // description courte
    Params      []ParamSpec // champs Zone 2 — validés par validateBackendConfig
}
```

Le binding Wails `GetPluginDescriptors() []PluginDescriptor` expose tous les plugins disponibles
(statiques + dynamiques) au frontend pour générer dynamiquement la Zone 2 de `SyncPointForm`.

---

## Règles de Validation — Frontend (UX)

Le formulaire `SyncPointForm` doit valider côté React **avant** d'appeler Wails :

| Champ | Règle |
|-------|-------|
| Name | Requis, 1–64 chars |
| Type | "webdav", "moosefs" ou "local" |
| SyncDir | Requis, non vide |
| RemotePath | Requis, commence par "/" |
| url (WebDAV) | Requis, format URL valide |
| username (WebDAV) | Requis |
| password (WebDAV) | Requis |
| master (MooseFS) | Requis |
| mountPath (MooseFS) | Requis, commence par "/" |
| rootPath (Local) | Requis, chemin absolu valide |

---

## Événements liés (#29, #30)

| Événement | Déclencheur | Payload |
|-----------|------------|---------|
| `backend:status-changed` | AddBackend, RemoveBackend, reconnexion | `BackendStatus` |
| `app:ready` | Démarrage app (backends chargés) | `{ version, backendsCount }` |
