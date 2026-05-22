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
[PLACEHOLDERS] --> Substituer {VAR} dans commandes et agents deployes
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
| **COMMANDES** | `.claude/commands/*.md` + `.claude/commands/context/` | Depuis `TEMPLATE_claude/commands/*.md` et `commands/context/`, déployé en `*.md` — gitignore |
| **AGENTS TEMPLATE** | `.claude/agents/*.template.md` + `.claude/agents/context/` | Depuis `TEMPLATE_claude/agents/*.md`, déployé en `*.template.md` — gitignore |
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
- **Pre-menu de reinitialisation** : toujours (silencieusement, avant l'analyse)
- **Reinitialisation option d)** : appliquer les changements detectes (fetch deja fait au pre-menu)

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

Les commandes sont déployées en `*.md` — directement invocables comme `/xxx`, jamais éditées manuellement.
Les agents sont déployés en `*.template.md` — les adaptations projet vont dans des fichiers `*.md` compagnons (voir COMMON.md §13).

```bash
mkdir -p .claude/commands .claude/agents

# Commandes : déployé en *.md (invocables directement comme /xxx)
for src in TEMPLATE_claude/commands/*.md; do
  dest=".claude/commands/$(basename $src)"
  cp "$src" "$dest"
  echo "  ✓ $dest"
done

# Agents : déployé en *.template.md
for src in TEMPLATE_claude/agents/*.md; do
  dest=".claude/agents/$(basename $src .md).template.md"
  cp "$src" "$dest"
  echo "  ✓ $dest"
done

# Contextes partagés (restent en *.md — lus directement, pas de convention template/projet)
cp -r TEMPLATE_claude/agents/context .claude/agents/context
cp -r TEMPLATE_claude/commands/context .claude/commands/context
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

### Etape M1b — Migration : renommer les commandes legacy *.template.md → *.md

> ⚠ **SCOPE STRICT** : uniquement `.claude/commands/` — ne jamais appliquer aux `.claude/agents/`.
> Les `*.template.md` agents sont des templates gitignorés ; les `*.md` agents sont des customisations trackées.
> Appliquer cette logique aux agents écraserait les fichiers projet.

Les commandes etaient deployees en `*.template.md` avant la v2.9.7. Les renommer avant de nettoyer le cache git.
Avant chaque renommage, détecter si le fichier contient des customisations (contenu différent du template).

```bash
CUSTOMIZED_COMMANDS=()
for f in .claude/commands/*.template.md; do
  [[ -f "$f" ]] || continue
  name=$(basename "$f" .template.md)
  dest="${f/.template.md/.md}"
  template="TEMPLATE_claude/commands/${name}.md"
  # Détecter si customisé — différent du template source
  if [[ -f "$template" ]] && ! cmp -s "$f" "$template"; then
    CUSTOMIZED_COMMANDS+=("$name")
    echo "  ⚠ commande customisée détectée : ${name} (sera préservée, non écrasée par le template)"
  fi
  mv "$f" "$dest"
  echo "  ✓ migration commande : $(basename $f) → $(basename $dest)"
done
```

Si `CUSTOMIZED_COMMANDS[]` non vide → informer l'utilisateur :
```
⚠ Commandes modifiées localement (non écrasées par le template) :
  - [nom] : diff détecté avec le template source
Pour rétablir le template, supprimer le fichier .claude/commands/[nom].md et relancer /init-project.
```

### Etape M2 — Nettoyer .claude/ des anciens fichiers template

```bash
git rm --cached .claude/commands/*.template.md 2>/dev/null || true
git rm --cached .claude/commands/*.md 2>/dev/null || true
# Note : après migration v3, les commandes sont en *.md (gitignored)
git rm --cached -r .claude/agents/context/ 2>/dev/null || true
git rm --cached -r .claude/commands/context/ 2>/dev/null || true
git rm --cached .claude/agents/*.template.md 2>/dev/null || true
git rm --cached .claude/agents/*.md 2>/dev/null || true
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

M2 a désindexé les `*.md` agents via `git rm --cached`. Re-tracker les fichiers compagnons agents
qui existent sur disque (customisations projet à préserver) avant de commiter.

```bash
git add .gitignore TEMPLATE_claude/.template-source.json
# Re-tracker les companions agents désindexés par M2 (hors dev-* et *.template.md)
for f in .claude/agents/*.md; do
  [[ -f "$f" ]] || continue
  name=$(basename "$f")
  [[ "$name" == dev-*.md ]] && continue  # dev-* gérés séparément
  git add "$f" 2>/dev/null && echo "  ✓ re-tracking companion agent : $name"
done
git commit -m "chore(claude): Migrate to v3 template architecture (TEMPLATE_claude/)

- TEMPLATE_claude/ fetched from GitHub, gitignored at root
- .claude/ now contains only project-specific files
- Untracked legacy template files from .claude/
- Agent companion files re-tracked after cache cleanup"
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

## Etape 5b : Plugin (optionnel)

```
6b. Ton projet inclut-il un plugin pour une plateforme existante ?
    a) VS Code Extension
    b) Obsidian Plugin
    c) WordPress Plugin
    d) Browser Extension (Chrome/Firefox)
    e) Plugin applicatif maison (preciser la plateforme)
    f) Pas de plugin
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
  "team_name": "<PROJECT_NAME>-team",
  "org": "<GITHUB_ORG>",
  "project": "<REPO_NAME>",
  "description": "<DESCRIPTION>",
  "version": "0.1.0",
  "initialized_at": "<TIMESTAMP>",
  "initialized_from": "analysis|manual|workshop",
  "stack": {
    "backend": { "language": "go", "framework": null },
    "frontend": { "language": "typescript", "framework": "react" },
    "mobile": null,
    "firmware": null,
    "plugin": { "platform": "VS Code Extension" },
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
  },
  "commands": {
    "build": "<BUILD_CMD>",
    "test": "<TEST_CMD>",
    "lint": "<LINT_CMD>",
    "audit": "<AUDIT_CMD>",
    "typecheck": "<TYPECHECK_CMD>"
  },
  "agents": {
    "idle_ttl_minutes": 15,
    "idle_warning_interval_minutes": 5
  }
}
```

Valeurs a deriver si elles ne sont pas fournies explicitement :

| Champ | Derivation |
|-------|-----------|
| `team_name` | `<PROJECT_NAME>-team` (minuscules, tirets) |
| `org` | `git remote get-url origin` → extraire l'organisation GitHub |
| `project` | `git remote get-url origin` → extraire le nom du repo (sans `.git`) |
| `commands.build` | Stack backend : `go build ./...` / `npm run build` / `python -m build` |
| `commands.test` | Stack : `go test ./...` / `npm test` / `pytest` |
| `commands.lint` | Stack : `golangci-lint run` / `npm run lint` / `ruff check .` |
| `commands.audit` | Stack : `govulncheck ./...` / `npm audit` / `pip-audit` |
| `commands.typecheck` | Frontend TS : `npm run typecheck` / `tsc --noEmit` — vide sinon |

### 2. Agents dev-*

| Stack | Template Source | Destination |
|-------|-----------------|-------------|
| Go | `TEMPLATE_claude/templates/dev-backend-go.md` | `.claude/agents/dev-backend.md` |
| Node.js | `TEMPLATE_claude/templates/dev-backend-node.md` | `.claude/agents/dev-backend.md` |
| Python | `TEMPLATE_claude/templates/dev-backend-python.md` | `.claude/agents/dev-backend.md` |
| React | `TEMPLATE_claude/templates/dev-frontend-react.md` | `.claude/agents/dev-frontend.md` |
| Vue.js | `TEMPLATE_claude/templates/dev-frontend-vue.md` | `.claude/agents/dev-frontend.md` |
| ESP32 | `TEMPLATE_claude/templates/dev-firmware-esp32.md` | `.claude/agents/dev-firmware.md` |
| Plugin (toute plateforme) | `TEMPLATE_claude/templates/dev-plugin.md` | `.claude/agents/dev-plugin.md` |

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

### 4. Application des placeholders dans les commandes et agents deployes

A executer **apres** la creation de `project-config.json`.

Lire les valeurs :

```bash
PROJECT_NAME=$(jq -r '.name'                         .claude/project-config.json)
TEAM_NAME=$(jq -r '.team_name'                       .claude/project-config.json)
ORG=$(jq -r '.org'                                   .claude/project-config.json)
PROJECT=$(jq -r '.project'                           .claude/project-config.json)
BUILD_CMD=$(jq -r '.commands.build     // ""'        .claude/project-config.json)
TEST_CMD=$(jq -r '.commands.test      // ""'         .claude/project-config.json)
LINT_CMD=$(jq -r '.commands.lint      // ""'         .claude/project-config.json)
AUDIT_CMD=$(jq -r '.commands.audit    // ""'         .claude/project-config.json)
TYPECHECK_CMD=$(jq -r '.commands.typecheck // ""'    .claude/project-config.json)
PLUGIN_PLATFORM=$(jq -r '.stack.plugin.platform // ""' .claude/project-config.json)
```

Echapper les caracteres speciaux sed (`&`, `\`, `|`) dans les valeurs de commandes
(un `&` dans la chaine de remplacement sed signifie "texte matche" — ex: `cd frontend && npm run build` serait corrompu sans echappement) :

```bash
escape_sed() { printf '%s' "$1" | sed 's/[&\|]/\\&/g'; }
BUILD_CMD_ESC=$(escape_sed "$BUILD_CMD")
TEST_CMD_ESC=$(escape_sed "$TEST_CMD")
LINT_CMD_ESC=$(escape_sed "$LINT_CMD")
AUDIT_CMD_ESC=$(escape_sed "$AUDIT_CMD")
TYPECHECK_CMD_ESC=$(escape_sed "$TYPECHECK_CMD")
```

Appliquer la substitution sur les fichiers deployes (commandes + agents generiques) :

```bash
for f in .claude/commands/*.md .claude/agents/*.template.md; do
  name=$(basename "$f")
  [[ "$name" == "init-project.md" ]] && continue  # contient des {VAR} d'exemple — ne pas substituer
  sed -i \
    -e "s|{PROJECT_NAME}|${PROJECT_NAME}|g" \
    -e "s|{TEAM_NAME}|${TEAM_NAME}|g"       \
    -e "s|{ORG}|${ORG}|g"                   \
    -e "s|{PROJECT}|${PROJECT}|g"            \
    -e "s|{BUILD_CMD}|${BUILD_CMD_ESC}|g"        \
    -e "s|{TEST_CMD}|${TEST_CMD_ESC}|g"          \
    -e "s|{LINT_CMD}|${LINT_CMD_ESC}|g"          \
    -e "s|{AUDIT_CMD}|${AUDIT_CMD_ESC}|g"        \
    -e "s|{TYPECHECK_CMD}|${TYPECHECK_CMD_ESC}|g" \
    -e "s|{PLUGIN_PLATFORM}|${PLUGIN_PLATFORM}|g" \
    "$f"
  echo "  ✓ placeholders appliques dans $name"
done
```

### 5. Finalisation

```bash
# CLAUDE.md depuis le template (remplacer les placeholders)
sed \
  -e "s|{PROJECT_NAME}|$PROJECT_NAME|g" \
  -e "s|{TEAM_NAME}|$TEAM_NAME|g" \
  -e "s|{ORG}|$ORG|g" \
  -e "s|{PROJECT}|$PROJECT|g" \
  -e "s|{BACKEND_TECH}|$BACKEND_TECH|g" \
  -e "s|{FRONTEND_TECH}|$FRONTEND_TECH|g" \
  -e "s|{DATABASE}|$DATABASE|g" \
  -e "s|{BUILD_CMD}|$BUILD_CMD|g" \
  -e "s|{TEST_CMD}|$TEST_CMD|g" \
  TEMPLATE_claude/CLAUDE_TEMPLATE.md > CLAUDE.md
echo "✓ CLAUDE.md généré"

# .gitignore projet
cp TEMPLATE_claude/gitignore-for-projects .gitignore
```

#### Labels GitHub de suivi de phase

Créer les labels de phase sur le repo GitHub (idempotent — `--force` met à jour si déjà existant) :

```bash
gh label create "PLANNING"  --color "0075ca" --description "Issue en cours de planification" --force
gh label create "EN COURS"  --color "e4e669" --description "Issue en cours de développement" --force
gh label create "EN REVIEW" --color "d876e3" --description "Issue en cours de revue de code"  --force
gh label create "EN QA"     --color "f9d0c4" --description "Issue en cours de tests QA"       --force
gh label create "DONE"      --color "0e8a16" --description "Issue livrée et validée"           --force
```

> Si le repo n'a pas encore de remote GitHub configuré, sauter cette étape et noter dans
> le Message de Fin : "Labels GitHub à créer manuellement ou relancer /init-project après `git remote add`."

#### Settings Claude Code

Les règles critiques du teamleader sont dans `CLAUDE.md` (bloc `TEAMLEADER_PROTOCOL`) — elles survivent aux compactages nativement via le chargement natif de CLAUDE.md.

Déployer `TEMPLATE_claude/settings.json` dans `.claude/settings.json` si absent :

```bash
if [ ! -f .claude/settings.json ]; then
  cp TEMPLATE_claude/settings.json .claude/settings.json
  echo "✓ .claude/settings.json créé"
else
  echo "  .claude/settings.json déjà présent — non écrasé"
fi
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
- /progression, /context-audit
- /start-session, /end-session
- /team-status

Bonne utilisation de Claude Code !
```

---

## Reinitialisation (projet v3)

Si `project-config.json` + `TEMPLATE_claude/` existent :

### Etape pre-menu — Fetch GitHub + Calcul automatique des changements

Avant d'afficher le menu :

**1. Fetcher silencieusement le template depuis GitHub** (procedure "Fetch du Template depuis GitHub").
Si le fetch echoue (pas de reseau, gh non auth) → continuer avec le template local en place,
afficher un avertissement discret : `⚠ Fetch GitHub impossible — analyse basee sur le template local (sync du <date>).`

**2. Calculer les changements** entre `TEMPLATE_claude/` (maintenant a jour) et les fichiers deployes
(`.claude/commands/`, `.claude/agents/`). Meme logique que l'etape d3 ci-dessous.

Pour chaque fichier MODIFIE, lire les deux versions et generer une explication courte
(1 ligne max) decrivant ce qui a change.

```
Ce projet est deja initialise (config du YYYY-MM-DD).
Template GitHub : CCoupel/claude_project_template — <commit> (fetch a l'instant)

Changements disponibles :
  [+] feature, hotfix                   ← 2 nouveaux
  [~] cdp       — <explication courte du changement>
  [~] bugfix    — <explication courte du changement>
  [!] old-command                       ← 1 reliquat
  (12 fichiers inchanges)

  → Aucun changement detecte            ← afficher si tout est INCHANGE

Voulez-vous :
a) Reconfigurer completement (ecrase la config)
b) Modifier certains parametres
c) Re-analyser le code (detecter les changements)
d) Appliquer les mises a jour detectees
e) Annuler
```

### Option d : Appliquer les mises a jour detectees

> Le fetch GitHub a deja ete effectue au pre-menu — `TEMPLATE_claude/` est a jour.
> Cette option calcule le diff precis et deploie les changements dans `.claude/`.

#### Etape d1b — Migration : renommer les commandes legacy *.template.md → *.md

> ⚠ **SCOPE STRICT** : uniquement `.claude/commands/` — ne jamais appliquer aux `.claude/agents/`.
> Les `*.template.md` agents sont des templates gitignorés ; les `*.md` agents sont des customisations trackées.
> Appliquer cette logique aux agents écraserait les fichiers projet.

Avant tout calcul, renommer les commandes `*.template.md` residuelles dans `.claude/commands/`
(déployées avant la v2.9.7 où les commandes étaient encore en `*.template.md`).
Avant chaque renommage, détecter si le fichier contient des customisations pour éviter
que d5 ne les écrase silencieusement.

```bash
CUSTOMIZED_COMMANDS=()
for f in .claude/commands/*.template.md; do
  [[ -f "$f" ]] || continue
  name=$(basename "$f" .template.md)
  dest="${f/.template.md/.md}"
  template="TEMPLATE_claude/commands/${name}.md"
  # Détecter si customisé — différent du template source
  if [[ -f "$template" ]] && ! cmp -s "$f" "$template"; then
    CUSTOMIZED_COMMANDS+=("$name")
    echo "  ⚠ commande customisée détectée : ${name} (sera préservée, non écrasée par d5)"
  fi
  mv "$f" "$dest"
  echo "  ✓ migration commande : $(basename $f) → $(basename $dest)"
done
```

Si `CUSTOMIZED_COMMANDS[]` non vide → informer l'utilisateur avant de continuer vers d5 :
```
⚠ Commandes modifiées localement (non écrasées par le template) :
  - [nom] : diff détecté avec le template source
Pour rétablir le template, supprimer le fichier .claude/commands/[nom].md et relancer l'option d.
```

#### Etape d2 — Calculer les noms deployes attendus

```bash
# Noms attendus pour les commandes — tous les *.md du top-level (glob non récursif)
# NB : context-audit.md est une commande normale, pas un fichier du sous-répertoire context/
EXPECTED_COMMANDS=$(for f in TEMPLATE_claude/commands/*.md; do
  basename "$f" .md
done)

# Noms attendus pour les agents, hors dev-*
EXPECTED_AGENTS=$(for f in TEMPLATE_claude/agents/*.md; do
  basename "$f" .md
done)
```

#### Etape d3 — Comparer avec les fichiers deployes

```bash
# Commandes template déployées (*.md ou *.template.md legacy — hors sous-répertoire context/ et hors init-project)
# Le filtre exclut le répertoire .claude/commands/context/ uniquement — pas les fichiers nommés context-*.md
# init-project est le bootstrapper lui-même : jamais traité comme reliquat ni supprimé
DEPLOYED_COMMANDS=$(
  { ls .claude/commands/*.md 2>/dev/null; ls .claude/commands/*.template.md 2>/dev/null; } \
  | grep -v '/commands/context/' \
  | grep -v 'init-project' \
  | xargs -I{} basename {} \
  | sed 's/\.template\.md$//' | sed 's/\.md$//' \
  | sort -u
)

# Agents template déployés (*.template.md uniquement — les *.md et dev-*.md sont des fichiers projet)
DEPLOYED_AGENTS=$(ls .claude/agents/*.template.md 2>/dev/null \
  | xargs -I{} basename {} .template.md)

# Contextes partagés (agents/context/ et commands/context/)
# Comparer chaque fichier source avec le fichier déployé
for src in TEMPLATE_claude/agents/context/*.md TEMPLATE_claude/commands/context/*.md; do
  [ -f "$src" ] || continue
  subdir=$(echo "$src" | grep -o 'agents/context\|commands/context')
  dest=".claude/${subdir}/$(basename $src)"
  if [ ! -f "$dest" ]; then
    statut="NOUVEAU"
  elif ! cmp -s "$src" "$dest"; then
    statut="MODIFIE"
  else
    statut="INCHANGE"
  fi
  # stocker dans CONTEXT_STATUS associatif : clé = "subdir/basename", valeur = statut
done
```

Pour chaque fichier compare, determiner le statut :

| Statut | Critere |
|--------|---------|
| `NOUVEAU` | Present dans EXPECTED, absent de DEPLOYED |
| `MODIFIE` | Present dans les deux, contenu different |
| `INCHANGE` | Present dans les deux, contenu identique |
| `RELIQUAT` | Present dans DEPLOYED, absent de EXPECTED |

#### Etape d4 — Presenter le rapport

Pour chaque fichier MODIFIE, lire les deux versions et generer une explication courte
(1 ligne max) decrivant ce qui a change (nouvelle regle, section ajoutee, comportement modifie...).

```
Synchronisation depuis github.com/<repo>

  Commit actuel  : abc1234  (synced: 2026-03-01)
  Dernier commit : def5678  (2026-04-16)

  Commandes :
  [+] feature                            ← nouveau
  [~] bugfix    — <explication courte>   ← modifie
  [=] backlog                            ← inchange (x12...)
  [!] old-command                        ← RELIQUAT (absent du nouveau template)

  Agents :
  [~] cdp       — <explication courte>   ← modifie
  [=] code-reviewer                      ← inchange (x7...)
  [!] old-agent                          ← RELIQUAT (absent du nouveau template)

  Contextes (agents/context/ et commands/context/) :
  [~] TEAMMATES_PROTOCOL — <explication courte>   ← modifie
  [=] COMMON, DEV_COMMON, GITHUB, VALIDATION_COMMON, CDP_WORKFLOWS, DEVELOPMENT, QUALITY (7 inchangés)

  Nouveaux   : N
  Modifies   : N
  Inchanges  : N
  Reliquats  : N  ← a supprimer

Actions :
  [A] Tout appliquer (nouveaux + modifies) et supprimer les reliquats
  [B] Appliquer uniquement les nouveaux et modifies (garder les reliquats)
  [C] Annuler
```

#### Etape d5 — Appliquer selon le choix

**Option A ou B — Deployer les fichiers nouveaux et modifies :**

```bash
for src in TEMPLATE_claude/commands/*.md; do
  dest=".claude/commands/$(basename $src)"
  name=$(basename "$src" .md)
  # Préserver les commandes customisées détectées en d1b
  if printf '%s\n' "${CUSTOMIZED_COMMANDS[@]}" | grep -q "^${name}$"; then
    echo "  [C] $(basename $src) — customisé localement, préservé (supprimer pour réinitialiser)"
    continue
  fi
  if ! cmp -s "$src" "$dest" 2>/dev/null; then
    cp "$src" "$dest"
    echo "  ✓ $(basename $src) mis a jour"
  fi
done

for src in TEMPLATE_claude/agents/*.md; do
  dest=".claude/agents/$(basename $src .md).template.md"
  if ! cmp -s "$src" "$dest" 2>/dev/null; then
    cp "$src" "$dest"
    echo "  ✓ agents/$(basename $src .md).template.md mis a jour"
  fi
done

# Contextes partagés — copier fichier par fichier pour reporter les changements
for src in TEMPLATE_claude/agents/context/*.md TEMPLATE_claude/commands/context/*.md; do
  [ -f "$src" ] || continue
  subdir=$(echo "$src" | grep -o 'agents/context\|commands/context')
  dest=".claude/${subdir}/$(basename $src)"
  mkdir -p ".claude/${subdir}"
  if ! cmp -s "$src" "$dest" 2>/dev/null; then
    cp "$src" "$dest"
    echo "  ✓ ${subdir}/$(basename $src) mis a jour"
  fi
done
```

**Etape systematique — Appliquer les placeholders sur TOUS les fichiers deployes :**

Scanner l'integralite de `.claude/commands/*.md`, `.claude/commands/context/*.md`, `.claude/agents/*.template.md` et `.claude/agents/context/*.md` et appliquer
la procedure "Application des placeholders" (section 4 ci-dessus) sur tous les fichiers,
en lisant les valeurs depuis `.claude/project-config.json` existant.

> **Exclure `init-project.md`** de cette substitution (contient des `{VAR}` d'exemple
> dans ses blocs de code — les remplacer le corromprait).

**Etape additionnelle — Synchroniser init-project.md lui-même :**

`init-project.md` n'est pas dans `TEMPLATE_claude/commands/` (c'est le bootstrapper).
Le fetcher depuis la racine du repo GitHub pour que les projets existants reçoivent
les mises à jour (Message de Fin, corrections de bugs, etc.) :

```bash
gh api repos/$TEMPLATE_REPO/contents/init-project.md \
  --jq '.content' | base64 -d > .claude/commands/init-project.md
echo "  ✓ .claude/commands/init-project.md mis à jour (depuis racine repo)"
```

**Option A uniquement — Supprimer les reliquats :**

```bash
# Supprimer les commandes reliquats
for name in $DEPLOYED_COMMANDS; do
  [[ "$name" == "init-project" ]] && continue  # bootstrapper — jamais supprimé
  if ! echo "$EXPECTED_COMMANDS" | grep -q "^${name}$"; then
    rm ".claude/commands/${name}.md"
    echo "  ✗ .claude/commands/${name}.md supprime (reliquat)"
  fi
done

# Supprimer les agents reliquats
for name in $DEPLOYED_AGENTS; do
  if ! echo "$EXPECTED_AGENTS" | grep -q "^${name}$"; then
    rm ".claude/agents/${name}.template.md"
    echo "  ✗ .claude/agents/${name}.template.md supprime (reliquat)"
  fi
done
```

#### Etape d5b — Détection de dérive template/projet

Exécutée à **chaque sync** pour détecter les dérives dans les deux sens.
Silencieuse si aucun `*.md` compagnon n'existe ou si tout est propre.

**Détection des fichiers compagnons :**

```bash
COMPANIONS=()
for tmpl in .claude/agents/*.template.md; do
  base=$(basename "$tmpl" .template.md)
  dir=$(dirname "$tmpl")
  companion="$dir/$base.md"
  [[ -f "$companion" ]] && COMPANIONS+=("$companion")
done
```

Si `COMPANIONS` est vide → sauter cette étape silencieusement.

**Analyse de chaque fichier compagnon :**

Pour chaque `xxx.md`, lire les deux fichiers et détecter les dérives dans les **deux sens** :

| Statut | Critère | Signal |
|--------|---------|--------|
| `IDENTIQUE` | `xxx.md` quasiment identique au template | Duplication inutile — peut être supprimé |
| `DERIVE-TEMPLATE` | Contenu de `xxx.md` couvert par le nouveau template | Template a rattrapé le projet — simplification possible |
| `DERIVE-PROJET` | Contenu ajouté dans `xxx.md` non présent dans le template | Dérive projet — vérifier que c'est intentionnel |
| `PROPRE` | `xxx.md` contient uniquement du contenu spécifique, sans overlap | Aucune action requise |

> **`DERIVE-TEMPLATE`** : une mise à jour du template intègre nativement ce que le projet
> avait customisé → la règle dans `xxx.md` est devenue redondante.
>
> **`DERIVE-PROJET`** : `xxx.md` a grossi depuis la dernière sync → vérifier que les ajouts
> sont intentionnels et non des duplications accidentelles.

**Rapport (affiché uniquement si au moins un fichier non-PROPRE) :**

```
Analyse drift template/projet :

  Commandes :
  [=] feature.md      — identique au template → peut être supprimé
  [↓] bugfix.md       — le template couvre maintenant "règle X" → simplification possible
  [↑] deploy.md       — 2 sections ajoutées depuis la dernière sync → vérifier intentionnel
  [*] backlog.md      — propre (contenu projet uniquement)

  Agents :
  [↓] cdp.md          — le template couvre maintenant "phase CLARIFICATION" → simplification possible
  [↑] qa.md           — 1 section ajoutée → vérifier intentionnel

  [=] N identiques  [↓] N simplifiables  [↑] N à vérifier  [*] N propres
```

**Actions proposées :**

```
  [N] Nettoyer automatiquement (supprimer IDENTIQUES, extraire DERIVE-PROJET vers xxx.md épuré)
  [I] Inspecter fichier par fichier
  [S] Ignorer — continuer sans modification
```

**Option N — Nettoyage automatique :**

Pour chaque fichier `IDENTIQUE` :
```bash
rm "$companion"
echo "  ✗ $(basename $companion) supprimé (identique au template)"
```

Pour chaque fichier `DERIVE-PROJET` ou `MIXTE` :
- Lire `xxx.md` (agent projet) et `xxx.template.md` (agent template)
- Identifier les blocs présents dans `xxx.md` mais absents du template
  (diff sémantique : sections ajoutées, règles supplémentaires, surcharges)
- Réécrire `xxx.md` avec uniquement ces blocs
- Confirmer : `"  ✓ $(basename $companion) — N blocs projet conservés"`

Pour chaque fichier `DERIVE-TEMPLATE` :
- Afficher la règle/section devenue redondante
- Proposer de la retirer de `xxx.md` avec confirmation

**Option I — Fichier par fichier :**

Pour chaque fichier non-PROPRE, afficher le diff annoté et proposer l'action :
```
[xxx.md] — dérive détectée

  [↓] Section "Règle X" — couverte par le template mis à jour → retirer ?
  [↑] Section "Règle Y" — ajout projet non présent dans le template → conserver ?

  [R] Retirer les redondances  [C] Conserver tel quel  [E] Editer manuellement
```

> Le système fonctionne correctement quelle que soit l'action choisie.
> La dérive est un signal de maintenance, pas une erreur bloquante.

#### Etape d6 — Mettre à jour le bloc TEAMLEADER_PROTOCOL dans CLAUDE.md

Le bloc entre `<!-- BEGIN TEAMLEADER_PROTOCOL -->` et `<!-- END TEAMLEADER_PROTOCOL -->` est maintenu par le template.  
Le remplacer par le contenu actuel de `TEMPLATE_claude/CLAUDE_TEMPLATE.md` (sans toucher au reste du `CLAUDE.md` projet) :

```bash
# Extraire le bloc du template vers un fichier temporaire
awk '/<!-- BEGIN TEAMLEADER_PROTOCOL/,/<!-- END TEAMLEADER_PROTOCOL -->/' \
  TEMPLATE_claude/CLAUDE_TEMPLATE.md > .claude/.protocol-block.tmp

# Remplacer le bloc dans CLAUDE.md
awk '
  /<!-- BEGIN TEAMLEADER_PROTOCOL/ { in_block=1; while((getline line < ".claude/.protocol-block.tmp") > 0) print line; next }
  /<!-- END TEAMLEADER_PROTOCOL -->/ { in_block=0; next }
  !in_block { print }
' CLAUDE.md > CLAUDE.md.tmp && mv CLAUDE.md.tmp CLAUDE.md

rm -f .claude/.protocol-block.tmp
echo "✓ CLAUDE.md — bloc TEAMLEADER_PROTOCOL mis à jour"
```

> Si `CLAUDE.md` ne contient pas encore les marqueurs (projet initialisé avant cette version du template) :
> ajouter manuellement la section entre marqueurs ou relancer `/init-project` option a (réinitialisation).

#### Etape d6b — Synchroniser settings.json

Déployer `TEMPLATE_claude/settings.json` dans `.claude/settings.json` si absent :

```bash
if [ ! -f .claude/settings.json ]; then
  cp TEMPLATE_claude/settings.json .claude/settings.json
  echo "✓ .claude/settings.json créé"
else
  echo "  .claude/settings.json déjà présent — non écrasé"
fi
```

#### Etape d7 — Vérifier et créer les labels GitHub de phase

S'assurer que les labels de suivi existent sur le repo (même commande que l'init, idempotent) :

```bash
gh label create "PLANNING"  --color "0075ca" --description "Issue en cours de planification" --force
gh label create "EN COURS"  --color "e4e669" --description "Issue en cours de développement" --force
gh label create "EN REVIEW" --color "d876e3" --description "Issue en cours de revue de code"  --force
gh label create "EN QA"     --color "f9d0c4" --description "Issue en cours de tests QA"       --force
gh label create "DONE"      --color "0e8a16" --description "Issue livrée et validée"           --force
```

#### Etape d8 — Rapport final

```
Synchronisation terminee.

  Commandes mises a jour            : N
  Agents mis a jour                 : N
  Reliquats supprimes               : N
  CLAUDE.md bloc TEAMLEADER_PROTOCOL : mis à jour
  Labels GitHub                     : vérifiés (PLANNING, EN COURS, EN REVIEW, EN QA, DONE)

  Fichiers PROJET preserves (non touches) :
    ✓ CLAUDE.md (hors bloc TEAMLEADER_PROTOCOL)
    ✓ .claude/project-config.json
    ✓ .claude/memory/
    ✓ .claude/agents/dev-*.md
```
