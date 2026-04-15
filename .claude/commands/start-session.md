# Commande /start-session

Demarrage de session : lecture de la memoire projet et creation de la team de travail.

## Argument recu

$ARGUMENTS

## Instructions

### Etape 1 — Lecture de la memoire projet

Lire `.claude/memory/MEMORY.md` (source de verite unique).

Extraire :
- Version courante et environnements
- Travail en cours (branche, phase, issues actives)
- Regles critiques du projet
- Corrections de comportement a appliquer

### Etape 2 — Creation de la TEAM

**Sans demander confirmation**, creer immediatement la team :

1. **TeamCreate** avec le nom `{TEAM_NAME}` (defini dans CLAUDE.md)

2. **Spawner TOUS les agents en parallele** (un seul message avec N appels Task) :

| Nom agent | Type (subagent_type) | Role | Modele |
|-----------|---------------------|------|--------|
| `cdp` | `cdp` | Chef de Projet — Team Leader | sonnet |
| `planner` | `implementation-planner` | Planification + contrats API | sonnet |
| `dev-backend` | `dev-backend` | Backend (stack detectee) | sonnet |
| `dev-frontend` | `dev-frontend` | Frontend (stack detectee) | sonnet |
| `code-reviewer` | `code-reviewer` | Revue de code | sonnet |
| `qa` | `qa` | Tests et validation | sonnet |
| `security` | `security` | Audit securite | sonnet |
| `doc-updater` | `doc-updater` | Documentation | sonnet |
| `deployer` | `deploy` | Deploiement QUALIF/PROD | sonnet |
| `infra` | `infra` | Infrastructure Docker/Helm/CI | sonnet |
| `marketing` | `marketing-release` | Communication de release | sonnet |

> **Adapter la liste** selon la stack du projet (definie dans `project-config.json`) :
> - Pas de frontend → ne pas spawner `dev-frontend`
> - Firmware → ajouter `dev-firmware` (subagent_type: `dev-firmware`)
> - Pas de K8s/Docker → `infra` optionnel

**Prompt pour `cdp`** (team leader) :

```
Lis .claude/agents/cdp.template.md et applique ces instructions pour toute la session.

Tu es le Chef de Projet de {PROJECT_NAME} dans la team {TEAM_NAME}.
Memoire projet : .claude/memory/MEMORY.md

Attends les instructions de l'utilisateur avant de demarrer un workflow.
```

**Prompt pour tous les autres agents** :

```
Lis .claude/agents/context/TEAMMATES_PROTOCOL.md puis .claude/agents/[nom].template.md,
et applique ces instructions pour toute la session.

Tu fais partie de la team {TEAM_NAME} sur le projet {PROJECT_NAME}.
Reste en mode IDLE et attends les ordres du CDP avant de commencer tout travail.
```

### Etape 3 — Confirmation a l'utilisateur

```markdown
## Session demarree — {PROJECT_NAME}

**Team** : {TEAM_NAME}
**Version** : [lue depuis MEMORY.md]
**Branche** : [lue depuis MEMORY.md]

**Agents actifs (IDLE)** :
- cdp (Chef de Projet — interlocuteur principal)
- planner / dev-backend / dev-frontend
- code-reviewer / qa / security
- doc-updater / deployer / infra / marketing

**Travail en cours** : [lu depuis MEMORY.md]

**Commandes disponibles** :
- `/feature <description>` — Nouvelle feature complete
- `/bugfix <description>` — Correction de bug
- `/backlog [description]` — Consulter ou traiter les GitHub Issues
- `/review [scope] [mode]` — Revue de code
- `/secu [scope]` — Audit securite
- `/deploy qualif|prod` — Deployer
- `/end-session` — Cloturer la session
```

## Regles

- La MEMORY projet est la **seule source de verite** au demarrage
- La TEAM est **toujours creee** sans demander confirmation
- Le nom de la TEAM est **toujours** `{TEAM_NAME}` (defini dans CLAUDE.md)
- Le CDP est **toujours le premier agent spawne** (team leader)
- Tous les autres agents demarrent en **mode IDLE strict** — ils attendent
  un ordre explicite du CDP via SendMessage, sans verifier la TaskList
