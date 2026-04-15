# Commande /end-session

Cloturer proprement la session de travail : archivage, mise a jour de la memoire,
verification de l'etat du projet.

## Usage

```
/end-session
```

## Quand l'utiliser

- En fin de session de travail
- Avant une longue interruption
- Apres une release majeure
- Quand on change de contexte de travail

## Workflow

```
/end-session
    |
    v
[ETAT] --> Verifier l'etat du projet
    |
    v
[DOC] --> Mettre a jour la documentation
    |
    v
[MEMOIRE] --> Mettre a jour .claude/memory/MEMORY.md
    |
    v
[GIT] --> S'assurer que tout est commite/pousse
    |
    v
[RAPPORT] --> Rapport de session
```

## Etapes Detaillees

### 1. ETAT — Verification du Projet

```bash
# Etat git
git status
git log --oneline -5

# Tests (si applicable)
{TEST_CMD}

# Build (si applicable)
{BUILD_CMD}
```

Verifier :
- Pas de changements non commites non intentionnels
- Tests passent
- Build OK

### 2. DOC — Documentation

Mettre a jour si necessaire :
- `CHANGELOG.md` : ajouter les changements de la session si pas encore fait
- `docs/` : mettre a jour les docs techniques impactees
- `README.md` : si des fonctionnalites majeures ont change

### 3. MEMOIRE — Mise a jour de `.claude/memory/MEMORY.md`

Mettre a jour les sections pertinentes :
- Version courante
- Travail en cours (phase actuelle)
- Decisions techniques prises pendant la session
- Issues GitHub ouvertes / fermees
- Points de contexte importants pour la prochaine session

### 4. GIT — Verification Git

```bash
# S'assurer que tout est pousse
git status
git push origin <branche-courante>

# Verifier qu'il n'y a pas de travail en attente
git stash list
```

Si des changements non commites existent :
- Demander confirmation : commiter, stasher, ou laisser ?

### 5. RAPPORT — Rapport de Session

```markdown
## Rapport de Session — <date>

**Duree** : [estimation]
**Branche** : <branche>
**Version** : <version courante>

### Ce qui a ete fait
- <tache 1> ✅
- <tache 2> ✅
- <tache 3> 🔄 (en cours)

### Decisions techniques
- <decision 1> — <raison>
- <decision 2> — <raison>

### Pour la prochaine session
- <point d'attention 1>
- <prochaine etape>

### Etat du projet
- Tests : [PASS|FAIL|N/A]
- Build : [OK|KO|N/A]
- Deploy : [PROD vX.Y.Z|staging|non deploye]
```

## Exemple

```
/end-session

[ETAT] Git : 0 modifications non commitees ✅
[ETAT] Tests : 47/47 passes ✅
[DOC] CHANGELOG.md : a jour ✅
[MEMOIRE] MEMORY.md mis a jour ✅
[GIT] Tout est pousse sur origin/feature/auth ✅

--- Rapport de Session ---
Branche : feature/auth
Travail : Implementation OAuth2 (phases PLAN + DEV completees)
Prochaine etape : /review puis /qa
```

## Agent

Execution directe — pas de delegation a un agent specialise.
Coordonne les mises a jour de documentation et de memoire.
