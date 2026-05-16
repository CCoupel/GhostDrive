# Procédure de Test — Lecture EC4+1 MooseFS (issue #114)

**Version** : v1.8  
**Date** : 2026-05-16  
**Testeur** : QA  
**Scope** : Plugin MooseFS — lecture de chunks erasure-coded EC4+1

---

## Prérequis

- [ ] **Environnement** : QUALIF (serveur MooseFS Pro 4.x avec EC activé)
- [ ] **Données** : Fichiers EC4+1 pré-existants sur le cluster (voir section Préparation)
- [ ] **Accès** : Credentials MooseFS valides + adresse master (`192.168.1.231:9421`)
- [ ] **Build** : Binaire GhostDrive v1.8 avec le plugin MooseFS
- [ ] **Règle absolue** : Accès lecture seule au cluster MooseFS — ne jamais modifier ni supprimer de fichiers

---

## Contexte Technique

Le protocole EC4+1 MooseFS Pro distribue chaque chunk sur 4 chunk-servers (DF0-DF3).  
Le physical chunk ID de chaque shard est dérivé du logical ID retourné par le master :

```
physical[i] = logical + 0x1000000000000000 + i × 0x0100000000000000
```

La lecture est shard-granulaire : pour un offset donné, seul 1 CS sur 4 est contacté.

---

## Préparation

### Vérification des fichiers EC sur le cluster

Avant de tester, vérifier qu'il existe des fichiers EC4+1 sur le volume de test :

```bash
# Vérifier les chunks EC sur le master
mfsfileinfo /mnt/mfs/test/ec_small.bin
mfsfileinfo /mnt/mfs/test/ec_large.bin
```

Les fichiers doivent afficher `chunks EC 4+1` dans la sortie de `mfsfileinfo`.

Si les fichiers de test n'existent pas, les créer avec le script POC :

```bash
# Générer les fichiers de test EC (lecture seule depuis GhostDrive — écrire via mfstools)
bash poc/generate_ec_test_files.sh
```

---

## Scénarios

### Scénario 1 — Téléchargement d'un fichier EC4+1 < 64 MiB (1 chunk)

**Objectif** : Vérifier qu'un fichier EC4+1 d'un seul chunk se télécharge correctement.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Configurer le backend MooseFS dans GhostDrive (master `192.168.1.231:9421`) | Backend connecté, statut "synced" | | |
| 2 | Sélectionner un fichier EC4+1 < 64 MiB (ex: `ec_small.bin`, 32 KiB) | Fichier listé dans GhostDrive | | |
| 3 | Déclencher le téléchargement du fichier | Progression affichée, pas d'erreur | | |
| 4 | Vérifier le fichier téléchargé : `sha256sum ec_small.bin` vs original | Checksums identiques | | |
| 5 | Vérifier les logs GhostDrive : `grep "readEC4At" ghostdrive.log` | Lignes `readEC4At chunkID=... shard=0` présentes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scénario 2 — Téléchargement d'un fichier EC4+1 > 64 MiB (multi-chunk)

**Objectif** : Vérifier le séquençage correct sur plusieurs chunks EC.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Sélectionner un fichier EC4+1 > 64 MiB (ex: `ec_large.bin`, 128 MiB) | Fichier listé dans GhostDrive | | |
| 2 | Déclencher le téléchargement | Progression de 0% à 100%, pas d'erreur | | |
| 3 | Vérifier le fichier : `sha256sum ec_large.bin` vs original | Checksums identiques | | |
| 4 | Dans les logs : vérifier que chunkID change entre les chunks | `chunkID=0x...` différent pour offset 0 et offset 64MiB | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scénario 3 — Non-régression chunks normaux (proto=0/1/2)

**Objectif** : S'assurer que les fichiers non-EC (chunks répliqués) fonctionnent toujours.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Sélectionner un fichier non-EC (répliqué normalement) | Fichier listé | | |
| 2 | Télécharger le fichier | Téléchargement sans erreur | | |
| 3 | Vérifier checksum | Identique à l'original | | |
| 4 | Vérifier logs : absence de `readEC4At` pour ce fichier | Aucune ligne `readEC4At` dans les logs | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scénario 4 — Réponse proto=3 avec ECParts détecté

**Objectif** : Vérifier que le master proto=3 est bien traité (ECParts=4 positionné).

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Activer les logs DEBUG dans GhostDrive (`LOG_LEVEL=debug`) | Logs verbeux activés | | |
| 2 | Télécharger un fichier EC4+1 | Téléchargement réussi | | |
| 3 | Grep : `grep "proto=3 EC chunk" ghostdrive.log` | Ligne `parseChunkInfo: proto=3 EC chunk chunkID=... ECParts=4` présente | | |
| 4 | Vérifier absence d'erreurs proto=3 : `grep "erasure-coded" ghostdrive.log` | Aucune erreur `erasure-coded` (cette erreur n'existe plus) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scénario 5 — Vérification intégrité EC (XOR sur les 4 shards)

**Objectif** : Valider l'intégrité des données en comparant la reconstruction Go avec la validation XOR du POC Python.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Télécharger un fichier EC via GhostDrive | Fichier OK en local | | |
| 2 | Valider le même fichier via le POC Python : `python poc/ec4_reader.py --verify <fichier>` | XOR des 4 shards = CF0 (parité) ✓ | | |
| 3 | Comparer les checksums des deux téléchargements | SHA256 identiques | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Critères de Validation

- [ ] Tous les fichiers EC4+1 (< 64 MiB et > 64 MiB) se téléchargent sans erreur
- [ ] Les checksums SHA256 sont identiques à l'original MooseFS
- [ ] Les logs confirment que `readEC4At` est utilisé pour les fichiers EC
- [ ] Aucune régression sur les fichiers non-EC (proto=0/1/2)
- [ ] Le log `parseChunkInfo: proto=3 EC chunk ECParts=4` est présent
- [ ] Aucune erreur `erasure-coded` ou `EC not supported` dans les logs

---

## Tests Automatisés de Référence

Les tests unitaires couvrant le comportement EC4+1 :

```bash
# Exécuter uniquement les tests EC
go test ./plugins/moosefs/internal/mfsclient/... -run "TestEC|TestReadEC4|TestDivCeil" -v

# Suite complète MooseFS avec détection de races
go test ./plugins/moosefs/... -race -v -count=1
```

Résultat attendu : tous les tests `TestEC*` et `TestReadEC4*` PASS.

---

## Notes QA

_Espace pour observations et anomalies détectées_

- Date test : ________
- Environnement : ________
- Version GhostDrive : v1.8
- Résultat global : [ ] PASS  [ ] FAIL avec réserves  [ ] FAIL

