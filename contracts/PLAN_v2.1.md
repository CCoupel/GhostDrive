# Plan d'Implementation : v2.1 — Files On-Demand (CF API)

> **Issues** : #122, #123, #124, #129  
> **Branche** : `feature/v2.1`  
> **Prérequis** : v2.0 livré (426bd3b) — `ReadAt`, `ChunkSize`, drive unifié `GhD:`  
> **Date** : 2026-05-20  
> **Auteur** : planner

---

## 1. Résumé

Implémentation des **Files On-Demand** via la Cloud Filter API (CF API) Windows.  
Quatre issues en 4 phases ordonnées sur une branche unique :

- **#122** : Enregistrement du sync root CF + création des placeholders au démarrage
- **#124** : Cache chunk BoltDB (TTL, ETag/mtime, LRU éviction) — indépendant, parallélisable Linux
- **#123** : Hydratation progressive via callbacks FETCH_DATA → TRANSFER_DATA
- **#129** : Badges shell (☁️ ⟳ ✓✓ ⚡) via `CfSetInSyncState`/`CfSetPinState`

---

## 2. Critères d'Acceptation

### #122 — Sync Root CF + Placeholders

- [ ] `CfRegisterSyncRoot(localPath, providerID, displayName, ...)` appelé au démarrage pour chaque backend avec `LocalPath` non vide
- [ ] `CfConnectSyncRoot(localPath, callbacks, ...)` établit la connexion CF
- [ ] `backend.List("/")` récursif → `CfCreatePlaceholders(items)` peuple `LocalPath` à vide
- [ ] Les placeholders apparaissent dans l'Explorateur avec icône ☁️ (CF_IN_SYNC_STATE_NOT_IN_SYNC)
- [ ] Désactiver un backend → `CfDisconnectSyncRoot` + `CfDeregisterSyncRoot`
- [ ] `go test ./internal/cfapi/...` passe (≥70% couv., Windows uniquement)

### #124 — Cache BoltDB

- [ ] `internal/cache/chunk_cache.go` implémente `ChunkCache` avec BoltDB
- [ ] `ChunkKey{BackendID, RemotePath, Offset}` → `ChunkEntry{Data, ETag, MTime, StoredAt}`
- [ ] TTL configurable (défaut 24h), expiration lazy au `Get`
- [ ] LRU éviction au dépassement de `AppConfig.CacheSizeMaxMB`
- [ ] Cache désactivé quand `AppConfig.CacheEnabled == false` (no-op en mémoire)
- [ ] Tests unitaires Linux (BoltDB fonctionne sur Linux)
- [ ] `go test ./internal/cache/...` passe (≥70% couv.)

### #123 — Hydratation FETCH_DATA

- [ ] Callback CF `OnFetchData` : vérifie cache → sinon `backend.ReadAt(offset, chunkSize)` → stocke en cache → `CfExecute(TRANSFER_DATA, data)`
- [ ] Séquence complète offset+length alignée sur `backend.ChunkSize()` (défaut 4 MiB si `ChunkSize()` retourne 0)
- [ ] `OnFetchData` émet `sync:progress` pendant le transfert (throttle 100ms)
- [ ] `OnCancelFetch` annule proprement le contexte ReadAt en cours
- [ ] Fichier entièrement hydraté → `CfSetInSyncState(CF_IN_SYNC_STATE_IN_SYNC)` → badge ✓✓
- [ ] Erreur réseau → `CfReportProviderProgress` avec erreur → badge ⚠ conservé ☁️
- [ ] `go test ./internal/cfapi/...` passe (≥70% couv., Windows uniquement)

### #129 — Badges Shell

- [ ] `CfSetInSyncState(localPath, CF_IN_SYNC_STATE_NOT_IN_SYNC)` → icône ☁️
- [ ] Pendant hydratation → `CfReportProviderProgress` → spinner ⟳ natif Windows
- [ ] `CfSetInSyncState(localPath, CF_IN_SYNC_STATE_IN_SYNC)` → icône ✓✓
- [ ] `CfSetPinState(localPath, CF_PIN_STATE_PINNED)` → icône ⚡ (fichier toujours local)
- [ ] `CfSetPinState(localPath, CF_PIN_STATE_UNPINNED)` → retour à ☁️ (déhydrater)
- [ ] Badges testés via smoke manuel Windows (pas de test unitaire automatisable)

---

## 3. Décisions Architecturales

### 3.1 CF API opère sur `LocalPath`, pas sur `GhD:`

**Raison** : La CF API exige un volume NTFS. Le drive WinFsp (`GhD:`) n'est pas NTFS — il est virtuel (cgofuse). La CF API ne peut pas s'y enregistrer.

**Conséquence** :
- CF sync root = `backend.Config.LocalPath` (ex: `C:\GhostDrive\MonNAS\`)
- WinFsp `GhD:` reste inchangé (continuer à servir les lectures virtuelles)
- Les deux coexistent : `GhD:\MonNAS\` (lecture virtuelle) + `C:\GhostDrive\MonNAS\` (placeholders CF avec badges)

### 3.2 Nouveau package `internal/cfapi/` — CGO Windows uniquement

Build tag `//go:build windows`. Ce package wrape la Win32 CF API via CGO :
- Headers requis : `cfapi.h` (Windows SDK ≥ 10.0.17763.0 / Win10 1809)
- Lib CGO : `-lcldapi` (mingw) ou `cldapi.lib` (MSVC)
- Le package expose une API Go pure — aucun type C ne fuit hors de `internal/cfapi/`

**Windows SDK recommandé** : `10.0.22621.0` (Windows 11 SDK, déjà présent dans `windows-latest` GitHub Actions). Minimum supporté : `10.0.17763.0`.

### 3.3 BoltDB activable/désactivable via `AppConfig.CacheEnabled`

`AppConfig.CacheEnabled` existe déjà. Quand `false` :
- `NewChunkCache()` retourne un `noopCache` (implémente `ChunkCache`, tout retourne cache miss)
- Aucune dépendance BoltDB initialisée, aucun fichier `.db` créé

Quand `true` (défaut) :
- BoltDB file : `{AppConfig.CacheDir}/chunks.db`
- `AppConfig.CacheDir` défaut : `%LOCALAPPDATA%\GhostDrive\cache\`
- `AppConfig.CacheSizeMaxMB` défaut : 512 MB

**Nouveau champ `AppConfig`** : `ChunkCacheTTLHours int` (défaut 24) — durée avant expiration lazy d'un chunk.

### 3.4 Chunk size par défaut : 4 MiB

Quand `backend.ChunkSize()` retourne 0, le Hydrator utilise `defaultChunkSize = 4 * 1024 * 1024` (4 MiB). Les requêtes FETCH_DATA sont découpées en tranches de `chunkSize` octets.

### 3.5 ProviderID GUID stable par installation

Le CF API `CfRegisterSyncRoot` requiert un GUID stable pour le `StorageProviderID`. Ce GUID est généré à la première installation et stocké dans `AppConfig.CloudProviderID string`. S'il est vide, `app.go` en génère un nouveau via `uuid.NewV4()`.

Nouveau champ `AppConfig` : `CloudProviderID string`.

### 3.6 Intégration dans `app.go` — après `MountUnified`

```
Startup order (app.go):
  1. LoadConfig()
  2. RegisterLocalBackend()
  3. ScanDynamicPlugins()
  4. ReconnectSavedBackends()
  5. driveManager.MountUnified(...)         ← WinFsp GhD: (existant)
  6. cfManager.StartAll(enabledBackends)    ← NEW: CF sync roots
  7. AutoStartSyncEngines()
```

---

## 4. Nouvelles Interfaces Go (Contrats)

### 4.1 `internal/cfapi/provider.go` (Windows)

```go
package cfapi

// SyncState est l'état CF d'un fichier/dossier local.
type SyncState int

const (
    SyncStateCloudOnly SyncState = iota // ☁️ placeholder non hydraté
    SyncStateSyncing                    // ⟳ hydratation en cours
    SyncStateSynced                     // ✓✓ hydraté / in-sync
    SyncStatePinned                     // ⚡ épinglé (toujours local)
    SyncStateUnpinned                   // retour ☁️ depuis épinglé
)

// PlaceholderInfo décrit un fichier à créer comme placeholder CF.
type PlaceholderInfo struct {
    RelativePath string    // relatif à localPath (ex: "docs/file.txt")
    FileSize     int64
    ModTime      time.Time
    FileID       string    // ETag ou version opaque (FileInfo.Version)
    IsDirectory  bool
}

// FetchRequest est passé au callback OnFetchData.
type FetchRequest struct {
    LocalPath string        // chemin absolu du fichier à hydrater
    Offset    int64         // offset demandé par Windows
    Length    int64         // longueur demandée
    opInfo    uintptr       // CF_OPERATION_INFO opaque (unexported)
}

// CFCallbacks sont les handlers fournis lors de CfConnectSyncRoot.
type CFCallbacks struct {
    OnFetchData         func(ctx context.Context, req FetchRequest) error
    OnCancelFetch       func(req FetchRequest)
    OnFetchPlaceholders func(ctx context.Context, localPath string) error
    OnDeleteCompletion  func(localPath string)
    OnRenameCompletion  func(oldPath, newPath string)
}

// SyncProvider gère le cycle de vie CF API pour un backend (un sync root).
type SyncProvider struct{ /* unexported */ }

func NewSyncProvider(localPath, providerID, displayName string) *SyncProvider
func (p *SyncProvider) Register() error
func (p *SyncProvider) Deregister() error
func (p *SyncProvider) Connect(cbs CFCallbacks) error
func (p *SyncProvider) Disconnect() error
func (p *SyncProvider) CreatePlaceholders(items []PlaceholderInfo) (int, error)
func (p *SyncProvider) UpdatePlaceholder(localPath string, fi PlaceholderInfo) error
func (p *SyncProvider) SetSyncState(localPath string, state SyncState) error
func (p *SyncProvider) ExecuteTransfer(req FetchRequest, data []byte, finalBlock bool) error
func (p *SyncProvider) ReportError(req FetchRequest, err error) error
```

### 4.2 `internal/cfapi/hydrator.go`

```go
package cfapi

// Hydrator est le bridge entre les callbacks CF et le backend + cache.
type Hydrator struct {
    backend   plugins.StorageBackend
    cache     cache.ChunkCache   // nil si cache désactivé
    provider  *SyncProvider
    chunkSize int64
    emitter   plugins.EventEmitter
}

func NewHydrator(
    backend   plugins.StorageBackend,
    cache     cache.ChunkCache,
    provider  *SyncProvider,
    emitter   plugins.EventEmitter,
) *Hydrator

// OnFetchData implémente CFCallbacks.OnFetchData.
// Séquence : cache hit → ExecuteTransfer | cache miss → ReadAt → cache.Put → ExecuteTransfer
func (h *Hydrator) OnFetchData(ctx context.Context, req FetchRequest) error

// OnFetchPlaceholders liste le backend et crée les placeholders manquants.
func (h *Hydrator) OnFetchPlaceholders(ctx context.Context, localPath string) error
```

### 4.3 `internal/cfapi/manager.go`

```go
package cfapi

// CFManager gère l'ensemble des SyncProviders (un par backend activé).
type CFManager struct{ /* unexported */ }

func NewCFManager(cfg *config.AppConfig, emitter plugins.EventEmitter) *CFManager

// Start enregistre et connecte le sync root CF pour un backend.
func (m *CFManager) Start(bc plugins.BackendConfig, backend plugins.StorageBackend, ch cache.ChunkCache) error

// Stop déconnecte et désenregistre un sync root CF.
func (m *CFManager) Stop(backendID string) error

// StartAll démarre tous les backends de la liste.
func (m *CFManager) StartAll(backends []placeholder.MountedBackend, caches map[string]cache.ChunkCache) error

// StopAll arrête tous les sync roots.
func (m *CFManager) StopAll() error

// SetSyncState expose SetSyncState pour app.go (ex: sync engine → badge update).
func (m *CFManager) SetSyncState(backendID, localPath string, state SyncState) error
```

### 4.4 `internal/cache/chunk_cache.go`

```go
package cache

// ChunkKey identifie un chunk dans le cache.
type ChunkKey struct {
    BackendID  string
    RemotePath string
    Offset     int64
}

// ChunkEntry est la valeur stockée dans BoltDB.
type ChunkEntry struct {
    Data     []byte
    ETag     string    // FileInfo.Version pour validation
    MTime    time.Time // pour invalidation sur mtime change
    StoredAt time.Time // pour TTL
    Size     int64     // len(Data) — redondant, utile pour stats sans décoder
}

// CacheStats expose des métriques de cache.
type CacheStats struct {
    Entries   int64
    SizeBytes int64
    Hits      int64
    Misses    int64
}

// ChunkCache est l'interface du cache de chunks.
type ChunkCache interface {
    // Get retourne le chunk si présent et non expiré (ETag/mtime cohérents).
    Get(ctx context.Context, key ChunkKey, currentETag string, currentMTime time.Time) (*ChunkEntry, bool)
    // Put stocke un chunk. Déclenche l'éviction LRU si nécessaire.
    Put(ctx context.Context, key ChunkKey, entry ChunkEntry) error
    // Invalidate supprime tous les chunks d'un fichier.
    Invalidate(ctx context.Context, backendID, remotePath string) error
    // InvalidateBackend supprime tous les chunks d'un backend.
    InvalidateBackend(ctx context.Context, backendID string) error
    // Stats retourne les métriques courantes.
    Stats() CacheStats
    Close() error
}

// NewBoltCache crée un ChunkCache BoltDB dans dbPath.
// ttl : durée avant expiration (ex: 24*time.Hour).
// maxBytes : taille max (ex: AppConfig.CacheSizeMaxMB * 1024 * 1024).
func NewBoltCache(dbPath string, ttl time.Duration, maxBytes int64) (ChunkCache, error)

// NewNoopCache retourne un cache désactivé (toujours miss, Put no-op).
func NewNoopCache() ChunkCache
```

---

## 5. Composants Impactés

| Composant | Changement |
|-----------|------------|
| **`internal/cfapi/`** | Nouveau package — provider.go, hydrator.go, manager.go, cgo_cfapi.c/.h (Windows) |
| **`internal/cache/`** | Nouveau — chunk_cache.go (BoltDB + noop) |
| **`internal/app/app.go`** | +`cfManager` field + démarrage CF après MountUnified + wiring cache |
| **`internal/config/config.go`** | +`ChunkCacheTTLHours int` + `CloudProviderID string` |
| **`go.mod`** | +`go.etcd.io/bbolt` (BoltDB) + `github.com/google/uuid` (si absent) |
| **`internal/placeholder/`** | Inchangé (WinFsp coexiste) |
| **`internal/sync/engine.go`** | Appel `cfManager.SetSyncState(...)` après upload/download réussi |
| **`internal/sync/download.go`** | Appel `cfManager.SetSyncState(backendID, localPath, SyncStateSynced)` post-download |
| **`contracts/CHANGELOG.md`** | Mise à jour |

---

## 6. Schéma BoltDB

```
chunks.db
├── bucket "chunks"
│     key   : "<backendID>\x00<remotePath>\x00<offsetHex>"  (big-endian 16 hex chars pour offset)
│     value : msgpack/gob de ChunkEntry{Data, ETag, MTime, StoredAt, Size}
│
├── bucket "lru"
│     key   : <timestamp_ns_big_endian 8 bytes>
│     value : "<backendID>\x00<remotePath>\x00<offsetHex>"  (référence arrière pour éviction)
│
└── bucket "meta"
      "total_size_bytes" → int64 (somme des Size)
      "hit_count"        → int64
      "miss_count"       → int64
```

**Politique TTL** : vérifié au `Get` — `time.Since(entry.StoredAt) > ttl` → miss + suppression lazy.

**Politique LRU** : au `Put`, si `total_size_bytes + len(data) > maxBytes`, supprimer les entrées les plus anciennes du bucket `lru` jusqu'à avoir assez de place.

**Politique invalidation ETag/mtime** : au `Get`, si `entry.ETag != currentETag || entry.MTime != currentMTime` → miss + suppression.

---

## 7. Tâches par Phase

### Phase 1 — #122 : CF Sync Root + Placeholders (Windows uniquement)

1. [ ] **Créer `internal/cfapi/cgo_cfapi.h`**
   - Fichier : `internal/cfapi/cgo_cfapi.h`
   - Déclarations C wrappant `cfapi.h` : `ghd_cf_register`, `ghd_cf_connect`, `ghd_cf_create_placeholders`, `ghd_cf_set_sync_state`, `ghd_cf_execute_transfer`, `ghd_cf_report_error`
   - Lien : `-lcldapi`

2. [ ] **Créer `internal/cfapi/cgo_cfapi.c`**
   - Fichier : `internal/cfapi/cgo_cfapi.c`
   - Implémentation C des fonctions déclarées dans `.h`
   - Gestion HRESULT → errno (mapping vers `syscall.Errno`)

3. [ ] **Créer `internal/cfapi/provider.go`** (build tag `//go:build windows`)
   - Fichier : `internal/cfapi/provider.go`
   - Types : `SyncState`, `PlaceholderInfo`, `FetchRequest`, `CFCallbacks`, `SyncProvider`
   - Méthodes : `Register`, `Deregister`, `Connect`, `Disconnect`, `CreatePlaceholders`, `UpdatePlaceholder`, `SetSyncState`, `ExecuteTransfer`, `ReportError`
   - Callbacks C → Go dispatch via `cgo_export` pattern

4. [ ] **Créer `internal/cfapi/provider_stub.go`** (build tag `//go:build !windows`)
   - Fichier : `internal/cfapi/provider_stub.go`
   - Stubs no-op pour compilation Linux (tests cache, CI Linux)

5. [ ] **Créer `internal/cfapi/manager.go`**
   - Fichier : `internal/cfapi/manager.go`
   - Types : `CFManager`
   - Méthodes : `NewCFManager`, `Start`, `Stop`, `StartAll`, `StopAll`, `SetSyncState`
   - Guard: sur Linux, `Start` = no-op (stubs)

6. [ ] **Modifier `internal/config/config.go`**
   - Fichier : `internal/config/config.go`
   - Ajouter `CloudProviderID string` (JSON: `"cloudProviderID"`)
   - Ajouter `ChunkCacheTTLHours int` (JSON: `"chunkCacheTTLHours"`, défaut: 24)
   - Migration: si `CloudProviderID == ""` → générer UUID au `Save()`

7. [ ] **Modifier `internal/app/app.go`**
   - Fichier : `internal/app/app.go`
   - Ajouter champ `cfManager *cfapi.CFManager`
   - Startup: appeler `cfManager.StartAll(enabledBackends, caches)` après `MountUnified`
   - Shutdown: appeler `cfManager.StopAll()` avant `UnmountUnified`
   - `SetBackendEnabled(enable=true)` : appeler `cfManager.Start(bc, backend, cache)`
   - `SetBackendEnabled(enable=false)` : appeler `cfManager.Stop(backendID)`

8. [ ] **Tests `internal/cfapi/`**
   - Fichiers : `internal/cfapi/manager_test.go`, `internal/cfapi/provider_test.go`
   - Tests Linux (stubs) : lifecycle `Start/Stop`, `SetSyncState` no-op
   - Tests Windows : registration smoke (require real FS path + Windows env)
   - Build tag `//go:build windows` pour tests CF réels

---

### Phase 2 — #124 : Cache BoltDB (Linux + Windows)

9. [ ] **Ajouter `go.etcd.io/bbolt` dans go.mod**
   - Fichier : `go.mod`, `go.sum`
   - Commande : `go get go.etcd.io/bbolt@v1.3.x`

10. [ ] **Créer `internal/cache/chunk_cache.go`**
    - Fichier : `internal/cache/chunk_cache.go`
    - Interface `ChunkCache`, types `ChunkKey`, `ChunkEntry`, `CacheStats`
    - `NewNoopCache()` — implémente `ChunkCache`, tout retourne miss/no-op

11. [ ] **Créer `internal/cache/bolt_cache.go`**
    - Fichier : `internal/cache/bolt_cache.go`
    - `NewBoltCache(dbPath, ttl, maxBytes)` ouvre/crée `chunks.db`
    - `Get` : lookup BoltDB → vérif TTL + ETag/mtime → hit ou miss
    - `Put` : write + update LRU bucket + eviction si dépassement maxBytes
    - `Invalidate` : scan prefix bucket "chunks" + delete matching
    - `InvalidateBackend` : scan prefix backendID + delete all
    - `Stats` : lecture bucket "meta"
    - `Close` : `db.Close()`

12. [ ] **Modifier `internal/app/app.go`**
    - Fichier : `internal/app/app.go`
    - Créer `map[string]cache.ChunkCache` (un par backend)
    - Si `AppConfig.CacheEnabled` : `cache.NewBoltCache(cacheDir+"/chunks.db", ttl, maxBytes)`
    - Sinon : `cache.NewNoopCache()`
    - Passer le cache au `cfapi.CFManager` et au `sync.Engine` (pour invalidation sur delete/upload)

13. [ ] **Modifier `internal/sync/engine.go` / `download.go`**
    - Fichiers : `internal/sync/engine.go`, `internal/sync/download.go`
    - Injection `ChunkCache` optionnelle (via `Engine.SetCache(cc cache.ChunkCache)`)
    - `Download` réussi : `cache.Invalidate(backendID, remotePath)` (fichier changé → invalider cache)
    - `Upload` réussi : `cache.Invalidate(backendID, remotePath)` (version locale devient référence)
    - Écoute `meta:updated` : si `MetadataChanged` → `cache.Invalidate` du fichier concerné

14. [ ] **Tests `internal/cache/`**
    - Fichiers : `internal/cache/bolt_cache_test.go`, `internal/cache/noop_cache_test.go`
    - `TestBoltCacheGetPut` : put → get hit
    - `TestBoltCacheTTL` : put → avance temps → get miss
    - `TestBoltCacheETagInvalidation` : put → get avec ETag différent → miss
    - `TestBoltCacheLRUEviction` : remplir jusqu'à maxBytes → vérifier éviction
    - `TestBoltCacheInvalidateBackend` : put N chunks → invalidate backend → all miss
    - `TestNoopCache` : put → get → always miss, no panic

---

### Phase 3 — #123 : Hydratation FETCH_DATA (Windows uniquement)

15. [ ] **Créer `internal/cfapi/hydrator.go`**
    - Fichier : `internal/cfapi/hydrator.go`
    - Types : `Hydrator`
    - `NewHydrator(backend, cache, provider, emitter, chunkSize)`
    - `OnFetchData` :
      1. Calculer offset aligné sur chunkSize
      2. `cache.Get(key, currentETag, currentMTime)` → hit → `provider.ExecuteTransfer`
      3. Miss → `backend.ReadAt(ctx, remotePath, offset, chunkSize)`
      4. `cache.Put(key, entry)` 
      5. `provider.ExecuteTransfer(req, data, isFinalBlock)`
      6. Émettre `sync:progress` (throttle 100ms)
    - `OnCancelFetch` : annuler ctx associé à la FetchRequest
    - `OnFetchPlaceholders` : `backend.List(ctx, relPath)` → `provider.CreatePlaceholders(items)`

16. [ ] **Modifier `internal/cfapi/manager.go`**
    - Fichier : `internal/cfapi/manager.go`
    - `Start` crée `Hydrator` et passe ses méthodes comme `CFCallbacks`
    - Mapper `localPath` → `remotePath` via `relPath = strings.TrimPrefix(localPath, syncRoot)`

17. [ ] **Tests `internal/cfapi/hydrator_test.go`** (Linux avec stubs)
    - `TestHydratorFetchData_CacheHit`
    - `TestHydratorFetchData_CacheMiss_ThenHit`
    - `TestHydratorFetchData_BackendError`
    - `TestHydratorCancelFetch`
    - `TestHydratorFetchPlaceholders`

---

### Phase 4 — #129 : Badges Shell (Windows uniquement)

18. [ ] **Vérifier intégration badges via `provider.SetSyncState`**
    - Fichier : `internal/cfapi/provider.go` (déjà créé Phase 1)
    - Confirmer que `CfSetInSyncState` + `CfSetPinState` mis en place en Phase 1 sont corrects
    - Ajouter `CfSetPinState` si non fait en Phase 1

19. [ ] **Modifier `internal/sync/download.go`**
    - Fichier : `internal/sync/download.go`
    - Post-download réussi : `cfManager.SetSyncState(backendID, localPath, SyncStateSynced)`
    - Début download : `cfManager.SetSyncState(backendID, localPath, SyncStateSyncing)` (optionnel pour cohérence — WinFsp ne trigger pas CF callbacks)

20. [ ] **Modifier `internal/sync/engine.go`**
    - Fichier : `internal/sync/engine.go`
    - Injection `cfManager` optionnelle (via `Engine.SetCFManager(m *cfapi.CFManager)`)
    - Après réconciliation complète : `cfManager.SetSyncState(backendID, "/", SyncStateSynced)` sur le root

21. [ ] **Exposer `SetPinState` dans les bindings Wails** (optionnel pour v2.1)
    - Fichier : `internal/app/app.go`
    - `PinFile(backendID, localPath string, pin bool) error` — permet à l'UI de "toujours garder localement"
    - Si non inclus en v2.1 → reporter en v2.2

22. [ ] **Tests manuels Windows (smoke #129)**
    - Créer placeholder → vérifier ☁️ dans l'Explorateur
    - Ouvrir fichier → vérifier spinner ⟳
    - Ouvrir terminé → vérifier ✓✓
    - Forcer pin → vérifier ⚡

---

## 8. Contrats API — Changements

### Contrats à créer/modifier

- [ ] `contracts/PLAN_v2.1.md` ← ce fichier
- [ ] `contracts/CHANGELOG.md` — entrée [20260520] v2.1

### Nouveaux événements Wails

| Événement | Payload | Direction | Issue |
|-----------|---------|-----------|-------|
| `cf:sync_state` | `{backendID, localPath, state: "cloud_only"\|"syncing"\|"synced"\|"pinned"}` | backend → frontend | #129 |
| `cf:hydration_progress` | `{backendID, localPath, byteDone, byteTotal}` | backend → frontend | #123 |

Ces événements sont optionnels en v2.1 (les badges Windows ne nécessitent pas de UI React). Priorité basse — implémenter si temps disponible.

### Nouveaux bindings Wails

| Méthode | Signature Go | Issue |
|---------|-------------|-------|
| `PinFile` | `(backendID, localPath string, pin bool) error` | #129 (optionnel v2.1) |
| `GetCacheStats` | `() map[string]CacheStats` | #124 (debug/settings UI) |

---

## 9. Tests Requis

| Suite | Fichiers | Plateforme | Couverture cible |
|-------|---------|------------|-----------------|
| Cache BoltDB | `internal/cache/*_test.go` | Linux + Windows | ≥70% |
| CF Manager stubs | `internal/cfapi/manager_test.go` | Linux | ≥70% |
| CF Provider (réel) | `internal/cfapi/provider_test.go` | Windows uniquement | smoke |
| Hydrator | `internal/cfapi/hydrator_test.go` | Linux (stubs) | ≥70% |
| Config migration | `internal/config/config_test.go` | Linux + Windows | ≥70% |
| Smoke manuel | — | Windows physique/VM | voir §2 |

**Stratégie** : le Hydrator et le CFManager sont testables sur Linux car les dépendances CF (provider, backend) sont injectées via interfaces. Les stubs provider_stub.go permettent `go test ./internal/cfapi/...` sur Linux.

---

## 10. Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|-----------|
| CF API callbacks non déclenchés (WinFsp + CF API conflit) | Moyen | Élevé | Valider dès Phase 1 sur Windows réel que `CfConnectSyncRoot` fonctionne sur `LocalPath` NTFS (≠ GhD:) |
| mingw `-lcldapi` non disponible dans la CI actuelle | Moyen | Élevé | Vérifier `.github/workflows/` — ajouter `WindowsSDK` install step si besoin |
| BoltDB corruption sur crash pendant hydratation | Faible | Moyen | Ouvrir BoltDB avec `Timeout: 1s` + `NoSync: false` ; recovery = supprimer `chunks.db` et repartir |
| LRU éviction trop agressive sous charge | Faible | Faible | Tuning `CacheSizeMaxMB` + tests de charge |
| `ReadAt` MooseFS offset > fileSize → panic | Faible | Moyen | Guard dans `OnFetchData` : `min(req.Offset+req.Length, fileSize)` avant ReadAt |
| Placeholders dupliqués si `CreatePlaceholders` appelé 2x | Faible | Faible | `CfCreatePlaceholders` est idempotent sur CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE ; sinon check `CfGetPlaceholderInfo` avant |
| `CloudProviderID` manquant → `CfRegisterSyncRoot` échoue | Faible | Élevé | Migration `config.go` génère UUID si vide ; log explicite si échec CF registration |

---

## 11. Estimation

| Issue | Complexité | Fichiers | Notes |
|-------|------------|---------|-------|
| #122 CF Sync Root | Élevée | 6 nouveaux + 2 modifiés | CGO Windows, peu de précédent dans codebase |
| #124 BoltDB Cache | Moyenne | 3 nouveaux + 2 modifiés | Go pur, bien documenté |
| #123 Hydratation | Moyenne | 2 nouveaux + 1 modifié | Dépend #122 et #124 |
| #129 Badges | Faible | 2 modifiés | Réutilise infra #122 |

- **Complexité globale** : Élevée (CGO Windows + nouveau paradigme CF API)
- **Nombre de fichiers** : ~15 nouveaux, ~8 modifiés
- **Ordre d'implémentation recommandé** : #122 → #124 (parallèle) → #123 → #129
- **Risque bloquant** : validation Windows CF API dès Phase 1 (tâche 1–3) avant de continuer

---

## 12. Notes

- **`GhD:` inchangé** : toutes les modifications sont dans `internal/cfapi/` et `internal/cache/`. Le package `internal/placeholder/` (WinFsp) n'est pas touché.
- **Pas de nouveau contrat gRPC** : CF API ne passe pas par le bus gRPC plugin — elle appelle directement les méthodes Go du backend via l'interface `StorageBackend` déjà en place.
- **`PinFile` binding Wails** (#129) est optionnel pour v2.1 — si le temps manque, les badges fonctionnent sans UI dédiée (CF API affiche les icônes nativement).
- **Tests d'intégration complets (#122, #123, #129)** nécessitent un environnement Windows. Le CI `windows-latest` GitHub Actions est suffisant si le Windows SDK CF API headers sont disponibles.
