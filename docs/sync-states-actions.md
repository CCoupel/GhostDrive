# Matrice États × Actions — GhostDrive

## États des objets

| ID | État | Description | Badge actuel | Badge v2.2 |
|----|------|-------------|-------------|------------|
| **L** | Local only | Présent uniquement en local, pas encore uploadé | — | ↑ (upload pending) |
| **R** | Remote only | Placeholder CF API, non hydraté | valise | ☁️ |
| **S** | In-sync | Présent des deux côtés, identique | — | ✓✓ |
| **U** | Uploading | Upload en cours | — | ⟳ |
| **D** | Downloading | Hydratation en cours | — | ⟳ |
| **P** | Pending upload | Modifié localement, pas encore uploadé | — | ↑ |
| **C** | Conflict | Modifié des deux côtés différemment | — | ⚠️ |
| **E** | Error | Échec sync (upload/download/réseau) | — | ⚠️ |
| **X** | Pinned | Toujours conservé en local | — | ⚡ |

## Matrice actions × états

| Action | L — Local only | R — Remote only | S — In-sync | U — Uploading | D — Downloading | P — Pending | C — Conflict | E — Error | X — Pinned |
|--------|---------------|-----------------|-------------|---------------|-----------------|-------------|-------------|-----------|-----------|
| **Ouvrir** | Ouvre normalement | Déclenche hydratation → ouvre | Ouvre normalement | Ouvre (données complètes côté local) | Attend fin hydratation (timeout → erreur OS) | Ouvre normalement | Ouvre version locale + avertissement ⚠️ | Ouvre normalement | Ouvre normalement |
| **Supprimer** | Supprime local uniquement | Supprime placeholder + supprime remote | Supprime local + remote | Annule upload + supprime | Annule hydratation + supprime placeholder | Supprime local + annule upload | Propose : supprimer local / remote / les deux | Supprime local + annule retry | Supprime local + remote |
| **Renommer** | Renomme local → marqué pending upload | Renomme placeholder + rename remote | Renomme local + remote (sync) | File en attente → renomme après upload | Renomme après hydratation | Renomme local (reste pending) | Renomme version locale ⚠️ | Renomme local + reprogramme retry | Renomme local + remote |
| **Déplacer** | Déplace local → pending | Déplace placeholder + move remote | Déplace local + remote | Attendre fin upload → déplacer | Attendre fin download → déplacer | Déplace + reste pending | ⚠️ demander confirmation | Déplace + retry au nouvel emplacement | Déplace local + remote |
| **Copier** | Copie locale → nouvelle entrée L | Hydrate + copie → nouvelle entrée L | Copie locale → nouvelle entrée P (à uploader) | Attendre + copie | Attendre + copie | Copie locale → nouvelle entrée P | Copie version locale → nouvelle entrée L | Copie locale | Copie locale → nouvelle entrée P |
| **Créer ici** | Crée local → état P | Dossier : crée local → état P | Crée local → état P | Crée local → état P (enfile après) | Crée local → état P (enfile après) | Crée local → état P | N/A | Crée local → état P | Crée local → reste X (pinné) |
| **Épingler (pin)** | — (déjà local, marquer X) | Hydrate immédiatement + marquer X | Marquer X (conservé même si supprimé remote) | Confirme après upload + X | Force hydratation complète + X | Upload d'abord + X | Épingle version locale + X | Retry + X | — (déjà épinglé) |
| **Désépingler (unpin)** | Upload d'abord → transforme en placeholder R | — (déjà placeholder) | Transforme en placeholder R (libère espace disque) | Après upload → placeholder | Annule → reste placeholder R | Upload → placeholder | N/A | N/A | Désépingle → S si in-sync, P si pending |
| **Forcer sync** | Upload maintenant | Télécharge maintenant (hydrate) | Vérifie intégrité hash | — (déjà en cours) | — (déjà en cours) | Upload maintenant | Propose : garder local / garder remote / merger | Retry immédiat | Re-vérifie remote + re-sync |
| **Résoudre conflit** | N/A | N/A | N/A | N/A | N/A | N/A | Garde local / Garde remote / Merge manuel | N/A | N/A |

## Notes

- **Conflit** : stratégie recommandée — rename automatique `file.conflict-YYYYMMDD.ext` + conserver les deux versions
- **Unpin** (L → placeholder) : upload préalable confirmé obligatoire avant suppression des données locales
- **Downloading + open** : timeout géré nativement par Windows CF API

---

## Référence

- **États UI** : voir `contracts/sync-icons.md` pour les badges et icônes
- **Architecture** : voir `README.md` section Architecture
- **Version actuelle** : v2.1.0+ avec Cloud Filter API Files On-Demand
