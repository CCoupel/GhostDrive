# Regles Communes a Tous les Agents

> **Ce fichier contient les regles obligatoires pour TOUS les agents {PROJECT_NAME}.**
> Chaque agent doit referencer ce fichier : `@import COMMON.md`
>
> **Contexte projet** : Voir `context/PROJECT_CONTEXT.md` pour le stack technique, la structure et les commandes.

---

## Gestion de la Todo List (OBLIGATOIRE)

Vous DEVEZ utiliser le tool `TodoWrite` pour suivre votre progression de maniere visible.

### Au Demarrage

1. **Creer une todo list** avec toutes les etapes de votre travail
2. Chaque tache doit avoir :
   - `content` : Description de la tache (forme imperative)
   - `status` : `pending` (ou `in_progress` pour la premiere)
   - `activeForm` : Description en forme progressive (anglais)

### Structure d'une Todo List

```json
[
  {"content": "Description en francais", "status": "in_progress", "activeForm": "English description"},
  {"content": "Deuxieme tache", "status": "pending", "activeForm": "Second task"},
  ...
]
```

### Affichage Attendu

Pendant l'execution, afficher visuellement la progression :

```
Tache [N]/[Total] : [Description]
   -- [Detail de ce qui est en cours...]

Tache [N]/[Total] terminee
   -- [Resume du resultat]
```

### Regles Strictes

| Regle | Description |
|-------|-------------|
| **Une seule tache `in_progress`** | Jamais plus d'une tache en cours a la fois |
| **Mise a jour immediate** | Mettre a jour la todo list apres CHAQUE changement |
| **Affichage visuel** | Toujours afficher la progression a l'utilisateur |
| **Jamais continuer sans MAJ** | Ne jamais passer a la tache suivante sans avoir mis a jour le statut |

---

## Affichage du Plan de Taches (OBLIGATOIRE)

### Au Demarrage

Afficher le plan COMPLET avec les icones de statut :

```
## Plan de Taches

1. [pending] Premiere tache
2. [pending] Deuxieme tache
3. [pending] Troisieme tache
```

### A Chaque Changement de Statut

Re-afficher le plan complet avec la progression :

```
## Plan de Taches [2/5]

1. [completed] Premiere tache
2. [in_progress] Deuxieme tache
3. [pending] Troisieme tache
4. [pending] Quatrieme tache
5. [pending] Cinquieme tache
```

### Icones de Statut

| Icone | Statut | Signification |
|-------|--------|---------------|
| `[pending]` | pending | En attente |
| `[in_progress]` | in_progress | En cours |
| `[completed]` | completed | Termine |
| `[failed]` | failed | Echoue |

---

## Notifications de Progression (OBLIGATOIRE)

### Au Demarrage de la Tache

Vous DEVEZ afficher immediatement un message de demarrage avec ce format :

```
**[NOM-AGENT] DEMARRE**
---------------------------------------
[Champ principal] : [Valeur]
[Champ secondaire] : [Valeur]
[Champ tertiaire] : [Valeur]
---------------------------------------
```

### A la Fin de la Tache

**En cas de succes :**
```
**[NOM-AGENT] TERMINE**
---------------------------------------
[Champ principal] : [Valeur]
[Resultat 1] : [Valeur]
[Resultat 2] : [Valeur]
[Statut final] : [OK / Pret pour X]
---------------------------------------
```

**En cas d'erreur :**
```
**[NOM-AGENT] ERREUR**
---------------------------------------
[Champ principal] : [Valeur]
Probleme : [Description]
Action requise : [Solution proposee]
---------------------------------------
```

### Notifications Intermediaires (OBLIGATOIRE pour workflows longs)

Pour les workflows longs, notifier entre chaque phase majeure avec le plan mis a jour :
```
**[NOM-AGENT] - Phase [N]/[Total] : [Nom de la phase]**
   -- [Ce qui va etre fait]

## Plan de Taches [N/Total]
[Plan complet avec statuts mis a jour]
```

---

## Regles de Communication

### Langue

- **Francais** : Prefere pour les messages utilisateur et la documentation
- **Anglais** : Pour le code, les commits, et les champs techniques (`activeForm`)

### Format des Messages

- Utiliser les box drawing characters pour les bordures
- Garder les messages concis mais informatifs

### Emojis Standards

| Emoji | Signification |
|-------|---------------|
| Demarrage | Lancement d'une tache |
| Succes | Tache terminee |
| Erreur | Tache echouee |
| Avertissement | Reserves ou attention |
| En cours | Tache en progression |
| Information | Detail principal |
| Documentation | Fichiers modifies |
| Version | Package ou release |
| Statistiques | Metriques |
| Objectif | Cible atteinte |
| Fix | Action corrective requise |
| Branche | Operations Git |
| Push | Export ou envoi |
| Tests | Execution de tests |
| Sauvegarde | Persistance |

---

## Gestion des Erreurs

### Comportement Attendu

1. **Documenter** l'erreur dans le rapport/summary
2. **Proposer** une solution si possible
3. **Signaler** au CDP/orchestrateur pour decision
4. **Ne jamais rester bloque en silence**

### Format de Signalement

```markdown
## Probleme Rencontre

**Type** : [Build / Test / Git / Autre]
**Description** : [Detail du probleme]
**Impact** : [Critique / Important / Mineur]
**Solution proposee** : [Si applicable]
```

---

## Coordination Inter-Agents

### Workflow Standard

```
PLAN -> [validation] -> DEV -> TEST-WRITER -> REVIEW -> QA -> [validation] -> DOC -> DEPLOY -> [validation]
```

**[validation] = Points de validation utilisateur obligatoires**

### Transmission de Contexte

Chaque agent doit :
1. Lire le **resume de l'agent precedent**
2. Produire un **resume structure** pour l'agent suivant
3. Documenter les **decisions prises** et **problemes rencontres**

### Points de Validation

| Agent | Validation produite | Destinataire |
|-------|---------------------|--------------|
| PLAN | Plan d'implementation | **Utilisateur** |
| DEV | Summary + commits | TEST-WRITER, REVIEW |
| TEST-WRITER | Fichiers de tests | REVIEW, QA |
| REVIEW | Review Report (APPROVED/REJECTED) | QA ou DEV |
| QA | QA Report (VALIDATED/NOT VALIDATED) | **Utilisateur** |
| DOC | Documentation finalisee | DEPLOY |
| DEPLOY | Deployment Report | **Utilisateur** |

---

## References Projet

> **Details complets** : Voir `context/PROJECT_CONTEXT.md`

### Fichiers Essentiels

| Fichier | Description |
|---------|-------------|
| `CLAUDE.md` | Architecture complete |
| `CHANGELOG.md` | Historique des versions |
| `{VERSION_FILE}` | Version actuelle |
| `contracts/*.md` | Contrats API |
