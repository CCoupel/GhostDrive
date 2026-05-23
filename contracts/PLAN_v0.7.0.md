# Plan d'Implementation : v0.7.x — Plugin Consolidation

> **Date** : 2026-04-29
> **Branche cible** : `feat/v0.7.x-plugin-consolidation` (depuis `main`)
> **Milestone** : v0.7.x
> **Issues** : #76, #77, #72, #73

---

## Resume

Consolidation de la couche plugin gRPC introduite en v0.6.0 : finalisation du
contrat `StorageBackend` + proto, couverture de tests d'integration sur le
bridge gRPC (handshake, erreurs, crash+watchdog), polish du SDK Go (echo
example + Makefile Linux), et mise à jour de la documentation développeur pour
refléter l'architecture réelle (go-plugin + gRPC, pas les plugins compilés
statiques).

Pas de nouveaux bindings Wails — aucun fichier `contracts/` a creer.

---

## Criteres d'Acceptation

- [ ] `go vet ./plugins/...` passe sans avertissement
- [ ] `go test ./plugins/...` passe (171 tests existants + nouveaux)
- [ ] Couverture `plugins/loader/` + `plugins/grpc/` >= 80%
- [ ] Couverture globale business packages >= 70% (CI)
- [ ] `make build` dans `plugins/sdk/go/` produit `echo.exe` (Windows) et `echo` (Linux)
- [ ] `docs/plugin-development.md` reflète l'architecture gRPC v0.6.0+
- [ ] Le mock plugin (`plugins/testdata/mock-plugin/`) compile en CI (Linux AMD64)
- [ ] Pas de regression sur les tests existants

---

## Composants Impactes

- **`plugins/plugin.go`** : audit godoc + sentinelles (issue #76)
- **`plugins/proto/storage.proto`** : reserved fields + versioning (issue #76)
- **`plugins/grpc/server.go`** : correction incoherence `GetQuota` error path (issue #76)
- **`plugins/grpc/client.go`** : revue godoc (issue #76)
- **`plugins/loader/grpc_loader.go`** : Linux scan + revue godoc (issue #76)
- **`plugins/testdata/mock-plugin/`** : nouveau — plugin mock compilable (issue #77)
- **`plugins/loader/grpc_loader_test.go`** : extension couverture (issue #77)
- **`plugins/grpc/client_test.go`** : extension couverture (issue #77)
- **`plugins/sdk/go/echo/main.go`** : polish commentaires (issue #72)
- **`plugins/sdk/go/Makefile`** : ajout cible Linux (issue #72)
- **`docs/plugin-development.md`** : refonte complete v0.7.0 (issue #73)
- **`.github/workflows/ci.yml`** : ajout build du mock plugin (issue #77)

---

## Points d'Attention Techniques

### Mock plugin en CI (critique)

Le mock plugin dans `plugins/testdata/mock-plugin/` doit etre un binaire Go
autonome compilable. Deux strategies possibles :

**Option A (recommandee) — compilation dans TestMain** :
```go
// grpc_loader_integration_test.go
func TestMain(m *testing.M) {
    // go build -o mock-plugin plugins/testdata/mock-plugin/
    buildMockPlugin(t)
    os.Exit(m.Run())
}
```
Avantage : pas de binaire commite, fonctionne cross-platform.
Inconvenient : allonge le temps de test (~5s), necessite `go` dans PATH (OK en CI).

**Option B — `go generate` + binaire commite** :
Commiter `echo.exe` dans testdata. Risque : binaire Windows uniquement, echoue sur CI Linux.

**Choix : Option A**. Le `TestMain` compile le mock avant les tests via
`exec.Command("go", "build", "-o", mockBinaryPath, "./testdata/mock-plugin/")`.
Utiliser `t.TempDir()` pour le binaire de sortie. Sur CI (Linux), le mock
compile sans CGO (`CGO_ENABLED=0`).

### Watchdog test timing

Le watchdog utilise des delais reels (1s, 2s, 4s). Les tests ne doivent pas
attendre ces delais. Deux approches :
- Injecter une `WatchdogConfig` avec des delais parametrables (refactoring de
  `grpc_loader.go` — faible risque, delais deviennent des champs de struct).
- Tester uniquement les comportements observables sans declencher le watchdog
  (crash + verifier status "failed" apres Shutdown).

**Choix** : parametrisation legere des delais via un champ optionnel
`watchdogDelays []time.Duration` dans `GRPCLoader`. Valeur par defaut : `{1s,
2s, 4s}`. Les tests utilisent `{10ms, 10ms, 10ms}`.

### Incoherence server.go — GetQuota

`GetQuota` retourne `{Error: err.Error()}` dans le champ proto au lieu
d'utiliser `mapBackendError`. Cela rompt la propagation des sentinelles
`ErrNotConnected` / `ErrFileNotFound` cote client pour cette methode.
Correction : migrer `GetQuota` vers le pattern `mapBackendError` (retourner un
gRPC status error) — alignement avec Delete, Move, List, Stat, CreateDir.

Note : `Upload` et `Download` utilisent intentionnellement `SendAndClose` /
`stream.Send` (pattern streaming), pas `mapBackendError` — c'est correct.

### Champ `warning` manquant dans proto

`BackendConfig.Warning` existe en Go mais n'a pas de champ correspondant dans
`BackendConfigProto`. Ce champ est utilise pour les warnings non-bloquants
(soft conflicts). Il n'est pas transmis plugin→loader dans le sens Connect
(le loader envoie la config, pas le plugin), donc l'absence est acceptable.
Documenter dans le proto avec un commentaire `// Note: Warning is loader-side only`.

### Scan Linux dans grpc_loader.go

Le code actuel scanne uniquement `*.exe`. Le bloc `otherMatches` est presente
comme "reserved for future". Pour v0.7.x : implementer le scan des executables
sans extension sur Linux/macOS (fichiers sans extension, bit executable set).
Utiliser `os.Stat(path).Mode()&0111 != 0` pour la detection.

### Makefile Linux

Ajouter une cible `build-linux` au Makefile existant :
```makefile
build-linux:
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -tags ignore -ldflags="-s -w" -o echo ./echo/
```
Et une cible `build-all` qui appelle les deux.

---

## Taches

### Phase 1 : Audit et Stabilisation Contrat (#76)

**Objectif** : finaliser les fichiers de contrat sans modifier le comportement
observable. Phase purement qualitative.

1. [ ] **Audit `plugins/plugin.go`**
   - Fichier : `plugins/plugin.go`
   - Verifier que le package doc ne mentionne plus "compile dans le binaire
     principal" (devenu obsolete depuis v0.6.0 avec go-plugin)
   - Mettre a jour le package doc pour mentionner les deux modes :
     plugins statiques (`plugins/<nom>/`) + plugins dynamiques (go-plugin gRPC)
   - Verifier exhaustivite des godoc sur chaque methode (preconditions,
     valeurs de retour, comportements aux limites)
   - Verifier les constantes `FileEventType` : s'assurer que toutes sont
     documentees et que la liste est complete
   - Aucune modification de signature

2. [ ] **Audit et finalisation `plugins/proto/storage.proto`**
   - Fichier : `plugins/proto/storage.proto`
   - Ajouter des blocs `reserved` sur les messages principaux pour prevenir
     la reutilisation de numeros de champs supprimes :
     - `BackendConfigProto` : `reserved 10 to 19;` (plage future)
     - `FileInfoProto` : `reserved 9 to 19;`
     - Service `StorageService` : ajouter commentaire de version
       `// Protocol version: 1 (HandshakeConfig.ProtocolVersion)`
   - Ajouter commentaire sur `BackendConfigProto` : champ `warning` absent
     intentionnellement (loader-side only, non transmis en Connect)
   - Verifier coherence interface Go ↔ service gRPC : chaque methode de
     `StorageBackend` doit avoir un RPC correspondant — audit croise
   - Regenerer les stubs si modification du proto :
     `protoc --go_out=. --go-grpc_out=. --go_opt=paths=source_relative
     --go-grpc_opt=paths=source_relative plugins/proto/storage.proto`
   - Fichiers generes a ne pas modifier manuellement :
     `plugins/proto/storage.pb.go`, `plugins/proto/storage_grpc.pb.go`

3. [ ] **Correction incoherence GetQuota dans `plugins/grpc/server.go`**
   - Fichier : `plugins/grpc/server.go`
   - Remplacer le pattern `{Error: err.Error()}` de `GetQuota` par
     `return nil, mapBackendError(err)` (alignement avec les autres methodes)
   - Mettre a jour le test correspondant dans `plugins/grpc/client_test.go`
     (ajouter `TestGRPCBackend_GetQuota_ErrorMapping`)

4. [ ] **Finalisation Linux scan dans `plugins/loader/grpc_loader.go`**
   - Fichier : `plugins/loader/grpc_loader.go`
   - Remplacer le bloc `otherMatches` commenté par une implementation reelle :
     scanner les fichiers sans extension avec bit executable set (`0111`)
   - Parametriser les delais watchdog (champ `watchdogDelays []time.Duration`
     avec valeur par defaut `{1s, 2s, 4s}`)
   - Exposer un constructeur `NewGRPCLoaderWithOptions(opts LoaderOptions)`
     pour les tests (ne pas casser `NewGRPCLoader()`)
   - Documenter le comportement cross-platform dans le package doc

### Phase 2 : Mock Plugin et Tests d'Integration (#77)

**Objectif** : creer le mock plugin compilable et atteindre >= 80% de
couverture sur `plugins/loader/` et `plugins/grpc/`.

**Dependance** : Phase 1 (delais parametrables dans `GRPCLoader` requis).

5. [ ] **Creer `plugins/testdata/mock-plugin/`**
   - Fichiers a creer :
     - `plugins/testdata/mock-plugin/main.go`
     - `plugins/testdata/mock-plugin/mock.go`
   - Contenu : plugin go-plugin minimal qui implemente `StorageBackend` avec
     des reponses hardcodees (similaire a `echo` mais sans `//go:build ignore`)
   - Le binaire doit repondre au handshake go-plugin correctement
   - `Name()` retourne `"mock"`, toutes les operations retournent succes
   - Ne pas utiliser `//go:build ignore` — ce fichier est destine aux tests
   - La compilation se fait dans `TestMain` via `exec.Command("go", "build", ...)`

6. [ ] **Extension `plugins/loader/grpc_loader_test.go` — tests d'integration**
   - Fichier : `plugins/loader/grpc_loader_test.go`
   - Ajouter `TestMain` qui compile le mock plugin avant les tests
   - Nouveaux tests a ajouter :
     - `TestGRPCLoader_ValidPlugin` — scan d'un mock plugin valide, verifier
       status "loaded", verifier que le plugin repond a Name()
     - `TestGRPCLoader_HandshakeFailed_WrongCookie` — binaire avec mauvais
       GHOSTDRIVE_PLUGIN cookie → status "failed"
     - `TestGRPCLoader_Timeout` — utiliser un binaire qui bloque au demarrage
       (ou un binaire invalide) et verifier le timeout go-plugin
     - `TestGRPCLoader_Watchdog_RestartOnCrash` — charger le mock, le tuer
       manuellement, verifier que le watchdog tente un redemarrage
       (utiliser `watchdogDelays: {10ms, 10ms, 10ms}`)
     - `TestGRPCLoader_Watchdog_MaxRetries` — simuler 3 crashes consecutifs,
       verifier que le status final est "failed" avec message "max restart
       attempts reached"
     - `TestGRPCLoader_Shutdown_KillsPlugin` — verifier que Shutdown termine
       bien le sous-processus (verifier `client.Exited()` apres Shutdown)

7. [ ] **Extension `plugins/grpc/client_test.go` — error mapping**
   - Fichier : `plugins/grpc/client_test.go`
   - Ajouter `TestGRPCBackend_GetQuota_ErrorMapping` (test la correction #76.3)
   - Ajouter `TestGRPCBackend_GetQuota_NotSupportedReturnsMinusOne` — verifier
     que (-1, -1, nil) est transmis correctement par le bridge
   - Ajouter `TestGRPCBackend_Upload_Disconnected` — verifier que ErrNotConnected
     est propage sur Upload quand le backend n'est pas connecte
   - Ajouter `TestGRPCBackend_Download_FileNotFound` — verifier ErrFileNotFound

8. [ ] **Mise a jour CI pour compiler le mock plugin**
   - Fichier : `.github/workflows/ci.yml`
   - Ajouter `CGO_ENABLED=0` a l'etape `go test` pour assurer la compatibilite
     cross-platform du mock plugin
   - Verifier que `go test ./plugins/...` couvre bien les nouveaux tests
     d'integration avec le mock
   - Pas de nouvelle step CI necessaire si TestMain gere la compilation

### Phase 3 : SDK Go — Polish et Linux (#72)

**Objectif** : rendre l'exemple echo propre et compilable pour Linux.

**Dependance** : Phase 1 (la reference a `loader.HandshakeConfig` est deja
presente — verifier, pas modifier).

9. [ ] **Polish `plugins/sdk/go/echo/main.go`**
   - Fichier : `plugins/sdk/go/echo/main.go`
   - Verifier que le fichier reference bien `loader.HandshakeConfig`
     (actuellement via `sdk.ServeConfig` → c'est correct, pas de copie locale)
   - Ameliorer les commentaires : ajouter description de chaque operation
     et clarifier le comportement "echo" (no-op avec log)
   - Ajouter un exemple d'utilisation des erreurs sentinelles dans les
     commentaires de methode
   - Retirer mention Windows uniquement dans le package doc (le binaire
     fonctionnera aussi sur Linux apres Makefile update)

10. [ ] **Mise a jour `plugins/sdk/go/Makefile`**
    - Fichier : `plugins/sdk/go/Makefile`
    - Ajouter cible `build-linux` :
      `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags ignore -ldflags="-s -w" -o echo ./echo/`
    - Ajouter cible `build-all` qui appelle `build` + `build-linux`
    - Mettre a jour les commentaires d'usage en tete de Makefile
    - Mettre a jour `clean` pour supprimer aussi le binaire `echo` Linux

11. [ ] **Mise a jour `plugins/sdk/go/README.md`**
    - Fichier : `plugins/sdk/go/README.md`
    - Mettre a jour l'etape 3 (Build) pour mentionner `make build-linux`
    - Ajouter une note sur le Makefile multi-cible

### Phase 4 : Documentation Developpeur (#73)

**Objectif** : refonte de `docs/plugin-development.md` pour refleter
l'architecture v0.6.0+ (go-plugin + gRPC externe).

**Dependance** : Phase 3 (Makefile finalise, echo reference correcte).

12. [ ] **Refonte `docs/plugin-development.md`**
    - Fichier : `docs/plugin-development.md`
    - Mettre a jour la version en en-tete : `v0.7.0`
    - **Section 1 — Vue d'ensemble** : distinguer les deux architectures :
      - Plugins statiques (compiles dans le binaire) : `plugins/local/`
      - Plugins dynamiques (go-plugin + gRPC) : tout autre backend
      - Ajouter schema ASCII du flux go-plugin (loader → subprocess → gRPC)
    - **Section 2 — Interface StorageBackend** : synchroniser avec `plugin.go`
      actuel (ajouter `GetQuota` qui etait marquee "reportee" en v0.4.0,
      mettre a jour `BackendConfig` avec `AutoSync` et `LocalPath`)
    - **Section 3 — Creer un plugin externe (go-plugin)** : guide pas-a-pas :
      1. Creer un module Go independant (ou utiliser go.work)
      2. Implementer `plugins.StorageBackend`
      3. Creer `main.go` avec `goplugin.Serve(sdk.ServeConfig(&MyPlugin{}))`
      4. Compiler : `make build` (Windows) ou `make build-linux` (Linux)
      5. Installer : deposer le binaire dans `<AppDir>/plugins/`
      6. Recharger : ReloadPlugins() ou redemarrer GhostDrive
    - **Section 4 — Conventions obligatoires** : idem v0.4.0 + ajouter
      regles specifiques aux plugins externes :
      - Le binaire ne doit pas tenir d'etat persistant entre deux instances
        (le watchdog peut le relancer)
      - `Name()` doit etre idempotent et sans I/O (appele avant Connect)
      - Pas de goroutines en fuite apres `Disconnect()`
    - **Section 5 — Transport gRPC** : section nouvelle :
      - Expliquer `HandshakeConfig` et pourquoi ne pas le copier
        (utiliser `loader.HandshakeConfig`)
      - Expliquer le mapping erreur Go sentinelle → gRPC code → sentinelle
        (round-trip via `mapBackendError` / `mapGRPCError`)
      - Note compatibilite polyglotte : le proto est dans `plugins/proto/`,
        un plugin peut etre implemente dans n'importe quel langage supportant
        gRPC (Python, Rust, etc.) — mais devra gerer le handshake go-plugin
    - **Section 6 — Tests** : mettre a jour avec le pattern `bufconn` de
      `plugins/grpc/client_test.go` comme reference pour les tests unitaires
      du bridge
    - **Section 7 — Checklist avant PR** : mettre a jour avec les nouveaux
      items (compilation binaire, test avec loader, ...) et retirer les
      references a l'ancien workflow "compile dans le binaire principal"

---

## Tests Requis

| Categorie | Fichier | Tests nouveaux |
|-----------|---------|---------------|
| Unite grpc | `plugins/grpc/client_test.go` | GetQuota error mapping, Upload/Download disconnected, Download NotFound |
| Integration loader | `plugins/loader/grpc_loader_test.go` | ValidPlugin, HandshakeFailed, Watchdog restart, Watchdog max retries, Shutdown kills |
| Mock plugin | `plugins/testdata/mock-plugin/` | Compile et repond au handshake |

**Couverture cible** :
- `plugins/grpc/` : >= 80%
- `plugins/loader/` : >= 80%
- Global (CI coverpkg) : >= 70%

**Commandes de validation** :
```bash
go test ./plugins/... -v -cover
go test ./plugins/grpc/... -coverprofile=grpc.out && go tool cover -func=grpc.out
go test ./plugins/loader/... -coverprofile=loader.out && go tool cover -func=loader.out
```

---

## Risques et Mitigations

| Risque | Probabilite | Impact | Mitigation |
|--------|-------------|--------|------------|
| Compilation mock plugin echoue en CI (Linux) | Moyen | Eleve | Utiliser `CGO_ENABLED=0`, tester en local sur Ubuntu avant push |
| Watchdog test flaky (timing) | Moyen | Moyen | Parametriser les delais a 10ms, utiliser `require.Eventually` avec timeout genereux |
| Regeneration proto casse les stubs existants | Faible | Eleve | Comparer le diff des `.pb.go` apres regeneration, commiter uniquement si identiques |
| Refonte doc invalide les workflows agents existants | Faible | Moyen | Garder les sections existantes, ajouter sans supprimer sauf si obsolete avere |
| Linux scan trouve des fichiers non-plugin | Faible | Faible | Filtrer par bit executable ET taille > 0 ET pas de `.md`/`.txt` |

---

## Estimation

- Complexite : Moyenne
- Nombre de fichiers modifies/crees : ~12
- Phases : 4
- Tests nouveaux : ~12 tests

---

## Ordre d'Execution Recommande

```
Phase 1 (#76) → Phase 2 (#77) → Phase 3 (#72) → Phase 4 (#73)
```

La Phase 2 depend de la Phase 1 (delais watchdog parametrables).
Les Phases 3 et 4 sont independantes entre elles mais logiquement apres Phase 1.
La Phase 4 reference les fichiers finalises en Phase 3 (Makefile Linux).

---

## Commits Conventionnels Attendus

```
feat(plugins): parametrize watchdog delays for testability (#76)
fix(plugins): align GetQuota error path with mapBackendError (#76)
feat(plugins): finalize storage.proto reserved fields + versioning comment (#76)
docs(plugins): update plugin.go package doc for static+dynamic arch (#76)
feat(plugins): implement Linux executable scan in GRPCLoader (#76)
test(plugins): add mock-plugin testdata + integration tests grpc_loader (#77)
test(plugins): extend grpc client_test coverage — error mapping (#77)
feat(sdk): add Linux build target to Makefile (#72)
docs(sdk): polish echo comments + README multi-platform build (#72)
docs(plugins): rewrite plugin-development.md for v0.7.0 gRPC arch (#73)
```
