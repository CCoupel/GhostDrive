# Commande /init-project

Initialisation interactive du projet pour configurer l'environnement Claude Code.

> **Documentation complete** : `TEMPLATE_claude/INITIALIZATION.md`

## Declenchement

- **Automatique** : Si `.claude/project-config.json` n'existe pas au demarrage
- **Manuel** : Commande `/init-project` pour reinitialiser ou modifier

## Workflow d'Initialisation

```
/init-project
    |
    v
[FETCH TEMPLATE] --> Fetcher TEMPLATE_claude/ depuis GitHub
    |                (TEMPLATE_claude/.template-source.json → gh api)
    v
[DEPLOY COMMANDES] --> Copier TEMPLATE_claude/commands/ → .claude/commands/
    |                  Copier TEMPLATE_claude/agents/   → .claude/agents/
    v
[DETECTION] --> Analyser le code existant
    |
    |-- Code detecte --> Proposer analyse auto ou manuel
    |
    |-- Projet vide --> Workshop ou questionnaire
    |
    v
[CONFIGURATION] --> Questions ou deduction
    |
    v
[GENERATION] --> project-config.json + agents dev-*
    |
    v
[FINALISATION] --> CLAUDE.md + .gitignore + CI/CD workflow
```

> La phase **FETCH TEMPLATE** est toujours executee en premier.
> Elle garantit que commandes, agents et contextes sont a la derniere version.
> Voir section "Fetch du Template depuis GitHub".

---

## Bootstrap : fichier unique

**Ce fichier est le seul fichier a copier** pour initialiser un nouveau projet.

```bash
mkdir -p .claude/commands

# Telecharger ce fichier
curl -o .claude/commands/init-project.md \
  https://raw.githubusercontent.com/CCoupel/claude_project_template/main/init-project.md

# Ouvrir dans Claude Code et lancer :
/init-project
```

`/init-project` fetchera automatiquement `TEMPLATE_claude/` depuis GitHub,
puis deploiera les commandes et agents dans `.claude/`.

---

## Architecture : separation TEMPLATE / PROJET

| Categorie | Emplacement | Comportement |
|-----------|-------------|--------------|
| **TEMPLATE** | `TEMPLATE_claude/` (racine projet) | Fetche depuis GitHub, gitignore, jamais edite manuellement |
| **COMMANDES** | `.claude/commands/` | Copies depuis `TEMPLATE_claude/commands/`, gitignore (sauf `init-project.md`) |
| **AGENTS TEMPLATE** | `.claude/agents/*.template.md` + `.claude/agents/context/` | Copies depuis `TEMPLATE_claude/agents/`, gitignore |
| **PROJET** | `.claude/CLAUDE.md`, `project-config.json`, `memory/`, `agents/dev-*.md` | Trackes dans git, jamais ecrases |

---

## Fetch du Template depuis GitHub

### URL du template (fallback bootstrap)

```
TEMPLATE_REPO_DEFAULT = "CCoupel/claude_project_template"
TEMPLATE_BRANCH_DEFAULT = "main"
```

> Si `TEMPLATE_claude/.template-source.json` n'existe pas → utiliser ces valeurs par defaut.

### Quand fetcher

- **Premiere initialisation** : toujours
- **Reinitialisation option d)** : synchronisation manuelle

### Procedure de fetch

#### 1. Lire la source (avec fallback)

```bash
if [ -f TEMPLATE_claude/.template-source.json ]; then
  TEMPLATE_REPO=$(cat TEMPLATE_claude/.template-source.json | jq -r '.repo')
  TEMPLATE_BRANCH=$(cat TEMPLATE_claude/.template-source.json | jq -r '.branch')
else
  TEMPLATE_REPO="CCoupel/claude_project_template"
  TEMPLATE_BRANCH="main"
fi
```

#### 2. Verifier si une mise a jour est disponible

```bash
KNOWN_COMMIT=$([ -f TEMPLATE_claude/.template-source.json ] && \
  cat TEMPLATE_claude/.template-source.json | jq -r '.commit // ""' || echo "")

LATEST_COMMIT=$(gh api repos/$TEMPLATE_REPO/commits/$TEMPLATE_BRANCH --jq '.sha')

if [ "$KNOWN_COMMIT" = "$LATEST_COMMIT" ]; then
  echo "Template deja a jour ($LATEST_COMMIT)"
  # Continuer quand meme (fichiers peuvent etre absents si gitignores)
fi
```

#### 3. Fetcher TEMPLATE_claude/ depuis GitHub

```bash
gh api repos/$TEMPLATE_REPO/git/trees/$TEMPLATE_BRANCH?recursive=1 \
  --jq '.tree[] | select(.type=="blob") | .path' \
  | grep -E '^TEMPLATE_claude/' \
  | while read FILE; do
      mkdir -p "$(dirname $FILE)"
      gh api repos/$TEMPLATE_REPO/contents/$FILE \
        --jq '.content' | base64 -d > "$FILE"
      echo "  ✓ $FILE"
    done
```

#### 4. Deployer dans .claude/

```bash
# Commandes (lues par Claude Code depuis .claude/commands/)
mkdir -p .claude/commands
cp -r TEMPLATE_claude/commands/. .claude/commands/

# Agents template
mkdir -p .claude/agents
cp TEMPLATE_claude/agents/*.template.md .claude/agents/ 2>/dev/null || true
cp -r TEMPLATE_claude/agents/context .claude/agents/context
```

#### 5. Mettre a jour TEMPLATE_claude/.template-source.json

```bash
TODAY=$(date +%Y-%m-%d)
cat > TEMPLATE_claude/.template-source.json <<EOF
{
  "repo": "$TEMPLATE_REPO",
  "branch": "$TEMPLATE_BRANCH",
  "commit": "$LATEST_COMMIT",
  "synced_at": "$TODAY"
}
EOF
echo "✓ TEMPLATE_claude/.template-source.json mis a jour ($LATEST_COMMIT)"
```

---

## Detection de version et migration

**Avant tout**, detecter la version du projet :

```bash
HAS_CONFIG=$([ -f .claude/project-config.json ] && echo "yes" || echo "no")
HAS_TEMPLATE_DIR=$([ -d TEMPLATE_claude ] && echo "yes" || echo "no")
HAS_OLD_SOURCE=$([ -f .claude/.template-source.json ] && echo "yes" || echo "no")
```

| `project-config.json` | `TEMPLATE_claude/` | `.claude/.template-source.json` | Diagnostic |
|-----------------------|--------------------|--------------------------------|------------|
| absent | absent | absent | Nouveau projet → flux normal |
| present | present | - | Projet v3 → Reinitialisation |
| present | absent | present | **Projet v2 → Migration v3** |
| present | absent | absent | **Projet v1 → Migration v3** |

---

## Migration v1/v2 → v3

Declenche si `project-config.json` existe mais `TEMPLATE_claude/` est absent.

```
Projet initialise avec une architecture anterieure (v1/v2).

Architecture actuelle :
  Fichiers template dans .claude/ (trackes ou gitignores localement).

Architecture v3 (cible) :
  TEMPLATE_claude/ a la racine — fetche depuis GitHub, gitignore
  .claude/ contient uniquement les fichiers PROJET

Migration requise. Continuer ? [O/n]
```

### Etape M1 — Fetch de TEMPLATE_claude/ depuis GitHub

Executer la procedure "Fetch du Template depuis GitHub" ci-dessus.

### Etape M2 — Nettoyer .claude/ des anciens fichiers template

```bash
git rm --cached -r .claude/commands/ 2>/dev/null || true
git rm --cached -r .claude/agents/context/ 2>/dev/null || true
git rm --cached .claude/agents/*.template.md 2>/dev/null || true
git rm --cached -r .claude/templates/ 2>/dev/null || true
git rm --cached .claude/.template-source.json 2>/dev/null || true
git rm --cached .claude/INITIALIZATION.md 2>/dev/null || true
git rm --cached .claude/CLAUDE_TEMPLATE.md 2>/dev/null || true
git rm --cached .claude/gitignore-for-projects 2>/dev/null || true
```

### Etape M3 — Appliquer le .gitignore

```bash
cp TEMPLATE_claude/gitignore-for-projects .gitignore
# (merger avec le .gitignore existant si necessaire)
```

### Etape M4 — Commiter la migration

```bash
git add .gitignore TEMPLATE_claude/.template-source.json
git commit -m "chore(claude): Migrate to v3 template architecture (TEMPLATE_claude/)

- TEMPLATE_claude/ fetched from GitHub, gitignored at root
- .claude/ now contains only project-specific files
- Untracked legacy template files from .claude/"
```

### Etape M5 — Rapport

```
Migration → v3 terminee.

  TEMPLATE_claude/ fetche depuis CCoupel/claude_project_template
  Commandes deployees dans .claude/commands/
  Agents template deployes dans .claude/agents/

  Fichiers PROJET preserves :
    ✓ .claude/CLAUDE.md
    ✓ .claude/project-config.json
    ✓ .claude/memory/
    ✓ .claude/agents/dev-*.md (si presents)
```

---

## Etape 0 : Detection de Code Existant

**IMPORTANT** : Avant de poser des questions, analyser le projet.

### Fichiers a detecter

| Fichier | Detection |
|---------|-----------|
| `package.json` | Node.js, dependances npm |
| `go.mod` | Go |
| `requirements.txt`, `pyproject.toml` | Python |
| `Cargo.toml` | Rust |
| `pom.xml`, `build.gradle` | Java |
| `*.csproj`, `*.sln` | C# / .NET |
| `composer.json` | PHP |
| `Gemfile` | Ruby |
| `platformio.ini` | ESP32 / Arduino |
| `docker-compose.yml` | Docker |
| `.github/workflows/` | GitHub Actions |
| `.gitlab-ci.yml` | GitLab CI |
| `Jenkinsfile` | Jenkins |

### Dependances a analyser (package.json)

| Dependance | Technologie |
|------------|-------------|
| `react`, `react-dom` | React |
| `vue` | Vue.js |
| `@angular/core` | Angular |
| `svelte` | Svelte |
| `next` | Next.js |
| `nuxt` | Nuxt |
| `express`, `fastify`, `koa`, `hapi` | Node.js backend |
| `prisma`, `@prisma/client` | Prisma ORM |
| `typeorm` | TypeORM |
| `mongoose` | MongoDB |
| `pg`, `mysql2`, `sqlite3` | SQL direct |
| `jest`, `vitest`, `mocha` | Tests |
| `cypress`, `playwright` | E2E |

### Proposition a l'utilisateur

**Si code detecte :**

```
Analyse du projet en cours...

Technologies detectees :
- Backend : Go (go.mod)
- Frontend : React + TypeScript (package.json)
- Database : PostgreSQL (prisma avec provider postgresql)
- CI/CD : GitHub Actions (.github/workflows/)
- Tests : Vitest, Playwright

Voulez-vous :
a) Initialiser avec cette configuration (recommande)
b) Initialiser manuellement (questionnaire complet)
c) Annuler
```

**Si projet vide :**

```
Ce projet ne contient pas encore de code.

Comment souhaitez-vous initialiser le projet ?

a) Workshop de cadrage (recommande)
   → Entretien guide pour definir vision, objectifs, stack et contraintes
   → Genere un CLAUDE.md complet et project-config.json
b) Questionnaire rapide
   → Questions directes sur la stack technique
c) Annuler
```

### Option a : Workshop de Cadrage

Mener un entretien structure en 6 phases.

**Phase 1 — Identification du probleme**
```
1. Quel est le nom du projet ?
2. Quel est le probleme central que ce projet cherche a resoudre ?
3. Pourquoi ce probleme n'est-il pas resolu aujourd'hui ?
4. Quelle est l'urgence ou l'impact si le probleme n'est pas resolu ?
```

**Phase 2 — Solution envisagee**
```
5. Comment le projet compte-t-il resoudre ce probleme ?
6. Quels sont les 3 cas d'usage principaux de la v1 ?
7. Quels cas d'usage sont explicitement hors perimetre (v1) ?
8. Existe-t-il des solutions concurrentes ? Qu'est-ce qui differencie ce projet ?
```

**Phase 3 — Parties prenantes et utilisateurs**
```
9.  Qui sont les utilisateurs finaux ? (roles, profils, niveau technique)
10. Qui sont les commanditaires / decideurs du projet ?
11. Y a-t-il des parties prenantes externes ?
12. Quel est le contexte de distribution ?
```

**Phase 4 — Contraintes et risques**
```
13. Quelles sont les contraintes de delai ?
14. Quelles sont les contraintes budgetaires ou d'equipe ?
15. Y a-t-il des contraintes techniques imposees ?
16. Y a-t-il des contraintes reglementaires ? (RGPD, HDS, PCI-DSS...)
17. Quels sont les principaux risques identifies ?
```

**Phase 5 — Stack et architecture**
```
→ Enchainer les etapes 1 a 10 du questionnaire standard
```

**Phase 6 — Conventions d'equipe**
```
18. Convention de nommage des branches ?
19. Format de commit ?
20. Politique de revue de code ?
21. Standards qualite specifiques ?
```

A la fin du workshop, generer `CLAUDE.md` complet, `project-config.json`, et les agents.

---

## Etape 1 : Informations Generales

```
1. Quel est le nom du projet ?
   [Detecte: nom depuis package.json/go.mod] Confirmer ou modifier ?

2. Decris brievement le projet (1-2 phrases) :
```

---

## Etape 2 : Stack Backend

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
```

---

## Etape 3 : Stack Frontend

```
4. Quelle technologie frontend utilises-tu ?
   a) React (Vite/CRA)
   b) React (Next.js)
   c) Vue.js (Vite)
   d) Vue.js (Nuxt)
   e) Angular
   f) Svelte / SvelteKit
   g) HTML/CSS/JS vanilla
   h) Aucun frontend
```

---

## Etape 4 : Mobile (optionnel)

```
5. As-tu une application mobile ?
   a) React Native
   b) Flutter
   c) iOS natif (Swift/SwiftUI)
   d) Android natif (Kotlin)
   e) Capacitor/Ionic
   f) Pas de mobile
```

---

## Etape 5 : Firmware/Hardware (optionnel)

```
6. As-tu du code firmware ou embarque ?
   a) ESP32 (Arduino/PlatformIO)
   b) ESP8266
   c) Raspberry Pi
   d) Arduino (AVR)
   e) STM32
   f) Pas de firmware
```

---

## Etape 6 : Base de Donnees

```
7. Quelle base de donnees utilises-tu ?
   a) PostgreSQL
   b) MySQL / MariaDB
   c) MongoDB
   d) SQLite
   e) Redis
   f) Firebase / Firestore
   g) Supabase
   h) Plusieurs (preciser)
   i) Aucune
```

---

## Etape 7 : CI/CD

```
8. Quel systeme CI/CD utilises-tu ?
   a) GitHub Actions
   b) GitLab CI
   c) Jenkins
   d) CircleCI
   e) Azure DevOps
   f) Bitbucket Pipelines
   g) Aucun
```

---

## Etape 8 : Deploiement

```
9. Comment deploies-tu ton application ?
   a) Docker / Docker Compose
   b) Kubernetes
   c) Serverless (AWS Lambda, Vercel, Netlify)
   d) VPS / Bare metal
   e) PaaS (Heroku, Railway, Render)
   f) Cloud Run / App Engine
```

---

## Etape 9 : Tests

```
10. Quels frameworks de tests utilises-tu ?
    Tests unitaires backend: ___
    Tests unitaires frontend: ___
    Tests E2E: ___
```

---

## Etape 10 : Securite

```
11. Quels aspects securite sont importants ?
    [ ] Authentification utilisateurs
    [ ] API publique
    [ ] Donnees sensibles (RGPD, sante, finance)
    [ ] Paiements (PCI-DSS)
    [ ] Multi-tenant
    [ ] Aucun aspect particulier
```

---

## Generation de la Configuration

### 1. project-config.json

```json
{
  "name": "<PROJECT_NAME>",
  "description": "<DESCRIPTION>",
  "version": "0.1.0",
  "initialized_at": "<TIMESTAMP>",
  "initialized_from": "analysis|manual|workshop",
  "stack": {
    "backend": { "language": "go", "framework": null },
    "frontend": { "language": "typescript", "framework": "react" },
    "mobile": null,
    "firmware": null,
    "database": { "primary": "postgresql", "orm": "prisma" }
  },
  "infrastructure": {
    "cicd": "github-actions",
    "deploy": "docker"
  },
  "testing": {
    "backend": ["go-test"],
    "frontend": ["vitest"],
    "e2e": ["playwright"]
  },
  "security": {
    "concerns": ["auth", "api-public"]
  }
}
```

### 2. Agents dev-*

| Stack | Template Source | Destination |
|-------|-----------------|-------------|
| Go | `TEMPLATE_claude/templates/dev-backend-go.md` | `.claude/agents/dev-backend.md` |
| Node.js | `TEMPLATE_claude/templates/dev-backend-node.md` | `.claude/agents/dev-backend.md` |
| Python | `TEMPLATE_claude/templates/dev-backend-python.md` | `.claude/agents/dev-backend.md` |
| React | `TEMPLATE_claude/templates/dev-frontend-react.md` | `.claude/agents/dev-frontend.md` |
| Vue.js | `TEMPLATE_claude/templates/dev-frontend-vue.md` | `.claude/agents/dev-frontend.md` |
| ESP32 | `TEMPLATE_claude/templates/dev-firmware-esp32.md` | `.claude/agents/dev-firmware.md` |

### 3. Workflow CI/CD

Copier depuis `TEMPLATE_claude/templates/workflows/` vers `.github/workflows/release.yml`
et remplacer les placeholders :

| Stack | Template |
|-------|----------|
| Go + React/Vue | `TEMPLATE_claude/templates/workflows/release-go-react.yml` |
| Autres | Generer un workflow minimal adapte |

| Placeholder | Exemple |
|-------------|---------|
| `{PROJECT_NAME}` | `MyApp` |
| `{BINARY_NAME}` | `myapp` |
| `{BACKEND_DIR}` | `backend` |
| `{FRONTEND_DIR}` | `frontend` |
| `{EMBED_DIR}` | `cmd/server/dist` |
| `{GO_MAIN_PKG}` | `./cmd/server` |
| `{VERSION_FILE}` | `config.json` |
| `{GO_VERSION}` | `1.22` |
| `{NODE_VERSION}` | `20` |
| `{MIN_BINARY_SIZE}` | `5242880` |

### 4. Finalisation

```bash
# CLAUDE.md depuis le template
cp TEMPLATE_claude/CLAUDE_TEMPLATE.md .claude/CLAUDE.md
# (remplacer les placeholders {{...}} avec les valeurs reelles)

# .gitignore projet
cp TEMPLATE_claude/gitignore-for-projects .gitignore
```

---

## Message de Fin

```
Projet "<PROJECT_NAME>" initialise avec succes !

Configuration :
- Backend  : <BACKEND>
- Frontend : <FRONTEND>
- Database : <DATABASE>
- CI/CD    : <CICD>
- Deploy   : <DEPLOY>

Agents generes :
- .claude/agents/dev-backend.md
- .claude/agents/dev-frontend.md

Commandes disponibles :
- /feature, /bugfix, /hotfix, /refactor
- /review, /qa, /secu
- /deploy qualif, /deploy prod
- /milestone new/status/close
- /backlog, /marketing

Bonne utilisation de Claude Code !
```

---

## Reinitialisation (projet v3)

Si `project-config.json` + `TEMPLATE_claude/` existent :

```
Ce projet est deja initialise (config du YYYY-MM-DD).
Template : CCoupel/claude_project_template — dernier sync : <date> (<commit>)

Voulez-vous :
a) Reconfigurer completement (ecrase la config)
b) Modifier certains parametres
c) Re-analyser le code (detecter les changements)
d) Synchroniser le template depuis GitHub
e) Annuler
```

### Option d : Synchronisation

Afficher le rapport avant action :

```
Synchronisation depuis github.com/<repo>

  Commit actuel  : abc1234  (synced: 2026-03-01)
  Dernier commit : def5678  (2026-04-16) ← mise a jour disponible

  NOUVEAUX : 1 fichier
  MODIFIES : 2 fichiers
  INCHANGES : 18 fichiers

  [A] Tout appliquer   [B] Nouveaux uniquement   [C] Annuler
```

Apres application, re-deployer dans `.claude/` :

```bash
cp -r TEMPLATE_claude/commands/. .claude/commands/
cp TEMPLATE_claude/agents/*.template.md .claude/agents/
cp -r TEMPLATE_claude/agents/context .claude/agents/context
```
