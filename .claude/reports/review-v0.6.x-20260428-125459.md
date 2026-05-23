# Revue de Code — v0.6.x Plugin Loader

> **Branche** : `feat/v0.6.x-plugin-loader`  
> **SHA** : `bc8fa11` (5 commits depuis base)  
> **Date** : 2026-04-28  
> **Mode** : Général + Sécurité  
> **Reviewer** : code-reviewer agent

---

## Résumé

- **Fichiers analysés** : 18 (proto, gRPC client/server/tests, loader, registry, SDK, app.go, manager.go, webdav, moosefs, go.mod, contracts)
- **Problèmes trouvés** : 12 (0 critiques, 6 majeurs, 6 mineurs)
- **Verdict** : **APPROUVE AVEC RESERVES**

L'implémentation est globalement solide. L'architecture go-plugin + gRPC est correctement structurée, le mapping d'erreurs est complet, les tests couvrent les happy paths et les error paths principaux. Quelques problèmes majeurs à corriger avant production (notamment go.mod, upload sans limite de taille, et tests manquants).

---

## Problèmes Majeurs
> Fortement recommandé de corriger avant merge/prod

### [MAJEUR-1] `go.mod` — dépendances directes marquées `// indirect`

- **Fichier** : `go.mod:34-35,61-62`
- **Description** : `github.com/hashicorp/go-plugin`, `go-hclog`, `google.golang.org/grpc` et `google.golang.org/protobuf` sont tous marqués `// indirect`. Ces packages sont **directement importés** dans `plugins/grpc/` et `plugins/loader/`. L'explication probable : `go mod tidy` a été exécuté sans les fichiers `.pb.go` générés (gitignorés), ce qui a rendu le package `plugins/proto` invisible pour le resolver. Risque : un `go mod tidy` sur checkout propre supprimerait ces dépendances et casserait le build CI.
- **Suggestion** : 
  1. Ajouter une étape `protoc` AVANT `go mod tidy` dans le workflow CI.
  2. Ou ajouter un fichier `tools/tools.go` avec `//go:build tools` et des imports explicites pour forcer la rétention des deps dans go.mod.
  3. En attendant : supprimer le `// indirect` sur ces 4 lignes manuellement après `go mod tidy` avec proto généré.

---

### [MAJEUR-2] Upload sans limite de taille dans `GRPCBackendServer`

- **Fichier** : `plugins/grpc/server.go:63-95`
- **Description** : La méthode `Upload` stocke les chunks reçus dans un fichier temporaire sans aucune limite de taille. Un client malveillant (ou un plugin bogué) peut envoyer des données en flux continu pour saturer le disque (déni de service par épuisement d'espace disque).
- **Suggestion** :
  ```go
  const maxUploadSize = 10 * 1024 * 1024 * 1024 // 10 GB — ajuster selon contraintes
  var totalWritten int64
  // dans la boucle de réception :
  totalWritten += int64(len(chunk.GetData()))
  if totalWritten > maxUploadSize {
      tmpFile.Close()
      return status.Errorf(codes.ResourceExhausted, "upload: taille maximale dépassée")
  }
  ```

---

### [MAJEUR-3] `context.Background()` sans timeout dans les appels lifecycle

- **Fichier** : `plugins/grpc/client.go:51,68,82,93`
- **Description** : Les méthodes `Name()`, `Connect()`, `Disconnect()`, `IsConnected()` utilisent `context.Background()` (pas de timeout). Si le plugin subprocess hang, ces appels bloquent indéfiniment, gelant le thread appelant (et potentiellement le démarrage de l'application).
- **Suggestion** :
  ```go
  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()
  resp, err := b.client.Name(ctx, &storagepb.NameRequest{})
  ```
  Note architecturale : l'interface `StorageBackend` ne prend pas de `context` pour ces méthodes — c'est une limitation de l'interface. Au minimum, utiliser un timeout de 5-10s pour les appels de lifecycle.

---

### [MAJEUR-4] Watchdog restart silently invalide les connexions existantes

- **Fichier** : `plugins/loader/grpc_loader.go:217-235`
- **Description** : Lors d'un restart de plugin, le loader re-registre une nouvelle factory dans `plugins.Register()`, mais les instances `GRPCBackend` déjà obtenues par le `BackendManager` pointent toujours vers l'ancienne connexion morte. Ces backends continuent d'exister en mémoire sans être marqués comme déconnectés — `IsConnected()` retournera éventuellement `false` (quand le gRPC client détecte la déconnexion), mais pas immédiatement.
- **Suggestion** : Après un restart réussi, émettre un événement Wails `plugin:restarting` / `plugin:restarted` (déjà dans le contrat) pour que l'app puisse appeler `b.Disconnect()` sur les backends affectés et les marquer comme déconnectés. Actuellement ces events ne sont émis qu'au niveau du contrat mais pas dans l'implémentation du watchdog.

---

### [MAJEUR-5] Tests manquants pour `Delete` et `Move` via le bridge gRPC

- **Fichier** : `plugins/grpc/client_test.go`
- **Description** : Les opérations `Delete` et `Move` sont implémentées côté client ET serveur, avec mapping d'erreurs (`mapBackendError`), mais ne disposent d'aucun test dans `client_test.go`. Le mock backend les implémente correctement (lignes 110-131), mais aucune assertion n'existe pour vérifier que le round-trip gRPC fonctionne, ni que les sentinel errors sont bien mappées.
- **Suggestion** : Ajouter :
  ```go
  func TestGRPCBackend_Delete(t *testing.T) { ... }
  func TestGRPCBackend_Move(t *testing.T) { ... }
  func TestGRPCBackend_ErrorMapping_DeleteNotFound(t *testing.T) { ... }
  ```

---

### [MAJEUR-6] Décalage contrat / implémentation sur le champ `Version`

- **Fichier** : `contracts/plugin-loader-bindings.md:46-49` vs `plugins/loader/grpc_loader.go:300`
- **Description** : Le contrat montre `"version": "1.0.0"` pour un plugin `echo` chargé. L'implémentation retourne systématiquement `"unknown"` pour tous les plugins dynamiques (la méthode `Version()` de `GRPCBackend` est hardcodée à `"unknown"`). Le frontend qui afficherait la version de l'echo plugin verrait `"unknown"` au lieu de `"1.0.0"`.
- **Suggestion** : Soit corriger l'exemple dans le contrat pour montrer `"unknown"`, soit implémenter la récupération de version via un RPC dédié (ex: `Name` étendu ou nouveau champ `Version` dans `NameResponse` proto). La deuxième option est plus propre mais sort du scope v0.6.0.

---

## Problèmes Mineurs
> Suggestions d'amélioration, non bloquants

### [MINEUR-1] `parentDir()` réimplémente `filepath.Dir()`

- **Fichier** : `plugins/grpc/client.go:397-405`
- **Description** : La fonction `parentDir()` réimplémente manuellement `filepath.Dir()` en parcourant les caractères `/` et `\`. La stdlib gère plus de cas edge (chemins réseau UNC, racines drive Windows...).
- **Suggestion** : Remplacer par `filepath.Dir(path)`.

---

### [MINEUR-2] Code mort dans `Scan()` — logique Linux inversée

- **Fichier** : `plugins/loader/grpc_loader.go:91-99`
- **Description** : Le bloc qui devrait inclure les binaires Linux (sans extension) contient une condition `continue` qui saute les fichiers SANS extension (`ext == ""`), ce qui est l'inverse de l'intention. Le bloc est marqué "reserved for future cross-platform extension" mais si on l'active tel quel, les plugins Linux seraient ignorés.
- **Suggestion** : Inverser la condition ou documenter clairement l'inversion intentionnelle :
  ```go
  if ext == ".exe" {
      continue // déjà dans matches
  }
  if ext != "" {
      continue // ignorer .md, .txt, etc.
  }
  // ici : fichier sans extension = plugin Linux candidat
  ```

---

### [MINEUR-3] `TestGRPCLoader_ScanInvalidExe` sans garde de timeout

- **Fichier** : `plugins/loader/grpc_loader_test.go:40-58`
- **Description** : Si `launchPlugin` bloque pour une raison inattendue (ex: le fake exe prend du temps à terminer sur certains OS), le test hangerait indéfiniment. Le commentaire dit "must not panic or block" mais il n'y a pas de `t.Deadline()` ou timer.
- **Suggestion** :
  ```go
  // En Go 1.21+, les tests ont une deadline par défaut si -timeout est passé.
  // Ajouter un contexte avec timeout explicite pour les tests loader.
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()
  ```

---

### [MINEUR-4] Tests manquants : Watch error path et GetQuota error

- **Fichier** : `plugins/grpc/client_test.go`
- **Description** :
  - Pas de test pour `Watch` quand le backend retourne `ErrNotConnected` (le error path de Watch côté serveur envoie un `WatchEvent{Error: ...}` et retourne `nil` — comportement particulier à tester).
  - Pas de test pour `GetQuota` quand le backend retourne une erreur.
  - Pas de test pour `CreateDir` via le bridge gRPC.

---

### [MINEUR-5] `TestDynamicRegistry_StopIdempotent` — dépend d'un effet de bord

- **Fichier** : `plugins/registry/dynamic_registry_test.go:82-89`
- **Description** : Le test vérifie que `Stop()` est idempotent, mais cette propriété dépend du fait que `Shutdown()` vide `l.entries` à la fin. Ce comportement est implicite et non documenté dans `GRPCLoader.Shutdown()`. Si l'implémentation change, le test casse silencieusement.
- **Suggestion** : Ajouter un commentaire dans `GRPCLoader.Shutdown()` stipulant explicitement "clearing entries ensures idempotency".

---

### [MINEUR-6] SDK `main.go` référence `MyPlugin` non défini

- **Fichier** : `plugins/sdk/go/main.go:21`
- **Description** : Le template référence `&MyPlugin{}` qui n'existe pas dans le fichier. C'est intentionnel (c'est un template `//go:build ignore`), mais un éditeur Go va afficher une erreur de compilation et peut perturber les nouveaux développeurs.
- **Suggestion** : Ajouter un commentaire explicite : `// MyPlugin doit être remplacé par votre implémentation — voir echo/main.go pour un exemple complet.`

---

## Points Positifs

- **Proto bien conçu** : Le fichier `storage.proto` couvre complètement l'interface `StorageBackend` (13 RPCs), avec des types de streaming correctement choisis (Upload client-streaming, Download et Watch server-streaming). Les messages sont complets et documentés.
- **Mapping d'erreurs correct** : La chaîne `mapBackendError` (serveur) → gRPC status codes → `mapGRPCError` (client) → sentinel errors Go est implémentée correctement et testée avec `errors.Is()`.
- **Tests gRPC in-process** : L'usage de `bufconn` pour les tests gRPC (sans port réseau) est la bonne pratique. Les 11 tests dans `client_test.go` couvrent bien les cas principaux.
- **Supervision watchdog** : L'implémentation du watchdog avec backoff exponentiel (1s → 2s → 4s, 3 tentatives) et `context.WithCancel` pour un arrêt propre est correcte.
- **Intégration app.go** : Le `dynRegistry` est initialisé AVANT la boucle de reconnexion des backends (comme requis par le contrat), et `Shutdown()` l'arrête proprement.
- **SDK echo** : Le plugin `echo` implémente toutes les méthodes de `StorageBackend` avec gestion correcte de `IsConnected`, `ErrNotConnected`, et `GetQuota(-1, -1, nil)`.
- **Fix init() webdav/moosefs** : L'ajout des fonctions `init()` d'auto-registration dans webdav et moosefs est le pattern correct, cohérent avec le plugin `local` et le `dynamic_registry_test.go`.
- **GetAvailableBackendTypes** : Filtre correctement les plugins avec `status == "loaded"` (exclusion des plugins en état "failed" ou "restarting").
- **GetLoadedPlugins/ReloadPlugins** : Conformes au contrat — jamais nil, émission de `plugin:reloaded` avec le count.

---

## Vérification Contrats API

| Binding | Contrat | Implémentation | Statut |
|---------|---------|----------------|--------|
| `GetLoadedPlugins() []PluginInfo` | ✓ | `app.go:202` | ✅ Conforme |
| `ReloadPlugins() error` | ✓ | `app.go:218` | ✅ Conforme |
| `GetAvailableBackendTypes()` inclut dynamiques | ✓ | `app.go:182` | ✅ Conforme |
| Event `plugin:loaded` | Contrat | ❌ Non émis dans watchdog | ⚠️ Partiel |
| Event `plugin:failed` | Contrat | ❌ Non émis dans watchdog | ⚠️ Partiel |
| Event `plugin:restarting` | Contrat | ❌ Non émis dans watchdog | ⚠️ Partiel |
| Event `plugin:reloaded` | ✓ | `app.go:226` | ✅ Conforme |
| `PluginInfo.Version` | `"1.0.0"` (exemple) | `"unknown"` (hardcodé) | ⚠️ Décalage |

> **Note** : Les événements `plugin:loaded`, `plugin:failed`, `plugin:restarting` sont définis dans le contrat mais non émis dans `grpc_loader.go`. Le contrat indique "Événements Wails émis par le loader" — mais le loader n'a pas accès au Wails runtime. Ces events devraient être émis par `DynamicRegistry` ou `App`, en demandant au loader de retourner les events via callback. Ce point est à clarifier lors du prochain sprint (v0.6.1).

---

## Régression Backends Statiques

- **webdav.go** : `init()` ajouté ✅, aucune modification fonctionnelle ✅
- **moosefs.go** : `init()` ajouté ✅, aucune modification fonctionnelle ✅
- **manager.go** : imports blancs inchangés ✅, `AvailableTypes()` délègue à `plugins.ListBackends()` ✅
- Pas de régression observable sur les patterns existants.

---

## Verdict Final

- [ ] APPROUVE — Prêt pour merge
- [X] **APPROUVE AVEC RESERVES** — Corriger les majeurs avant déploiement prod
- [ ] REFUSE — Corrections critiques requises

**Conditions de levée des réserves :**
1. **MAJEUR-1** (go.mod indirect) — Régler avant tag de release pour éviter la casse CI en clean checkout
2. **MAJEUR-2** (upload size cap) — Sécurité, à corriger avant prod
3. **MAJEUR-5** (tests Delete/Move) — Couverture de contrat
4. **MAJEUR-6** (Version mismatch) — Aligner contrat ou implémenter

Les MAJEUR-3 (context timeout) et MAJEUR-4 (watchdog events) peuvent être traités en v0.6.1 si le planning le permet.
