# Guide d'implémentation de plugin GhostDrive

> **Version** : v0.7.0
> **Audience** : développeurs Go (humains et agents Claude Code)
> **Temps estimé** : 2-4 h pour un backend simple (filesystem, HTTP)

---

## 1. Vue d'ensemble

### Deux architectures de plugin

GhostDrive supporte deux modes de plugin :

#### Plugins statiques
Compilés directement dans le binaire GhostDrive. Actuellement, seul le plugin "local" (`plugins/local/`) est construit de cette manière. La factory dans `plugins/registry.go` mappe les noms de type aux fonctions constructeurs.

#### Plugins dynamiques (go-plugin + gRPC)
N'importe quel backend compilé comme un **binaire standalone** et placé dans `<AppDir>/plugins/`. Le loader (`plugins/loader/grpc_loader.go`) découvre ces binaires au démarrage, négocie la poignée de main go-plugin (`plugins/loader.HandshakeConfig`), et fait le pont entre chaque plugin via le transport gRPC défini dans `plugins/proto/storage.proto`.

Les développeurs de plugins doivent commencer par `plugins/sdk/go/` (exemple echo + Makefile) et implémenter `StorageBackend` dans leur propre binaire.

### Flux architectural (plugins dynamiques)

```
┌─────────────────────────┐
│   GhostDrive binary     │
│                         │
│  ┌─────────────────────┐│
│  │   GRPCLoader        ││
│  │  (go-plugin client) ││
│  └──────────┬──────────┘│
└─────────────┼───────────┘
              │ (fork + exec)
              ↓
    ┌────────────────────────┐
    │  Plugin subprocess     │
    │ (standalone binary)    │
    │                        │
    │ ┌────────────────────┐ │
    │ │ StorageBackend     │ │
    │ │ (implementation)   │ │
    │ │                    │ │
    │ │ goplugin.Serve()   │ │
    │ └──────────┬─────────┘ │
    │            │ gRPC      │
    │  ┌─────────▼─────────┐ │
    │  │ Storage Service   │ │
    │  │ (grpc bridge)     │ │
    │  └───────────────────┘ │
    └────────────────────────┘
              ↕ gRPC + go-plugin handshake
         (via stdio pipe)
```

### Cycle de vie d'un plugin dynamique

```
GRPCLoader.Scan()
  ├── Découvre binaires dans <AppDir>/plugins/
  ├── Lance chaque binaire en subprocess
  ├── Négocie le handshake go-plugin
  │   - Vérifie cookie GHOSTDRIVE_PLUGIN = storage.v1
  │   - Établit le protocole gRPC
  ├── Appelle Name() pour enregistrer dans la factory
  ├── Watchdog : redémarre en cas de crash
  │   - Délais par défaut : 1s → 2s → 4s (3 tentatives max)
  │   - Après N crashes, marque comme "failed"
  └── À Disconnect() : termine le subprocess

Cycle d'une instance backend :
  Connect(config) → Watch / Upload / Download / Delete / Move / Stat / List / CreateDir → Disconnect()
```

---

## 2. L'interface `StorageBackend` — référence complète

Source : `plugins/plugin.go`

### Types partagés

#### `FileInfo`

```go
type FileInfo struct {
    // Name est le nom de base de l'entrée (sans séparateur).
    Name string
    // Path est le chemin slash-séparé retourné par le backend,
    // relatif à la racine RemotePath. Ne commence jamais avec une lettre de lecteur.
    Path string
    // Size est la taille en octets du contenu du fichier. Zéro pour les répertoires.
    Size int64
    // IsDir vaut true quand l'entrée est un répertoire.
    IsDir bool
    // ModTime est l'horodatage de dernière modification rapporté par le backend.
    ModTime time.Time
    // ETag est la balise HTTP ou un token de version équivalent, si disponible.
    ETag string
    // IsPlaceholder indique que le fichier est un placeholder Files-On-Demand
    // (contenu non encore hydraté localement).
    IsPlaceholder bool
    // IsCached indique que le contenu du fichier est présent dans le cache local.
    IsCached bool
}
```

#### `FileEvent` / `FileEventType`

```go
type FileEventType string

const (
    FileEventCreated  FileEventType = "created"   // Nouveau fichier ou répertoire
    FileEventModified FileEventType = "modified"  // Contenu ou métadonnées changés
    FileEventDeleted  FileEventType = "deleted"   // Entrée supprimée
    FileEventRenamed  FileEventType = "renamed"   // Entrée déplacée
)

type FileEvent struct {
    Type      FileEventType // Type de changement
    Path      string        // Chemin slash-séparé de l'entrée affectée
    OldPath   string        // Chemin précédent pour FileEventRenamed ; vide sinon
    Timestamp time.Time     // Quand le changement a été détecté
    Source    string        // "local" pour les changements locaux, "remote" pour le backend
}
```

#### `BackendConfig`

```go
type BackendConfig struct {
    ID       string            // Identifiant unique de cette instance backend
    Name     string            // Label affiché dans l'UI
    Type     string            // Sélectionne le plugin ("webdav", "moosefs", "echo", etc.)
    Enabled  bool              // Participe ou non à la sync
    AutoSync bool              // Démarre auto la sync au Connect (défaut : false)
    Params   map[string]string // Config spécifique au plugin (voir contracts/backend-config.md)
    SyncDir  string            // Deprecated : utiliser LocalPath à la place
    RemotePath string          // Racine sur le backend (ex: "/GhostDrive")
    LocalPath  string          // Point de sync local — destination GhostDrive sur le PC
    Warning   string           // Message de validation non-bloquant (loader-side only)
}
```

#### `ProgressCallback`

```go
type ProgressCallback func(done, total int64)
// done : octets transférés jusqu'ici (monotoniquement croissant)
// total : taille totale attendue (-1 si inconnue)
// Le callback ne doit pas bloquer
```

### Erreurs sentinelles

| Sentinel | Utilisation |
|----------|-------------|
| `plugins.ErrNotConnected` | N'importe quelle opération appelée sans connexion active |
| `plugins.ErrFileNotFound` | Le chemin distant demandé n'existe pas |

**Convention de wrapping (critique)** :

```go
// ✓ Correct — permet errors.Is(err, plugins.ErrNotConnected)
return fmt.Errorf("myplugin: upload %s: %w", remote, plugins.ErrNotConnected)

// ✗ Incorrect — casse errors.Is et la propagation gRPC
return errors.New("myplugin: not connected")
```

### Méthodes de l'interface

#### Identification

| Méthode | Signature | Contrat |
|---------|-----------|---------|
| `Name()` | `Name() string` | Retourne l'identifiant du plugin en minuscules (ex: "webdav", "echo"). Immutable ; peut être appelé avant `Connect()`. |

#### Connexion

| Méthode | Signature | Contrat |
|---------|-----------|---------|
| `Connect()` | `Connect(config BackendConfig) error` | Initialise le backend avec la config fournie. Valide les Params obligatoires et probe le backend (ex: PROPFIND pour WebDAV, vérifier que le chemin existe pour local). Retourne une erreur descriptive si le backend est inaccessible ou mal configuré. Rappeler `Connect` sur un backend déjà connecté le reconnecter. |
| `Disconnect()` | `Disconnect() error` | Libère les ressources (connexions ouvertes, goroutines, etc.). Après `Disconnect`, toutes les opérations sauf `Connect` doivent retourner `ErrNotConnected`. Sans danger d'appeler sur un backend déjà déconnecté (no-op). |
| `IsConnected()` | `IsConnected() bool` | Retourne true si `Connect` a réussi et `Disconnect` n'a pas été appelé depuis. Thread-safe ; ne réalise pas d'I/O. |

#### Opérations fichiers

| Méthode | Signature | Contrat |
|---------|-----------|---------|
| `Upload()` | `Upload(ctx context.Context, local, remote string, progress ProgressCallback) error` | Copie le fichier local à `local` vers le chemin distant `remote`. Les répertoires intermédiaires ne sont PAS créés automatiquement ; appelez `CreateDir` d'abord si nécessaire. `progress` peut être nil. Retourne `ErrNotConnected` si pas connecté. |
| `Download()` | `Download(ctx context.Context, remote, local string, progress ProgressCallback) error` | Copie le fichier distant à `remote` vers le chemin local `local`. Le répertoire parent de `local` est créé s'il n'existe pas. Retourne `ErrFileNotFound` (wrapped) si `remote` n'existe pas. |
| `Delete()` | `Delete(ctx context.Context, remote string) error` | Supprime le fichier ou le répertoire à `remote`. Supprimer un répertoire non-vide est implémentation-défini (les plugins peuvent refuser ou supprimer récursivement). Retourne `ErrFileNotFound` (wrapped) si absent. |
| `Move()` | `Move(ctx context.Context, oldPath, newPath string) error` | Renomme ou déplace l'entrée à `oldPath` vers `newPath` sur le backend. Écrase `newPath` s'il existe déjà. |

#### Navigation

| Méthode | Signature | Contrat |
|---------|-----------|---------|
| `List()` | `List(ctx context.Context, path string) ([]FileInfo, error)` | Retourne les enfants directs du répertoire à `path`. L'entrée du répertoire lui-même n'est PAS incluse dans le résultat. Retourne une slice vide (jamais nil) si le répertoire est vide. Retourne `ErrFileNotFound` (wrapped) si `path` n'existe pas ou est un fichier. |
| `Stat()` | `Stat(ctx context.Context, path string) (*FileInfo, error)` | Retourne les métadonnées du fichier ou du répertoire à `path`. Retourne `ErrFileNotFound` (wrapped) si absent. |
| `CreateDir()` | `CreateDir(ctx context.Context, path string) error` | Crée le répertoire à `path`. No-op si le répertoire existe déjà (pas d'erreur). Les répertoires parents ne sont PAS créés ; utilisez des appels récursifs. |

#### Surveillance

| Méthode | Signature | Contrat |
|---------|-----------|---------|
| `Watch()` | `Watch(ctx context.Context, path string) (<-chan FileEvent, error)` | Démarre la surveillance de `path` pour les changements et émet les `FileEvent` sur le canal retourné. Le canal se ferme quand `ctx` est annulé. Les implémentations peuvent utiliser des notifications natives (inotify, FSEvents) ou du polling ; documentez l'approche et l'intervalle détectable minimum. La taille de buffer du canal doit être ≥ 64 pour absorber les bursts d'événements. |

#### Quota

| Méthode | Signature | Contrat |
|---------|-----------|---------|
| `GetQuota()` | `GetQuota(ctx context.Context) (free, total int64, err error)` | Retourne l'espace libre et total (en octets) du backend. Les plugins qui ne supportent pas la quota doivent retourner `(-1, -1, nil)` plutôt qu'une erreur. Retourne `ErrNotConnected` si pas connecté. |

---

## 3. Créer un plugin externe (go-plugin) — guide pas-à-pas

### Étape 1 — Créer un module Go indépendant

Vous pouvez créer un plugin dans un dépôt séparé ou au sein du même repo via `go.work`.

**Option A — Dépôt séparé** :
```bash
mkdir my-plugin && cd my-plugin
go mod init github.com/myorg/my-plugin
```

**Option B — Dans le même repo (go.work)** :
```bash
cd <GhostDrive-root>
go work use ./my-plugin
```

### Étape 2 — Copier l'exemple echo

```bash
# Option A : depuis le template
cp -r plugins/sdk/go/my-plugin/ /path/to/my-plugin

# Option B : depuis le repo GhostDrive (si dans le même go.work)
cp -r plugins/sdk/go /path/to/my-plugin
cd my-plugin
```

### Étape 3 — Implémenter `StorageBackend`

Ouvrez `main.go` et créez une struct qui satisfait `plugins.StorageBackend` :

```go
package main

import (
    "context"
    "fmt"
    
    sdk "github.com/CCoupel/GhostDrive/plugins/sdk/go"
    "github.com/CCoupel/GhostDrive/plugins"
    goplugin "github.com/hashicorp/go-plugin"
)

// MyPlugin est votre implémentation.
// Le nom doit être en minuscules, sans espaces.
type MyPlugin struct {
    connected bool
    config    plugins.BackendConfig
}

// Name retourne l'identifiant du plugin.
// Cette valeur DOIT correspondre à BackendConfig.Type dans la config utilisateur.
func (p *MyPlugin) Name() string { return "myplugin" }

// Connect initialise le backend.
func (p *MyPlugin) Connect(cfg plugins.BackendConfig) error {
    // Valider les params obligatoires
    if cfg.Params["url"] == "" {
        return fmt.Errorf("myplugin: connect: url param is required")
    }
    p.config = cfg
    p.connected = true
    return nil
}

// ... implémenter toutes les autres méthodes de StorageBackend
```

Voici un template avec godoc minimales pour chaque méthode :

```go
func (p *MyPlugin) Disconnect() error {
    p.connected = false
    return nil
}

func (p *MyPlugin) IsConnected() bool { return p.connected }

func (p *MyPlugin) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
    if !p.connected {
        return fmt.Errorf("myplugin: upload: %w", plugins.ErrNotConnected)
    }
    // Implémenter le transfert
    return nil
}

// ... et les 11 autres méthodes ...
```

Pour un exemple complet, consultez `plugins/sdk/go/echo/main.go`.

### Étape 4 — Configurer le serveur go-plugin

Votre `main()` doit servir votre implémentation via go-plugin :

```go
func main() {
    // IMPORTANT : utiliser sdk.ServeConfig pour injecter la HandshakeConfig correcte
    goplugin.Serve(sdk.ServeConfig(&MyPlugin{}))
}
```

**Ne pas copier `HandshakeConfig`** — utiliser `loader.HandshakeConfig` depuis le SDK. C'est critique pour la compatibilité.

### Étape 5 — Compiler pour Windows et Linux

Utilisez le Makefile fourni :

```bash
# Windows uniquement (AMD64)
make build
# → myplugin.exe  (Windows AMD64)

# Linux uniquement (AMD64)
make build-linux
# → myplugin      (Linux AMD64, extensionless, execute bit set)

# Les deux
make build-all
```

Ou manuellement :

```bash
# Windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags ignore -ldflags="-s -w" -o myplugin.exe .

# Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags ignore -ldflags="-s -w" -o myplugin .
```

**Important** : le flag `-tags ignore` est requis pour que les binaires skip les dépendances spécifiques à GhostDrive.

### Étape 6 — Installer dans GhostDrive

Copiez le binaire dans le répertoire des plugins de GhostDrive :

```bash
# Windows
copy myplugin.exe <AppDir>\plugins\myplugin.exe

# Linux — le loader détecte les exécutables sans extension
cp myplugin <AppDir>/plugins/myplugin
chmod +x <AppDir>/plugins/myplugin
```

Où `<AppDir>` est le répertoire contenant `GhostDrive.exe` (Windows) ou le binaire `ghostdrive` (Linux).

### Étape 7 — Charger et tester

Le plugin est chargé à l'un de ces moments :

- **Au démarrage de GhostDrive** : le loader scanne `<AppDir>/plugins/` automatiquement
- **Via ReloadPlugins()** : depuis la page Settings > Plugins

Votre plugin apparaît dans le sélecteur **Add Backend** sous le nom retourné par `Name()`.

---

## 4. Conventions obligatoires

### Stateless entre les instances

Le watchdog peut redémarrer votre plugin à tout moment (crash, timeout, etc.). **Votre binaire ne doit pas conserver d'état persistant** entre les redémarrages. Pas de :

- Fichiers temporaires qui survivent la fin du processus
- Locks de fichiers hors du contexte d'une session
- Sockets Unix qui ne sont pas cleanupées à la fin

```go
// ✓ Bon : chaque instance est indépendante
type MyPlugin struct {
    connected bool
    // config...
}

// ✗ Mauvais : état persistant
var globalCache = make(map[string][]byte) // ne survivra pas au redémarrage
```

### `Name()` idempotent et sans I/O

`Name()` est appelé **avant `Connect()`** par le loader pour enregistrer le plugin. Il ne doit jamais :

- Accéder au réseau
- Lire/écrire des fichiers
- Faire d'autres I/O

```go
// ✓ Bon
func (p *MyPlugin) Name() string { return "myplugin" }

// ✗ Mauvais
func (p *MyPlugin) Name() string {
    // Ne pas faire ceci !
    return detectPluginName() // I/O, peut échouer
}
```

### Pas de goroutines en fuite après `Disconnect()`

Quand `Disconnect()` est appelé, terminez proprement toutes les goroutines lancées par votre plugin. Sinon, le watchdog ne pourra pas tuer le subprocess.

```go
type MyPlugin struct {
    connected bool
    cancel    context.CancelFunc // pour annoncer l'arrêt
}

func (p *MyPlugin) Connect(cfg plugins.BackendConfig) error {
    ctx, cancel := context.WithCancel(context.Background())
    p.cancel = cancel
    
    // Lancer les goroutines avec ce context
    go p.watcherLoop(ctx)
    return nil
}

func (p *MyPlugin) Disconnect() error {
    p.connected = false
    if p.cancel != nil {
        p.cancel() // ← arrête toutes les goroutines qui écoutent ctx
    }
    return nil
}
```

### Autres conventions

- **Package** : `main` (plugins dynamiques) ou `plugins/<nom>/` (statiques)
- **Struct** : `MyPlugin` ou le nom du plugin en CamelCase
- **Constructeur** : les plugins dynamiques n'en ont pas (la factory ne s'applique pas)
- **Chemins** : toujours slash-séparés (`/`) même sur Windows, relatifs à `RemotePath`

---

## 5. Transport gRPC

### HandshakeConfig

Le handshake go-plugin assure que le client (GhostDrive) et le serveur (votre plugin) parlent la même langue. Vous **ne devez pas copier** `HandshakeConfig` ; utiliser celui du SDK :

```go
// CORRECT
goplugin.Serve(sdk.ServeConfig(&MyPlugin{}))
// ↑ sdk.ServeConfig injecte automatiquement loader.HandshakeConfig

// INCORRECT
goplugin.Serve(&goplugin.ServeConfig{
    HandshakeConfig: goplugin.HandshakeConfig{
        ProtocolVersion: 1,
        MagicCookieKey: "GHOSTDRIVE_PLUGIN",
        MagicCookieValue: "storage.v1",
    },
    // ...
})
// ↑ Ne pas le redéfinir !
```

### Mapping erreur Go ↔ gRPC

Les erreurs Go sentinelles sont **round-trippées** à travers le pont gRPC via un mécanisme de mapping :

**Go → gRPC** (côté serveur) :
```
plugins.ErrNotConnected ──┐
                           ├──→ mapBackendError(err) ──→ gRPC status error
plugins.ErrFileNotFound ──┘      + message d'erreur
```

**gRPC → Go** (côté client) :
```
gRPC status error ──→ mapGRPCError(status) ──→ plugins.ErrNotConnected
  + message          ou plugins.ErrFileNotFound
```

Pour que ce round-trip fonctionne, **wrappez toujours vos erreurs** avec les sentinelles :

```go
// Le loader reconnaît et récupère la sentinelle
return fmt.Errorf("myplugin: stat %s: %w", path, plugins.ErrFileNotFound)

// Côté client, errors.Is fonctionne correctement
err := backend.Stat(ctx, "/missing")
if errors.Is(err, plugins.ErrFileNotFound) {
    // ✓ Condition satisfaite
}
```

### Compatibilité polyglotte

Le proto est défini dans `plugins/proto/storage.proto`. Vous pouvez implémenter un plugin dans n'importe quel langage supportant gRPC (Python, Rust, C#, etc.) :

```bash
# Générer les stubs pour votre langage
protoc --python_out=. plugins/proto/storage.proto
# ou
protoc --rust_out=. plugins/proto/storage.proto
```

**Cependant**, vous devez toujours gérer le handshake go-plugin (magic cookie), qui est spécifique à HashiCorp go-plugin. Les implémentations non-Go doivent en émettre les signaux sur stdout/stderr.

Exemple (pseudo-code Rust) :

```rust
use std::io::Write;

fn main() {
    // Handshake go-plugin
    let handshake_response = serde_json::json!({
        "Protocol": "grpc",
        "ProtocolVersion": 1,
        "Addr": "[::1]:port",
        "MagicCookieKey": "GHOSTDRIVE_PLUGIN",
        "MagicCookieValue": "storage.v1",
    });
    println!("{}", serde_json::to_string(&handshake_response).unwrap());
    std::io::stdout().flush().unwrap();
    
    // Lancer le serveur gRPC sur le port
    // ...
}
```

---

## 6. Tests

### Pattern bufconn pour les tests unitaires gRPC

L'approche `bufconn` (in-process buffer connection) permet de tester votre bridge gRPC sans vraiment faire du networking. C'est le pattern utilisé dans `plugins/grpc/client_test.go` :

```go
package grpc_test

import (
    "context"
    "net"
    "testing"
    
    "google.golang.org/grpc"
    "google.golang.org/grpc/test/bufconn"
    
    grpcbridge "github.com/CCoupel/GhostDrive/plugins/grpc"
    storagepb "github.com/CCoupel/GhostDrive/plugins/proto"
    "github.com/CCoupel/GhostDrive/plugins"
)

const bufSize = 1 << 20 // 1 MB

// Démarrer une paire serveur/client en mémoire
func newTestPair(t *testing.T, impl plugins.StorageBackend) (*grpcbridge.GRPCBackend, func()) {
    t.Helper()
    
    // Buffer listener
    lis := bufconn.Listen(bufSize)
    srv := grpc.NewServer()
    storagepb.RegisterStorageServiceServer(srv, &grpcbridge.GRPCBackendServer{Impl: impl})
    
    go func() { _ = srv.Serve(lis) }()
    
    // Client connecté au listener en mémoire
    conn, err := grpc.NewClient("passthrough://bufnet",
        grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
            return lis.DialContext(ctx)
        }),
    )
    require.NoError(t, err)
    
    backend := grpcbridge.NewGRPCBackend(conn)
    cleanup := func() {
        conn.Close()
        srv.GracefulStop()
        lis.Close()
    }
    return backend, cleanup
}

// Test exemple
func TestUpload(t *testing.T) {
    impl := &myMockBackend{}
    backend, cleanup := newTestPair(t, impl)
    defer cleanup()
    
    err := backend.Upload(context.Background(), "/local/file.txt", "/remote/file.txt", nil)
    assert.NoError(t, err)
}
```

### Tests minimaux requis pour votre plugin

| Test | Description |
|------|-------------|
| `TestConnect_Valid` | Connexion avec config valide, vérifier `IsConnected() == true` |
| `TestConnect_MissingParam` | Params obligatoires manquants, retour erreur |
| `TestConnect_BackendUnreachable` | Backend inaccessible, retour erreur descriptive |
| `TestDisconnect_AllOpsReturnErrNotConnected` | Après `Disconnect()`, vérifier que toutes les ops retournent `ErrNotConnected` |
| `TestUploadDownload_Roundtrip` | Upload → Download → contenu identique |
| `TestList_Empty` | Répertoire vide → slice vide |
| `TestList_Populated` | Répertoire peuplé → enfants corrects |
| `TestList_NotFound` | Chemin inexistant → `ErrFileNotFound` |
| `TestStat_Exists` | Fichier existant → métadonnées correctes |
| `TestStat_NotFound` | Fichier inexistant → `ErrFileNotFound` |
| `TestDelete_Exists` | Suppression d'un fichier existant |
| `TestDelete_NotFound` | Suppression inexistant → `ErrFileNotFound` |
| `TestCreateDir_New` | Créer un nouveau répertoire |
| `TestCreateDir_Exists` | Répertoire existant → no-op (pas d'erreur) |
| `TestMove_Rename` | Renommage simple `oldPath` → `newPath` |
| `TestMove_Overwrite` | Overwrite si `newPath` existe |
| `TestWatch_ContextCancelled` | Le canal se ferme quand le context est annulé |

Lancez vos tests avec :

```bash
go test ./... -v -cover
```

Visez une couverture minimale de **70%** sur votre plugin.

---

## 7. Checklist avant PR

- [ ] `go vet ./...` — aucun warning
- [ ] `go test ./... -v -cover` — couverture ≥ 70%
- [ ] `make build` (Windows) produit un binaire `.exe`
- [ ] `make build-linux` (Linux) produit un binaire sans extension
- [ ] Binaire testé avec `GRPCLoader` — vérifié que le handshake réussit
- [ ] `Name()` retourne une valeur immutable et distincte
- [ ] Pas de goroutines en fuite après `Disconnect()`
- [ ] Erreurs wrappées avec les sentinelles `plugins.ErrNotConnected` / `plugins.ErrFileNotFound`
- [ ] Commentaires godoc sur chaque méthode
- [ ] `contracts/backend-config.md` — section du plugin avec les Params obligatoires
- [ ] Branche : `feat/myplugin` ou équivalent

---

## Notes

- **Quota non-supportée** : si votre backend ne supporte pas la quota, retournez `(-1, -1, nil)`.
- **Plugin Windows uniquement** : votre binaire ne compile que pour Windows ? C'est OK, mais documentez-le. Les utilisateurs Linux verront une erreur de handshake au chargement (watchdog marquera le plugin comme "failed").
- **Threading** : `StorageBackend` doit être **thread-safe** — les appels peuvent arriver de goroutines différentes. Utilisez un `sync.RWMutex` si vous tenez de l'état.

---

## Ressources

- **Interface complète** : `plugins/plugin.go`
- **Exemple référence** : `plugins/sdk/go/echo/main.go`
- **Pattern test** : `plugins/grpc/client_test.go`
- **Loader** : `plugins/loader/grpc_loader.go`
- **SDK** : `plugins/sdk/go/`
