# dev-backend — adaptations GhostDrive

Complete .claude/agents/dev-backend.template.md ; en cas de divergence explicite ci-dessous, ce fichier fait foi.

## Regles projet

- Commits atomiques avec messages conventionnels (`feat(sync): ...`, `fix(webdav): ...`, `plugin(moosefs): ...`)

## Expertise GhostDrive

- Go 1.21+ — moteur de synchronisation
- Wails v2 — bindings Go backend exposés au frontend React
- Interface plugin `StorageBackend` — abstraction des backends de stockage
- Windows Cloud Filter API (`cfapi`) — placeholders Files On-Demand
- WinFsp — alternative/complement pour montage filesystem
- `golang.org/x/net/webdav` — serveur WebDAV in-memory pour tests
- `testify` — assertions et mocks pour tests unitaires

## Structure Projet

```
ghostdrive/
├── cmd/
│   └── ghostdrive/
│       └── main.go           # Point d'entree Wails (wails.Run)
├── internal/
│   ├── app/                  # App Wails — bindings exposes au frontend
│   │   └── app.go
│   ├── sync/                 # Moteur de synchronisation
│   │   ├── engine.go         # Orchestration sync bidirectionnelle
│   │   ├── watcher.go        # Surveillance filesystem local
│   │   ├── conflict.go       # Resolution de conflits
│   │   └── engine_test.go
│   ├── placeholder/          # Files On-Demand (Windows)
│   │   ├── provider.go       # Cloud Filter API provider
│   │   ├── hydration.go      # Telechargement a la demande
│   │   └── provider_test.go
│   ├── cache/                # Cache local
│   │   ├── manager.go
│   │   └── manager_test.go
│   └── config/               # Configuration application
│       ├── config.go
│       └── config_test.go
├── plugins/
│   ├── plugin.go             # Interface StorageBackend
│   ├── registry.go           # Registre des plugins disponibles
│   ├── webdav/               # Plugin WebDAV (V1)
│   │   ├── webdav.go
│   │   └── webdav_test.go
│   └── moosefs/              # Plugin MooseFS (V1)
│       ├── moosefs.go
│       └── moosefs_test.go
├── contracts/                # Contrats Wails (Go <-> React)
└── tests/                    # Tests d'integration
    └── sync_integration_test.go
```

## Interface Plugin (contrat public)

```go
// plugins/plugin.go — NE PAS MODIFIER sans mise a jour de tous les plugins

type BackendConfig map[string]interface{}

type FileInfo struct {
    Name    string
    Path    string
    Size    int64
    ModTime time.Time
    IsDir   bool
}

type FileEvent struct {
    Type FileEventType // Created, Modified, Deleted, Renamed
    Path string
    OldPath string // pour Renamed
}

type ProgressCallback func(bytesTransferred, totalBytes int64)

type StorageBackend interface {
    // Identification
    Name() string
    Version() string

    // Connexion
    Connect(config BackendConfig) error
    Disconnect() error
    IsConnected() bool

    // Operations fichiers
    Upload(ctx context.Context, local, remote string, progress ProgressCallback) error
    Download(ctx context.Context, remote, local string, progress ProgressCallback) error
    Delete(ctx context.Context, remote string) error
    Move(ctx context.Context, oldPath, newPath string) error

    // Navigation
    List(ctx context.Context, path string) ([]FileInfo, error)
    Stat(ctx context.Context, path string) (*FileInfo, error)
    CreateDir(ctx context.Context, path string) error

    // Surveillance (pour sync temps reel)
    Watch(ctx context.Context, path string) (<-chan FileEvent, error)
}
```

**Regles absolues pour les plugins** :
- Implémenter l'integralite de l'interface
- Tester avec mock ou serveur in-memory (jamais d'infra reelle en CI)
- Documenter la config dans `docs/plugins/<nom>.md`

## Bindings Wails (contracts/)

Les bindings Wails sont les methodes de `App` accessibles depuis le frontend.
Format de contrat dans `contracts/wails-bindings.md` :

```markdown
### GetBackends() []BackendInfo
Retourne la liste des plugins disponibles et leur etat de connexion.

### AddSyncPoint(localPath, remotePath, backendName string) error
Ajoute un point de synchronisation.
```

**Regle** : creer le contrat AVANT d'implementer la methode.

## Conventions

### Gestion d'Erreurs

```go
// Toujours wrapper avec contexte
if err != nil {
    return fmt.Errorf("webdav: upload %s: %w", remotePath, err)
}

// Erreurs custom pour les plugins
var ErrNotConnected = errors.New("backend not connected")
var ErrFileNotFound = errors.New("remote file not found")
```

### Tests (table-driven)

```go
func TestWebDAVUpload(t *testing.T) {
    // Demarrer un serveur WebDAV in-memory
    handler := &webdav.Handler{
        FileSystem: webdav.NewMemFS(),
        LockSystem: webdav.NewMemLS(),
    }
    srv := httptest.NewServer(handler)
    defer srv.Close()

    tests := []struct {
        name    string
        local   string
        remote  string
        wantErr bool
    }{
        {"upload simple", "testdata/file.txt", "/remote/file.txt", false},
        {"upload dossier inexistant", "testdata/missing.txt", "/remote/", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            backend := &WebDAVBackend{}
            backend.Connect(BackendConfig{"url": srv.URL})
            err := backend.Upload(context.Background(), tt.local, tt.remote, nil)
            if (err != nil) != tt.wantErr {
                t.Errorf("Upload() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

<!-- Derogation projet : commandes build/test -->
## Commandes

```bash
# Build binaire Windows depuis Linux/WSL
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o ghostdrive.exe ./cmd/ghostdrive

# Build avec Wails (inclut frontend)
wails build -platform windows/amd64

# Tests
go test ./... -v -cover -coverprofile=coverage.out

# Couverture
go tool cover -html=coverage.out
```

## Cloud Filter API (Placeholders)

La fonctionnalite Files On-Demand est le coeur de V1.
Package Windows : `github.com/wailsapp/wails/v2/pkg/options` + bindings `cfapi`.

```go
// internal/placeholder/provider.go
// Enregistre GhostDrive comme Cloud Provider Windows
// Les fichiers apparaissent dans l'explorateur mais ne sont pas telecharges
// jusqu'a ce que l'utilisateur les ouvre (hydration a la demande)
```

**Attention** : Cette API est Windows-only. Utiliser des build tags :
```go
//go:build windows
// +build windows
```

### Cross-compile CF API (Linux → Windows)

- `CGO_LDFLAGS=-lcldapi -lole32` ; `libcldapi.a` genere depuis `internal/cfapi/cldapi.def` (31 exports, bundle)
  via `x86_64-w64-mingw32-dlltool` (`sudo` en CI) — le runner Linux n'a pas `cldapi.dll`.
- `IID_IAsyncInfo` : ne pas lier `-lruntimeobject` de MinGW — definir `static const GUID IID_GHD_IAsyncInfo`
  dans le `.c`.

<!-- Derogation projet : checklist implementation -->
## Checklist Implementation

- [ ] Modele(s) / interface(s) dans le bon package
- [ ] Tests unitaires (coverage >70%)
- [ ] Tests integration avec serveur in-memory si backend
- [ ] Contrat Wails dans `contracts/` si nouveau binding
- [ ] Build tags windows/linux si code plateforme-specifique
- [ ] Gestion d'erreurs avec contexte
- [ ] Documentation dans `docs/` si nouveau plugin
