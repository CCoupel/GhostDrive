# Procédure de Test — Performance copies de masse MooseFS (issue #163)

**Version** : v2.2.2 (dev)
**Date** : 2026-08-13
**Testeur** : QA
**Scope** : Plugin MooseFS — débit de téléchargement fichier unique + copie de masse

---

## Contexte

L'issue #163 documente des copies de masse lentes (~8 Mo/s). Le diagnostic
(`_work/reports/plan-20260812-155238.md`) montre un **plafond de latence
intra-fichier**, pas un manque de pipelining inter-fichiers comme le titre de
l'issue le suggérait : chaque bloc de lecture (64 KiB) coûte deux
aller-retours réseau (un `locateChunk` vers le master + un `ReadChunk` vers le
chunk server), soit ~4 ms/aller-retour × des milliers d'aller-retours par
fichier.

Le correctif (B1 + B2) :
- **B1** — cache de localisation de chunk (`plugins/moosefs/internal/mfsclient/chunklocationcache.go`) :
  un seul `locateChunk` par chunk MooseFS (64 Mio) au lieu d'un par bloc de 64 KiB.
- **B2** — granularité de lecture relevée (`readEC4At` accepte désormais des
  lectures jusqu'à la taille d'un shard, avec découpe aux frontières de shard) :
  beaucoup moins d'aller-retours `ReadChunk` par fichier.

**Cette procédure est le test qui valide ou invalide l'ensemble du correctif**
(CA7, CA8) — un tableau avant/après chiffré est **obligatoire**, pas une
impression qualitative.

**Rappel — règle d'accès MooseFS** : accès en **LECTURE SEULE ABSOLUE** au
cluster MooseFS. Ne créer, modifier ni supprimer aucun contenu sur le cluster
réel pendant cette procédure ; n'utiliser que des fichiers déjà existants.

---

## Prérequis

- [ ] **Environnement** : QUALIF (serveur MooseFS Pro 4.x, cluster de test)
- [ ] **Données** : un fichier de référence volumineux (≥ 100 Mio, idéal : un
      des fichiers déjà cités dans l'issue #163, ex. `P2 S14 T2.MP4` /
      130 037 031 o) et un dossier contenant plusieurs dizaines de fichiers de
      taille comparable (copie de masse réaliste)
- [ ] **Build** : binaire GhostDrive avec le plugin MooseFS incluant B1+B2
      (`bugfix/mass-copy-pipelining`)
- [ ] **Outils de mesure** : chronomètre / horloge système précise à la
      seconde ; `Get-FileHash`/`sha256sum` pour vérifier l'intégrité après
      transfert (non-régression #160/#162 — CA9-CA11)

---

## Méthode

1. Vider (ou invalider) le cache local GhostDrive pour les fichiers de test
   avant **chaque** mesure — un cache déjà chaud fausserait la comparaison.
2. Répéter chaque mesure **3 fois** et retenir la médiane (élimine les
   variations ponctuelles réseau/disque).
3. Consigner les valeurs BRUTES des 3 essais, pas seulement la médiane, dans
   les notes QA.

---

## Scénario 1 — Débit d'un fichier unique

**Objectif** : mesurer la durée et le débit de téléchargement d'un fichier de
référence volumineux, avant et après B1+B2.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Vider le cache local pour le fichier de référence | Placeholder non hydraté | | |
| 2 | Ouvrir le fichier depuis l'Explorateur Windows (double-clic ou copie vers un dossier local), chronométrer du déclenchement à la fin du transfert | Durée mesurée | | |
| 3 | Calculer le débit : taille du fichier (o) ÷ durée (s) | Débit en Mo/s | | |
| 4 | Répéter les étapes 1-3 deux fois de plus (3 essais au total) | 3 mesures cohérentes (hors aléas ponctuels) | | |
| 5 | Vérifier l'intégrité : comparer la somme de contrôle du fichier téléchargé à l'original | Checksums identiques | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Scénario 2 — Copie de masse d'un dossier complet

**Objectif** : mesurer le temps total de copie d'un dossier complet
(plusieurs dizaines de fichiers volumineux) depuis l'Explorateur Windows,
avant et après B1+B2.

| Étape | Action | Résultat Attendu | Résultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Vider le cache local pour tous les fichiers du dossier de test | Aucun placeholder hydraté | | |
| 2 | Lancer la copie du dossier complet vers un emplacement local, chronométrer du lancement à la fin de la barre de progression Explorer | Durée totale mesurée | | |
| 3 | Vérifier l'absence d'erreur dans les logs GhostDrive pendant la copie (`grep -i error ghostdrive.log`) | Aucune erreur | | |
| 4 | Vérifier l'intégrité d'un échantillon de fichiers copiés (au moins 3, taille + somme de contrôle) | Tailles et checksums identiques aux originaux | | |
| 5 | Répéter les étapes 1-4 deux fois de plus (3 essais au total) | 3 mesures cohérentes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Tableau avant/après (OBLIGATOIRE)

À remplir avec les médianes des 3 essais de chaque scénario. Les colonnes
« avant » proviennent soit d'une mesure sur le binaire pré-#163 (base
`feature/v2.2-workflow-objets` avant le merge de `bugfix/mass-copy-pipelining`),
soit — à défaut de rejouer un ancien binaire — des logs de production cités
dans l'issue #163 (voir le tableau arithmétique du plan, à reporter tel quel
si aucune remesure « avant » n'est possible).

| Mesure | Avant (#163) | Après (B1+B2) | Facteur d'amélioration |
|---|---|---|---|
| Fichier unique — durée (s) | | | |
| Fichier unique — débit (Mo/s) | | | |
| Copie de masse — durée totale (s) | | | |
| `locateChunk` appels / fichier (voir `TestDownload_NetworkRoundtripBaseline`, `go test -run TestDownload_NetworkRoundtripBaseline -v ./plugins/moosefs/`) | | | |
| `ReadChunk` appels / fichier | | | |

**Critère de succès (CA7, CA8)** : débit fichier unique et temps de copie de
masse « très supérieurs » à la ligne de base — un facteur d'amélioration net
et reproductible sur les 3 essais, pas un gain marginal dans le bruit de
mesure.

---

## Non-régression à vérifier pendant cette procédure

- [ ] **CA9 (#160)** : aucune erreur `unexpected response cmd` dans les logs
      pendant les scénarios 1 et 2
- [ ] **CA10 (#162)** : ouvrir deux fois le même fichier volumineux
      simultanément (2 fenêtres Explorer) → un seul téléchargement dans les
      logs (`grep -c "ensureDownloaded\|Download" ghostdrive.log` pour ce
      fichier)
- [ ] **CA11 (#160/#162)** : après le scénario 2, vérifier qu'aucun fichier
      copié n'a une taille locale différente de sa taille distante (pas de
      troncature réintroduite par la nouvelle granularité de lecture)

---

## Tests Automatisés de Référence

```bash
# Cache de localisation de chunk (B1)
go test ./plugins/moosefs/internal/mfsclient/... -race -v -run "TestChunkLocationCache_"

# Granularité de lecture (B2)
go test ./plugins/moosefs/internal/mfsclient/... -race -v -run "TestReadEC4At_MultiMiBRead|TestReadEC4At_ShardBoundarySplit|TestReadChunk_MultiFrameCRC"

# Non-régression #160/#162 à la nouvelle granularité
go test ./plugins/moosefs/internal/mfsclient/... -race -v -run "TestReadChunk_NOPSkip_LargeMultiFrame"
go test ./plugins/moosefs/... -race -v -run "TestDownload_LastBlockTruncation"

# Baseline chiffrée (#163 task 2) — à rejouer après B1+B2 pour le tableau
# avant/après ci-dessus ; voir le commentaire du test pour la méthodologie.
go test ./plugins/moosefs/... -v -run "TestDownload_NetworkRoundtripBaseline"

# Non-régression complète du plugin
go test ./plugins/moosefs/... -race -count=1
```

Résultat attendu : tous PASS. `TestDownload_NetworkRoundtripBaseline` documente un
comptage d'aller-retours (pas un simple pass/fail) — voir sa sortie `t.Logf`
pour les chiffres à reporter dans le tableau avant/après.

---

## Notes QA

_Espace pour observations, essais bruts (3 valeurs par mesure), anomalies._

- Date test : ________
- Environnement : ________
- Version GhostDrive : ________
- Fichier de référence utilisé (nom, taille) : ________
- Résultat global : [ ] PASS  [ ] FAIL avec réserves  [ ] FAIL
