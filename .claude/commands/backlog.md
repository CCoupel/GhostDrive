# Commande /backlog

Consulter et traiter les issues du backlog du repo Git.

## Usage

```
/backlog                        # Lister toutes les issues ouvertes
/backlog <description>          # Rechercher les issues correspondantes et lancer le workflow
```

## Argument recu

$ARGUMENTS

---

## Mode 1 : Liste (sans argument)

Si `$ARGUMENTS` est vide -> afficher les issues ouvertes.

### Etapes

1. Executer `gh issue list --state open --limit 50`
2. Afficher sous forme de tableau :

```markdown
## Backlog - Issues ouvertes

| # | Titre | Labels | Assignee | Maj |
|---|-------|--------|----------|-----|
| 42 | Ajouter auth OAuth2 | feature | - | 2025-01-10 |
| 38 | Crash au demarrage iOS | bug | @user | 2025-01-08 |
| 35 | Refactor module auth | refactor | - | 2025-01-05 |
```

3. Proposer les actions disponibles :

```
Pour traiter une issue :
  /backlog <titre ou #numero>    Rechercher et lancer le workflow
  /backlog #42                   Traiter directement l'issue #42
```

### Filtres disponibles

L'utilisateur peut affiner avec des mots-cles :

| Syntaxe | Effet |
|---------|-------|
| `/backlog` | Toutes les issues ouvertes |
| `/backlog label:bug` | Issues avec le label "bug" |
| `/backlog label:feature` | Issues avec le label "feature" |
| `/backlog @me` | Issues qui me sont assignees |
| `/backlog milestone:<nom>` | Issues d'un milestone |

**Implementation des filtres :**
- Detecter si `$ARGUMENTS` correspond a un filtre (`label:`, `@me`, `milestone:`)
- Traduire en flags `gh issue list` correspondants (`--label`, `--assignee @me`, `--milestone`)
- Sinon -> Mode recherche (voir Mode 2)

---

## Mode 2 : Recherche et Workflow (avec description)

Si `$ARGUMENTS` est non vide et n'est pas un filtre -> mode recherche.

### Etapes

#### 1. Recherche

```bash
gh issue list --state open --search "<$ARGUMENTS>" --limit 10
```

Afficher les resultats :

```markdown
## Issues correspondantes a "<description>"

1. #42 - Ajouter l'authentification OAuth2  [feature]
2. #38 - Auth: crash au login avec compte Google  [bug]
3. #31 - Refactorer le module d'authentification  [refactor]

Entrez le numero de l'issue a traiter (ou 0 pour annuler) :
```

Si aucune issue trouvee :
```markdown
Aucune issue ne correspond a "<description>".

Options :
- Modifier la recherche
- Lancer directement le workflow sans issue liee :
  → /feature <description>
  → /bugfix <description>
```

#### 2. Selection

- L'utilisateur selectionne une issue par son numero dans la liste
- Ou entre directement `#<numero>` pour cibler une issue precise

#### 3. Chargement de l'issue

```bash
gh issue view <numero> --json number,title,body,labels,assignees,milestone
```

Afficher le resume :

```markdown
## Issue #<numero> - <titre>

**Labels :** feature, priority:high
**Assignee :** @user
**Milestone :** v2.5.0

**Description :**
<body de l'issue>
```

#### 4. Detection du type de workflow

Analyser les labels de l'issue pour determiner le workflow adapte :

| Labels detectes | Workflow lance |
|-----------------|----------------|
| `bug`, `fix`, `defect` | `/bugfix` |
| `hotfix`, `urgent`, `critical` | `/hotfix` |
| `refactor`, `tech-debt`, `cleanup` | `/refactor` |
| `security`, `vulnerability` | `/secu` |
| `feature`, `enhancement`, `new` | `/feature` |
| (aucun label reconnu) | Demander a l'utilisateur |

Si aucun label ne permet de determiner le type :
```markdown
Quel type de workflow lancer pour cette issue ?

1. /feature  - Nouvelle fonctionnalite
2. /bugfix   - Correction de bug
3. /hotfix   - Correctif urgent
4. /refactor - Refactoring
5. /secu     - Audit securite

Votre choix :
```

#### 5. Lancement du workflow

Construire la description a partir du titre et du corps de l'issue :

```
<description> = "#<numero> - <titre>"
```

Puis dispatcher vers le workflow correspondant avec cette description :

```
/feature "#42 - Ajouter l'authentification OAuth2"
/bugfix  "#38 - Crash au login avec compte Google"
```

Le workflow demarre avec la description enrichie de l'issue, ce qui permet
au CDP de referencer l'issue dans les commits et le CHANGELOG.

---

## Format des Commits

Lors du workflow lance depuis `/backlog`, les commits doivent referencer l'issue :

```bash
# Format recommande
feat(auth): Implémenter OAuth2 (#42)
fix(auth): Corriger crash login Google (#38)
```

Le CDP prend en compte le numero d'issue dans `$ARGUMENTS` pour formater
automatiquement les messages de commit.

---

## Exemples

```bash
/backlog                              # Lister toutes les issues ouvertes
/backlog label:bug                    # Lister les issues tagees "bug"
/backlog @me                          # Lister les issues qui me sont assignees
/backlog authentification             # Rechercher les issues liees a l'auth
/backlog #42                          # Traiter directement l'issue #42
/backlog OAuth Google connexion       # Recherche multi-mots
```

---

## Prerequis

- CLI GitHub (`gh`) installe et authentifie (`gh auth login`)
- Le projet doit etre un repo GitHub (remote `origin` pointe vers GitHub)

Si `gh` n'est pas disponible :
```markdown
La commande /backlog necessite le CLI GitHub (gh).
- Installation : https://cli.github.com
- Authentification : gh auth login

Alternative : consulter les issues directement sur GitHub.
```

---

## Agent

Execution directe sans delegation — utilise `gh` pour interroger l'API GitHub,
puis dispatch vers le workflow approprie (`/feature`, `/bugfix`, `/hotfix`,
`/refactor`, ou `/secu`) en fonction des labels de l'issue selectionnee.
