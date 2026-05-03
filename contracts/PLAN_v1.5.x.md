# Contrat API — Plugin MooseFS (v1.5.x)

> **Date** : 2026-05-03  
> **Issues** : #26 #27 #92  
> **Milestone** : v1.5.x

---

## 1. Descripteur Plugin (`StorageBackend.Describe()`)

```go
plugins.PluginDescriptor{
    Type:        "moosefs",
    DisplayName: "MooseFS",
    Description: "Synchronise via un cluster MooseFS (protocole natif TCP)",
    Params: []plugins.ParamSpec{
        {Key: "masterHost",   Label: "Adresse master",          Type: ParamTypeString, Required: true,  Placeholder: "192.168.1.10"},
        {Key: "masterPort",   Label: "Port master",             Type: ParamTypeNumber, Required: false, Default: "9421"},
        {Key: "subDir",       Label: "Sous-répertoire",         Type: ParamTypeString, Required: false, Default: "/", Placeholder: "/GhostDrive"},
        {Key: "pollInterval", Label: "Intervalle Watch (ms)",   Type: ParamTypeNumber, Required: false, Default: "30000"},
    },
}
```

---

## 2. Configuration `BackendConfig.Params`

| Clé          | Type     | Requis | Défaut  | Description |
|--------------|----------|--------|---------|-------------|
| `masterHost` | string   | ✅ oui | —       | IP ou hostname du serveur MooseFS master |
| `masterPort` | number   | non    | `9421`  | Port TCP du master |
| `subDir`     | string   | non    | `"/"`   | Répertoire de base sur le cluster ; tous les chemins sont relatifs à cette racine |
| `pollInterval` | number | non    | `30000` | Intervalle de polling Watch (ms) |

### Exemple de BackendConfig

```json
{
  "id": "mfs-prod",
  "name": "Mon NAS MooseFS",
  "type": "moosefs",
  "enabled": false,
  "params": {
    "masterHost": "192.168.1.10",
    "masterPort": "9421",
    "subDir": "/GhostDrive",
    "pollInterval": "30000"
  },
  "localPath": "C:\\GhostDrive\\MonNAS",
  "mountPoint": "E:"
}
```

---

## 3. Comportement `GetQuota`

MooseFS v1.5.x n'expose pas les quotas cluster via le protocole TCP minimal.

- **Retour garanti** : `(-1, -1, nil)` quand la connexion est établie.  
- **Retour si non connecté** : `(0, 0, plugins.ErrNotConnected)`.

Le frontend doit traiter `free == -1 && total == -1` comme « quota non disponible »
et masquer la barre de quota (même comportement que pour WebDAV sans RFC 4331).

---

## 4. Protocole TCP interne (`mfsclient`)

> ⚠️ Les constantes numériques utilisées dans `plugins/moosefs/internal/mfsclient/protocol.go`
> sont des identifiants **internes GhostDrive** non-compatibles avec le protocole officiel
> MooseFS. Elles garantissent la cohérence entre le client et le fake server de tests.
>
> **Avant de connecter un cluster MooseFS de production**, valider les constantes
> contre la source officielle : `github.com/moosefs/moosefs`.

### Format de frame

```
[cmd uint32 BE][payloadLen uint32 BE][payload bytes]
```

### Commandes (client → serveur)

| Constante       | Valeur | Description |
|-----------------|--------|-------------|
| CmdFUSEGETATTR  | 501    | Obtenir les attributs d'un nœud |
| CmdFUSEMKNOD    | 502    | Créer un fichier |
| CmdFUSEMKDIR    | 503    | Créer un répertoire |
| CmdFUSEUNLINK   | 504    | Supprimer un fichier |
| CmdFUSERMDIR    | 505    | Supprimer un répertoire vide |
| CmdFUSEREAD     | 506    | Lire les données d'un nœud |
| CmdFUSEWRITE    | 507    | Écrire les données d'un nœud |
| CmdFUSEREADDIR  | 508    | Lister le contenu d'un répertoire |

---

## 5. Limitation Move (v1.5.x)

`Move(oldPath, newPath)` est implémenté de façon conservative :
1. `Download(oldPath)` → fichier temporaire local
2. `Delete(oldPath)`
3. `Upload(tmpFile, newPath)`

Cette implémentation génère du trafic réseau supplémentaire mais est correcte.
Le support RENAME natif (`CmdFUSERENAME`) est prévu pour v1.6.x.

---

## 6. Volname WinFsp dynamique (#92)

Le label du drive virtuel WinFsp est désormais dynamique :

```go
volName := "GhostDrive"
if len(backends) > 0 && backends[0].Name != "" {
    volName = backends[0].Name
}
// fmt.Sprintf("uid=-1,gid=-1,volname=%s", volName)
```

- Si `backends[0].Name` est vide ou `backends` est nil → fallback `"GhostDrive"` (retro-compatible)
- Le champ utilisé est `MountedBackend.Name` (défini dans `internal/placeholder/placeholder.go`)
