# Procédure de Test — Lecture EC4+1 MooseFS (issue #114)

**Version** : v1.8  
**Date** : 2026-05-16  
**Testeur** : QA  
**Scope** : Plugin MooseFS — lecture de chunks erasure-coded EC4+1

> **Mise à jour 2026-08-12** — ajout du Scénario 6 (bugfix #160 : keepalive
> `ANTOAN_NOP` traité à tort comme erreur fatale pendant une lecture CS lente,
> provoquant l'échec systématique de l'ouverture de gros fichiers et des
> relances d'`Open` en boucle côté Windows) et du Scénario 7 (bugfix #162 —
> plan révision 2 : un téléchargement interrompu laissait un fichier tronqué
> servi comme complet pendant jusqu'à une heure, perte de données silencieuse
> corrigée dans `internal/placeholder`). Voir
> `_work/reports/plan-20260812-102022.md` et
> `docs/diagrams/moosefs-ec4-read-statemachine.md`.
>
> **Mise à jour 2026-08-13** — bugfix #163 (copies de masse lentes, cache de
> localisation de chunk + granularité de lecture) : voir la procédure dédiée
> [`mass-copy-performance-163.md`](mass-copy-performance-163.md) (mesure de
> débit avant/après, obligatoire pour valider ce correctif).

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

### Scénario 6 — Bugfix #160 : ouverture rapide d'un gros fichier MP4 (régression NOP keepalive)

**Contexte** : l'issue #160 documente un `unexpected response cmd 0` provoquant l'échec
systématique de l'ouverture de gros fichiers EC4+1 (~1 min avant l'échec observé, puis Windows
relance l'`Open` en boucle sans jamais aboutir). La cause racine est un keepalive
`ANTOAN_NOP` légitime émis par le chunk server pendant une lecture lente, traité à tort comme
une erreur fatale (voir `docs/diagrams/moosefs-ec4-read-statemachine.md`). Ce scénario valide
le correctif et sert de non-régression pour toute évolution future du protocole de lecture CS.

**Rappel — règle d'accès MooseFS** : accès en **LECTURE SEULE ABSOLUE** au cluster MooseFS
pendant tout ce scénario — ne jamais créer, modifier ni supprimer de contenu sur le cluster
réel ; n'utiliser que des fichiers déjà existants (ex. `ec_large.bin` ou tout MP4/fichier
volumineux déjà présent sur le volume de test).

**Objectif** : vérifier qu'un gros fichier MP4 (ou tout fichier EC4+1 volumineux) s'ouvre et se
prévisualise sans erreur `unexpected response cmd`, en un temps très inférieur à la ligne de
base (~1 min) mesurée avant #160.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Vider/invalider le cache local GhostDrive pour le fichier de test si applicable | Placeholder non hydraté avant le test | | |
| 2 | Noter l'heure de référence puis ouvrir un gros fichier MP4 EC4+1 (> 100 Mio si disponible) depuis l'Explorateur Windows (double-clic ou aperçu) | La lecture démarre sans blocage visible | | |
| 3 | Mesurer le temps écoulé entre le déclenchement de l'`Open` et l'affichage de la preview / le premier octet lu | Temps < 10 s (ligne de base avant #160 : ~1 min) | | |
| 4 | Répéter les étapes 2-3 en ouvrant plusieurs fichiers volumineux d'un même dossier (dossier multi-fichiers) | Chaque fichier s'ouvre en un temps comparable, pas de dégradation cumulative | | |
| 5 | `grep "unexpected response cmd" ghostdrive.log` sur la fenêtre de temps du test | Aucune occurrence | | |
| 6 | Compter les `Open` du/des fichier(s) testé(s) dans les logs CF API | Un seul `Open` par fichier — pas de relance en boucle par Windows | | |
| 7 | Comparer le temps mesuré à l'étape 3 avec la ligne de base historique (~1 min, cf. issue #160) | Amélioration nette et reproductible | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scénario 7 — Bugfix #162 : intégrité du cache après échec de téléchargement (perte de données)

**Contexte** : le diagnostic de #160 (révision 2) a révélé un défaut d'intégrité distinct et
plus sévère (D7) : un téléchargement interrompu (keepalive mal géré, coupure réseau, arrêt de
GhostDrive) laissait un fichier **tronqué** dans le cache local, servi ensuite **comme s'il
était complet** pendant jusqu'à une heure (`cacheTTL`), alors que `Getattr` annonçait la taille
réelle distante. Le correctif (téléchargement vers fichier temporaire + renommage atomique,
validation du cache par comparaison de taille) rend cette troncature persistante structurellement
impossible. Voir `docs/diagrams/moosefs-ec4-read-statemachine.md` §2bis.

**Rappel — règle d'accès MooseFS** : accès en **LECTURE SEULE ABSOLUE** au cluster MooseFS —
ne provoquer l'échec que côté GhostDrive (coupure réseau locale, arrêt du process, kill de la
connexion), jamais en modifiant ou en arrêtant un service MooseFS.

**Objectif** : vérifier qu'après un téléchargement interrompu en cours de route, la
**prochaine** lecture du même fichier retourne des données **complètes et intègres** — jamais
un fichier tronqué servi silencieusement comme complet.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Choisir un fichier EC4+1 volumineux (> 64 MiB, plusieurs chunks) non encore présent dans le cache local | Cache absent pour ce fichier | | |
| 2 | Déclencher l'ouverture du fichier puis, pendant le téléchargement (avant la fin), couper la connexion réseau locale (désactiver l'interface Wi-Fi/Ethernet quelques secondes) ou tuer le process GhostDrive | Le téléchargement échoue en cours de route | | |
| 3 | Vérifier dans le dossier de cache temporaire (`%TEMP%\ghostdrive\...`) qu'**aucun** fichier tronqué n'est présent au chemin de cache final (seul un `.ghostdrive.tmp` éventuel, ou rien) | Pas de fichier partiel exploitable au chemin final | | |
| 4 | Rétablir la connexion réseau / relancer GhostDrive si nécessaire, puis rouvrir le même fichier | Un nouveau téléchargement complet démarre (pas de faux "cache frais") | | |
| 5 | Comparer la taille du fichier servi à la taille distante (`mfsfileinfo` ou propriétés Explorateur) | Tailles identiques | | |
| 6 | Comparer la somme de contrôle (`sha256sum` / `Get-FileHash`) du fichier servi avec l'original MooseFS | Checksums identiques — aucune troncature | | |
| 7 | Ouvrir simultanément (2 fenêtres/onglets) le même gros fichier pendant qu'aucun cache n'existe encore | Un seul téléchargement observé dans les logs (`ensureDownloaded`/`Download` appelé une seule fois), les deux ouvertures aboutissent | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Critères de Validation

- [ ] Tous les fichiers EC4+1 (< 64 MiB et > 64 MiB) se téléchargent sans erreur
- [ ] Les checksums SHA256 sont identiques à l'original MooseFS
- [ ] Les logs confirment que `readEC4At` est utilisé pour les fichiers EC
- [ ] Aucune régression sur les fichiers non-EC (proto=0/1/2)
- [ ] Le log `parseChunkInfo: proto=3 EC chunk ECParts=4` est présent
- [ ] Aucune erreur `erasure-coded` ou `EC not supported` dans les logs
- [ ] **#160** : aucune erreur `unexpected response cmd` lors de l'ouverture de gros fichiers EC4+1
- [ ] **#160** : le temps d'ouverture d'un gros fichier EC4+1 est très inférieur à la ligne de base ~1 min
- [ ] **#160** : un seul `Open` par fichier — pas de relance en boucle côté Windows CF API
- [ ] **#162** : après un échec de téléchargement provoqué, aucun fichier tronqué n'est jamais servi comme complet
- [ ] **#162** : la relecture après échec retourne un fichier de taille et de somme de contrôle identiques à l'original
- [ ] **#162** : deux ouvertures concurrentes du même fichier ne déclenchent qu'un seul téléchargement

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

### Tests bugfix #160 (protocole — keepalive NOP en lecture CS / retry / pool)

```bash
go test ./plugins/moosefs/internal/mfsclient/... -race -v -run \
  "TestReadChunk_NOPskip|TestReadChunk_NOPBadLength|TestReadChunk_NOPFlood|TestReadChunk_UnknownCmd|TestReadChunk_InactivityTimeout|TestReadEC4Cmd0|TestRetry_|TestCSPool_MaxIdleAge"
```

Note : `TestReadChunk_InactivityTimeout` exécute volontairement un sous-test de ~20-25 s
(lecture lente mais progressive qui doit survivre à `csReadChunkDeadline`) — ne pas s'inquiéter
d'un temps d'exécution total de la suite `mfsclient` de l'ordre de la minute.

Résultat attendu : tous PASS, y compris `TestWriteChunk_*` et `TestCSPool_*` existants
(non-régression du chemin d'écriture — le correctif #160 ne touche que la boucle de lecture).

### Tests bugfix #162 (intégrité du cache local — `internal/placeholder`)

```bash
# Windows uniquement (build tag) — vérification de compilation croisée possible depuis un autre OS :
#   GOOS=windows go vet ./internal/placeholder/...
go test ./internal/placeholder/... -race -v -run \
  "TestIsCacheFresh_SizeMismatch_ReturnsFalse|TestEnsureDownloaded_PartialRemovedOnError|TestEnsureDownloaded_RedownloadsTruncatedCache|TestEnsureDownloaded_ConcurrentOpensSingleDownload"

# Non-régression backend-agnostique (Phase 3 touche ensureDownloaded, commun à tous les backends) :
go test ./plugins/webdav/... ./plugins/local/... -race -v
```

Résultat attendu : tous PASS. `TestEnsureDownloaded_RedownloadsTruncatedCache` est le test de
non-régression de la perte de données (D7) — un fichier tronqué pré-existant dans le cache ne
doit jamais être servi tel quel.

---

## Notes QA

_Espace pour observations et anomalies détectées_

- Date test : ________
- Environnement : ________
- Version GhostDrive : v1.8
- Résultat global : [ ] PASS  [ ] FAIL avec réserves  [ ] FAIL

