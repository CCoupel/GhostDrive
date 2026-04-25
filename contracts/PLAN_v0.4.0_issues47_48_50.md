# Plan d'Implémentation : v0.4.0 — Plugin LOCAL, Tests, Registry (Issues #47, #48, #50)

> **Date** : 2026-04-23
> **Branche** : `feat/v0.4.0-plugin-local`
> **Ordre imposé** : #47 → #48 → #50

---

## Contrats à mettre à jour

- [ ] `contracts/wails-bindings.md` — Ajouter `GetAvailableBackendTypes() []string` (existe dans app.go mais non documenté)

---

## Résumé

Ce plan couvre les trois issues restantes du milestone v0.4.0 :

- **#47** : Implémentation complète du plugin `local` (StorageBackend pour répertoires locaux/montés NAS/SMB/NFS)
- **#48** : Tests unitaires du plugin local (≥ 70% couverture), avec `t.TempDir()`
- **#50** : Registry de plugins dynamique (`plugins/registry.go`) + enregistrement des plugins via `init()` + mise à jour du contrat Wails

L'ordre est imposé par les dépendances : #47 doit exister avant d'être testé (#48) et avant d'être enregistré dans le registry (#50).

---

## Critères d'Acceptation

- [ ] `plugins/local/local.go` implémente `StorageBackend` — `Name()` retourne `"local"`
- [ ] Toutes les méthodes du plugin local sont thread-safe (`sync.RWMutex`)
- [ ] `Watch` utilise `fsnotify` (v1.9.0, déjà dans go.mod) et ferme le channel à `ctx.Done()`
- [ ] `go test ./plugins/local/... -cover` : couverture ≥ 70%
- [ ] `plugins/registry.go` : `Register`, `Get`, `ListBackends` fonctionnels
- [ ] `internal/backends/manager.go` utilise le registry (switch hardcodé supprimé)
- [ ] `validateBackendConfig` dans `app.go` supporte `"local"` (via registry)
- [ ] `GetAvailableBackendTypes()` retourne `["local", "moosefs", "webdav"]` (trié)
- [ ] `contracts/wails-bindings.md` documenté avec `GetAvailableBackendTypes`
- [ ] `go test ./...` : VERT
- [ ] `wails build` : PASS

---

## Composants Impactés

- **Backend** : `plugins/local/` (nouveau), `plugins/registry.go` (nouveau), `internal/backends/manager.go` (modifié), `internal/app/app.go` (modifié)
- **Frontend** : Aucun changement (bindings Wails existants suffisent)
- **Tests** : `plugins/local/local_test.go` (nouveau), `plugins/registry_test.go` (nouveau)
- **Contracts** : `contracts/wails-bindings.md` (ajout `GetAvailableBackendTypes`)

---

## Tâches

### Phase 1 — Issue #47 : Plugin LOCAL (implémentation)

**Dépendances** : Aucune (démarre en premier)
**Fichier produit** : `plugins/local/local.go` (créé)

#### Tâche 1.1 — Créer `plugins/local/local.go`

Fichier : `plugins/local/local.go` (nouveau)

Contenu attendu :

**Sentinelles** :
```go
var (
    ErrNotConnected = fmt.Errorf("local: %w", plugins.ErrNotConnected)
    ErrFileNotFound = fmt.Errorf("local: %w", plugins.ErrFileNotFound)
)
```

**Struct** :
```go
type Backend struct {
    mu        sync.RWMutex
    connected bool
    rootPath  string
}
```

**Méthodes à implémenter** :

| Méthode | Logique |
|---------|---------|
| `Name()` | Retourne `"local"` |
| `Connect(cfg)` | Extraire `cfg.Params["rootPath"]`, valider non vide, `os.Stat(rootPath)` pour vérifier existence, stocker `b.rootPath`, `b.connected = true` |
| `Disconnect()` | `b.connected = false`, `b.rootPath = ""` |
| `IsConnected()` | `b.mu.RLock()` → retourne `b.connected` |
| `absPath(rel)` (privé) | `filepath.Join(b.rootPath, filepath.FromSlash(rel))` — normalise les slashes Windows |
| `Upload(ctx, local, remote, progress)` | `os.Open(local)` → Stat pour total → `os.Create(absPath(remote))` → `io.Copy` avec progressReader |
| `Download(ctx, remote, local, progress)` | `os.Open(absPath(remote))` (mapper `ErrNotExist` → `ErrFileNotFound`) → `os.MkdirAll(dir(local))` → `os.Create(local)` → `io.Copy` avec progressWriter |
| `Delete(ctx, remote)` | `os.Remove(absPath(remote))` → mapper `ErrNotExist` → `ErrFileNotFound` |
| `Move(ctx, old, new)` | `os.Rename(absPath(old), absPath(new))` → mapper `ErrNotExist` → `ErrFileNotFound` |
| `List(ctx, path)` | `os.ReadDir(absPath(path))` → mapper `ErrNotExist` → `ErrFileNotFound` → construire `[]FileInfo` |
| `Stat(ctx, path)` | `os.Stat(absPath(path))` → mapper `ErrNotExist` → `ErrFileNotFound` → construire `*FileInfo` |
| `CreateDir(ctx, path)` | `os.Mkdir(absPath(path), 0755)` → ignorer `os.ErrExist` |
| `Watch(ctx, path)` | `fsnotify.NewWatcher()` → `watcher.Add(absPath(path))` → goroutine traduisant `watcher.Events` → `plugins.FileEvent` → fermer watcher + channel à `ctx.Done()` |

**Watch — détail** :
- Importer `github.com/fsnotify/fsnotify` (déjà dans go.mod v1.9.0)
- Traduire `fsnotify.Create` → `FileEventCreated`, `Write` → `FileEventModified`, `Remove` → `FileEventDeleted`, `Rename` → `FileEventRenamed`
- Buffer channel : 64 (obligatoire selon interface)
- Documenter dans Godoc : "Watch uses fsnotify for native push notifications on the local filesystem."

**Helpers progress** : copier `progressReader` + `progressWriter` du template (identique à webdav).

**Note** : L'entrée `init()` pour le registry sera ajoutée en Phase 3 (tâche 3.3).

---

### Phase 2 — Issue #48 : Tests plugin LOCAL

**Dépendances** : Phase 1 terminée
**Fichier produit** : `plugins/local/local_test.go` (créé)

#### Tâche 2.1 — Créer `plugins/local/local_test.go`

Fichier : `plugins/local/local_test.go` (nouveau)
Package : `local_test` (boîte noire)

**Tests à implémenter** :

| Nom du test | Ce qui est testé |
|-------------|-----------------|
| `TestConnect_Valid` | Connect avec rootPath existant → `IsConnected() == true` |
| `TestConnect_InvalidPath` | Connect avec rootPath inexistant → erreur non nil |
| `TestConnect_MissingParam` | Connect sans param `rootPath` → erreur non nil |
| `TestDisconnect` | Connect → Disconnect → `IsConnected() == false` |
| `TestErrNotConnected_Upload` | Upload sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestErrNotConnected_Download` | Download sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestErrNotConnected_Delete` | Delete sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestErrNotConnected_Move` | Move sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestErrNotConnected_List` | List sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestErrNotConnected_Stat` | Stat sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestErrNotConnected_CreateDir` | CreateDir sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestErrNotConnected_Watch` | Watch sans Connect → `errors.Is(err, plugins.ErrNotConnected)` |
| `TestUploadDownloadRoundtrip` | Écrire fichier source → Upload → Download → comparer contenu byte-à-byte |
| `TestList_Empty` | CreateDir vide → List → slice vide (non nil) |
| `TestList_WithFiles` | Upload 2 fichiers → List → vérifier noms et `IsDir` |
| `TestList_NotFound` | List répertoire inexistant → `errors.Is(err, plugins.ErrFileNotFound)` |
| `TestStat_File` | Upload → Stat → vérifier `Name`, `Size`, `IsDir==false` |
| `TestStat_Dir` | CreateDir → Stat → vérifier `IsDir==true` |
| `TestStat_NotFound` | Stat chemin inexistant → `errors.Is(err, plugins.ErrFileNotFound)` |
| `TestDelete` | Upload → Delete → Stat retourne `ErrFileNotFound` |
| `TestDelete_NotFound` | Delete chemin inexistant → `errors.Is(err, plugins.ErrFileNotFound)` |
| `TestCreateDir_Idempotent` | CreateDir deux fois → pas d'erreur |
| `TestMove` | Upload → Move → Stat oldPath `ErrNotFound`, Stat newPath OK |
| `TestWatch_ReceivesCreate` | Watch → créer fichier dans TempDir → recevoir event `FileEventCreated` (ctx avec timeout 2s) |
| `TestWatch_ClosesOnCancel` | Watch → cancel ctx → channel fermé |

**Pattern helper** :
```go
func newConnectedBackend(t *testing.T) *local.Backend {
    b := local.New()
    cfg := plugins.BackendConfig{
        Params: map[string]string{"rootPath": t.TempDir()},
    }
    require.NoError(t, b.Connect(cfg))
    return b
}
```

---

### Phase 3 — Issue #50 : Registry + Binding Wails

**Dépendances** : Phase 1 terminée (plugin local doit exister pour s'enregistrer)

#### Tâche 3.1 — Créer `plugins/registry.go`

Fichier : `plugins/registry.go` (nouveau)
Package : `plugins`

```go
// Registry maps backend type names to factory functions.
// Plugins register themselves via init():
//
//   func init() { plugins.Register("mytype", func() StorageBackend { return New() }) }
var (
    registryMu sync.RWMutex
    registry   = make(map[string]func() StorageBackend)
)

// Register associates a factory function with a backend type name.
// Called from plugin init() functions; panics on duplicate registration.
func Register(name string, factory func() StorageBackend) { ... }

// Get returns a new StorageBackend instance for the given type name.
// Returns an error if the type is not registered.
func Get(name string) (StorageBackend, error) { ... }

// ListBackends returns a sorted slice of all registered backend type names.
func ListBackends() []string { ... }
```

#### Tâche 3.2 — Créer `plugins/registry_test.go`

Fichier : `plugins/registry_test.go` (nouveau)

| Test | Ce qui est testé |
|------|-----------------|
| `TestRegister_Get` | Register("mock", factory) → Get("mock") retourne nouvelle instance |
| `TestGet_Unknown` | Get("nonexistent") → erreur wrappant "unknown" |
| `TestListBackends_Sorted` | Register plusieurs → ListBackends contient les noms triés alphabétiquement |
| `TestGet_ReturnsNewInstance` | Get deux fois → deux instances distinctes |

#### Tâche 3.3 — Ajouter `init()` dans le plugin LOCAL

Fichier modifié : `plugins/local/local.go`

En fin de fichier :
```go
// init registers this plugin in the global registry.
func init() {
    plugins.Register("local", func() plugins.StorageBackend { return New() })
}
```

> **Note** : Le registry est conçu pour accueillir des `init()` futurs. Seul `plugins/local` s'enregistre en v0.4.0. WebDAV et MooseFS ajouteront leur `init()` lors de leur implémentation (v1.x).

#### Tâche 3.4 — Modifier `internal/backends/manager.go`

Fichier modifié : `internal/backends/manager.go`

**Changements** :
1. Supprimer les imports directs `plugins/webdav` et `plugins/moosefs`
2. Ajouter import side-effect pour déclencher le `init()` du plugin local :
   ```go
   import (
       _ "github.com/CCoupel/GhostDrive/plugins/local"
   )
   ```
   (Les imports `_` webdav et moosefs seront ajoutés lors de leur enregistrement en v1.x)
3. Remplacer `InstantiateBackend` switch par :
   ```go
   func InstantiateBackend(bc plugins.BackendConfig) (plugins.StorageBackend, error) {
       return plugins.Get(bc.Type)
   }
   ```
4. Remplacer `AvailableTypes()` par :
   ```go
   func AvailableTypes() []string {
       return plugins.ListBackends()
   }
   ```

#### Tâche 3.5 — Modifier `internal/app/app.go`

Fichier modifié : `internal/app/app.go`

**Changements** :
1. `validateBackendConfig` — remplacer le check de type hardcodé :
   ```go
   // Avant :
   if bc.Type != "webdav" && bc.Type != "moosefs" {
       return fmt.Errorf("type invalide: %q", bc.Type)
   }
   // Après :
   if _, err := plugins.Get(bc.Type); err != nil {
       return fmt.Errorf("type invalide: %q", bc.Type)
   }
   ```
2. Ajouter validation `rootPath` dans le switch params de `validateBackendConfig` :
   ```go
   case "local":
       if bc.Params["rootPath"] == "" {
           return fmt.Errorf("rootPath requis pour Local")
       }
   ```
3. `GetAvailableBackendTypes()` — déjà connecté à `backends.AvailableTypes()` qui appellera `plugins.ListBackends()` via tâche 3.4 → **pas de changement requis**

#### Tâche 3.6 — Mettre à jour `contracts/wails-bindings.md`

Fichier modifié : `contracts/wails-bindings.md`

Ajouter dans la section "Configuration" (après `GetConfig`) :

```markdown
### GetAvailableBackendTypes

```
Signature : GetAvailableBackendTypes() []string
Frontend  : window.go.App.GetAvailableBackendTypes()
Retour    : Liste triée des types de backends disponibles (ex: ["local", "moosefs", "webdav"])
Erreur    : –
```

Retourne les types de plugins compilés dans le binaire. Utilisé par le formulaire d'ajout de backend pour peupler le sélecteur de type.
```

---

## Tests Requis

| Scope | Commande | Critère |
|-------|----------|---------|
| Plugin local | `go test ./plugins/local/... -v -cover` | Couverture ≥ 70% |
| Registry | `go test ./plugins/... -v` | Tous VERT |
| Intégration totale | `go test ./... -v` | Tous VERT |
| Build | `wails build` | PASS (pas de régression) |

---

## Fichiers Impactés — Récapitulatif

| Fichier | Action | Issue |
|---------|--------|-------|
| `plugins/local/local.go` | **CRÉÉ** | #47 |
| `plugins/local/local_test.go` | **CRÉÉ** | #48 |
| `plugins/registry.go` | **CRÉÉ** | #50 |
| `plugins/registry_test.go` | **CRÉÉ** | #50 |
| `plugins/local/local.go` | modifié — `init()` ajouté (Phase 3) | #50 |
| `internal/backends/manager.go` | modifié — registry + imports side-effect | #50 |
| `internal/app/app.go` | modifié — `validateBackendConfig` "local" | #50 |
| `contracts/wails-bindings.md` | modifié — `GetAvailableBackendTypes` | #50 |

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| `init()` non déclenché si plugin non importé | Moyen | Élevé | Imports `_` obligatoires dans `manager.go` (tâche 3.4) |
| Upload cross-filesystem si `local` ≠ `rootPath` FS | Faible | Moyen | Utiliser `io.Copy` (pas `os.Rename`) — déjà prévu |
| Goroutine Watch fuite si `ctx` jamais annulé | Moyen | Moyen | `TestWatch_ClosesOnCancel` valide ce cas |
| Chemin Windows `D:\path` + slashes mixtes | Moyen | Moyen | Helper `absPath` avec `filepath.FromSlash` — géré en tâche 1.1 |
| `TestWatch_ReceivesCreate` flaky (timing fsnotify) | Moyen | Faible | Context timeout 2s + channel bufférisé (64) |
| Registry panic sur double Register | Faible | Moyen | Documenter l'interdit dans le Godoc de `Register` |

---

## Estimation

- **Complexité** : Moyenne
- **Nombre de fichiers** : 8 (4 créés, 4 modifiés)
- **Dépendances entre phases** : linéaires (#47 → #48 → #50)
- **Pas de nouveau binaire requis** : fsnotify déjà en go.mod

---

## Notes Architecturales

1. **`fsnotify` vs polling** : Le plugin local utilise fsnotify (push natif) contrairement au webdav qui utilise polling 30s. Documenter dans le Godoc de Watch.

2. **Registry dans package `plugins`** : Placer `registry.go` dans `plugins` (pas `internal/`) permet aux plugins de s'auto-enregistrer sans import circulaire, car les plugins importent déjà `plugins`.

3. **Side-effect imports dans `manager.go`** : Alternative à une fonction `RegisterAll()` centrale — plus extensible car ajouter un plugin = ajouter un import `_` dans `manager.go` et un `init()` dans le plugin.

4. **`GetAvailableBackendTypes()` vs `ListBackends()`** : Le binding Wails existant s'appelle `GetAvailableBackendTypes()` et est déjà utilisé. Pas de renommage pour éviter de casser le frontend. La méthode interne du registry s'appelle `ListBackends()` (nom demandé par l'issue #50).

5. **`validateBackendConfig` via registry** : Plus robuste que le switch hardcodé — tout nouveau plugin enregistré est automatiquement validé sans modifier `app.go`.
