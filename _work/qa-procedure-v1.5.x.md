# Procédure de Test QA — GhostDrive v1.5.x

**Version** : 1.5.x  
**Date** : 2026-05-03  
**Branche** : feature/v1.5.x  
**Testeur** : QA  
**Issues couvertes** : #93, #92, #26, #27

---

## Prérequis

- [ ] Branche `feature/v1.5.x` checkoutée
- [ ] Go 1.21 installé (`/usr/local/go/bin/go version`)
- [ ] Node.js + npm installés (`node --version`)
- [ ] **Tests automatisés** : environnement Linux/Mac/WSL suffisant
- [ ] **Tests manuels WinFsp** : Windows avec WinFsp installé (`%ProgramFiles%\WinFsp\bin\winfsp-x64.dll` présent)
- [ ] **Tests manuels MooseFS** : accès optionnel à un master MooseFS (sinon : validé via tests automatisés uniquement)

---

## Partie 1 — Tests Automatisés

> Exécuter dans l'ordre. Un échec bloque la suite.

### 1.1 — Suite complète Go

```bash
cd /mnt/c/Users/cyril/Documents/VScode/GITHUB/GhostDrive
/usr/local/go/bin/go test ./... -v -count=1 -race
```

**Résultat attendu** :
- Toutes les suites se terminent avec `PASS`
- Aucune data race détectée
- Aucune panique (`panic`)

**Verdict** : [ ] PASS  [ ] FAIL

---

### 1.2 — Coverage MooseFS (seuil ≥ 70%)

```bash
cd /mnt/c/Users/cyril/Documents/VScode/GITHUB/GhostDrive
/usr/local/go/bin/go test ./plugins/moosefs/... ./plugins/moosefs/internal/... -cover -count=1 -v
```

**Résultat attendu** :
- `coverage: XX.X% of statements` ≥ **70%** sur `plugins/moosefs`
- `coverage: XX.X% of statements` ≥ **70%** sur `plugins/moosefs/internal/mfsclient`
- Tous les cas de test passent :
  - `TestConnect_success` / `TestConnect_unreachable`
  - `TestList_rootDir`
  - `TestStat_existingFile` / `TestStat_notFound`
  - `TestCreateDir`
  - `TestUploadDownload_roundtrip`
  - `TestDelete_file`
  - `TestMove_fileRename`
  - `TestWatch_detectCreated` / `TestWatch_detectDeleted`
  - `TestGetQuota_returnsMinusOne`
  - `TestNotConnected_allOps`
  - `TestDial_success` / `TestDial_refused`
  - `TestReadDir_root`
  - `TestGetAttr_file` / `TestGetAttr_notfound`
  - `TestMknod_createFile`
  - `TestWrite_appendChunks`
  - `TestRead_content`
  - `TestUnlink_file`
  - `TestRmdir_emptyDir`
  - `TestMkdir_createDir`

**Verdict** : [ ] PASS  [ ] FAIL  
Coverage mfsclient : _______ %  
Coverage moosefs   : _______ %

---

### 1.3 — Frontend (si modifié)

```bash
cd /mnt/c/Users/cyril/Documents/VScode/GITHUB/GhostDrive/frontend
npm test -- --run
```

**Résultat attendu** :
- Toutes les suites Vitest passent (`BackendConfig.test.tsx`, `SyncPointForm.test.tsx`)
- Aucune erreur de compilation TypeScript

**Verdict** : [ ] PASS  [ ] FAIL  [ ] SKIPPED (frontend non modifié)

---

### 1.4 — Build MooseFS

```bash
cd /mnt/c/Users/cyril/Documents/VScode/GITHUB/GhostDrive/plugins/moosefs
make linux   # ou: make windows (cross-compile)
```

**Résultat attendu** :
- Binaire `ghostdrive-moosefs` (Linux) ou `ghostdrive-moosefs.exe` (Windows) produit sans erreur
- Aucun warning `go vet`

**Verdict** : [ ] PASS  [ ] FAIL

---

## Partie 2 — Vérifications Manuelles

> Requiert Windows avec GhostDrive et WinFsp installés.  
> Environnement : QUALIF ou LOCAL Windows.

---

### Scénario 2.1 — Issue #93 : Suppression du badge "Manuel"

**Objectif** : Vérifier que le badge texte "Manuel" n'apparaît plus sur les cards backend en mode `autoSync=off`, et que l'icône `RefreshCw` grisée reste le seul indicateur visuel.

**Prérequis** : GhostDrive lancé, au moins un backend configuré.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir GhostDrive → onglet Paramètres backends | Interface chargée | | |
| 2 | Sélectionner (ou créer) un backend avec `autoSync = OFF` | Backend présent dans la liste | | |
| 3 | S'assurer que le backend est **activé** (toggle ON) | Backend vert/actif | | |
| 4 | Observer la card du backend | Le texte **"Manuel"** est **absent** | | |
| 5 | Observer la card du backend | L'icône `RefreshCw` grisée est **présente** (bouton sync manuel) | | |
| 6 | Activer `autoSync = ON` sur ce backend | La card ne doit toujours pas afficher "Manuel" | | |
| 7 | Remettre `autoSync = OFF` | `RefreshCw` grisée réapparaît, toujours sans badge "Manuel" | | |
| 8 | Désactiver le backend (toggle OFF) | La card affiche l'état désactivé, sans badge "Manuel" | | |

**Verdict** : [ ] PASS  [ ] FAIL

**Notes** : _______________________________________________

---

### Scénario 2.2 — Issue #92 : Volume Label WinFsp dynamique

**Objectif** : Vérifier que le drive `GhD:` monté via WinFsp affiche comme label le nom du premier backend actif, et non plus la valeur statique "GhostDrive".

**Prérequis** : Windows avec WinFsp installé, GhostDrive lancé.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Configurer un backend nommé **"MonNAS"** | Backend créé et visible | | |
| 2 | Activer le backend "MonNAS" | GhD: monté | | |
| 3 | Ouvrir l'Explorateur Windows | Le drive `GhD:` affiche le label **"MonNAS"** (pas "GhostDrive") | | |
| 4 | Vérifier via propriétés du drive (clic droit → Propriétés) | Le nom de volume affiché est "MonNAS" | | |
| 5 | Désactiver le backend, reconfigurer avec le nom **"Mon NAS"** (espace) | Backend renommé | | |
| 6 | Activer → vérifier dans l'Explorateur | Label = **"Mon NAS"** (espace conservé) | | |
| 7 | Désactiver, reconfigurer avec un nom **> 15 caractères** (ex: "MonSuperNASPerso") | Backend renommé | | |
| 8 | Activer → vérifier dans l'Explorateur | Label affiché sans troncature (WinFsp accepte les noms longs) | | |
| 9 | Désactiver le backend | Drive `GhD:` démonté | | |
| 10 | Réactiver le backend | Label réapparaît correctement | | |
| 11 | **Fallback** : tester avec un backend dont le nom est vide ou absent | Label fallback = **"GhostDrive"** | | |

**Verdict** : [ ] PASS  [ ] FAIL

**Notes** : _______________________________________________

---

### Scénario 2.3 — Issue #26 : Plugin MooseFS (accès master requis)

> **IMPORTANT** : Ce scénario nécessite un accès à un master MooseFS fonctionnel.  
> Si aucun master n'est disponible, marquer **BLOQUÉ — nécessite master de prod**  
> et valider via les tests automatisés (Partie 1.2) uniquement.

**Prérequis** :
- [ ] Master MooseFS accessible : `<masterHost>:<masterPort>` (défaut port 9421)
- [ ] Droits lecture/écriture sur le chemin configuré
- [ ] Un fichier de test local disponible (ex: `test-upload.txt`)

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ajouter un backend de type **MooseFS** | Formulaire avec champs : Adresse master, Port, Sous-répertoire | | |
| 2 | Renseigner `masterHost` valide + `masterPort` (ex: 9421) | Champs validés | | |
| 3 | Sauvegarder et activer le backend | État affiché : **connecté** | | |
| 4 | Dans la vue fichiers GhostDrive, naviguer vers ce backend | Liste des fichiers/dossiers affichée (peut être vide) | | |
| 5 | Uploader un fichier test (`test-upload.txt`) | Fichier visible dans l'interface + présent sur le serveur | | |
| 6 | Télécharger le fichier uploadé | Contenu identique au fichier source (checksum identique) | | |
| 7 | Créer un dossier via l'interface | Dossier visible dans la liste | | |
| 8 | Supprimer le fichier test | Fichier absent de la liste et du serveur | | |
| 9 | Désactiver le backend | État affiché : **déconnecté** | | |
| 10 | Tester un masterHost **invalide** | Message d'erreur explicite à la connexion | | |

**Verdict** : [ ] PASS  [ ] FAIL  [ ] BLOQUÉ — nécessite master de prod

**Master utilisé** : _______________________________  
**Notes** : _______________________________________________

---

### Scénario 2.4 — Issue #27 : Tests intégration MooseFS (validation indirecte)

> Ce scénario est validé **entièrement via les tests automatisés** (cf. 1.2).  
> Il n'y a pas de vérification manuelle supplémentaire à effectuer.

| Étape | Action | Résultat Attendu | OK ? |
|-------|--------|-----------------|------|
| 1 | Exécuter `go test ./plugins/moosefs/...` (cf. 1.2) | Tous les cas de test passent | |
| 2 | Vérifier la coverage ≥ 70% | Seuil atteint sur les deux packages | |
| 3 | Vérifier l'absence de goroutine leaks (race detector) | Aucune race condition détectée avec `-race` | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Partie 3 — Tests de Non-Régression

### 3.1 — Régression mount_windows (volname dynamique)

**Objectif** : Vérifier que la modification du `volname` dans `mount_windows.go` ne casse pas les tests existants du package `placeholder`.

```bash
cd /mnt/c/Users/cyril/Documents/VScode/GITHUB/GhostDrive
/usr/local/go/bin/go test ./internal/placeholder/... -v -count=1
```

**Résultat attendu** :
- Tous les tests de `internal/placeholder/` passent
- Pas de régression sur `manager_test.go`, `placeholder_test.go`, `router_test.go`

**Verdict** : [ ] PASS  [ ] FAIL

---

### 3.2 — Régression Frontend (BackendConfig)

**Objectif** : S'assurer que la suppression du badge "Manuel" n'a pas cassé les tests existants de `BackendConfig.test.tsx`.

```bash
cd /mnt/c/Users/cyril/Documents/VScode/GITHUB/GhostDrive/frontend
npm test -- --run BackendConfig
```

**Résultat attendu** :
- Tous les tests de `BackendConfig.test.tsx` passent
- Aucun test qui vérifiait la présence du badge "Manuel" ne fail de façon inattendue  
  *(si un test testait le badge "Manuel" et fail, c'est **attendu** : le test doit être mis à jour)*

**Verdict** : [ ] PASS  [ ] FAIL

---

## Critères de Validation Finale

- [ ] **1.1** `go test ./...` : tous les packages Go passent avec `-race`
- [ ] **1.2** Coverage MooseFS ≥ 70% sur `moosefs` et `mfsclient`
- [ ] **1.3** Tests Vitest frontend passent
- [ ] **1.4** Build binaire `ghostdrive-moosefs` réussi
- [ ] **2.1** Badge "Manuel" absent ; `RefreshCw` grisée conservée (#93)
- [ ] **2.2** Label WinFsp = nom du backend, fallback "GhostDrive" si vide (#92)
- [ ] **2.3** Plugin MooseFS connecte, liste, upload, download, delete, disconnect (#26) — ou BLOQUÉ justifié
- [ ] **2.4** Tests intégration MooseFS validés via automatisés (#27)
- [ ] **3.1** Aucune régression `internal/placeholder/`
- [ ] **3.2** Aucune régression frontend inattendue

---

## Verdict Global

| Résultat | Condition |
|----------|-----------|
| **VALIDÉ** | Tous les critères cochés (2.3 peut être BLOQUÉ si pas de master) |
| **RÉSERVES** | ≤ 2 critères mineurs non bloquants, avec justification |
| **NON VALIDÉ** | 1.1 fail, 1.2 coverage < 70%, ou régression critique |

**Verdict QA** : [ ] VALIDÉ  [ ] VALIDÉ AVEC RÉSERVES  [ ] NON VALIDÉ

**Testeur** : _______________  
**Date de test** : _______________  

---

## Notes QA

<!-- Espace libre pour observations, anomalies, screenshots, etc. -->
