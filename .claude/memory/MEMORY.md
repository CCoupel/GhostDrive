# MEMORY.md — Memoire Projet

> **Usage** : Ce fichier est la source de verite pour demarrer une nouvelle session.
> Mis a jour par `/end-session` et par le CDP apres chaque changement significatif.
> Ne pas versionner d'informations ephemeres (taches en cours, logs) — uniquement
> ce qui est utile pour la PROCHAINE session.

---

## Projet

| Parametre | Valeur |
|-----------|--------|
| Nom | `{PROJECT_NAME}` |
| Repository | `{REPO_URL}` |
| Version actuelle | `{CURRENT_VERSION}` |
| Derniere mise a jour | `{LAST_UPDATED}` |

---

## Versions

| Environnement | Version | Date deploy |
|---------------|---------|-------------|
| Production | `{PROD_VERSION}` | `{PROD_DATE}` |
| Staging / Qualif | `{STAGING_VERSION}` | `{STAGING_DATE}` |
| En developpement | `{DEV_VERSION}` | — |

**Schema versioning** : `X.Y.Z`
- X : Breaking change (migration requise)
- Y : Nouvelle feature (incremente par PLAN)
- Z : Bugfix / patch (incremente par DEV)

---

## Travail en Cours

### Phase Actuelle

**Phase** : `{CURRENT_PHASE}` _(ex: DEV, REVIEW, QA, DEPLOY)_
**Branche** : `{CURRENT_BRANCH}`
**Description** : `{CURRENT_TASK}`

### Issues GitHub Actives

| # | Titre | Labels | Priorite |
|---|-------|--------|----------|
| — | — | — | — |

### Prochaines Etapes

1. `{NEXT_STEP_1}`
2. `{NEXT_STEP_2}`

---

## Architecture

> Completer selon le projet. Supprimer les sections non applicables.

### Stack Technique

| Composant | Technologie | Version |
|-----------|-------------|---------|
| Backend | `{BACKEND}` | `{VERSION}` |
| Frontend | `{FRONTEND}` | `{VERSION}` |
| Base de donnees | `{DATABASE}` | `{VERSION}` |
| Infrastructure | `{INFRA}` | — |

### Environnements

| Env | URL / Acces | Notes |
|-----|-------------|-------|
| Dev local | `localhost:{PORT}` | — |
| Staging | `{STAGING_URL}` | — |
| Production | `{PROD_URL}` | — |

### Fichiers Cles

| Fichier | Role |
|---------|------|
| `{VERSION_FILE}` | Source de verite pour la version |
| `CHANGELOG.md` | Historique des versions |
| `CLAUDE.md` | Architecture et regles du projet |

---

## Decisions Techniques

> Decisions importantes prises — utile pour ne pas revenir dessus.

| Decision | Raison | Date |
|----------|--------|------|
| `{DECISION_1}` | `{REASON_1}` | `{DATE_1}` |

---

## Regles Critiques du Projet

> Regles non-negociables specifiques a CE projet. Completer lors de l'init.

- `{RULE_1}` _(ex: Toujours builder le frontend avant le backend Go)_
- `{RULE_2}` _(ex: Tests obligatoires pour tout nouveau endpoint)_
- `{RULE_3}` _(ex: Jamais de secrets dans le code)_

---

## Points d'Attention

> Elements a surveiller ou risques identifies.

- `{ATTENTION_1}` _(ex: Migration DB planifiee pour v3.0.0)_
- `{ATTENTION_2}`

---

## Corrections de Comportement Claude

> Retours utilisateur sur la facon de travailler — ne pas reproduire les erreurs listees.

| Comportement a eviter | Comportement attendu |
|-----------------------|----------------------|
| `{AVOID_1}` | `{EXPECTED_1}` |

---

## Comment Utiliser ce Fichier

### Au demarrage d'une session

1. Lire ce fichier en entier
2. Verifier la version courante dans `{VERSION_FILE}`
3. Verifier l'etat git (`git status`, `git log --oneline -5`)
4. Reprendre depuis "Travail en Cours"

### En fin de session (`/end-session`)

1. Mettre a jour "Version actuelle" si elle a change
2. Mettre a jour "Travail en Cours"
3. Ajouter les nouvelles decisions techniques
4. Mettre a jour les "Issues GitHub Actives"
5. Commiter : `chore(memory): Update session state`
