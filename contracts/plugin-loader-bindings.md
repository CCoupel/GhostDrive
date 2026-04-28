# Contrats — Plugin Loader Bindings (Go → Frontend)

> **Version** : v0.6.0  
> **Feature** : Plugin Loader go-plugin (#65, #67)  
> **Règle** : Le backend implémente ; le frontend consomme sans modifier ce fichier.

---

## Nouveaux Types

### PluginInfo

```go
// PluginInfo décrit un plugin dynamique chargé depuis <AppDir>/plugins/*.exe
type PluginInfo struct {
    // Name est le type du plugin (ex: "echo", "myplugin") — correspond à BackendConfig.Type
    Name    string `json:"name"`
    // Version est la version déclarée par le plugin (via RPC Name étendu, ou "unknown")
    Version string `json:"version"`
    // Path est le chemin absolu vers le binaire .exe
    Path    string `json:"path"`
    // Status est l'état courant du plugin
    Status  string `json:"status"` // "loaded" | "failed" | "restarting"
    // Error contient le message d'erreur si Status == "failed"
    Error   string `json:"error,omitempty"`
}
```

---

## Bindings Wails

### GetLoadedPlugins

```
Signature : GetLoadedPlugins() []PluginInfo
Frontend  : window.go.App.GetLoadedPlugins()
Retour    : []PluginInfo (slice vide si aucun plugin dynamique, jamais null)
Erreur    : –
```

Retourne la liste des plugins dynamiques chargés depuis `<AppDir>/plugins/*.exe`.
N'inclut **pas** les plugins statiques compilés (`local`, `webdav`, `moosefs`).

**Exemple de réponse** :
```json
[
  { "name": "echo",     "version": "unknown", "path": "C:\\GhostDrive\\plugins\\echo.exe",     "status": "loaded" },
  { "name": "myplugin", "version": "unknown", "path": "C:\\GhostDrive\\plugins\\myplugin.exe", "status": "failed", "error": "exit status 1" }
]
```

> **Note** : Le champ `version` retourne `"unknown"` pour tous les plugins dynamiques en v0.6.x.
> Un RPC dédié `GetVersion` sera introduit en v0.6.1 pour exposer la version déclarée par le plugin.

---

### ReloadPlugins

```
Signature : ReloadPlugins() error
Frontend  : window.go.App.ReloadPlugins()
Retour    : null en cas de succès
Erreur    : string décrivant l'erreur
```

Rescanne `<AppDir>/plugins/*.exe` sans redémarrage de l'application.
- Arrête proprement les plugins en cours (Shutdown)
- Relance un nouveau scan (Start)
- Les backends déjà connectés utilisant un plugin dynamique sont **déconnectés** et doivent être reconnectés manuellement
- Émet l'événement `plugin:reloaded` après succès

---

## Événements Wails émis par le loader

| Événement | Payload | Déclencheur |
|-----------|---------|-------------|
| `plugin:loaded` | `PluginInfo` | Un plugin .exe est chargé avec succès |
| `plugin:failed` | `PluginInfo` | Un plugin .exe échoue au chargement ou crashe définitivement |
| `plugin:restarting` | `PluginInfo` | Le watchdog tente un redémarrage (tentative 1, 2 ou 3) |
| `plugin:reloaded` | `{ count: int }` | `ReloadPlugins()` terminé, nombre de plugins chargés |

---

## Compatibilité GetAvailableBackendTypes

```
Signature : GetAvailableBackendTypes() []string  (inchangée)
Frontend  : window.go.App.GetAvailableBackendTypes()
```

Ce binding existant inclut **désormais** les plugins dynamiques dans sa réponse.
Exemple après chargement de `echo.exe` :
```json
["echo", "local", "moosefs", "webdav"]
```

**Changement** : rétrocompatible — le frontend n'a pas besoin de modification pour bénéficier des plugins dynamiques dans le sélecteur "Type" du formulaire `AddBackend`.
