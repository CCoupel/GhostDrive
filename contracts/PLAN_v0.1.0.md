# Plan d'Implémentation : GhostDrive v0.1.0

> **Date** : 2026-04-16  
> **Statut** : En attente de validation  
> **Branche cible** : `feat/v0.1.0-initial`

---

## Contrats API (Contract-First — CRÉÉS)

- [x] `contracts/models.md` — Types partagés Go ↔ Frontend (FileInfo, SyncState, BackendConfig, etc.)
- [x] `contracts/wails-bindings.md` — Méthodes Go exposées au frontend (App struct)
- [x] `contracts/wails-events.md` — Événements Go → Frontend (sync:state-changed, sync:progress, etc.)

---

## Résumé

GhostDrive v0.1.0 est la première version complète du client de synchronisation Windows.
Elle comprend le moteur de sync bidirectionnel, les plugins WebDAV et MooseFS, les placeholders
Files On-Demand via Cloud Filter API/WinFsp, un cache local, et une UI Wails (React + TypeScript).
La distribution se fait par binaires compilés via GitHub Actions.

---

## Critères d'Acceptation

- [ ] `go build ./...` passe sans erreur sur Windows AMD64 et Linux ARM64
- [ ] `go test ./... -cover` ≥ 70% coverage
- [ ] Plugin WebDAV : Connect, Upload, Download, List, Delete, Watch fonctionnels (serveur in-memory)
- [ ] Plugin MooseFS : Connect, List, Upload, Download fonctionnels (mock)
- [ ] Moteur sync : synchronisation bidirectionnelle détectée et appliquée correctement
- [ ] Placeholders : fichier placeholder créé sur Windows, hydration au clic
- [ ] Cache local : activation/désactivation, stats, purge
- [ ] Frontend : SyncStatus affiché en temps réel via Wails events
- [ ] Frontend : formulaire Settings backend complet (add/edit/test/delete)
- [ ] `wails build` produit un binaire Windows fonctionnel
- [ ] CI/CD : GitHub Actions build et release sur tag `v*`
- [ ] `vitest` frontend passe

---

## Composants Impactés

- **Backend Go** : Structure complète (go.mod, wails.json, cmd, internal, plugins)
- **Frontend React** : App Wails complète (tray, settings, status, services)
- **CI/CD** : GitHub Actions rewrite complet (release.yml adapté Wails)
- **Database** : Aucune — fichiers config.json locaux uniquement

---

## Tâches — DISPATCH BACKEND

### Phase 0 : Infrastructure Projet (Prérequis)

1. [ ] **Initialiser go.mod**
   - Fichier : `go.mod`
   - Description : `module github.com/CCoupel/GhostDrive`, `go 1.21`
   - Dépendances initiales : `github.com/wailsapp/wails/v2`, `golang.org/x/net`

2. [ ] **Créer wails.json**
   - Fichier : `wails.json`
   - Description : Config Wails (name, outputfilename, frontend dir, info)
   - Template standard Wails v2

3. [ ] **Créer config.json**
   - Fichier : `config.json`
   - Description : `{ "version": "0.1.0" }` — source de vérité versioning

4. [ ] **Créer .gitignore Go/Wails**
   - Fichier : `.gitignore` (mettre à jour)
   - Ajouter : `build/`, `frontend/dist/`, `*.exe`, `*.log`, `cache/`

5. [ ] **Créer CHANGELOG.md**
   - Fichier : `CHANGELOG.md`
   - Format Keep-a-Changelog : section `[0.1.0]` préparée

---

### Phase 1 : Interface Plugin & Types Partagés

6. [ ] **Définir StorageBackend interface**
   - Fichier : `plugins/plugin.go`
   - Description : Interface complète + types (BackendConfig, FileInfo, FileEvent, ProgressCallback)
   - Implémenter les types de `contracts/models.md`

7. [ ] **Définir les types partagés**
   - Fichier : `internal/types/types.go`
   - Description : SyncState, SyncError, BackendStatus, ProgressEvent, CacheStats
   - Ces types sont utilisés par App et exposés au frontend via Wails

---

### Phase 2 : Configuration

8. [ ] **Implémenter internal/config**
   - Fichier : `internal/config/config.go`
   - Description : Load/Save AppConfig depuis `config.json` (XDG ou AppData sur Windows)
   - Valider les chemins, créer le répertoire si manquant
   - Ne jamais logger les mots de passe

9. [ ] **Tests config**
   - Fichier : `internal/config/config_test.go`
   - Tests : Load depuis fichier temporaire, Save round-trip, defaults

---

### Phase 3 : Plugin WebDAV

10. [ ] **Implémenter plugins/webdav**
    - Fichier : `plugins/webdav/webdav.go`
    - Dépendance : `golang.org/x/net/webdav` (client HTTP standard Go)
    - Implémenter : `Connect`, `Disconnect`, `Upload`, `Download`, `Delete`, `List`, `Stat`, `Watch`, `CreateDir`
    - Watch : polling toutes les 30s (WebDAV n'a pas de push natif)
    - Paramètres : url, username, password (TLS supporté)

11. [ ] **Tests WebDAV**
    - Fichier : `plugins/webdav/webdav_test.go`
    - Stratégie : serveur WebDAV in-memory (`golang.org/x/net/webdav.Handler`)
    - Tests : Upload/Download round-trip, List, Delete, Stat, Connect invalid URL

---

### Phase 4 : Plugin MooseFS

12. [ ] **Implémenter plugins/moosefs**
    - Fichier : `plugins/moosefs/moosefs.go`
    - Stratégie V1 : accès via point de montage FUSE (le dossier monté est accessible comme un FS local)
    - Paramètres : mountPath (chemin vers le point de montage MooseFS)
    - Implémenter : List (os.ReadDir), Upload (io.Copy), Download (io.Copy), Delete (os.Remove), Watch (fsnotify)
    - Note : Connect vérifie que mountPath existe et est accessible

13. [ ] **Tests MooseFS**
    - Fichier : `plugins/moosefs/moosefs_test.go`
    - Stratégie : répertoire temporaire (`os.MkdirTemp`) simulant le montage
    - Tests : List, Upload, Download, Delete, Watch events

14. [ ] **Documentation plugin**
    - Fichier : `docs/plugin-development.md`
    - Description : Guide pour créer un nouveau plugin (interface, tests requis, paramètres)

---

### Phase 5 : Cache Local

15. [ ] **Implémenter internal/cache**
    - Fichier : `internal/cache/cache.go`
    - Description : Gestion du cache local des fichiers téléchargés
    - Fonctionnalités : Put(path, reader), Get(path) (io.ReadCloser), Evict(path), Stats(), Clear(), LRU eviction si maxSize dépassé
    - Stockage : répertoire configurable (défaut : `%APPDATA%/GhostDrive/cache`)

16. [ ] **Tests cache**
    - Fichier : `internal/cache/cache_test.go`
    - Tests : Put/Get round-trip, Stats, Clear, LRU eviction, concurrent access

---

### Phase 6 : Moteur de Synchronisation

17. [ ] **Implémenter internal/sync — structures de base**
    - Fichier : `internal/sync/engine.go`
    - Description : SyncEngine struct, New(backend, config), Start(ctx), Stop(), Pause(), ForceSync()
    - State machine : idle → syncing → idle / error

18. [ ] **Implémenter la réconciliation bidirectionnelle**
    - Fichier : `internal/sync/reconciler.go`
    - Algorithme : 
      1. Lister remote (List) + lister local (os.ReadDir)
      2. Comparer par path + ModTime + ETag
      3. Résolution de conflits : last-write-wins (V1)
      4. Générer la liste d'actions (Upload, Download, Delete, Mkdir)
    - Journalisation des conflits dans `sync.log`

19. [ ] **Implémenter le watcher local**
    - Fichier : `internal/sync/watcher.go`
    - Dépendance : `github.com/fsnotify/fsnotify`
    - Description : Surveille le dossier local, émet des FileEvents sur changement
    - Debounce 500ms pour éviter les événements en rafale

20. [ ] **Implémenter le dispatcher**
    - Fichier : `internal/sync/dispatcher.go`
    - Description : Exécute les actions de sync avec concurrence limitée (max 4 goroutines)
    - Émet des événements Wails (progress, state-changed, error) via un EventEmitter injecté

21. [ ] **Tests moteur sync**
    - Fichier : `internal/sync/engine_test.go`, `internal/sync/reconciler_test.go`
    - Stratégie : filesystem temporaire (`os.MkdirTemp`), mock StorageBackend
    - Tests : upload new file, download new remote file, conflict resolution, delete propagation

---

### Phase 7 : Placeholders Files On-Demand

22. [ ] **Définir l'interface PlaceholderManager**
    - Fichier : `internal/placeholder/placeholder.go`
    - Description : Interface + factory selon OS (Windows: Cloud Filter API, Linux: fichiers .ghost)
    - Méthodes : CreatePlaceholder(FileInfo), Hydrate(path, reader), IsPlaceholder(path) bool

23. [ ] **Implémenter pour Windows (Cloud Filter API)**
    - Fichier : `internal/placeholder/placeholder_windows.go`
    - Build tag : `//go:build windows`
    - Dépendance : WinFsp SDK ou bindings Cloud Filter API via CGO
    - Description : Crée des placeholders NTFS, intercèpte les ouvertures, déclenche l'hydration
    - Émet les événements Wails `placeholder:hydration-started` et `placeholder:hydration-done`
    - **Risque majeur** : voir section Risques

24. [ ] **Implémenter fallback Linux**
    - Fichier : `internal/placeholder/placeholder_linux.go`
    - Build tag : `//go:build linux`
    - Description : Fichiers `.ghost` (JSON metadata) + téléchargement à la demande via CLI
    - Fonctionnalité réduite — principalement pour les tests CI

25. [ ] **Tests placeholders**
    - Fichier : `internal/placeholder/placeholder_test.go`
    - Stratégie : tests sur la version Linux uniquement en CI (Windows testé manuellement)

---

### Phase 8 : Point d'Entrée Wails

26. [ ] **Implémenter app.go (App struct)**
    - Fichier : `app.go`
    - Description : Struct App avec tous les bindings définis dans `contracts/wails-bindings.md`
    - Injecter : SyncEngine, BackendManager, CacheManager, PlaceholderManager, Config
    - Implémenter `startup(ctx)` et `shutdown(ctx)`

27. [ ] **Implémenter cmd/ghostdrive/main.go**
    - Fichier : `cmd/ghostdrive/main.go`
    - Description : Point d'entrée Wails v2 standard
    - Config Wails : systray activé, fenêtre masquée au démarrage si `StartMinimized`
    - Assets : `frontend/dist` embarqués via `//go:embed`

28. [ ] **Implémenter BackendManager**
    - Fichier : `internal/backends/manager.go`
    - Description : Gère le cycle de vie des backends (connect, disconnect, status polling)
    - Factory : instancie WebDAV ou MooseFS selon `BackendConfig.Type`

---

## Tâches — DISPATCH FRONTEND

### Phase 9 : Infrastructure Frontend

29. [ ] **Initialiser frontend Wails**
    - Fichier : `frontend/package.json`
    - Description : React + TypeScript + Vite (template Wails v2)
    - Version : `"version": "0.1.0"` — doit correspondre à config.json
    - Dépendances : `react`, `react-dom`, `@vitejs/plugin-react`, `vitest`

30. [ ] **Configurer les services Wails**
    - Fichier : `frontend/src/services/wails.ts`
    - Description : Wrapper typé autour de `window.go.App.*` et `window.runtime.EventsOn`
    - Exporter des fonctions typées basées sur `contracts/wails-bindings.md`

31. [ ] **Définir les types TypeScript**
    - Fichier : `frontend/src/types/models.ts`
    - Description : Reprendre tous les types de `contracts/models.md` en TypeScript
    - Types : FileInfo, SyncState, BackendConfig, BackendStatus, ProgressEvent, etc.

---

### Phase 10 : Composants UI

32. [ ] **Implémenter TrayMenu**
    - Fichier : `frontend/src/components/tray/TrayMenu.tsx`
    - Description : Menu contextuel systray (état sync, backends connectés, actions rapides)
    - Actions : Open settings, Pause/Resume sync, Open sync folder, Quit
    - État : icône dynamique selon SyncStatus (idle/syncing/error)

33. [ ] **Implémenter StatusPanel**
    - Fichier : `frontend/src/components/status/StatusPanel.tsx`
    - Description : Panneau d'état de synchronisation en temps réel
    - Affiche : SyncState, barre de progression, fichier courant, liste d'erreurs
    - Écoute : événements `sync:state-changed`, `sync:progress`, `sync:error`

34. [ ] **Implémenter SettingsPanel**
    - Fichier : `frontend/src/components/settings/SettingsPanel.tsx`
    - Description : Configuration des backends et options de l'app
    - Formulaires : Add/Edit backend (WebDAV et MooseFS), Test connection, Supprimer
    - Options : cache activé/désactivé, taille max cache, démarrage automatique

35. [ ] **Implémenter BackendForm**
    - Fichier : `frontend/src/components/settings/BackendForm.tsx`
    - Description : Formulaire dynamique selon le type de backend (webdav/moosefs)
    - Validation côté frontend + appel `TestBackendConnection` avant sauvegarde

36. [ ] **Implémenter App.tsx principal**
    - Fichier : `frontend/src/App.tsx`
    - Description : Router principal (Settings / Status), écoute `app:ready`
    - Gestion de l'état global via `useReducer` ou Context API

37. [ ] **Implémenter les hooks Wails**
    - Fichier : `frontend/src/hooks/useSyncState.ts`, `useBackends.ts`, `useEvents.ts`
    - Description : Hooks React encapsulant les appels Wails et la gestion des événements

---

### Phase 11 : Tests Frontend

38. [ ] **Tests composants**
    - Fichier : `frontend/src/components/**/*.test.tsx`
    - Framework : vitest + @testing-library/react
    - Tests : rendu StatusPanel, formulaire BackendForm, validation inputs

---

## Tâches — DISPATCH INFRA/CI

### Phase 12 : CI/CD

39. [ ] **Réécrire release.yml pour Wails**
    - Fichier : `.github/workflows/release.yml`
    - Description : Adapter le pipeline existant (qui référence BuzzControl/server-go) pour GhostDrive + Wails
    - Stage 1 - Checking : vérifier que `config.json`, `frontend/package.json` et le tag git sont synchronisés
    - Stage 2 - Compiling :
      - Windows AMD64 : `wails build -platform windows/amd64` (runner `windows-latest`)
      - Linux ARM64 : build Go pur sans Wails renderer (CLI mode ou build conditionnel)
    - Stage 3 - Releasing : `gh release create` avec les binaires

40. [ ] **Créer workflow CI (tests)**
    - Fichier : `.github/workflows/ci.yml`
    - Description : Exécuté sur chaque PR/push
    - Jobs : `go test ./... -cover`, `go vet`, `vitest`, `go build` (vérification compilation)

---

## Tests Requis

- [ ] Tests unitaires backend : `go test ./... -v -cover` (seuil 70%)
  - `internal/config/` — Load/Save config
  - `internal/cache/` — Put/Get/Evict/Stats
  - `internal/sync/` — Réconciliation bidirectionnelle, watcher
  - `plugins/webdav/` — Serveur in-memory
  - `plugins/moosefs/` — Répertoire temporaire
- [ ] Tests intégration : `tests/integration/sync_test.go` — sync end-to-end avec WebDAV in-memory
- [ ] Tests frontend : `vitest` — composants React (StatusPanel, BackendForm)
- [ ] Test manuel Windows : placeholders Files On-Demand (Cloud Filter API)

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| Cloud Filter API — bindings CGO complexes | Élevée | Élevé | Utiliser WinFsp comme couche d'abstraction ou une lib Go existante (ex: `github.com/capnspacehook/pie-loader` patterns). Fallback : fichiers .ghost en V1 si trop complexe. |
| Wails build sur Linux pour cible Windows | Moyenne | Moyen | Cross-compilation Wails nécessite `wails build -platform windows/amd64` sur un runner Windows (GitHub Actions `windows-latest`) |
| MooseFS — dépendance FUSE | Moyenne | Moyen | V1 via mountPath local, pas de bindings natifs MooseFS. Le client monte lui-même via MooseFS FUSE. Documenter l'installation pré-requise. |
| Conflits de sync (last-write-wins) | Faible | Moyen | Journaliser les conflits dans `sync.log`. Avertir l'utilisateur via event `sync:error`. Versionning dans V3. |
| Credentials en clair dans config.json | Faible | Élevé | V1 : avertissement dans l'UI. Chiffrement AES-256 avec clé machine (DPAPI Windows) prévu en V2. |
| CGO disabled en cross-compilation | Élevée | Élevé | Les placeholders Windows nécessitent CGO=1. Build Windows doit se faire sur runner Windows. |

---

## Estimation

- **Complexité** : Élevée
- **Nombre de fichiers** : ~45 fichiers Go + ~20 fichiers TypeScript
- **Phases** : 12 phases, 40 tâches

---

## Ordre de Dispatch Recommandé (CDP)

```
Phase 0 → dev-backend (infrastructure)
Phase 1-2 → dev-backend (types + config)
Phase 3-4 → dev-backend (plugins WebDAV + MooseFS) [parallélisable]
Phase 5-6 → dev-backend (cache + sync engine)
Phase 7-8 → dev-backend (placeholders + App Wails)
Phase 9-11 → dev-frontend (frontend React) [peut démarrer après Phase 1 via contrats]
Phase 12 → infra (CI/CD)
```

**Parallélisation possible** :
- Backend Phase 3 + Phase 4 (WebDAV et MooseFS sont indépendants)
- Frontend Phase 9-11 peut démarrer dès Phase 1 terminée (les contrats définissent l'API)
- CI/CD Phase 12 peut démarrer en parallèle du Frontend

---

## Notes

1. **Cloud Filter API** : La fonctionnalité placeholder est la plus risquée. Évaluer en Phase 7 si on utilise WinFsp (plus simple, lib Go disponible) ou Cloud Filter API direct (plus natif mais CGO complexe). Décision à prendre avant implémentation.

2. **Wails systray** : Wails v2 supporte le systray nativement (`options.SystemTray`). L'icône doit être un fichier `.ico` embarqué dans les assets.

3. **Config storage** : Sur Windows, utiliser `%APPDATA%\GhostDrive\config.json`. Sur Linux, `~/.config/ghostdrive/config.json`.

4. **MooseFS V1** : L'approche via mountPath (FUSE) simplifie grandement l'implémentation. Le plugin MooseFS agit comme un plugin "Local Folder" pointant vers un montage FUSE existant.

5. **Le workflow release.yml doit être entièrement réécrit** — il référence actuellement "BuzzControl" et une structure de projet différente (`server-go/`).
