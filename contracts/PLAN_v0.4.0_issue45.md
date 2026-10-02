# Plan d'Implémentation : Issue #45 — Interface StorageBackend + Guide Plugin

> **Version cible** : v0.4.0
> **Branche** : `feat/v0.4.0-plugin-local` (branche unique pour tout le milestone v0.4.0 — issues #45, #47, #48)
> **Issue** : #45 (bloque #47 et #48)
> **Date** : 2026-04-23

---

## Périmètre (Scope)

### ✅ Inclus dans cette issue

| Livrable | Fichier |
|---------|---------|
| Interface `StorageBackend` finalisée | `plugins/plugin.go` |
| Template plugin vierge | `plugins/template/template.go` |
| Guide développeur plugin | `docs/plugin-development.md` |
| Contrats + CHANGELOG | `contracts/backend-config.md`, `CHANGELOG.md` |

### ❌ Exclu (traité dans #47 / #48)

- Toute modification de `plugins/webdav/` ou `plugins/moosefs/`
- Implémentation de `GetQuota` dans les plugins existants
- Création des tests MooseFS manquants

---

## Résumé

L'interface `StorageBackend` dans `plugins/plugin.go` est fonctionnelle mais manque de
stabilisation formelle : sentinelles d'erreurs dupliquées dans chaque plugin, absence de
Godoc complets sur les méthodes et types, et absence de contrat documenté sur les
conventions de comportement. Le guide développeur (`docs/plugin-development.md`) est
absent. Cette issue stabilise le contrat interne Go et fournit aux développeurs (humains
et agents Claude Code) les outils pour implémenter un nouveau backend en autonomie.

---

## Critères d'Acceptation

- [ ] `plugins/plugin.go` définit `ErrNotConnected` et `ErrFileNotFound` comme sentinelles partagées
- [ ] `plugins/plugin.go` a des Godoc complets sur l'interface et tous les types (`FileInfo`, `FileEvent`, `BackendConfig`, `ProgressCallback`)
- [ ] `plugins/plugin.go` documente les conventions de comportement de chaque méthode (chemin, erreur, contexte)
- [ ] `plugins/template/template.go` existe et implémente `StorageBackend` en entier (stubs)
- [ ] Le template a le build tag `//go:build ignore` (non compilé en production)
- [ ] `docs/plugin-development.md` existe et guide un développeur de zéro jusqu'à une PR valide
- [ ] `go build ./...` passe (webdav et moosefs toujours valides — ils référencent encore leurs propres sentinel errors jusqu'à #47)
- [ ] `CHANGELOG.md` mis à jour (section v0.4.0)

---

## Composants Impactés

- **Backend** : `plugins/plugin.go` uniquement (interface + types + sentinelles)
- **Template** : `plugins/template/template.go` (nouveau fichier)
- **Documentation** : `docs/plugin-development.md` (nouveau fichier)
- **Contrats** : `contracts/backend-config.md` (ajout type `"local"` à venir)
- **Changelog** : `CHANGELOG.md`
- **webdav / moosefs** : aucune modification (leurs `ErrNotConnected` locaux restent jusqu'à #47)
- **Frontend** : aucun impact

---

## Tâches

### Phase 0 : Init Git — Branche Milestone (dev-backend)

**Prérequis** : aucun — première action absolue

0. [ ] **Créer la branche `feat/v0.4.0-plugin-local` depuis `main`**
   - Fichier(s) : aucun (opération Git)
   - Commande : `git checkout main && git pull && git checkout -b feat/v0.4.0-plugin-local`
   - Description : Toutes les issues du milestone v0.4.0 (#45, #47, #48) se développent
     sur cette branche unique. Jamais de commit direct sur `main`.

---

### Phase 1 : Stabiliser `plugins/plugin.go` (dev-backend)

**Prérequis** : Phase 0

1. [ ] **Centraliser les sentinelles d'erreurs dans `plugins/plugin.go`**
   - Fichier : `plugins/plugin.go`
   - Description : Ajouter dans `plugin.go` :
     ```go
     var (
         ErrNotConnected = errors.New("backend: not connected")
         ErrFileNotFound = errors.New("backend: file not found")
     )
     ```
   - Note : Les plugins existants (`webdav`, `moosefs`) gardent leurs propres sentinel errors
     pour l'instant — la migration vers `plugins.ErrNotConnected` est dans le scope de #47.
     Le template (Phase 2) utilisera directement `plugins.ErrNotConnected`.

2. [ ] **Compléter les Godoc sur l'interface `StorageBackend` et tous les types**
   - Fichier : `plugins/plugin.go`
   - Description : Documenter chaque méthode de l'interface avec :
     - **Pré-condition** : ex. "doit être appelé après `Connect()`, sinon retourne `ErrNotConnected`"
     - **Convention de chemin** : chemins slash-séparés, relatifs à `RemotePath`
     - **Comportement sur erreur** : utiliser `fmt.Errorf("...: %w", ErrNotConnected)` pour le wrapping
     - **Concurrence** : préciser si thread-safe
   - Couvrir aussi : `FileInfo`, `FileEvent`, `FileEventType`, `BackendConfig`, `ProgressCallback`

3. [ ] **Documenter la convention `GetQuota` (interface uniquement, pas d'impl)**
   - Fichier : `plugins/plugin.go`
   - Description : **Ne PAS encore ajouter `GetQuota` à l'interface** — cette décision est
     reportée à #47 où le premier plugin aura besoin de quota réel.
     Ajouter un commentaire TODO dans `plugin.go` :
     ```go
     // TODO(v0.4.0+): Consider adding GetQuota(ctx context.Context) (free, total int64, error)
     // once the LOCAL plugin (issue #47) validates the need. Plugins not supporting quota
     // should return (-1, -1, nil).
     ```
   - Justification : Garder l'interface minimale et stable. Pas d'interface bloating avant validation.

4. [ ] **Mettre à jour le commentaire de `BackendConfig.Type`**
   - Fichier : `plugins/plugin.go`
   - Description : Changer le commentaire inline de `Type string` :
     - Avant : `// "webdav" | "moosefs"`
     - Après : `// "webdav" | "moosefs" | "local" (v0.4.0)`

---

### Phase 2 : Template Plugin (dev-backend)

**Prérequis** : Phase 1 (interface stabilisée)

5. [ ] **Créer `plugins/template/template.go`**
   - Fichier : `plugins/template/template.go` (à créer)
   - Description : Squelette complet implémentant `StorageBackend` :
     - Build tag `//go:build ignore` en première ligne (ne compile pas)
     - Package `template`
     - Import de `plugins` pour `plugins.ErrNotConnected`, `plugins.ErrFileNotFound`
     - Struct `Backend` avec `sync.RWMutex` + `connected bool`
     - Fonction `New() *Backend`
     - Toutes les méthodes de l'interface en stubs retournant des erreurs explicites
       (`errors.New("template: <method> not implemented")`) OU `ErrNotConnected`
       si `!b.IsConnected()`
     - Commentaires `// TODO: implémenter` sur chaque méthode
     - Sentinel errors locaux héritant de `plugins.ErrNotConnected` et `ErrFileNotFound`
     - `progressReader` / `progressWriter` helpers inclus (pattern copié de webdav)

---

### Phase 3 : Guide Développeur (dev-backend)

**Prérequis** : Phase 1 + Phase 2

6. [ ] **Créer `docs/plugin-development.md`**
   - Fichier : `docs/plugin-development.md` (à créer)
   - Description : Guide complet structuré en sections :

     **1. Vue d'ensemble**
     - Architecture plugin GhostDrive (interface Go compilée, pas .dll, pas gRPC)
     - Cycle de vie : `Connect → Watch → Upload/Download → Disconnect`

     **2. L'interface `StorageBackend` — référence complète**
     - Chaque méthode : signature, comportement attendu, conventions
     - Les types partagés : `FileInfo`, `FileEvent`, `BackendConfig`, `ProgressCallback`
     - Erreurs sentinelles : utiliser `plugins.ErrNotConnected` et `plugins.ErrFileNotFound`

     **3. Créer un plugin — guide pas-à-pas**
     - Copier `plugins/template/template.go` → `plugins/<nom>/<nom>.go`
     - Supprimer le build tag `//go:build ignore`
     - Implémenter chaque méthode (ordre conseillé : Connect → Stat → List → Upload → Download → Delete → Move → CreateDir → Watch)
     - Enregistrer le plugin dans la factory (référencer `app.go`)

     **4. Conventions obligatoires**
     - Nommage : package = `plugins/<nom>/`, `Name()` retourne `"<nom>"` en minuscules
     - Chemins : slash-séparés, relatifs à `RemotePath`, jamais absolus
     - Thread-safety : toute struct doit utiliser `sync.RWMutex`
     - Wrapping d'erreurs : `fmt.Errorf("myplugin: connect: %w", err)` — toujours préfixer
     - `Watch` : le channel doit être fermé quand `ctx` est annulé

     **5. Tests**
     - Pattern recommandé selon le type de backend :
       - Filesystem local → `t.TempDir()` comme mount point
       - HTTP/WebDAV → `httptest.NewServer()` + handler in-memory
       - Réseau opaque → mock interface `StorageBackend`
     - Tests minimaux requis : Connect, Upload/Download roundtrip, List, Delete, Stat,
       CreateDir, Move, NotConnected errors
     - Commande : `go test ./plugins/<nom>/... -v -cover`

     **6. Checklist avant PR**
     - [ ] `go vet ./plugins/<nom>/...` sans warning
     - [ ] `go test ./plugins/<nom>/... -cover` ≥ 70%
     - [ ] `go build ./...` passe
     - [ ] `contracts/backend-config.md` mis à jour avec les `Params` du nouveau plugin
     - [ ] `CHANGELOG.md` mis à jour

---

### Phase 4 : Contrats + Changelog (doc-updater)

**Prérequis** : Phases 0-3 terminées

7. [ ] **Mettre à jour `contracts/backend-config.md`**
   - Fichier : `contracts/backend-config.md`
   - Description : Ajouter la section `"local"` avec ses paramètres prévus :
     ```
     local:
       rootPath : "/chemin/absolu/vers/dossier"   // répertoire racine du backend local
     ```
   - Marquer comme "v0.4.0 — implémenté dans #47"

8. [ ] **Mettre à jour `CHANGELOG.md`**
   - Fichier : `CHANGELOG.md`
   - Description : Créer/compléter la section `## [0.4.0] — Unreleased` avec :
     - `feat(plugin): stabilisation interface StorageBackend — sentinelles partagées, Godoc complets`
     - `feat(plugin): template de plugin vierge (plugins/template/template.go)`
     - `docs(plugin): guide d'implémentation de plugin (docs/plugin-development.md)`

---

## Dépendances entre Tâches

```
Phase 0 (tâche 0 — branche Git)
    └── → TOUTES les phases suivantes

Phase 1 (tâches 1-4)
    └── → Phase 2 (tâche 5) : template utilise les sentinelles de plugin.go
    └── → Phase 3 (tâche 6) : guide documente l'interface finalisée

Phase 1 + Phase 2
    └── → Phase 3 (tâche 6) : guide référence le template

Phase 1-3
    └── → Phase 4 (tâches 7-8) : contrats + changelog

Phase 1 validée
    └── → Issues #47 et #48 peuvent démarrer (interface Go stable)
```

---

## Agents Impliqués

| Agent | Tâches | Phase |
|-------|--------|-------|
| **dev-backend** | 0 — création branche | Phase 0 |
| **dev-backend** | 1, 2, 3, 4 — plugin.go | Phase 1 |
| **dev-backend** | 5 — template | Phase 2 |
| **dev-backend** | 6 — guide doc (contenu technique) | Phase 3 |
| **doc-updater** | 7, 8 — contrats + CHANGELOG | Phase 4 |

---

## Tests Requis

- [ ] `go build ./...` — aucune erreur de compilation (template ignoré via `//go:build ignore`)
- [ ] `go test ./plugins/webdav/... -v` — toujours 100% pass (aucune modification)
- [ ] Vérification manuelle : le template référence bien `plugins.ErrNotConnected`

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| Le template avec `//go:build ignore` perturbe le linter | Faible | Faible | Tester avec `go vet ./...` après création |
| `plugins.ErrNotConnected` dans le template, mais webdav/moosefs gardent les leurs → confusion future | Moyenne | Faible | Documenter explicitement dans le guide que la migration sera faite dans #47 |
| `docs/plugin-development.md` trop long / pas utilisable par un agent | Faible | Moyen | Structurer avec des sections claires, titres précis, pas de prose |

---

## Estimation

- **Complexité** : Faible
- **Nombre de fichiers touchés** : 5
  - Modifiés : `plugins/plugin.go`, `contracts/backend-config.md`, `CHANGELOG.md`
  - Créés : `plugins/template/template.go`, `docs/plugin-development.md`
- **Effort** : 1 session dev-backend + 1 session doc-updater

---

## Notes

- `GetQuota` n'est pas ajoutée à l'interface dans cette issue — décision reportée à #47
  pour éviter l'interface bloating avant validation avec un plugin réel.
- La branche `feat/v0.4.0-plugin-local` est partagée par #45, #47, #48 — tous les commits
  du milestone v0.4.0 y vivent jusqu'au squash merge final vers `main`.
- Le guide doc doit être utilisable par un agent Claude Code autonome : sections courtes,
  exemples de code concrets, checklist explicite.
