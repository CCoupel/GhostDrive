# Claude Code Project Template

Template de gestion de projet pour Claude Code.

## Installation

1. Copier le contenu du dossier `.claude/` dans votre projet
2. Copier `CLAUDE_TEMPLATE.md` vers `CLAUDE.md` a la racine du projet
3. Au premier demarrage, Claude detectera que le projet n'est pas initialise et lancera `/init-project`

## Structure

```
.claude/
├── README.md                 # Ce fichier
├── INITIALIZATION.md         # Guide complet d'initialisation (publiable)
├── CLAUDE_TEMPLATE.md        # Template CLAUDE.md (copier vers ../CLAUDE.md)
├── project-config.json       # Configuration generee (apres init)
├── agents/                   # Agents specialises
│   ├── cdp.template.md       # Chef de projet / orchestrateur
│   ├── implementation-planner.template.md
│   ├── code-reviewer.template.md
│   ├── qa.template.md
│   ├── security.template.md
│   ├── doc-updater.template.md
│   └── deploy.template.md
├── commands/                 # Commandes slash
│   ├── init-project.md       # Initialisation projet
│   ├── feature.template.md
│   ├── bugfix.template.md
│   ├── hotfix.template.md
│   ├── refactor.template.md
│   ├── review.template.md
│   ├── qa.template.md
│   ├── secu.md
│   └── deploy.template.md
└── templates/                # Templates par technologie
    ├── dev-backend-go.md
    ├── dev-backend-node.md
    ├── dev-backend-python.md
    ├── dev-frontend-react.md
    ├── dev-frontend-vue.md
    └── dev-firmware-esp32.md
```

## Premiere Utilisation

Au premier demarrage, Claude detecte automatiquement l'etat du projet.

> **Documentation complete** : [INITIALIZATION.md](INITIALIZATION.md)

### Etape 1 : Detection de code existant

Claude analyse le projet pour detecter :
- Fichiers de configuration (`package.json`, `go.mod`, `requirements.txt`, etc.)
- Dependances et frameworks utilises
- CI/CD et outils de deploiement

### Etape 2 : Proposition d'initialisation

**Si du code existe :**
```
Technologies detectees :
- Backend : Go (go.mod)
- Frontend : React + TypeScript (package.json)
- CI/CD : GitHub Actions

Voulez-vous :
a) Initialiser avec cette configuration (recommande)
b) Initialiser manuellement (questionnaire complet)
c) Annuler
```

**Si projet vide :** Questionnaire complet pose.

### Questions (mode manuel ou complement) :

1. **Nom du projet** et **Description**
2. **Stack backend** : Go, Node.js, Python, Java, etc.
3. **Stack frontend** : React, Vue, Angular, etc.
4. **Mobile** : React Native, Flutter, natif, etc.
5. **Firmware** : ESP32, Raspberry Pi, etc.
6. **Base de donnees** : PostgreSQL, MongoDB, SQLite, etc.
7. **CI/CD** : GitHub Actions, GitLab CI, Jenkins, etc.
8. **Deploiement** : Docker, Kubernetes, VPS, etc.
9. **Tests** : Frameworks utilises
10. **Securite** : Preoccupations specifiques

### Resultat :

- `project-config.json` genere avec la configuration
- Agents de dev crees selon la stack (ex: `dev-backend.md`, `dev-frontend.md`)
- CLAUDE.md mis a jour avec les informations du projet

## Commandes Disponibles

### Developpement

| Commande | Description |
|----------|-------------|
| `/feature <desc>` | Nouvelle fonctionnalite (workflow complet) |
| `/bugfix <desc>` | Correction de bug |
| `/hotfix <desc>` | Correction urgente production |
| `/refactor <desc>` | Refactoring sans changement fonctionnel |

### Validation

| Commande | Description |
|----------|-------------|
| `/review` | Revue de code |
| `/qa` | Tests et validation qualite |
| `/secu` | Audit de securite |

### Deploiement

| Commande | Description |
|----------|-------------|
| `/deploy qualif` | Deploiement en qualification |
| `/deploy prod` | Deploiement en production |

### Utilitaires

| Commande | Description |
|----------|-------------|
| `/init-project` | (Re)initialiser la configuration |
| `/doc` | Mettre a jour la documentation |
| `/cdp <desc>` | Lancer l'orchestrateur complet |

## Workflows

### Workflow Standard (/feature, /bugfix)

```
PLAN --> DEV --> TEST --> REVIEW --> QA --> DOC --> DEPLOY
```

### Workflow Securite (/secu)

```
SCAN --> DEPS --> SECRETS --> OWASP --> REPORT --> FIX
```

### Workflow Hotfix (/hotfix)

```
ANALYSE --> FIX --> TESTS CRITIQUES --> DEPLOY PROD --> POST-MORTEM
```

## Agents

### Agents de Workflow (toujours presents)

- **CDP** : Chef de projet, orchestre les autres agents
- **Planner** : Cree les plans d'implementation
- **Reviewer** : Revue de code
- **QA** : Tests et validation
- **Security** : Audit de securite
- **Doc** : Documentation
- **Deploy** : Deploiement

### Agents de Dev (generes selon stack)

- **dev-backend** : Genere depuis `templates/dev-backend-*.md`
- **dev-frontend** : Genere depuis `templates/dev-frontend-*.md`
- **dev-firmware** : Genere depuis `templates/dev-firmware-*.md`
- **dev-mobile** : Genere depuis `templates/dev-mobile-*.md`

## Personnalisation

### Ajouter un template technologique

1. Creer un fichier dans `templates/` (ex: `dev-backend-rust.md`)
2. Suivre le format des templates existants
3. Mettre a jour `/init-project` pour proposer cette option

### Modifier un workflow

1. Editer l'agent concerne dans `agents/`
2. Adapter les etapes selon les besoins du projet

### Ajouter une commande

1. Creer un fichier dans `commands/` (ex: `perf.md`)
2. Documenter l'usage et le workflow
3. Creer l'agent associe si necessaire

## Bonnes Pratiques

1. **Toujours initialiser** avant de commencer a travailler
2. **Utiliser les commandes** appropriees selon le contexte
3. **Suivre les workflows** pour une qualite constante
4. **Documenter** les decisions importantes
5. **Commiter regulierement** avec des messages clairs

## Support

Ce template est concu pour etre utilise avec Claude Code (CLI Anthropic).

Pour signaler un probleme ou suggerer une amelioration, ouvrir une issue sur le repository du template.
