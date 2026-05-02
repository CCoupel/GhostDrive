# Contrat — Plugin Describe() & UI Dynamique

> Version : 1.0.0 | Issues : #78, #79, #80 | Branche : `feat/plugin-describe`

---

## 1. Types Go (`plugins/plugin.go`)

### ParamType

```go
type ParamType string

const (
    ParamTypeString   ParamType = "string"
    ParamTypePassword ParamType = "password"
    ParamTypePath     ParamType = "path"     // affiche un bouton "Parcourir"
    ParamTypeSelect   ParamType = "select"   // Options est requis
    ParamTypeBool     ParamType = "bool"
    ParamTypeNumber   ParamType = "number"
)
```

### ParamSpec

```go
type ParamSpec struct {
    Key         string    // clé dans BackendConfig.Params (ex: "url", "username")
    Label       string    // libellé UI (ex: "URL serveur WebDAV")
    Type        ParamType
    Required    bool
    Default     string    // valeur pré-remplie (peut être vide)
    Placeholder string    // hint dans l'input
    Options     []string  // utilisé seulement si Type == ParamTypeSelect
    HelpText    string    // texte d'aide sous le champ (optionnel)
}
```

### PluginDescriptor

```go
type PluginDescriptor struct {
    Type        string      // même valeur que Name() — ex: "local", "webdav"
    DisplayName string      // libellé affiché dans le sélecteur de type
    Description string      // description courte du plugin
    Params      []ParamSpec // spécification des champs Zone 2 (Remote)
}
```

---

## 2. Méthode StorageBackend

```go
// Describe retourne le descripteur statique du plugin.
// Appelable AVANT Connect() — ne doit effectuer aucun I/O.
// Ne retourne jamais d'erreur : un descripteur minimal est toujours valide.
Describe() PluginDescriptor
```

---

## 3. Proto gRPC (`plugins/proto/storage.proto`)

### Messages nouveaux (champs 20–27 pour éviter conflits avec la réserve 10–19)

```proto
message ParamSpecProto {
  string key         = 20;
  string label       = 21;
  string type        = 22;   // "string" | "password" | "path" | "select" | "bool" | "number"
  bool   required    = 23;
  string default_val = 24;
  string placeholder = 25;
  repeated string options = 26;
  string help_text   = 27;
}

message DescribeRequest {}

message DescribeResponse {
  string type         = 20;
  string display_name = 21;
  string description  = 22;
  repeated ParamSpecProto params = 23;
}
```

### RPC nouveau

```proto
service StorageService {
  // ... (existants) ...
  // Plugin introspection — callable before Connect
  rpc Describe (DescribeRequest) returns (DescribeResponse);
}
```

---

## 4. `ServeInProcess` (`plugins/grpc/inprocess.go`)

```go
// ServeInProcess démarre un serveur gRPC in-process via bufconn et retourne
// un GRPCBackend client connecté à ce serveur.
// Le cleanup doit être appelé lors du Shutdown de l'application.
func ServeInProcess(impl plugins.StorageBackend) (*GRPCBackend, func(), error)
```

**Implémentation** :
- `bufconn.Listen(1024 * 1024)` (buffer 1 MB)
- `grpc.NewServer()` + `RegisterStorageServiceServer`
- `go server.Serve(listener)` dans une goroutine
- `grpc.NewClient("bufnet", grpc.WithContextDialer(...), grpc.WithTransportCredentials(insecure.NewCredentials()))`
- cleanup = `server.Stop(); listener.Close(); conn.Close()`

> **Import** : `google.golang.org/grpc/test/bufconn` — inclus dans `google.golang.org/grpc v1.80.0`
> (déjà présent dans `go.mod`), aucune dépendance supplémentaire.

---

## 5. Binding Wails (`internal/app/app.go`)

```go
// GetPluginDescriptors retourne les descripteurs de tous les plugins disponibles
// (statiques + dynamiques). Appelé par le frontend pour générer la Zone 2.
func (a *App) GetPluginDescriptors() []plugins.PluginDescriptor
```

**Comportement** :
- Retourne les descripteurs mis en cache lors du démarrage des plugins
- Cache local : `a.descriptors map[string]plugins.PluginDescriptor`
- Ne spawn aucun subprocess — lecture seule depuis le cache
- Si aucun plugin disponible : retourne `[]PluginDescriptor{}` (jamais nil)

---

## 6. Types TypeScript (`frontend/src/types/ghostdrive.ts`)

```typescript
export type ParamType = 'string' | 'password' | 'path' | 'select' | 'bool' | 'number';

export interface ParamSpec {
  key:         string;
  label:       string;
  type:        ParamType;
  required:    boolean;
  default:     string;
  placeholder: string;
  options:     string[];
  helpText:    string;
}

export interface PluginDescriptor {
  type:        string;
  displayName: string;
  description: string;
  params:      ParamSpec[];
}
```

---

## 7. Exemples PluginDescriptor

### Plugin `local`

```go
plugins.PluginDescriptor{
    Type:        "local",
    DisplayName: "Local / Réseau",
    Description: "Synchronise depuis un dossier local ou un partage réseau (SMB, NFS…)",
    Params: []plugins.ParamSpec{
        {
            Key:         "rootPath",
            Label:       "Dossier source",
            Type:        plugins.ParamTypePath,
            Required:    true,
            Placeholder: `D:\Photos\...`,
            HelpText:    "Répertoire à synchroniser vers le point de sync local",
        },
    },
}
```

### Plugin `webdav`

```go
plugins.PluginDescriptor{
    Type:        "webdav",
    DisplayName: "WebDAV",
    Description: "Synchronise via un serveur WebDAV (Nextcloud, ownCloud, NAS…)",
    Params: []plugins.ParamSpec{
        {Key: "url",          Label: "URL serveur",              Type: plugins.ParamTypeString,   Required: true,  Placeholder: "https://nas.local/dav"},
        {Key: "authType",     Label: "Authentification",          Type: plugins.ParamTypeSelect,   Required: false, Default: "basic", Options: []string{"basic", "bearer"}},
        {Key: "username",     Label: "Nom d'utilisateur",         Type: plugins.ParamTypeString,   Required: false, Placeholder: "admin"},
        {Key: "password",     Label: "Mot de passe",              Type: plugins.ParamTypePassword, Required: false},
        {Key: "token",        Label: "Token Bearer",              Type: plugins.ParamTypePassword, Required: false, HelpText: "Requis si authType=bearer"},
        {Key: "tlsSkipVerify",Label: "Ignorer erreurs TLS",       Type: plugins.ParamTypeBool,     Required: false, Default: "false", HelpText: "Accepter les certificats auto-signés"},
        {Key: "pollInterval", Label: "Intervalle Watch (ms)",     Type: plugins.ParamTypeNumber,   Required: false, Default: "30000"},
    },
}
```

---

## 8. Ordre d'initialisation dans `Startup()`

```
ServeInProcess(local.New()) → Register("local", factory)   [avant dynRegistry.Start()]
dynRegistry.Start()                                          [scanne les .exe]
Peupler a.descriptors depuis dynRegistry.GetPluginDescriptors()
Boucle reconnexion backends                                  [valide les types — "local" déjà présent]
```

---

## 9. Règles

- `Describe()` est appelable AVANT `Connect()` — aucun I/O autorisé
- Un descripteur minimal (juste `Type = Name()`) est toujours valide
- `GetPluginDescriptors()` retourne `[]PluginDescriptor{}` jamais `nil`
- Le plugin `local` n'utilise plus `init()`/`Register()` — enregistrement explicite dans `Startup()`
