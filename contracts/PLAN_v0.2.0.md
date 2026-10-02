# Plan d'Implementation : Milestone v0.2.0 — UI Wails Complète

> **Version cible** : 0.2.0
> **Branche** : `feat/ui-v0.2.0`
> **Issues** : #28 (systray), #29 (config backend), #30 (sync points), #31 (état sync temps réel)
> **Contrats** : `contracts/backend-config.md`, `contracts/sync-state.md`, `contracts/tray-menu.md`

---

## Contrats API créés

- [x] `contracts/backend-config.md` — Bindings AddBackend, RemoveBackend, TestBackendConnection, GetConfig/SaveConfig (#29, #30)
- [x] `contracts/sync-state.md` — Modèle BackendSyncState, méthodes sync, événements temps réel (#31)
- [x] `contracts/tray-menu.md` — Configuration systray Wails, menu natif, icônes dynamiques (#28)

---

## Résumé

Implémentation de l'UI complète GhostDrive v0.2.0 : point d'entrée Wails manquant (`cmd/ghostdrive/main.go`), systray natif Windows, page de configuration backends (WebDAV/MooseFS), configuration des points de synchronisation, et vue d'état de synchronisation en temps réel.

---

## Critères d'Acceptation

- [ ] L'application se compile avec `wails build` sans erreur
- [ ] L'icône systray apparaît dans la barre des tâches Windows
- [ ] Le menu contextuel tray affiche les actions (Ouvrir, Sync, Pause, Paramètres, Quitter)
- [ ] Le formulaire d'ajout de backend WebDAV valide les champs et teste la connexion
- [ ] Le formulaire d'ajout de backend MooseFS valide les champs requis
- [ ] La liste des backends s'affiche avec statut connecté/déconnecté
- [ ] Les points de synchronisation (SyncDir + RemotePath) sont configurables par backend
- [ ] La vue état de sync affiche le statut en temps réel (idle/syncing/paused/error)
- [ ] Les transferts actifs s'affichent avec barre de progression par fichier
- [ ] Les erreurs de sync s'affichent dans le panneau status
- [ ] `go test ./... -cover` passe avec couverture ≥ 70%
- [ ] `npm run test` (vitest) passe

---

## Composants Impactés

- **Backend** : `cmd/ghostdrive/` (ABSENT, à créer), `internal/app/` (ABSENT, à créer), `internal/backends/` (manager à créer)
- **Frontend** : `frontend/src/components/tray/`, `frontend/src/components/settings/`, `frontend/src/components/status/`, `frontend/src/hooks/`
- **Assets** : `assets/` (icônes tray ICO)
- **Contracts** : `contracts/` (3 nouveaux fichiers créés)

---

## Tâches

### Phase 1 : Versioning

1. [ ] Mettre à jour la version à 0.2.0
   - Fichier(s) : `config.json` (champ `version`), `frontend/package.json` (champ `version`)
   - Description : Synchroniser les 2 sources de vérité avant tout code

---

### Phase 2 : Contrats Wails ✅ (terminée par le Planner)

2. [x] Créer `contracts/backend-config.md` — bindings config backend + sync points (#29, #30)
3. [x] Créer `contracts/sync-state.md` — modèle BackendSyncState + méthodes + événements (#31)
4. [x] Créer `contracts/tray-menu.md` — configuration systray + menu natif + icônes (#28)

---

### Phase 3 : Backend — Bindings Go/Wails

> **Dépendance** : Lire `contracts/` avant d'implémenter. Go-side only.

5. [ ] Créer `cmd/ghostdrive/main.go` — Point d'entrée Wails + configuration systray
   - Fichier(s) : `cmd/ghostdrive/main.go` (NOUVEAU)
   - Description : Struct `App`, méthodes `startup(ctx)`/`shutdown(ctx)`, configuration `wails.Run()` avec `HideWindowOnClose: true`, systray options, binding `ghostApp`
   - Dépend de : #6 (App struct), assets ICO

6. [ ] Créer `internal/app/app.go` — Struct App + toutes les méthodes Wails exposées
   - Fichier(s) : `internal/app/app.go` (NOUVEAU)
   - Description : Struct `App` avec champs `ctx`, `cfg`, `backends`, `engines`. Implémenter toutes les méthodes du contrat `wails-bindings.md` : GetConfig, SaveConfig, AddBackend, RemoveBackend, TestBackendConnection, GetSyncState, StartSync, StopSync, PauseSync, ForceSync, ListFiles, DownloadFile, OpenSyncFolder, GetCacheStats, ClearCache, GetBackendStatuses, GetVersion, Quit

7. [ ] Créer `internal/backends/manager.go` — BackendManager (cycle de vie des backends)
   - Fichier(s) : `internal/backends/manager.go` (NOUVEAU)
   - Description : Map `backendID → StorageBackend`, méthodes Add/Remove/Get/List, instanciation selon Type ("webdav"→`webdav.New()`, "moosefs"→`moosefs.New()`), Connect/Disconnect lifecycle, émission de `backend:status-changed`

8. [ ] Implémenter l'émetteur d'événements Wails dans `internal/app/app.go`
   - Fichier(s) : `internal/app/app.go`
   - Description : Méthode `emit(event string, payload any)` qui appelle `runtime.EventsEmit(app.ctx, event, payload)`. Passer cet émetteur au SyncEngine et BackendManager via interface `EventEmitter`

9. [ ] Câbler le SyncEngine dans App — StartSync/StopSync/PauseSync/ForceSync
   - Fichier(s) : `internal/app/app.go`, `internal/sync/engine.go`
   - Description : App maintient un `map[string]*sync.Engine` (un par backend actif). StartSync instancie et démarre un Engine. StopSync l'arrête. GetSyncState agrège les états de tous les engines en `SyncState` avec le nouveau champ `Backends []BackendSyncState`

10. [ ] Créer les icônes tray ICO
    - Fichier(s) : `assets/tray-idle.ico`, `assets/tray-syncing.ico`, `assets/tray-paused.ico`, `assets/tray-error.ico`
    - Description : 32x32 ICO format. Peut être un placeholder monochrome pour v0.2.0. Requis par Wails pour le systray Windows.

11. [ ] Configurer le menu tray natif dans `cmd/ghostdrive/main.go`
    - Fichier(s) : `cmd/ghostdrive/main.go`
    - Description : Implémenter `buildTrayMenu()` selon `contracts/tray-menu.md`. Callbacks : openWindow (`runtime.WindowShow`), forceSyncAll, togglePause, openSettings (emit `tray:open-settings`), quit (`runtime.Quit`). Mettre à jour le tooltip tray à chaque `sync:state-changed`.

12. [ ] Mettre à jour `internal/types/types.go` — Ajouter BackendSyncState
    - Fichier(s) : `internal/types/types.go`
    - Description : Ajouter struct `BackendSyncState` (voir `contracts/sync-state.md`). Mettre à jour `SyncState` pour inclure `Backends []BackendSyncState` et `ActiveTransfers []ProgressEvent`.

---

### Phase 4 : Frontend — Composants React/TypeScript

> **Dépendance** : Lire `contracts/` (ne pas modifier). Utiliser `window.go.App.*` via `frontend/src/services/wails.ts`.

#### 4.1 — Systray (#28)

13. [ ] Mettre à jour `frontend/src/components/tray/TrayStatus.tsx`
    - Fichier(s) : `frontend/src/components/tray/TrayStatus.tsx`
    - Description : Afficher icône status + texte + compteur fichiers en attente. Props : `syncState: SyncState`. Écouter `tray:open-settings` pour naviguer vers l'onglet settings.

14. [ ] Mettre à jour `frontend/src/components/tray/TrayMenu.tsx`
    - Fichier(s) : `frontend/src/components/tray/TrayMenu.tsx`
    - Description : Barre d'actions : boutons Pause/Reprendre, Force Sync, Paramètres, Quitter. Câbler sur `ghostdriveApi.pauseSync/startSync/forceSync/quit`. Désactiver les boutons selon l'état courant.

#### 4.2 — Configuration Backend (#29)

15. [ ] Mettre à jour `frontend/src/components/settings/SyncPointForm.tsx` — Formulaire ajout backend
    - Fichier(s) : `frontend/src/components/settings/SyncPointForm.tsx`
    - Description : Formulaire complet avec sélecteur de Type (WebDAV/MooseFS), champs conditionnels selon type (voir `contracts/backend-config.md`), validation côté client, bouton "Tester la connexion" (appelle `testBackendConnection`), bouton "Ajouter" (appelle `addBackend`). Afficher résultat test (connecté/erreur + espace libre).

16. [ ] Mettre à jour `frontend/src/components/settings/BackendConfig.tsx` — Carte backend
    - Fichier(s) : `frontend/src/components/settings/BackendConfig.tsx`
    - Description : Afficher nom, type, statut (badge connecté/erreur), SyncDir, RemotePath. Boutons : Ouvrir dossier (`openSyncFolder`), Supprimer (`removeBackend` + confirmation modal), Start/Stop sync. Polling statut via `useBackends`.

17. [ ] Mettre à jour `frontend/src/components/settings/SettingsPage.tsx` — Onglets
    - Fichier(s) : `frontend/src/components/settings/SettingsPage.tsx`
    - Description : Onglet "Backends" (liste BackendConfig + bouton Ajouter → SyncPointForm). Onglet "Préférences" (StartMinimized, AutoStart, CacheEnabled, CacheSizeMaxMB). Onglet "Cache" (stats + bouton vider).

#### 4.3 — Points de Synchronisation (#30)

18. [ ] Ajouter sélection de SyncDir et RemotePath dans SyncPointForm
    - Fichier(s) : `frontend/src/components/settings/SyncPointForm.tsx`
    - Description : Champ `SyncDir` avec bouton "Parcourir" (ouvre dialog natif via Wails `runtime.OpenDirectoryDialog`). Champ `RemotePath` texte libre (ex: "/GhostDrive"). Valider que SyncDir n'est pas vide et RemotePath commence par "/".

#### 4.4 — Vue État Synchronisation Temps Réel (#31)

19. [ ] Mettre à jour `frontend/src/hooks/useSyncStatus.ts` — Hook complet
    - Fichier(s) : `frontend/src/hooks/useSyncStatus.ts`
    - Description : Initialiser via `getSyncState()`. Écouter `sync:state-changed` (remplace l'état complet), `sync:progress` (met à jour `activeTransfers` Map), `sync:error` (ajoute à la liste, max 50), `sync:file-event`. Nettoyer les transferts terminés (percent >= 100 ou state-changed idle). Retourner `{ syncState, activeTransfers, errors, loading }`.

20. [ ] Mettre à jour `frontend/src/components/status/SyncStatus.tsx` — Vue temps réel
    - Fichier(s) : `frontend/src/components/status/SyncStatus.tsx`
    - Description : Afficher statut global + statut par backend (selon `SyncState.Backends[]`). Liste des transferts actifs (ProgressBar par fichier). Section erreurs (liste déroulante si > 3). Boutons Start/Stop/Pause/Force par backend. Date dernière sync en format relatif.

21. [ ] Mettre à jour `frontend/src/components/status/ProgressBar.tsx`
    - Fichier(s) : `frontend/src/components/status/ProgressBar.tsx`
    - Description : Afficher nom fichier tronqué, direction (↑/↓), pourcentage, bytes formatés (Mo/Go). Props : `transfer: ProgressEvent`.

22. [ ] Mettre à jour `frontend/src/components/status/FileList.tsx`
    - Fichier(s) : `frontend/src/components/status/FileList.tsx`
    - Description : Liste des fichiers récents synchronisés (FileEvent). Icône selon type (créé/modifié/supprimé/renommé), chemin, source (local/remote), timestamp relatif.

23. [ ] Mettre à jour `frontend/src/hooks/useBackends.ts`
    - Fichier(s) : `frontend/src/hooks/useBackends.ts`
    - Description : Ajouter méthodes `addBackend(config)`, `removeBackend(id)`, `testConnection(config)`. S'assurer que le polling à 10s utilise `getBackendStatuses()`. Mettre à jour le statut à chaque `backend:status-changed`.

24. [ ] Mettre à jour `frontend/src/types/ghostdrive.ts` — Nouveaux types v0.2.0
    - Fichier(s) : `frontend/src/types/ghostdrive.ts`
    - Description : Ajouter `BackendSyncState` interface. Mettre à jour `SyncState` avec `backends: BackendSyncState[]` et `activeTransfers: ProgressEvent[]`. Ajouter `tray:open-settings` et `tray:action` dans `WailsEventMap`.

25. [ ] Mettre à jour `frontend/src/services/wails.ts`
    - Fichier(s) : `frontend/src/services/wails.ts`
    - Description : Vérifier que tous les appels correspondent aux méthodes implémentées en Go. Ajouter abonnements aux événements `tray:open-settings` et `tray:action`.

---

### Phase 5 : Tests

26. [ ] Tests unitaires Go — `internal/app/app_test.go`
    - Fichier(s) : `internal/app/app_test.go` (NOUVEAU)
    - Description : Tester GetConfig/SaveConfig, AddBackend (validation + mock backend), RemoveBackend, GetSyncState (agrégation). Utiliser testify/mock pour StorageBackend.

27. [ ] Tests intégration Go — Backend WebDAV avec serveur in-memory
    - Fichier(s) : `tests/webdav_integration_test.go`
    - Description : Démarrer un serveur WebDAV in-memory (`golang.org/x/net/webdav`), tester AddBackend + StartSync + StopSync. Vérifier les événements émis via EventEmitter mock.

28. [ ] Tests Frontend — Validation formulaire SyncPointForm
    - Fichier(s) : `frontend/src/components/settings/SyncPointForm.test.tsx` (NOUVEAU)
    - Description : Tester la validation des champs requis, le comportement selon le type sélectionné (WebDAV vs MooseFS), et l'appel à `testBackendConnection`.

29. [ ] Tests Frontend — Hook useSyncStatus
    - Fichier(s) : `frontend/src/hooks/useSyncStatus.test.ts` (NOUVEAU)
    - Description : Tester l'initialisation, la mise à jour sur événements, la gestion des transferts actifs, le plafond d'erreurs à 50.

---

## Tests Requis

- [ ] Tests unitaires Go : `go test ./internal/... -v -cover` (seuil 70%)
- [ ] Tests intégration : `go test ./tests/... -v -tags integration`
- [ ] Build Wails : `wails build` (Windows cible)
- [ ] Tests frontend : `npm run test` (vitest)
- [ ] Smoke test UI : vérifier systray, formulaires, vue sync

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| API Systray Wails v2 limitée/instable sur Windows | Moyen | Élevé | Tester tôt avec une app Wails minimaliste ; consulter docs Wails v2 systray |
| `cmd/ghostdrive/main.go` absent bloque tout le build | Élevé | Élevé | Tâche #5 est la première tâche backend ; blocage si non fait |
| Icônes ICO manquantes bloquent le build | Moyen | Élevé | Utiliser des placeholders 32x32 ICO dès le début (tâche #10) |
| Désynchronisation types Go/TypeScript | Moyen | Moyen | Contrats comme référence ; valider avec le build Wails qui régénère les bindings |
| `runtime.OpenDirectoryDialog` non disponible sur Linux | Faible | Faible | Feature Windows-first ; mettre un champ texte fallback |
| SyncState agrégation complexe multi-backend | Faible | Moyen | Logique d'agrégation simple (tâche #9) avec tests unitaires |

---

## Estimation

- **Complexité** : Moyenne-Élevée
- **Nombre de fichiers** : ~20 fichiers (7 nouveaux, 13 modifiés)
- **Répartition** : Backend 40% / Frontend 45% / Tests 15%

---

## Ordre d'Exécution Recommandé

```
Phase 1 (version) → Phase 3.tâches 12,10,5,6,7,8,9,11 → Phase 4 (frontend) → Phase 5 (tests)
```

**Tâches critiques (bloquantes)** :
1. Tâche 5 : `cmd/ghostdrive/main.go` — sans ça, rien ne build
2. Tâche 6 : `internal/app/app.go` — sans ça, pas de méthodes Wails
3. Tâche 10 : assets ICO — sans ça, le systray crashe au démarrage
4. Tâche 12 : types Go — avant les composants frontend

**Tâches parallélisables** :
- Phase 4 frontend entière (une fois les types Go stables)
- Tests Go et tests frontend (indépendants)

---

## Notes

- La branche `feat/ui-v0.2.0` existe déjà (créée précédemment)
- Les contrats v0.1.0 dans `contracts/` sont stables et compatibles v0.2.0 (pas de breaking change)
- `internal/sync/engine.go` existe mais l'interface `EventEmitter` doit être passée depuis l'App
- Les bindings Wails générés dans `frontend/wailsjs/` sont régénérés automatiquement par `wails build`
- Ne jamais modifier `frontend/wailsjs/` manuellement — fichiers auto-générés
