# Guide d'Initialisation de Projet

Ce document decrit le processus complet d'initialisation d'un projet avec Claude Code.

## Declenchement Automatique

L'initialisation est declenchee automatiquement si :
- Le fichier `.claude/project-config.json` n'existe pas
- Claude Code demarre sur un projet utilisant ce template

## Processus d'Initialisation

### Etape 0 : Detection de Code Existant

**Avant de poser les questions**, Claude analyse le projet :

```
Analyse du projet en cours...

Fichiers detectes :
- package.json (Node.js)
- src/App.tsx (React + TypeScript)
- server/main.go (Go backend)
- docker-compose.yml (Docker)
- .github/workflows/ (GitHub Actions)
```

**Si du code existe**, Claude propose :

```
Je detecte du code existant dans ce projet.

Voulez-vous :
a) Initialiser en analysant le code existant (recommande)
   → Je vais deduire la stack et la configuration
b) Initialiser manuellement (questionnaire complet)
   → Repondre a toutes les questions
c) Annuler l'initialisation
```

### Etape 0a : Analyse Automatique (si option a)

Claude analyse les fichiers pour deduire :

| Fichier/Pattern | Deduction |
|-----------------|-----------|
| `package.json` | Node.js, dependances frontend/backend |
| `go.mod` | Go backend |
| `requirements.txt`, `pyproject.toml` | Python |
| `pom.xml`, `build.gradle` | Java |
| `Cargo.toml` | Rust |
| `*.csproj` | C# / .NET |
| `platformio.ini` | ESP32/Arduino firmware |
| `docker-compose.yml` | Docker deployment |
| `.github/workflows/` | GitHub Actions CI |
| `.gitlab-ci.yml` | GitLab CI |
| `Jenkinsfile` | Jenkins |
| `prisma/schema.prisma` | Prisma + PostgreSQL/MySQL |
| `src/App.tsx`, `src/App.jsx` | React frontend |
| `src/App.vue` | Vue.js frontend |
| `src/app/` + `angular.json` | Angular frontend |

**Detection des dependances (package.json)** :

| Dependance | Deduction |
|------------|-----------|
| `react`, `react-dom` | React frontend |
| `vue` | Vue.js frontend |
| `@angular/core` | Angular frontend |
| `express`, `fastify`, `koa` | Node.js backend |
| `next` | Next.js (fullstack) |
| `prisma`, `@prisma/client` | Prisma ORM |
| `mongoose` | MongoDB |
| `pg`, `mysql2` | SQL database |
| `jest`, `vitest` | Test framework |
| `cypress`, `playwright` | E2E testing |

**Resultat de l'analyse** :

```
Analyse terminee. Configuration detectee :

Projet : mon-projet (depuis package.json)
Backend : Go (go.mod detecte)
Frontend : React + TypeScript (react, typescript dans package.json)
Database : PostgreSQL (prisma avec postgresql provider)
CI/CD : GitHub Actions (.github/workflows/)
Deploiement : Docker (docker-compose.yml)
Tests : Vitest (frontend), Go test (backend)

Cette configuration vous convient ?
a) Oui, generer les agents
b) Non, modifier certains elements
c) Non, questionnaire complet
```

### Etape 1 : Informations Generales

Si questionnaire manuel ou complement :

```
1. Quel est le nom du projet ?
   > [Texte libre, ou detecte depuis package.json/go.mod]

2. Decris brievement le projet (1-2 phrases) :
   > [Texte libre]
```

### Etape 2 : Stack Backend

```
3. Quelle technologie backend utilises-tu ?
   a) Go
   b) Node.js (JavaScript/TypeScript)
   c) Python (FastAPI/Django/Flask)
   d) Java / Kotlin (Spring)
   e) C# / .NET
   f) PHP (Laravel/Symfony)
   g) Ruby (Rails)
   h) Rust (Actix/Axum)
   i) Aucun backend

   [Detecte : X] Confirmer ou changer ?
```

### Etape 3 : Stack Frontend

```
4. Quelle technologie frontend utilises-tu ?
   a) React (Create React App, Vite)
   b) React (Next.js)
   c) Vue.js (Vite)
   d) Vue.js (Nuxt)
   e) Angular
   f) Svelte / SvelteKit
   g) HTML/CSS/JS vanilla
   h) Aucun frontend

   [Detecte : X] Confirmer ou changer ?
```

### Etape 4 : Mobile (optionnel)

```
5. As-tu une application mobile ?
   a) React Native
   b) Flutter
   c) iOS natif (Swift/SwiftUI)
   d) Android natif (Kotlin)
   e) Capacitor/Ionic
   f) Pas de mobile
```

### Etape 5 : Firmware/Hardware (optionnel)

```
6. As-tu du code firmware ou embarque ?
   a) ESP32 (Arduino/PlatformIO)
   b) ESP8266
   c) Raspberry Pi
   d) Arduino (AVR)
   e) STM32
   f) Autre microcontroleur
   g) Pas de firmware
```

### Etape 6 : Base de Donnees

```
7. Quelle base de donnees utilises-tu ?
   a) PostgreSQL
   b) MySQL / MariaDB
   c) MongoDB
   d) SQLite
   e) Redis (cache/db)
   f) Firebase / Firestore
   g) Supabase
   h) Plusieurs (preciser)
   i) Aucune

   [Detecte : X] Confirmer ou changer ?
```

### Etape 7 : ORM / Query Builder

```
8. Quel ORM ou query builder utilises-tu ?
   a) Prisma
   b) TypeORM
   c) Sequelize
   d) Drizzle
   e) GORM (Go)
   f) SQLAlchemy (Python)
   g) Django ORM
   h) Aucun (SQL brut)
   i) Autre
```

### Etape 8 : CI/CD

```
9. Quel systeme CI/CD utilises-tu ?
   a) GitHub Actions
   b) GitLab CI
   c) Jenkins
   d) CircleCI
   e) Travis CI
   f) Azure DevOps
   g) Bitbucket Pipelines
   h) Aucun

   [Detecte : X] Confirmer ou changer ?
```

### Etape 9 : Deploiement

```
10. Comment deploies-tu ton application ?
    a) Docker / Docker Compose
    b) Kubernetes
    c) Serverless (AWS Lambda, Vercel, Netlify)
    d) VPS / Bare metal
    e) PaaS (Heroku, Railway, Render)
    f) Cloud Run / App Engine
    g) Autre

    [Detecte : X] Confirmer ou changer ?
```

### Etape 10 : Tests

```
11. Quels frameworks de tests utilises-tu ?

    Tests unitaires backend :
    [ ] Jest  [ ] Vitest  [ ] Mocha
    [ ] Go test  [ ] Pytest  [ ] JUnit
    [ ] Autre: ___

    Tests unitaires frontend :
    [ ] Jest  [ ] Vitest  [ ] Testing Library
    [ ] Autre: ___

    Tests E2E :
    [ ] Cypress  [ ] Playwright  [ ] Selenium
    [ ] Autre: ___

    [Detecte : X] Confirmer ou completer ?
```

### Etape 11 : Securite

```
12. Quels aspects securite sont importants pour ce projet ?
    (plusieurs choix possibles)

    [ ] Authentification utilisateurs (login, OAuth)
    [ ] API publique (rate limiting, API keys)
    [ ] Donnees sensibles (RGPD, sante, finance)
    [ ] Paiements (PCI-DSS)
    [ ] Multi-tenant (isolation des donnees)
    [ ] Application critique (haute disponibilite)
    [ ] Aucun aspect particulier
```

### Etape 12 : Structure du Projet

```
13. Quelle est la structure de ton projet ?

    a) Monorepo (tout dans un repo)
    b) Multi-repo (repos separes)
    c) Monorepo avec workspaces (npm/yarn/pnpm)
    d) Turborepo / Nx
```

## Generation de la Configuration

Une fois les reponses collectees, Claude genere :

### 1. project-config.json

```json
{
  "name": "mon-projet",
  "description": "Description du projet",
  "version": "0.1.0",
  "initialized_at": "2024-01-15T10:30:00Z",
  "initialized_from": "analysis",
  "stack": {
    "backend": {
      "language": "go",
      "framework": null,
      "orm": "gorm"
    },
    "frontend": {
      "language": "typescript",
      "framework": "react",
      "meta_framework": null
    },
    "mobile": null,
    "firmware": null,
    "database": {
      "primary": "postgresql",
      "cache": "redis"
    }
  },
  "infrastructure": {
    "cicd": "github-actions",
    "deploy": "docker",
    "containerized": true
  },
  "testing": {
    "backend": ["go-test"],
    "frontend": ["vitest", "testing-library"],
    "e2e": ["playwright"]
  },
  "security": {
    "concerns": ["auth", "api-public"],
    "compliance": []
  },
  "structure": {
    "type": "monorepo",
    "paths": {
      "backend": "server/",
      "frontend": "web/",
      "shared": "shared/"
    }
  }
}
```

### 2. Agents de Developpement

Selon la stack, Claude copie et adapte les templates :

| Stack detectee | Template | Agent genere |
|----------------|----------|--------------|
| Go | `templates/dev-backend-go.md` | `agents/dev-backend.md` |
| Node.js | `templates/dev-backend-node.md` | `agents/dev-backend.md` |
| Python | `templates/dev-backend-python.md` | `agents/dev-backend.md` |
| React | `templates/dev-frontend-react.md` | `agents/dev-frontend.md` |
| Vue.js | `templates/dev-frontend-vue.md` | `agents/dev-frontend.md` |
| ESP32 | `templates/dev-firmware-esp32.md` | `agents/dev-firmware.md` |

### 3. Mise a jour CLAUDE.md

Les placeholders `{{...}}` sont remplaces avec les vraies valeurs :

```markdown
| Parametre | Valeur |
|-----------|--------|
| Nom du projet | mon-projet |
| Backend | Go |
| Frontend | React + TypeScript |
| Base de donnees | PostgreSQL |
| CI/CD | GitHub Actions |
| Deploiement | Docker |
```

### 4. Commandes personnalisees

Les fichiers `.template.md` sont copies vers leurs versions finales :

```
commands/feature.template.md → commands/feature.md
commands/bugfix.template.md → commands/bugfix.md
...
```

## Reinitialisation

Si `/init-project` est execute sur un projet deja initialise :

```
Ce projet est deja initialise (config du 2024-01-15).

Voulez-vous :
a) Reconfigurer completement (ecrase la config actuelle)
b) Mettre a jour certains parametres
c) Re-analyser le code (detecter les changements)
d) Annuler
```

### Option b : Mise a jour partielle

```
Quel element voulez-vous modifier ?
a) Stack backend
b) Stack frontend
c) Base de donnees
d) CI/CD
e) Deploiement
f) Tests
g) Securite
h) Retour
```

### Option c : Re-analyse

Utile si le projet a evolue (nouveau framework ajoute, migration, etc.).
Claude re-analyse et propose les mises a jour.

## Validation Finale

```
Configuration prete !

Projet : mon-projet
Backend : Go (server/)
Frontend : React + TypeScript (web/)
Database : PostgreSQL
CI/CD : GitHub Actions
Deploy : Docker

Agents a generer :
- dev-backend.md (Go)
- dev-frontend.md (React)

Commandes disponibles :
- /feature, /bugfix, /hotfix, /refactor
- /review, /qa, /secu
- /deploy qualif, /deploy prod

Confirmer et generer ? (o/n)
```

## Post-Initialisation

Apres initialisation :

```
Projet "mon-projet" initialise avec succes !

Prochaines etapes recommandees :
1. Verifier les agents generes dans .claude/agents/
2. Lancer /secu pour un audit de securite initial
3. Commencer a travailler avec /feature ou /bugfix

Bonne utilisation de Claude Code !
```
