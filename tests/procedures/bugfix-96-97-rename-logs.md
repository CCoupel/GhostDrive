# Procédure de Test — Bugfix #96 et #97 (Rename MooseFS + Logs Plugin UI)

**Version** : 1.5.0
**Date** : 2026-05-08
**Testeur** : QA
**Branche** : `feature/v1.5.x`
**Issues** : [#96](https://github.com/CCoupel/GhostDrive/issues/96) · [#97](https://github.com/CCoupel/GhostDrive/issues/97)

---

## Contexte

Deux régressions identifiées en v1.5.0 sur le backend MooseFS :

| Issue | Symptôme avant fix | Fix appliqué |
|-------|-------------------|--------------|
| **#97** | Renommer un dossier dans l'explorateur Windows retournait `ERROR_IO_DEVICE` (0x8007045D) sans jamais déclencher le callback FUSE | `Getattr` : `errors.Is(err, ErrFileNotFound)` → `-ENOENT` (au lieu de `-EIO`) |
| **#96** | Les messages d'erreur des plugins (connexion échouée, etc.) apparaissaient en niveau **INFO** dans l'onglet Logs de l'UI | Détection par mots-clés dans `store.go` : upgrade automatique INFO → ERROR/WARN si le message contient `error`, `failed`, `warn`, etc. |

---

## Prérequis

### Environnement

- [ ] OS : **Windows 10/11** (x64)
- [ ] GhostDrive v1.5.0 installé depuis `build/qualif/1.5.0/` (commit `693cacd` ou ultérieur)
- [ ] WinFsp installé (version compatible cgofuse)
- [ ] Plugin `moosefs.ghdp` présent dans le dossier du binaire

### Données

- [ ] Accès à un master MooseFS (ou simulateur TCP) pour le scénario #97
- [ ] Pour simuler une erreur #96 : il suffit de configurer un backend MooseFS avec une adresse master **inaccessible** (ex : `127.0.0.1:9421` si aucun master ne tourne)

### Droits

- [ ] Droits administrateur pour monter le drive WinFsp (`GhD:`)
- [ ] Onglet **Logs** visible dans l'UI GhostDrive

---

## Scénario 1 — Bug #97 : Rename dossier MooseFS

### Objectif

Vérifier que renommer un dossier depuis l'explorateur Windows réussit (pas de `ERROR_IO_DEVICE`).

### Prérequis spécifiques

- Backend MooseFS configuré, connecté et monté sur `GhD:`
- Le dossier `GhD:\<backend>\testdir` existe (le créer si nécessaire via "Nouveau dossier" dans l'explorateur)

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Ouvrir l'explorateur Windows, naviguer vers `GhD:\<nom-backend>\` | Le contenu du backend est listé | | |
| 2 | Créer un dossier nommé `testdir` si inexistant | Dossier créé sans erreur | | |
| 3 | Clic droit sur `testdir` → **Renommer** | L'invite de renommage s'affiche | | |
| 4 | Saisir `testdir-renamed` et valider (Entrée) | Le dossier est renommé en `testdir-renamed` sans popup d'erreur | | |
| 5 | Vérifier que `testdir-renamed` est visible dans `GhD:\<nom-backend>\` | Dossier présent avec le nouveau nom | | |
| 6 | Vérifier côté MooseFS (CLI ou UI) que le dossier a bien été renommé | `testdir` absent, `testdir-renamed` présent | | |

### Résultat attendu (après fix)

Le renommage s'effectue silencieusement. Aucun popup `ERROR_IO_DEVICE` (code 0x8007045D).

### Résultat avant fix (référence)

L'explorateur affichait une boîte de dialogue :
> "Impossible de renommer testdir. Le périphérique ne répond pas."
> Code : 0x8007045D / ERROR_IO_DEVICE

### Critères PASS / FAIL

- **PASS** : renommage réussi, dossier visible avec nouveau nom, aucune erreur
- **FAIL** : popup d'erreur affiché (tout code), ou dossier non renommé côté MooseFS

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 2 — Bug #97 bis : Rename fichier MooseFS (golden path)

### Objectif

Valider que le renommage de fichier (pas seulement dossier) fonctionne également.

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Dans `GhD:\<nom-backend>\`, créer un fichier texte `test.txt` (copier-coller depuis le bureau) | Fichier présent | | |
| 2 | Renommer `test.txt` → `test-renamed.txt` | Fichier renommé sans erreur | | |
| 3 | Vérifier côté MooseFS que `test-renamed.txt` existe | Fichier présent avec nouveau nom | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 3 — Bug #97 : Rename vers une destination déjà existante

### Objectif

Vérifier le comportement quand la destination du rename existe déjà.

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Créer deux dossiers : `dirA` et `dirB` | Les deux dossiers sont visibles dans `GhD:\<nom-backend>\` | | |
| 2 | Tenter de renommer `dirA` en `dirB` (destination existe déjà) | L'explorateur affiche une boîte de confirmation de remplacement, **ou** retourne une erreur claire — mais **pas** `ERROR_IO_DEVICE` | | |

> Note : le comportement exact (écrasement ou erreur) dépend du plugin MooseFS. L'objectif est d'éviter `ERROR_IO_DEVICE`.

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 4 — Bug #96 : Logs plugin affichés au bon niveau (ERROR)

### Objectif

Vérifier que les messages d'erreur d'un plugin MooseFS apparaissent en rouge (niveau **ERROR**) dans l'onglet Logs, et non en gris/blanc (niveau INFO).

### Prérequis spécifiques

- Configurer un backend MooseFS avec un **master inaccessible** :
  - Adresse : `127.0.0.1` port `9421` (ou n'importe quelle IP injoignable)
  - Nom : `TestErrLogs`

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Ouvrir GhostDrive, naviguer vers l'onglet **Logs** | L'onglet Logs est vide ou contient des entrées existantes | | |
| 2 | Ajouter le backend `TestErrLogs` (master inaccessible) et cliquer "Connecter" | La tentative de connexion échoue | | |
| 3 | Observer les nouvelles entrées dans l'onglet Logs | Des entrées apparaissent avec le message type : `mfsclient: connection failed` ou `dial tcp ... connection refused` | | |
| 4 | Vérifier la **couleur** / **badge de niveau** des entrées d'erreur | Les entrées sont affichées en **rouge** avec badge **ERROR** (non en gris/INFO) | | |
| 5 | Vérifier que la source est `plugin/moosefs.ghdp` | La colonne Source affiche `plugin/moosefs.ghdp` | | |

### Résultat attendu (après fix)

Les messages d'erreur de connexion MooseFS (contenant les mots-clés `error`, `failed`, `refused`) sont **détectés et promus** au niveau `ERROR` dans l'UI, même si le plugin les a émis via `logger.Info` en interne.

### Résultat avant fix (référence)

Les mêmes messages apparaissaient avec le badge **INFO** (couleur neutre), indiscernables des messages de statut normaux.

### Critères PASS / FAIL

- **PASS** : les messages d'erreur de connexion sont visibles en **ERROR** (rouge) dans l'onglet Logs
- **FAIL** : les messages d'erreur restent affichés en INFO, ou n'apparaissent pas du tout

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 5 — Bug #96 : Logs INFO normaux non dégradés

### Objectif

Vérifier que les messages informatifs du plugin (connexion réussie, session active) restent au niveau **INFO** et ne sont pas promus à tort.

### Prérequis spécifiques

- Backend MooseFS configuré avec un master **accessible**

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Connecter le backend MooseFS (master accessible) | Connexion réussie | | |
| 2 | Observer l'onglet Logs | Des messages de connexion/session apparaissent (ex : `sessionID=42 registered`) | | |
| 3 | Vérifier le niveau de ces messages | Les messages informatifs sont affichés en **INFO** (non promus à ERROR) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Critères de Validation Globaux

- [ ] Scénario 1 PASS — renommage dossier sans `ERROR_IO_DEVICE`
- [ ] Scénario 2 PASS — renommage fichier fonctionnel
- [ ] Scénario 3 PASS — rename vers destination existante : pas de `ERROR_IO_DEVICE`
- [ ] Scénario 4 PASS — erreurs plugin affichées en **ERROR** dans Logs
- [ ] Scénario 5 PASS — messages INFO normaux non promus
- [ ] Aucune régression sur les opérations MooseFS existantes (List, Stat, Upload, Download)

---

## Notes QA

> Espace pour observations, captures d'écran, numéro de build testé.

**Build testé** : ___________________
**Date de test** : ___________________
**Testeur** : ___________________

---

## Références

- Fix #97 : `internal/placeholder/router.go` — `errors.Is(err, plugins.ErrFileNotFound)` comme check primaire dans `Getattr`
- Fix #96 : `internal/logging/store.go` — upgrade par mots-clés même quand `levelExplicit=true` et niveau INFO
- Tests automatisés non-régression :
  - `internal/placeholder/router_test.go` — `TestGhostFS_Getattr_ErrFileNotFound_ReturnsENOENT` + `TestGhostFS_Getattr_WrappedErrFileNotFound_ReturnsENOENT`
  - `internal/logging/store_test.go` — `TestParseLine_PluginProxyLog_ErrorUpgrade` + `TestParseLine_PluginProxyLog_WarnUpgrade` + `TestParseLine_PluginProxyLog_InfoStaysInfo`
