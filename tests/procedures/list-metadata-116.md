# Procédure de Test — Bugfix #116 (MooseFS List() — taille et date)

**Version** : 1.8.0
**Date** : 2026-05-16
**Testeur** : QA
**Branche** : `main`
**Issue** : [#116](https://github.com/CCoupel/GhostDrive/issues/116)
**Fix SHA** : 910b5a0

---

## Contexte

Avant le fix, `List()` du backend MooseFS retournait des métadonnées vides pour chaque entrée :

| Champ | Valeur avant fix | Valeur attendue |
|-------|-----------------|-----------------|
| Taille | 0 octet | Taille réelle du fichier |
| Date de modification | 01/01/1970 00:00 (epoch) | Date réelle de modification |

**Cause** : La commande `GETDIR` (flags=0) du protocole MooseFS ne retourne pas les attributs dans la réponse ReadDir. Le fix consiste à appeler `GetAttr(nodeID)` pour chaque entrée listée, afin de récupérer `Size` et `ModTime`.

**Test unitaire couverture automatique** : `TestList_populatesMetadata` dans `plugins/moosefs/moosefs_test.go`.

---

## Prérequis

### Environnement

- [ ] OS : **Windows 10/11** (x64)
- [ ] GhostDrive v1.8.0 installé depuis `build/qualif/1.8.0/` (commit `910b5a0` ou ultérieur)
- [ ] WinFsp installé (version compatible cgofuse)
- [ ] Plugin `moosefs.ghdp` présent dans le dossier du binaire
- [ ] Accès à un master MooseFS **accessible** avec des fichiers de tailles variées

### Données de test

- [ ] Un répertoire MooseFS contenant au moins :
  - 1 fichier de petite taille (< 1 KB) — ex : `small.txt`
  - 1 fichier de grande taille (> 1 MB) — ex : `large.bin`
  - 1 sous-répertoire — ex : `subdir/`
- [ ] Les tailles et dates de modification réelles des fichiers de test sont connues à l'avance

### Droits

- [ ] Droits administrateur pour monter le drive WinFsp (`GhD:`)
- [ ] Accès en lecture au backend MooseFS (règle : **lecture seule absolue**)

---

## Scénario 1 — MooseFS : Taille et date affichées correctement (golden path)

### Objectif

Vérifier que les fichiers listés dans `GhD:` affichent leur taille réelle et une date de modification non nulle (après fix #116).

### Prérequis spécifiques

- Backend MooseFS configuré, connecté et monté sur `GhD:`
- Les fichiers de test sont présents sur le backend (voir section Données de test)

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Ouvrir l'explorateur Windows, naviguer vers `GhD:\<nom-backend>\` | Le contenu du backend est listé sans erreur | | |
| 2 | Sélectionner la vue **Détails** (clic droit dans le fond de la fenêtre → Affichage → Détails) | Les colonnes Nom, Date de modification, Taille sont visibles | | |
| 3 | Localiser `small.txt` dans la liste | Le fichier est présent | | |
| 4 | Vérifier la colonne **Taille** de `small.txt` | Taille affichée ≠ 0 et correspond à la taille réelle connue | | |
| 5 | Vérifier la colonne **Date de modification** de `small.txt` | Date affichée ≠ 01/01/1970, correspond à la date réelle | | |
| 6 | Localiser `large.bin` dans la liste | Le fichier est présent | | |
| 7 | Vérifier la colonne **Taille** de `large.bin` | Taille affichée ≠ 0 et correspond à la taille réelle connue | | |
| 8 | Vérifier la colonne **Date de modification** de `large.bin` | Date affichée ≠ 01/01/1970, correspond à la date réelle | | |
| 9 | Localiser `subdir/` dans la liste | Le sous-répertoire est présent | | |
| 10 | Vérifier la colonne **Date de modification** de `subdir/` | Date affichée ≠ 01/01/1970 | | |
| 11 | Faire un clic droit sur `small.txt` → **Propriétés** | La fenêtre Propriétés s'ouvre | | |
| 12 | Vérifier "Taille" dans les Propriétés | Correspond à la taille réelle (cohérent avec l'affichage colonne) | | |

### Résultat attendu (après fix)

- Colonne **Taille** : affiche la taille réelle en Ko/Mo/Go selon la taille du fichier — jamais 0.
- Colonne **Date de modification** : affiche une date valide (année ≥ 2020) — jamais 01/01/1970.

### Résultat avant fix (référence)

- Colonne **Taille** : affichait systématiquement `0 Ko` pour tous les fichiers.
- Colonne **Date de modification** : affichait `01/01/1970 00:00` pour toutes les entrées.

### Critères PASS / FAIL

- **PASS** : taille ≠ 0 ET date ≠ epoch pour chaque fichier vérifié (étapes 4, 5, 7, 8, 10, 12)
- **FAIL** : au moins une entrée affiche `0 Ko` ou `01/01/1970`

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 2 — MooseFS : Navigation dans un sous-répertoire

### Objectif

Vérifier que les métadonnées sont correctes également pour les fichiers dans les sous-répertoires (pas seulement à la racine).

### Prérequis spécifiques

- `subdir/` contient au moins 1 fichier de taille connue

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Double-cliquer sur `subdir/` pour naviguer dedans | Le contenu du sous-répertoire est listé | | |
| 2 | Vérifier la colonne **Taille** de chaque fichier présent | Taille ≠ 0 et correspond à la taille réelle | | |
| 3 | Vérifier la colonne **Date de modification** de chaque fichier | Date ≠ 01/01/1970 | | |
| 4 | Naviguer vers le répertoire parent (bouton Précédent) | Retour au répertoire racine du backend sans erreur | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 3 — Régression : Backend WebDAV non affecté

### Objectif

Vérifier que le fix #116 (qui ne touche que `plugins/moosefs/moosefs.go`) n'a pas introduit de régression sur le backend WebDAV.

### Prérequis spécifiques

- Backend WebDAV configuré, connecté et monté sur `GhD:` (serveur WebDAV accessible)
- Au moins 1 fichier de taille connue sur le backend WebDAV

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Naviguer vers `GhD:\<nom-backend-webdav>\` en vue Détails | Contenu listé sans erreur | | |
| 2 | Vérifier la colonne **Taille** des fichiers | Taille affichée correctement (inchangé vs avant le fix) | | |
| 3 | Vérifier la colonne **Date de modification** des fichiers | Date affichée correctement (inchangé vs avant le fix) | | |
| 4 | Tenter d'ouvrir un fichier (double-clic) | Fichier s'ouvre normalement dans l'application associée | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 4 — Régression : Backend LOCAL non affecté

### Objectif

Vérifier que le fix #116 n'a pas introduit de régression sur le backend LOCAL.

### Prérequis spécifiques

- Backend LOCAL configuré, connecté et monté sur `GhD:`
- Le répertoire source LOCAL contient des fichiers de tailles variées

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Naviguer vers `GhD:\<nom-backend-local>\` en vue Détails | Contenu listé sans erreur | | |
| 2 | Vérifier la colonne **Taille** des fichiers | Taille identique à celle du dossier source Windows (référence via l'explorateur) | | |
| 3 | Vérifier la colonne **Date de modification** des fichiers | Date identique à la date réelle des fichiers source | | |
| 4 | Créer un fichier texte dans `GhD:\<nom-backend-local>\` via Nouveau → Document texte | Fichier créé, visible dans les deux chemins (GhD: et source) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 5 — MooseFS : Performance de listage (régression perf)

### Objectif

Vérifier que l'appel `GetAttr()` par entrée ne dégrade pas le temps de listage de manière perceptible sur un répertoire de taille raisonnable.

### Prérequis spécifiques

- Répertoire MooseFS contenant **10 à 50 fichiers**

### Étapes

| # | Action | Résultat attendu | Résultat obtenu | OK ? |
|---|--------|-----------------|----------------|------|
| 1 | Naviguer dans `GhD:\<nom-backend>\` (répertoire avec 10-50 fichiers) | Le listage se complète | | |
| 2 | Mesurer visuellement le temps de chargement de la liste | Listage visible en < 5 secondes pour 50 fichiers | | |
| 3 | Appuyer sur F5 pour rafraîchir | La liste se recharge en < 5 secondes | | |

> Note : l'appel `GetAttr()` par entrée est un appel réseau supplémentaire par fichier — acceptable pour V1 sur des répertoires de taille courante. Si le testeur constate une lenteur sur > 100 fichiers, noter dans la section Notes QA.

**Verdict** : [ ] PASS  [ ] FAIL

---

## Critères de Validation Globaux

- [ ] Scénario 1 PASS — taille et date réelles affichées pour fichiers à la racine
- [ ] Scénario 2 PASS — métadonnées correctes dans les sous-répertoires
- [ ] Scénario 3 PASS — backend WebDAV non régressé
- [ ] Scénario 4 PASS — backend LOCAL non régressé
- [ ] Scénario 5 PASS — performance acceptable (< 5s pour ≤ 50 fichiers)
- [ ] Aucun affichage de `0 Ko` ou `01/01/1970` sur le backend MooseFS

---

## Notes QA

> Espace pour observations, captures d'écran, numéro de build testé, latence observée.

**Build testé** : ___________________
**Date de test** : ___________________
**Testeur** : ___________________
**Serveur MooseFS** : ___________________

---

## Références

- Fix : `plugins/moosefs/moosefs.go` — `List()` appelle `GetAttr(nodeID)` par entrée (SHA 910b5a0)
- Test automatisé non-régression : `plugins/moosefs/moosefs_test.go` — `TestList_populatesMetadata`
- Issue : [#116 — GhD: taille=0 et date=01/01/1970 pour fichiers MooseFS](https://github.com/CCoupel/GhostDrive/issues/116)
