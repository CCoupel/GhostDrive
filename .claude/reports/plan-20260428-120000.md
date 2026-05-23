# Plan d'Implémentation : v0.6.x — Plugin Loader go-plugin

> **Date** : 2026-04-28  
> **Issues** : #64, #65, #67, #68  
> **Branche cible** : `feat/v0.6.x-plugin-loader`  
> **Hors scope** : Extism #66, #70 (reportés v0.7.x)

---

## Contrats API

- [x] `contracts/plugin-loader-bindings.md` — nouveaux bindings Wails : `GetLoadedPlugins`, `ReloadPlugins`
- [x] `contracts/CHANGELOG.md` — [NEW] `GetLoadedPlugins`, `ReloadPlugins` ; [CHANGED] `GetAvailableBackendTypes` (inclut plugins dynamiques)
- [ ] `plugins/proto/storage.proto` — contrat gRPC (source of truth, .pb.go non commités)

---

## Résumé

v0.6.x introduit un système de plugins dynamiques basé sur **HashiCorp go-plugin + gRPC + Protobuf**.
Les plugins tiers compilent en `.exe` indépendant et sont chargés au démarrage depuis `<AppDir>/plugins/*.exe`.
Le registry statique existant (`local`, `webdav`, `moosefs`) est **conservé sans modification** ; le registry dynamique s'y ajoute.

---

## Critères d'Acceptation

- [ ] `plugins/proto/storage.proto` définit l'ensemble des méthodes de `StorageBackend`
- [ ] `GRPCBackend` (client) implémente `StorageBackend` à 100% et passe les tests unitaires avec mock gRPC
- [ ] `GRPCBackendServer` compile et expose les méthodes proto pour les plugins tiers
- [ ] `grpc_loader.go` scanne `<AppDir>/plugins/*.exe`, lance chaque plugin via go-plugin, l'enregistre dans `plugins.Register()`
- [ ] Supervision : un plugin crashé redémarre automatiquement (max 3 tentatives, backoff exponentiel)
- [ ] `dynamic_registry.go` intégré dans `app.Startup()` **avant** la reconnexion des backends sauvegardés
- [ ] `GetAvailableBackendTypes()` retourne les types statiques + dynamiques
- [ ] `GetLoadedPlugins()` retourne la liste des plugins dynamiques avec leur statut
- [ ] `ReloadPlugins()` rescanne le dossier sans redémarrage de l'app
- [ ] SDK : plugin `echo` compile en `echo.exe` via `make build` et s'intègre sans erreur
- [ ] CI : `protoc` génère les stubs avant `go test`, couverture ≥ 70% maintenue
- [ ] Aucune régression sur les 147+ tests existants

---

## Composants Impactés

- **Backend** : `plugins/proto/`, `plugins/grpc/`, `plugins/loader/`, `plugins/registry/`, `plugins/sdk/go/`
- **Frontend** : Nouveau binding `GetLoadedPlugins` lisible (UI en v0.6.1+, pas dans ce scope)
- **Infrastructure** : `go.mod` (3 nouvelles dépendances), `.github/workflows/ci.yml` (étape protoc)

---

## Nouvelles Dépendances go.mod

```
google.golang.org/grpc           v1.x   — transport gRPC
google.golang.org/protobuf       v1.x   — runtime protobuf
github.com/hashicorp/go-plugin   v1.x   — loader subprocess + handshake
```

---

## Tâches

### Phase 0 : Fondations (dépendances + CI)

1. [ ] **Ajouter les dépendances go.mod**
   - Fichier(s) : `go.mod`, `go.sum`
   - Commande : `go get google.golang.org/grpc google.golang.org/protobuf github.com/hashicorp/go-plugin`
   - Contrainte : vérifier la compatibilité CGO (go-plugin est pure Go, pas de CGO requis)

2. [ ] **Mettre à jour `.github/workflows/ci.yml` — étape protoc**
   - Fichier(s) : `.github/workflows/ci.yml`
   - Ajouter avant `go vet` :
     ```yaml
     - name: Install protoc + plugins
       run: |
         sudo apt-get install -y protobuf-compiler
         go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
         go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
     - name: Generate protobuf stubs
       run: protoc --go_out=. --go-grpc_out=. plugins/proto/storage.proto
     ```
   - Les fichiers `.pb.go` sont générés à chaque CI et **non commités**
   - Ajouter `.pb.go` dans `.gitignore` (pattern : `plugins/proto/*.pb.go`)

3. [ ] **Mettre à jour `.gitignore`**
   - Fichier(s) : `.gitignore`
   - Ajouter : `plugins/proto/*.pb.go`

---

### Phase 1 : Contrat gRPC — Proto + Stubs (#64)

4. [ ] **Créer `plugins/proto/storage.proto`**
   - Fichier(s) : `plugins/proto/storage.proto`
   - Package proto : `ghostdrive.storage.v1`
   - Options Go : `option go_package = "github.com/CCoupel/GhostDrive/plugins/proto;storagepb"`
   - Méthodes RPC à définir :

   | RPC | Type | Correspondance Go |
   |-----|------|-------------------|
   | `Name` | Unary | `Name() string` |
   | `Connect` | Unary | `Connect(BackendConfig) error` |
   | `Disconnect` | Unary | `Disconnect() error` |
   | `IsConnected` | Unary | `IsConnected() bool` |
   | `Upload` | Client-streaming | `Upload(ctx, local, remote, progress)` |
   | `Download` | Server-streaming | `Download(ctx, remote, local, progress)` |
   | `Delete` | Unary | `Delete(ctx, remote)` |
   | `Move` | Unary | `Move(ctx, oldPath, newPath)` |
   | `List` | Unary | `List(ctx, path) []FileInfo` |
   | `Stat` | Unary | `Stat(ctx, path) *FileInfo` |
   | `CreateDir` | Unary | `CreateDir(ctx, path)` |
   | `Watch` | Server-streaming | `Watch(ctx, path) <-chan FileEvent` |
   | `GetQuota` | Unary | `GetQuota(ctx) (free, total, err)` |

   - Messages à définir : `BackendConfigProto`, `FileInfoProto`, `FileEventProto`, `UploadChunk`, `UploadResult`, `DownloadChunk`, `WatchEvent`, `QuotaResponse`
   - Mapping erreurs : champ `error string` dans chaque réponse, sentinel errors mappées à `codes.NotFound` / `codes.FailedPrecondition`

   > **Note conception** :
   > - `Upload` client-streaming : le client envoie des chunks + métadonnées en premier message. La progress est calculée localement (bytes envoyés / taille fichier).
   > - `Download` server-streaming : le serveur envoie des chunks avec `bytes_done` + `bytes_total` pour que le client invoque la ProgressCallback.
   > - `Watch` server-streaming : le serveur envoie des `WatchEvent` jusqu'à annulation du contexte.

5. [ ] **Implémenter `plugins/grpc/client.go` — GRPCBackend**
   - Fichier(s) : `plugins/grpc/client.go`
   - Struct `GRPCBackend` qui satisfait `plugins.StorageBackend`
   - Reçoit une connexion `*grpc.ClientConn` (fournie par le loader)
   - `Upload` : lit le fichier local, stream les chunks, compte les bytes pour la ProgressCallback
   - `Download` : reçoit les chunks, écrit localement, invoque ProgressCallback via `bytes_done`/`bytes_total`
   - `Watch` : consomme le stream server, convertit `WatchEvent` proto → `plugins.FileEvent`, écrit dans le channel retourné (buffer 64)
   - Gestion de `ctx.Done()` pour fermer proprement le stream Watch
   - Mapping erreurs gRPC → sentinel errors Go (`codes.NotFound` → `plugins.ErrFileNotFound`, `codes.FailedPrecondition` → `plugins.ErrNotConnected`)

6. [ ] **Implémenter `plugins/grpc/server.go` — GRPCBackendServer**
   - Fichier(s) : `plugins/grpc/server.go`
   - Interface `GRPCPluginImpl` que les plugins tiers implémentent (wrappée autour de `StorageBackend`)
   - Struct `GRPCBackendServer` qui implémente le service proto généré
   - Délègue chaque RPC à une instance `StorageBackend` fournie par le plugin tiers
   - `Upload` : reçoit les chunks, reconstruit le fichier dans un temp dir, appelle `backend.Upload()`
   - `Download` : appelle `backend.Download()`, streame le résultat en chunks (taille chunk : 64 KB)
   - `Watch` : appelle `backend.Watch()`, lit le channel, envoie chaque event jusqu'à `ctx.Done()`

7. [ ] **Écrire les tests `plugins/grpc/client_test.go`**
   - Fichier(s) : `plugins/grpc/client_test.go`
   - Mock gRPC server in-process (`bufconn`)
   - Tester : Connect/Disconnect, List, Stat, Watch (goroutine + cancel), Upload (petit fichier), Download (petit fichier)
   - Tester le mapping d'erreurs gRPC → sentinel

---

### Phase 2 : Plugin Loader go-plugin (#65)

8. [ ] **Implémenter `plugins/loader/grpc_loader.go`**
   - Fichier(s) : `plugins/loader/grpc_loader.go`
   - Struct `GRPCLoader` avec :
     ```go
     type GRPCLoader struct {
         pluginsDir string
         clients    map[string]*plugin.Client  // nom → go-plugin client
         mu         sync.RWMutex
     }
     ```
   - Méthodes :
     - `Scan(pluginsDir string) error` — scanne `*.exe`, lance chaque plugin, enregistre
     - `Shutdown() error` — kill tous les plugins proprement
     - `GetLoadedPlugins() []PluginInfo` — liste des plugins chargés avec statut
   - Protocole go-plugin :
     - `HandshakeConfig` avec `ProtocolVersion=1`, `MagicCookieKey="GHOSTDRIVE_PLUGIN"`, `MagicCookieValue="storage.v1"`
     - `PluginMap` : map `"storage"` → `GRPCPlugin` (wrapper go-plugin autour de GRPCBackend)
   - Pour chaque `.exe` scanné :
     1. Lancer via `plugin.NewClient()` avec `Cmd: exec.Command(path)`, `AllowedProtocols: [ProtocolGRPC]`
     2. Obtenir `grpc.ClientConn` via `client.GRPCClient()`
     3. Instancier `GRPCBackend` avec la conn
     4. Appeler `backend.Name()` pour obtenir le nom du type
     5. Appeler `plugins.Register(name, factory)` où `factory` retourne un nouveau `GRPCBackend` sur la même conn
     6. Stocker `*plugin.Client` dans `clients[name]` pour supervision + shutdown
   - **Supervision crash** :
     - Lancer une goroutine de watchdog par plugin
     - Si `client.Exited()` → attendre (backoff : 1s, 2s, 4s) et relancer jusqu'à 3 tentatives
     - Après 3 échecs : marquer le plugin `status=failed`, émettre un log

9. [ ] **Écrire les tests `plugins/loader/grpc_loader_test.go`**
   - Fichier(s) : `plugins/loader/grpc_loader_test.go`
   - Test de scan avec dossier vide (no-op)
   - Test de scan avec un .exe inexistant (erreur gracieuse)
   - Test de `Shutdown()` avec plugins non chargés

---

### Phase 3 : Registry Dynamique (#67)

10. [ ] **Créer `plugins/registry/dynamic_registry.go`**
    - Fichier(s) : `plugins/registry/dynamic_registry.go`
    - Struct `DynamicRegistry` :
      ```go
      type DynamicRegistry struct {
          loader    *loader.GRPCLoader
          pluginDir string
      }
      ```
    - Méthodes :
      - `NewDynamicRegistry(pluginsDir string) *DynamicRegistry`
      - `Start() error` — appelle `loader.Scan()`, enregistre les plugins
      - `Stop() error` — appelle `loader.Shutdown()`
      - `ListAvailablePlugins() []PluginInfo` — static + dynamic (statique = `plugins.ListBackends()` natifs)
      - `Reload() error` — `Stop()` + `Start()` (invalidation + rescan)
    - **Coexistence avec le registry statique** :
      - Les plugins statiques (`local`, `webdav`, `moosefs`) sont déjà enregistrés via `init()` dans leurs packages
      - Les plugins dynamiques appellent `plugins.Register()` avec leur nom → apparaissent dans `plugins.ListBackends()`
      - Pas de modification de `plugins/registry.go` (statique)

11. [ ] **Écrire les tests `plugins/registry/dynamic_registry_test.go`**
    - Fichier(s) : `plugins/registry/dynamic_registry_test.go`
    - Test `Start()` avec dossier plugins vide (aucun .exe → aucune erreur)
    - Test `ListAvailablePlugins()` retourne au minimum `["local", "webdav", "moosefs"]`

12. [ ] **Intégrer le registry dynamique dans `internal/app/app.go`**
    - Fichier(s) : `internal/app/app.go`
    - Ajouter un champ `dynRegistry *registry.DynamicRegistry` dans `App`
    - Dans `Startup()`, **avant** la boucle de reconnexion des backends sauvegardés :
      ```go
      // Scan plugins/*.exe and register dynamic backends
      appDir := filepath.Dir(os.Executable()) // ou via Wails runtime
      a.dynRegistry = registry.NewDynamicRegistry(filepath.Join(appDir, "plugins"))
      if err := a.dynRegistry.Start(); err != nil {
          log.Printf("app: plugin scan: %v", err)
      }
      ```
    - Dans `Shutdown()` : `a.dynRegistry.Stop()`
    - Modifier `GetAvailableBackendTypes()` pour appeler `a.dynRegistry.ListAvailablePlugins()` si dynRegistry non nil, sinon fallback sur `backends.AvailableTypes()`
    - **Point d'attention** : `validateBackendConfig()` appelle `plugins.Get(bc.Type)` → si le plugin dynamique est enregistré avant la validation, ça marche sans modification

13. [ ] **Ajouter les nouveaux bindings Wails dans `internal/app/app.go`**
    - Fichier(s) : `internal/app/app.go`
    - Méthodes à ajouter :
      ```go
      // GetLoadedPlugins retourne les plugins dynamiques chargés avec leur statut.
      func (a *App) GetLoadedPlugins() []registry.PluginInfo

      // ReloadPlugins rescanne <AppDir>/plugins/*.exe sans redémarrage.
      func (a *App) ReloadPlugins() error
      ```
    - `GetLoadedPlugins()` : délègue à `a.dynRegistry.ListAvailablePlugins()` filtré sur plugins dynamiques uniquement
    - `ReloadPlugins()` : délègue à `a.dynRegistry.Reload()`

---

### Phase 4 : Plugin SDK (#68)

14. [ ] **Créer le template SDK `plugins/sdk/go/`**
    - Fichier(s) : `plugins/sdk/go/plugin.go` (interface wrapper), `plugins/sdk/go/main.go` (entry point template)
    - `main.go` template — point d'entrée standard d'un plugin go-plugin :
      ```go
      func main() {
          plugin.Serve(&plugin.ServeConfig{
              HandshakeConfig: ghostdrive.HandshakeConfig,
              Plugins: plugin.PluginSet{
                  "storage": &grpc.GRPCPlugin{Impl: &MyPlugin{}},
              },
              GRPCServer: plugin.DefaultGRPCServer,
          })
      }
      ```
    - Exporter `HandshakeConfig` depuis `plugins/loader/grpc_loader.go` pour que le SDK puisse l'importer

15. [ ] **Créer le plugin d'exemple `echo`**
    - Fichier(s) : `plugins/sdk/go/echo/main.go`
    - Implémente `GRPCPluginImpl` (satisfait `StorageBackend`)
    - `Name()` retourne `"echo"` 
    - `Connect()` : valide que `rootPath` param est présent
    - `List()` : retourne une liste statique `["echo-file.txt"]`
    - `Upload()` / `Download()` : log + retourne success
    - `Watch()` : retourne un channel vide (pas de surveillance)
    - `GetQuota()` : retourne `(-1, -1, nil)`

16. [ ] **Créer `plugins/sdk/go/Makefile`**
    - Fichier(s) : `plugins/sdk/go/Makefile`
    - Cibles : `build` (compile `echo.exe` Windows AMD64), `clean`
    - Cross-compile depuis Linux via `GOOS=windows GOARCH=amd64 go build`

17. [ ] **Créer `plugins/sdk/go/README.md`**
    - Guide en 5 étapes :
      1. Copier le template `plugins/sdk/go/`
      2. Implémenter les méthodes de `StorageBackend`
      3. `make build` → `myplugin.exe`
      4. Placer dans `<AppDir>/plugins/myplugin.exe`
      5. Relancer GhostDrive (ou appeler `ReloadPlugins()`)

---

### Phase 5 : Contrats + Documentation

18. [ ] **Créer `contracts/plugin-loader-bindings.md`**
    - Fichier(s) : `contracts/plugin-loader-bindings.md`
    - Documenter :
      - `GetLoadedPlugins() []PluginInfo`
      - `ReloadPlugins() error`
      - `PluginInfo` struct
      - Événements Wails émis par le loader (ex: `plugin:loaded`, `plugin:failed`)

19. [ ] **Créer `contracts/CHANGELOG.md`**
    - Fichier(s) : `contracts/CHANGELOG.md`
    - Entrée :
      ```
      ## [20260428] — v0.6.x plugin-loader

      - [NEW] `GetLoadedPlugins() []PluginInfo` — liste des plugins dynamiques chargés
      - [NEW] `ReloadPlugins() error` — rescan sans redémarrage
      - [CHANGED] `GetAvailableBackendTypes()` — inclut désormais les plugins dynamiques (rétrocompatible)
      ```

---

## Tests Requis

- [ ] **Tests unitaires** : `plugins/grpc/client_test.go` — GRPCBackend avec mock gRPC (bufconn), tous les happy paths + error paths
- [ ] **Tests unitaires** : `plugins/loader/grpc_loader_test.go` — scan dossier vide, scan avec .exe invalide, shutdown propre
- [ ] **Tests unitaires** : `plugins/registry/dynamic_registry_test.go` — Start/Stop/Reload, ListAvailablePlugins inclut les statiques
- [ ] **Tests intégration** : test end-to-end loader → echo plugin (nécessite `echo.exe` pré-compilé, exécution sur Windows uniquement ou skip en CI Linux via `t.Skip()`)
- [ ] **Régression** : `go test ./... -cover` — vérifier que la couverture reste ≥ 70%
- [ ] **Build** : `go build ./...` sans erreur
- [ ] **Proto** : `protoc` sans warning (`--fatal_warnings`)

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| Incompatibilité go-plugin + CGO (WinFsp) | Moyen | Élevé | go-plugin est pure Go, pas de CGO ; tester sur matrice CI séparément |
| Complexité Upload client-streaming + ProgressCallback | Moyen | Moyen | Progress calculée localement (bytes envoyés) sans aller-retour gRPC |
| Conflit de nom entre plugin statique et dynamique | Faible | Élevé | `Register()` écrase silencieusement → documenter : ne pas nommer un plugin dynamique `local`/`webdav`/`moosefs` |
| Crash watchdog fuite goroutine | Moyen | Moyen | Utiliser `context.WithCancel` pour fermer le watchdog à Shutdown |
| protoc version drift CI | Faible | Moyen | Pinner la version de protoc dans le step CI (`PB_REL=3.25.1`) |
| `os.Executable()` dans Wails sous Windows | Faible | Moyen | Tester `os.Executable()` vs chemin Wails runtime, fallback sur `filepath.Dir(os.Args[0])` |
| Temps de scan au démarrage si beaucoup de plugins | Faible | Faible | Scan asynchrone avec timeout 5s par plugin |

---

## Estimation

- **Complexité** : Élevée (gRPC + go-plugin + supervision)
- **Nombre de fichiers nouveaux** : ~14
- **Fichiers modifiés** : 4 (`go.mod`, `ci.yml`, `.gitignore`, `internal/app/app.go`)
- **Phases** : 5 phases, séquentielles (P1 → P2 → P3 → P4, P5 en parallèle de P4)

---

## Notes Architecturales

### Flux d'appel : chargement d'un plugin dynamique

```
app.Startup()
  └── DynamicRegistry.Start()
        └── GRPCLoader.Scan("<AppDir>/plugins/")
              ├── exec("echo.exe") via go-plugin
              ├── gRPC handshake
              ├── GRPCBackend.Name() → "echo"
              └── plugins.Register("echo", factory)
                    └── [accessible via plugins.Get("echo") et plugins.ListBackends()]

app.AddBackend({Type: "echo", ...})
  └── validateBackendConfig() → plugins.Get("echo") ✓
  └── InstantiateBackend() → factory() → GRPCBackend{conn}
  └── backend.Connect(bc) → gRPC Connect RPC
```

### Coexistence des registries

```
plugins.registry (map interne)
  ├── "local"   ← enregistré par plugins/local/init()
  ├── "webdav"  ← enregistré par plugins/webdav/init()
  ├── "moosefs" ← enregistré par plugins/moosefs/init()
  └── "echo"    ← enregistré par GRPCLoader.Scan() au démarrage
```

Le registry statique `plugins/registry.go` n'est **pas modifié**. Le loader appelle simplement `plugins.Register()` pour les plugins dynamiques.

### Exclusion des stubs .pb.go du dépôt

```
.gitignore :
plugins/proto/*.pb.go    # généré par protoc à chaque CI build

CI step (avant go vet) :
  protoc --go_out=. --go-grpc_out=. plugins/proto/storage.proto
```

### HandshakeConfig partagé SDK ↔ Loader

```go
// Exporté depuis plugins/loader/grpc_loader.go
var HandshakeConfig = plugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   "GHOSTDRIVE_PLUGIN",
    MagicCookieValue: "storage.v1",
}
```

Le SDK importe ce package pour garantir la compatibilité.
