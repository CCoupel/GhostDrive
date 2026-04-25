# Guide d'implémentation de plugin GhostDrive

> **Version** : v0.4.0
> **Audience** : développeurs Go (humains et agents Claude Code)
> **Temps estimé** : 2-4 h pour un backend simple (filesystem, HTTP)

---

## 1. Vue d'ensemble

### Architecture plugin

GhostDrive utilise une architecture **plugin Go compilé** : chaque backend de stockage est un package Go indépendant sous `plugins/<nom>/` qui implémente l'interface `plugins.StorageBackend`. Les plugins sont **compilés dans le binaire principal** — il n'y a pas de chargement dynamique (.dll / .so / gRPC).

```
plugins/
├── plugin.go          ← interface StorageBackend + types partagés + sentinelles
├── registry.go        ← factory : BackendConfig.Type → StorageBackend
├── webdav/            ← plugin WebDAV (référence)
├── moosefs/           ← plugin MooseFS
├── local/             ← plugin Local (v0.4.0, issue #47)
└── template/
    └── template.go    ← squelette à copier (//go:build ignore)
```

### Cycle de vie d'un plugin

```
New()
  └── Connect(config)
        ├── Watch(ctx, path) ─► channel events
        ├── Upload / Download / Delete / Move / List / Stat / CreateDir
        └── Disconnect()
```

- **`New()`** : crée une instance non connectée. Aucun I/O.
- **`Connect()`** : valide la config, probe le backend, établit la connexion.
- Les opérations fichiers requièrent `IsConnected() == true`.
- **`Disconnect()`** : libère les ressources, ferme les goroutines.

---

## 2. L'interface `StorageBackend` — référence complète

Source : `plugins/plugin.go`

### Types partagés

#### `FileInfo`

```go
type FileInfo struct {
    Name          string    // nom de base (sans séparateur)
    Path          string    // chemin slash-séparé relatif à RemotePath
    Size          int64     // taille en octets (0 pour les répertoires)
    IsDir         bool
    ModTime       time.Time
    ETag          string    // token de version HTTP ou équivalent (peut être vide)
    IsPlaceholder bool      // fichier Files-On-Demand non hydraté
    IsCached      bool      // contenu présent dans le cache local
}
```

#### `FileEvent` / `FileEventType`

```go
type FileEventType string
const (
    FileEventCreated  FileEventType = "created"
    FileEventModified FileEventType = "modified"
    FileEventDeleted  FileEventType = "deleted"
    FileEventRenamed  FileEventType = "renamed"
)

type FileEvent struct {
    Type      FileEventType
    Path      string    // chemin affecté
    OldPath   string    // ancien chemin (FileEventRenamed uniquement)
    Timestamp time.Time
    Source    string    // "local" | "remote"
}
```

#### `BackendConfig`

```go
type BackendConfig struct {
    ID         string            // identifiant unique de l'instance
    Name       string            // label affiché dans l'UI
    Type       string            // "webdav" | "moosefs" | "local"
    Enabled    bool
    Params     map[string]string // clés spécifiques au plugin (voir contracts/backend-config.md)
    SyncDir    string            // chemin local absolu à synchroniser
    RemotePath string            // racine sur le remote (ex: "/GhostDrive")
}
```

#### `ProgressCallback`

```go
type ProgressCallback func(done, total int64)
// done  : octets transférés jusqu'ici
// total : taille totale attendue (-1 si inconnue)
// Ne doit pas bloquer.
```

### Erreurs sentinelles

| Sentinel | Utilisation |
|----------|-------------|
| `plugins.ErrNotConnected` | Toute opération appelée sans connexion active |
| `plugins.ErrFileNotFound` | Chemin distant inexistant |

**Convention de wrapping** :
```go
// Correct — permet errors.Is(err, plugins.ErrNotConnected)
return fmt.Errorf("myplugin: upload %s: %w", remote, plugins.ErrNotConnected)

// Incorrect — casse errors.Is
return errors.New("myplugin: not connected")
```

### Méthodes de l'interface

| Méthode | Pré-condition | Comportement |
|---------|--------------|--------------|
| `Name() string` | aucune | Retourne l'identifiant du plugin. Immuable. |
| `Connect(cfg)` | aucune | Valide les params, probe le backend, set `connected = true` |
| `Disconnect()` | aucune | Libère ressources, `connected = false`. No-op si déjà déconnecté. |
| `IsConnected()` | aucune | Thread-safe, pas d'I/O |
| `Upload(ctx, local, remote, cb)` | connecté | Crée/remplace le fichier distant. Répertoires intermédiaires **non** créés. |
| `Download(ctx, remote, local, cb)` | connecté | Crée les répertoires locaux parents si absents. Retourne `ErrFileNotFound` si absent. |
| `Delete(ctx, remote)` | connecté | Retourne `ErrFileNotFound` si absent. Suppression récursive : behavior défini par plugin. |
| `Move(ctx, old, new)` | connecté | Écrase `new` s'il existe déjà. |
| `List(ctx, path)` | connecté | Retourne slice vide (jamais nil) si vide. N'inclut pas le répertoire lui-même. |
| `Stat(ctx, path)` | connecté | Retourne `ErrFileNotFound` si absent. |
| `CreateDir(ctx, path)` | connecté | No-op si le répertoire existe déjà. Ne crée pas les parents. |
| `Watch(ctx, path)` | connecté | Channel fermé quand `ctx` est annulé. Buffer ≥ 64. |

---

## 3. Créer un plugin — guide pas-à-pas

### Étape 1 — Copier le template

```bash
cp plugins/template/template.go plugins/<nom>/<nom>.go
```

### Étape 2 — Adapter le package et les identifiants

1. Changer `package template` → `package <nom>`
2. **Supprimer** la ligne `//go:build ignore` (première ligne)
3. Remplacer tous les occurrences de `"template"` dans les identifiants par `"<nom>"`

### Étape 3 — Implémenter les méthodes

Ordre conseillé (chaque étape est testable indépendamment) :

1. **`Connect`** — valider les params, établir la connexion, probe
2. **`Disconnect` / `IsConnected`** — gestion d'état (déjà fourni dans le template)
3. **`Stat`** — opération atomique, utile pour tester la connexion
4. **`List`** — navigation répertoire
5. **`Upload`** / **`Download`** — transferts (utiliser `progressReader` / `progressWriter`)
6. **`Delete`** — suppression
7. **`Move`** — déplacement/renommage
8. **`CreateDir`** — création de répertoire
9. **`Watch`** — surveillance (polling ou événements natifs)

### Étape 4 — Enregistrer le plugin dans la factory

> **Note** : `plugins/registry.go` est défini dans l'issue #50 (milestone v0.4.0).
> Cette étape est requise mais ne peut être complétée qu'une fois #50 mergée.

Dans `plugins/registry.go`, ajouter :

```go
import "github.com/CCoupel/GhostDrive/plugins/<nom>"

// Dans la map ou le switch de la factory :
case "<nom>":
    return <nom>.New(), nil
```

### Étape 5 — Documenter les Params

Mettre à jour `contracts/backend-config.md` avec la section du nouveau plugin :

```markdown
### <nom>

| Clé | Obligatoire | Description | Exemple |
|-----|-------------|-------------|---------|
| `rootPath` | oui | Chemin absolu du répertoire racine | `/mnt/storage` |
```

---

## 4. Conventions obligatoires

### Nommage

- Package : `plugins/<nom>/` — un seul mot en minuscules
- `Name()` retourne `"<nom>"` — correspond à `BackendConfig.Type` dans la config
- Struct principale : `Backend`
- Constructeur : `New() *Backend`

### Chemins

- **Slash-séparés** (`/`) même sur Windows
- **Relatifs à `RemotePath`** — jamais de chemin absolu avec lettre de lecteur
- Les méthodes acceptent les chemins avec ou sans `/` initial

### Thread-safety

- Chaque `Backend` doit embarquer un `sync.RWMutex`
- Acquérir le RLock pour les reads (`IsConnected`, champs de config)
- Acquérir le Lock pour les writes (`Connect`, `Disconnect`)
- Les opérations longues (Upload, Download) **ne doivent pas** tenir le mutex

```go
type Backend struct {
    mu        sync.RWMutex
    connected bool
    // champs de config...
}

func (b *Backend) IsConnected() bool {
    b.mu.RLock()
    defer b.mu.RUnlock()
    return b.connected
}
```

### Wrapping d'erreurs

Toujours préfixer avec le nom du plugin :

```go
// Schéma : "<plugin>: <opération> [<path>]: <cause>"
return fmt.Errorf("myplugin: connect: probe failed: %w", err)
return fmt.Errorf("myplugin: upload %s: %w", remote, err)
return fmt.Errorf("myplugin: stat %s: %w", path, plugins.ErrFileNotFound)
```

### Canal `Watch`

```go
func (b *Backend) Watch(ctx context.Context, path string) (<-chan plugins.FileEvent, error) {
    if !b.IsConnected() {
        return nil, fmt.Errorf("myplugin: watch: %w", plugins.ErrNotConnected)
    }
    ch := make(chan plugins.FileEvent, 64) // buffer ≥ 64
    go func() {
        defer close(ch) // fermer quand ctx est annulé
        // ...
        <-ctx.Done()
    }()
    return ch, nil
}
```

---

## 5. Tests

### Pattern selon le type de backend

| Type de backend | Approche recommandée |
|-----------------|---------------------|
| Filesystem local | `t.TempDir()` comme racine |
| HTTP / WebDAV | `httptest.NewServer()` + handler in-memory |
| Réseau opaque | Mock `plugins.StorageBackend` |

### Exemple — backend HTTP

```go
func TestMyPluginUpload(t *testing.T) {
    // Démarrer un faux serveur HTTP
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method == http.MethodPut {
            w.WriteHeader(http.StatusCreated)
        }
    }))
    defer srv.Close()

    b := New()
    err := b.Connect(plugins.BackendConfig{
        Params: map[string]string{"url": srv.URL},
    })
    require.NoError(t, err)

    // Créer un fichier temporaire
    f, _ := os.CreateTemp(t.TempDir(), "upload-*")
    f.WriteString("hello")
    f.Close()

    err = b.Upload(context.Background(), f.Name(), "/remote/hello.txt", nil)
    assert.NoError(t, err)
}
```

### Tests minimaux requis

| Test | Description |
|------|-------------|
| `TestConnect` | Connexion valide, params manquants, backend inaccessible |
| `TestDisconnect` | Après disconnect, toutes les ops retournent `ErrNotConnected` |
| `TestUploadDownload` | Roundtrip : upload → download → contenu identique |
| `TestList` | Répertoire vide, répertoire peuplé, chemin inexistant |
| `TestStat` | Fichier existant, fichier inexistant (`ErrFileNotFound`) |
| `TestDelete` | Suppression existant, suppression inexistant |
| `TestCreateDir` | Création nouvelle, création existante (no-op) |
| `TestMove` | Renommage simple |
| `TestWatch` | Annulation via context |

### Commande

```bash
go test ./plugins/<nom>/... -v -cover
```

---

## 6. Checklist avant PR

```
[ ] go vet ./plugins/<nom>/...           → aucun warning
[ ] go test ./plugins/<nom>/... -cover   → couverture ≥ 70%
[ ] go build ./...                       → compilation sans erreur
[ ] contracts/backend-config.md          → section <nom> ajoutée avec les Params
[ ] CHANGELOG.md                         → entrée plugin(<nom>) dans la section Unreleased
[ ] plugins/registry.go                  → case "<nom>" enregistré dans la factory
[ ] docs/plugins/<nom>.md (optionnel)    → documentation utilisateur du backend
```

---

## Notes

- **`GetQuota`** n'est pas encore dans l'interface (décision reportée à #47).
  Quand elle sera ajoutée, les plugins ne supportant pas le quota retourneront `(-1, -1, nil)`.
- Les plugins `webdav` et `moosefs` ont leurs propres `ErrNotConnected` locaux jusqu'à l'issue #47.
  Les nouveaux plugins wrappent directement les sentinelles partagées :
  ```go
  var ErrNotConnected = fmt.Errorf("<nom>: %w", plugins.ErrNotConnected)
  ```
  (voir pattern dans `plugins/template/template.go`)
- La branche `feat/v0.4.0-plugin-local` héberge tous les commits du milestone v0.4.0 (#45, #50, #47, #48).
