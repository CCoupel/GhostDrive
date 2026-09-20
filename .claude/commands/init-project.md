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
| **COMMANDES** | `.claude/commands/*.md` | Depuis `TEMPLATE_claude/commands/*.md`, déployé en `*.md` — gitignore, pas de compagnon |
| **AGENTS TEMPLATE** | `.claude/agents/*.template.md` | Depuis `TEMPLATE_claude/agents/*.md`, déployé en `*.template.md` — gitignore |
| **CONTEXTES PARTAGES** | `.claude/{commands,agents}/context/*.template.md` + compagnon `.claude/{commands,agents}/context/*.md` optionnel | Depuis `TEMPLATE_claude/{commands,agents}/context/*.md` — meme convention template/compagnon que les agents |
| **PROJET** | `.claude/CLAUDE.md`, `project-config.json`, `memory/`, `agents/dev-*.md`, compagnons `agents/*.md` et `context/*.md` | Trackes dans git, jamais ecrases |

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

# Reference = le commit du dernier TAG (pas le HEAD de branche) : un commit
# non tagge est une version intermediaire non stabilisee, elle ne doit ni
# etre proposee en mise a jour ni etre deployee.
LATEST_TAG=$(gh api repos/$TEMPLATE_REPO/tags --jq '.[0].name // empty')
LATEST_COMMIT=$(gh api repos/$TEMPLATE_REPO/tags --jq '.[0].commit.sha // empty')

if [ "$KNOWN_COMMIT" = "$LATEST_COMMIT" ]; then
  echo "Template deja a jour ($LATEST_TAG - $LATEST_COMMIT)"
  # Continuer quand meme (fichiers peuvent etre absents si gitignores)
fi
```

#### 3. Fetcher TEMPLATE_claude/ depuis GitHub

```bash
# Fetch au commit du tag resolu a l'etape precedente (pas la branche) : les
# fichiers deployes viennent toujours d'une version stabilisee et taggee.
gh api repos/$TEMPLATE_REPO/git/trees/$LATEST_COMMIT?recursive=1 \
  --jq '.tree[] | select(.type=="blob") | .path' \
  | grep -E '^TEMPLATE_claude/' \
  | while read FILE; do
      mkdir -p "$(dirname $FILE)"
      gh api "repos/$TEMPLATE_REPO/contents/$FILE?ref=$LATEST_COMMIT" \
        --jq '.content' | base64 -d > "$FILE"
      echo "  ✓ $FILE"
    done
```

#### 4. Deployer dans .claude/

Les commandes sont déployées en `*.md` — directement invocables comme `/xxx`, jamais éditées manuellement.
Les agents sont déployés en `*.template.md` — les adaptations projet vont dans des fichiers `*.md` compagnons (voir COMMON.md §13).
Les contextes partagés (`context/COMMON.md`, `context/GITHUB.md`...) suivent la même convention que les agents : déployés en `*.template.md`, avec un compagnon `*.md` optionnel pour les adaptations projet (voir COMMON.md §13).

```bash
mkdir -p .claude/commands .claude/agents .claude/agents/context .claude/commands/context

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

# Contextes partagés : même convention que les agents — déployé en *.template.md,
# compagnon *.md optionnel pour les adaptations projet (jamais créé automatiquement)
for src in TEMPLATE_claude/agents/context/*.md; do
  dest=".claude/agents/context/$(basename $src .md).template.md"
  cp "$src" "$dest"
  echo "  ✓ $dest"
done
for src in TEMPLATE_claude/commands/context/*.md; do
  dest=".claude/commands/context/$(basename $src .md).template.md"
  cp "$src" "$dest"
  echo "  ✓ $dest"
done
```

#### 5. Mettre a jour TEMPLATE_claude/.template-source.json

```bash
TODAY=$(date +%Y-%m-%d)
cat > TEMPLATE_claude/.template-source.json <<EOF
{
  "repo": "$TEMPLATE_REPO",
  "branch": "$TEMPLATE_BRANCH",
  "tag": "$LATEST_TAG",
  "commit": "$LATEST_COMMIT",
  "synced_at": "$TODAY"
}
EOF
echo "✓ TEMPLATE_claude/.template-source.json mis a jour ($LATEST_TAG - $LATEST_COMMIT)"
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

## Migration du schema `infrastructure` (deploy → environments[])

Independante de la migration de structure ci-dessus (v1/v2 → v3) — s'applique des que
`HAS_CONFIG=yes`, quel que soit le chemin emprunte ensuite (Reinitialisation ou Migration
v1/v2 → v3). Avant l'introduction du modele BUILD/PUBLISH/DEPLOY a 3 phases (voir
`TEMPLATE_claude/agents/deploy.md`), `infrastructure` portait un seul mecanisme de deploiement
(`infrastructure.deploy`, string) commun a tous les environnements. Il est remplace par
`infrastructure.environments[]` (tableau ordonne, mecanisme publish/deploy propre a chaque
environnement — voir Etape 8). Detecter et convertir automatiquement :

```bash
OLD_DEPLOY=$(jq -r '.infrastructure.deploy // empty' .claude/project-config.json 2>/dev/null)
HAS_ENVIRONMENTS=$(jq -e '.infrastructure.environments' .claude/project-config.json >/dev/null 2>&1 && echo yes || echo no)

# Normaliser vers l'enum utilise par la generation des fichiers d'environnement (section
# "3bis. Fichiers d'environnement") — l'ancien champ etait du texte libre (ex: "docker").
case "$OLD_DEPLOY" in
  docker|docker-compose) MECH=docker-compose ;;
  kubernetes|k8s|helm)   MECH=kubernetes ;;
  serverless)            MECH=serverless ;;
  vps|bare-metal)        MECH=vps ;;
  paas)                  MECH=paas ;;
  cloud-run|cloudrun|app-engine) MECH=cloud-run ;;
  *)                     MECH=docker-compose ;;  # repli par defaut, signale a l'utilisateur ci-dessous
esac
```

Si `OLD_DEPLOY` non vide ET `HAS_ENVIRONMENTS=no` → config au format precedent, informer et convertir :

```
Configuration de deploiement au format precedent detectee (infrastructure.deploy: "<OLD_DEPLOY>").
Le nouveau modele separe BUILD (compilation) / PUBLISH (mise a disposition, mecanisme par
environnement) / DEPLOY (installation) — voir TEMPLATE_claude/agents/deploy.md.

Conversion automatique proposee :
  QUALIF : publish.mode = promote      (reutilise l'artefact tel quel, zero rebuild)
  PROD   : publish.mode = rebuild-ci   (merge + tag officiel, rebuild deterministe via CI)
  deploy.mechanism (les deux environnements) = "<MECH>" (normalise depuis "<OLD_DEPLOY>")

Convertir maintenant ? [O/n] — Non bloquant : repondre "n" laisse infrastructure.deploy en
l'etat (les commandes /build, /publish <env>, /deploy <env> nouvellement synchronisees ne
fonctionneront pas correctement tant que la conversion n'est pas faite).
```

Si confirme :

```bash
jq --arg mech "$MECH" '
  .infrastructure.environments = [
    { "name": "QUALIF", "order": 1,
      "publish": { "mode": "promote", "target": "build/qualif_v{X.Y.Z}/" },
      "deploy":  { "mechanism": $mech } },
    { "name": "PROD", "order": 2,
      "publish": { "mode": "rebuild-ci", "trigger": "git-tag", "pipeline": ".github/workflows/release.yml" },
      "deploy":  { "mechanism": $mech } }
  ] | del(.infrastructure.deploy)
' .claude/project-config.json > /tmp/project-config.json.tmp \
  && mv /tmp/project-config.json.tmp .claude/project-config.json

echo "✓ infrastructure.environments genere depuis infrastructure.deploy=\"$OLD_DEPLOY\" (mecanisme normalise : $MECH)."
echo "  Verifier/ajuster manuellement les cibles (docker-compose.*.yml, chart Helm, pipeline CI) si besoin."

# Generer les fichiers d'environnement correspondants (sinon agents/deploy.md reference des
# fichiers inexistants) — meme logique que la section "3bis. Fichiers d'environnement"
# ci-dessous, executee ici immediatement apres la conversion de schema.
```

> Executer ensuite la generation des fichiers d'environnement (section "3bis. Fichiers
> d'environnement" ci-dessous) pour ces deux environnements — indispensable, `agents/deploy.md`
> reference desormais `.claude/agents/environments/{publish,deploy}.{qualif,prod}.template.md`
> a chaque tache PUBLISH/DEPLOY.

> Cette conversion ne devine que le mecanisme de deploiement (repris tel quel pour les deux
> environnements) et le mecanisme de publication par defaut (`promote` QUALIF /
> `rebuild-ci` PROD) — elle ne peut pas deviner des cibles specifiques (fichier
> docker-compose different par environnement, chemin Helm...) : les signaler comme a
> verifier manuellement dans le rapport de fin d'execution (Option d, Etape d8, ou message de
> fin en flux Migration v1/v2 → v3).

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

### Etape M1c — Migration : renommer les contextes legacy `context/X.md` → `context/X.template.md`

> ⚠ **SCOPE STRICT** : uniquement les fichiers directement dans `.claude/agents/context/` et
> `.claude/commands/context/` — ne renomme jamais un fichier qui est déjà un compagnon `.template.md`.

Avant l'introduction du pattern template/compagnon pour `context/`, ces fichiers étaient déployés
en `context/X.md` (gitignorés en bloc, sans compagnon). Les renommer en `context/X.template.md` —
sauf si un `.template.md` existe déjà pour ce nom (déjà migré).

```bash
for f in .claude/agents/context/*.md .claude/commands/context/*.md; do
  [[ -f "$f" ]] || continue
  dest="${f%.md}.template.md"
  [[ -f "$dest" ]] && continue   # déjà migré
  mv "$f" "$dest"
  echo "  ✓ migration contexte : $(basename $f) → $(basename $dest)"
done
```

### Etape M2 — Nettoyer .claude/ des anciens fichiers template

```bash
git rm --cached .claude/commands/*.template.md 2>/dev/null || true
git rm --cached .claude/commands/*.md 2>/dev/null || true
# Note : après migration v3, les commandes sont en *.md (gitignored)
git rm --cached .claude/agents/context/*.template.md 2>/dev/null || true
git rm --cached .claude/commands/context/*.template.md 2>/dev/null || true
git rm --cached .claude/agents/*.template.md 2>/dev/null || true
git rm --cached .claude/agents/*.md 2>/dev/null || true
git rm --cached -r .claude/templates/ 2>/dev/null || true
git rm --cached .claude/.template-source.json 2>/dev/null || true
git rm --cached .claude/INITIALIZATION.md 2>/dev/null || true
git rm --cached .claude/CLAUDE_TEMPLATE.md 2>/dev/null || true
git rm --cached .claude/gitignore-for-projects 2>/dev/null || true
```

> Seuls les `*.template.md` de `context/` sont désindexés — les compagnons `context/*.md`
> (issus de la migration M1c ou déjà présents) restent/redeviennent trackés, comme pour les
> compagnons `agents/*.md`.

### Etape M3 — Appliquer le .gitignore

```bash
cp TEMPLATE_claude/gitignore-for-projects .gitignore
# (merger avec le .gitignore existant si necessaire)
```

### Etape M4 — Commiter la migration

M2 a désindexé les `*.md` agents via `git rm --cached`. Re-tracker les fichiers compagnons agents
et contextes qui existent sur disque (customisations projet à préserver) avant de commiter.

```bash
git add .gitignore TEMPLATE_claude/.template-source.json
# Re-tracker les companions agents désindexés par M2 (hors dev-* et *.template.md)
for f in .claude/agents/*.md; do
  [[ -f "$f" ]] || continue
  name=$(basename "$f")
  [[ "$name" == dev-*.md ]] && continue  # dev-* gérés séparément
  git add "$f" 2>/dev/null && echo "  ✓ re-tracking companion agent : $name"
done
# Re-tracker les companions context/ (issus de M1c ou déjà présents), hors *.template.md
for f in .claude/agents/context/*.md .claude/commands/context/*.md; do
  [[ -f "$f" ]] || continue
  git add "$f" 2>/dev/null && echo "  ✓ re-tracking companion contexte : $(basename $f)"
done
git commit -m "chore(claude): Migrate to v3 template architecture (TEMPLATE_claude/)

- TEMPLATE_claude/ fetched from GitHub, gitignored at root
- .claude/ now contains only project-specific files
- Untracked legacy template files from .claude/
- Agent and context companion files re-tracked after cache cleanup"
```

### Etape M5 — Rapport

```
Migration → v3 terminee.

  TEMPLATE_claude/ fetche depuis CCoupel/claude_project_template
  Commandes deployees dans .claude/commands/
  Agents template deployes dans .claude/agents/
  Contextes partages deployes dans .claude/agents/context/ et .claude/commands/context/

  Fichiers PROJET preserves :
    ✓ .claude/CLAUDE.md
    ✓ .claude/project-config.json (hors migration ponctuelle du schema `infrastructure`, voir
      section "Migration du schema `infrastructure`" — appliquee avant cette etape si besoin)
    ✓ .claude/memory/
    ✓ .claude/agents/dev-*.md (si presents)
    ✓ .claude/agents/*.md et context/*.md compagnons (si presents)
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

## Etape 8 : Environnements et Deploiement

```
9. Comment deploies-tu ton application ? (mecanisme d'installation par defaut)
   a) Docker / Docker Compose
   b) Kubernetes / Helm
   c) Serverless (AWS Lambda, Vercel, Netlify)
   d) VPS / Bare metal
   e) PaaS (Heroku, Railway, Render)
   f) Cloud Run / App Engine

9bis. Quels environnements de release utilises-tu, dans l'ordre de promotion ?
   a) QUALIF puis PROD (defaut)
   b) DEV puis QUALIF puis PROD
   c) QUALIF puis PRE-PROD puis PROD
   d) Personnalise — lister les noms, dans l'ordre de promotion
```

Chaque environnement declare dans `infrastructure.environments[]` recoit un mecanisme
`publish.mode` :
- **`promote`** (defaut pour tout environnement sauf le dernier) — reutilise tel quel
  l'artefact du BUILD (ou de l'environnement precedent), zero rebuild.
- **`rebuild-ci`** (defaut pour le dernier environnement de la chaine, generalement PROD) —
  merge + tag officiel, declenche un rebuild deterministe via la CI choisie a l'Etape 7.

Le mecanisme `deploy.mechanism` de chaque environnement reprend par defaut la reponse a la
question 9 (docker-compose / helm / serverless / vps / paas / cloud-run) — proposer a
l'utilisateur de le personnaliser par environnement uniquement s'il le demande explicitement.

> Seule la chaine QUALIF -> PROD est cablee dans l'orchestration CDP (voir
> `agents/cdp.template.md`, section "Points de Validation Utilisateur"). Un environnement
> supplementaire (DEV, PRE-PROD...) est genere dans la config mais se publie/deploie
> manuellement via `/publish <env>` et `/deploy <env>`, hors flux CDP automatise — le signaler
> a l'utilisateur s'il en ajoute.
>
> Cette etape ne cree que la configuration et les procedures generiques (section 3bis) — jamais
> les artefacts d'infra eux-memes (Dockerfile, `docker-compose.<env>.yml`, chart Helm...). Ceux-ci
> sont scaffoldes a la demande par l'agent `infra`, au premier deploiement sur chaque
> environnement (voir `agents/infra.md` section "Mode Validation", etape 0) — pas a l'init.

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
  "src_dir": "<SRC_DIR>",
  "version_file": "<VERSION_FILE>",
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
    "environments": [
      {
        "name": "QUALIF",
        "order": 1,
        "publish": { "mode": "promote", "target": "build/qualif_v{X.Y.Z}/" },
        "deploy":  { "mechanism": "docker-compose", "target": "docker-compose.qualif.yml" }
      },
      {
        "name": "PROD",
        "order": 2,
        "publish": { "mode": "rebuild-ci", "trigger": "git-tag", "pipeline": ".github/workflows/release.yml" },
        "deploy":  { "mechanism": "docker-compose", "target": "docker-compose.prod.yml" }
      }
    ]
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
    "typecheck": "<TYPECHECK_CMD>",
    "coverage": "<COVERAGE_CMD>"
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
| `commands.coverage` | Stack : `go test -cover ./...` / `npm run test -- --coverage` / `pytest --cov` |
| `src_dir` | Detection Etape 0 (repertoire source principal) ou stack par defaut : `src`, `cmd`... |
| `version_file` | Fichier source de verite de la version (ex: `package.json`, `config.json`, `VERSION`) |
| `infrastructure.environments` | Defaut `[QUALIF, PROD]` (Etape 8, question 9bis) ; `publish.mode` = `promote` pour tous sauf le dernier (`rebuild-ci`) ; `deploy.mechanism` reprend la reponse a la question 9 pour chaque environnement, sauf personnalisation explicite |
| Nom de fichier des environnements | `infrastructure.environments[].name` normalise : minuscules, espaces/underscores → tirets (ex. `PRE-PROD` → `pre-prod`) — utilise pour `.claude/agents/environments/{publish,deploy}.<nom>.template.md` (section 3bis) |

### 2. Agents dev-*

| Stack | Template Source | Destination |
|-------|-----------------|-------------|
| Go | `TEMPLATE_claude/templates/dev-backend-go.md` | `.claude/agents/dev-backend.template.md` |
| Node.js | `TEMPLATE_claude/templates/dev-backend-node.md` | `.claude/agents/dev-backend.template.md` |
| Python | `TEMPLATE_claude/templates/dev-backend-python.md` | `.claude/agents/dev-backend.template.md` |
| React | `TEMPLATE_claude/templates/dev-frontend-react.md` | `.claude/agents/dev-frontend.template.md` |
| Vue.js | `TEMPLATE_claude/templates/dev-frontend-vue.md` | `.claude/agents/dev-frontend.template.md` |
| ESP32 | `TEMPLATE_claude/templates/dev-firmware-esp32.md` | `.claude/agents/dev-firmware.template.md` |
| Plugin (toute plateforme) | `TEMPLATE_claude/templates/dev-plugin.md` | `.claude/agents/dev-plugin.template.md` |

> Même convention que les agents génériques (§ précédent) : déployé en `.template.md`,
> avec un compagnon `.md` optionnel pour les adaptations projet.

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

### 3bis. Fichiers d'environnement (publish/deploy par environnement)

Pour chaque entree d'`infrastructure.environments[]` (generee a l'Etape 8 ou par la migration
de schema, voir section "Migration du schema `infrastructure`") : copier la source generique
correspondante depuis `TEMPLATE_claude/templates/environments/` vers
`.claude/agents/environments/{publish,deploy}.<env>.template.md`, ou `<env>` est le nom de
l'environnement normalise (minuscules, espaces/underscores → tirets — ex. `PRE-PROD` →
`pre-prod`).

Selection de la source, par mecanisme (pas par nom d'environnement) :

| Champ | Valeur | Source |
|-------|--------|--------|
| `publish.mode` | `promote` | `TEMPLATE_claude/templates/environments/publish-promote.md` |
| `publish.mode` | `rebuild-ci` | `TEMPLATE_claude/templates/environments/publish-rebuild-ci.md` |
| `deploy.mechanism` | `docker-compose` | `TEMPLATE_claude/templates/environments/deploy-docker-compose.md` |
| `deploy.mechanism` | `kubernetes` ou `helm` | `TEMPLATE_claude/templates/environments/deploy-kubernetes-helm.md` |
| `deploy.mechanism` | `serverless` | `TEMPLATE_claude/templates/environments/deploy-serverless.md` |
| `deploy.mechanism` | `vps` | `TEMPLATE_claude/templates/environments/deploy-vps.md` |
| `deploy.mechanism` | `paas` | `TEMPLATE_claude/templates/environments/deploy-paas.md` |
| `deploy.mechanism` | `cloud-run` | `TEMPLATE_claude/templates/environments/deploy-cloud-run.md` |

Detection best-effort des variables deja referencees dans les fichiers existants du projet
(noms uniquement — **aucune valeur n'est jamais lue ni copiee**, seuls les motifs `${VAR}` et
`secrets.VAR` sont extraits) :

```bash
scan_var_names() {
  [ "$#" -eq 0 ] && return
  grep -ohE '\$\{[A-Za-z_][A-Za-z0-9_]*|\$[A-Za-z_][A-Za-z0-9_]*|secrets\.[A-Za-z_][A-Za-z0-9_]*' "$@" 2>/dev/null \
    | sed -E 's/^\$\{?//; s/^secrets\.//' | sort -u
}
# Ajoute les noms absents a un .env.example existant (touch si absent), jamais de valeur,
# jamais de ligne existante touchee. Ecrit un en-tete seulement si au moins un nom est ajoute.
merge_env_example() {
  local file="$1" header="$2"; shift 2
  touch "$file"
  local added=0
  for v in "$@"; do
    [ -n "$v" ] || continue
    grep -q "^${v}=" "$file" 2>/dev/null && continue
    if [ "$added" -eq 0 ]; then printf '\n%s\n' "$header" >> "$file"; fi
    echo "${v}=" >> "$file"
    added=$((added + 1))
  done
  echo "$added"
}

# Global (fichiers sans suffixe d'environnement) -> .env.example a la racine
GLOBAL_FILES=$(ls docker-compose.yml docker-compose.yaml Dockerfile .github/workflows/*.y*ml 2>/dev/null)
GLOBAL_VARS=$(scan_var_names $GLOBAL_FILES)
if [ -n "$GLOBAL_VARS" ]; then
  N=$(merge_env_example .env.example \
    "# Variables detectees dans les fichiers existants du projet (docker-compose.yml, workflows...) — a verifier manuellement, aucune valeur lue ni copiee :" \
    $GLOBAL_VARS)
  [ "$N" -gt 0 ] && echo "  ✓ .env.example (racine) enrichi de $N variable(s) detectee(s) — a verifier manuellement"
fi
```

```bash
mkdir -p .claude/agents/environments
jq -c '.infrastructure.environments[]' .claude/project-config.json | while read -r ENV; do
  NAME=$(echo "$ENV" | jq -r '.name')
  ENV_LOWER=$(echo "$NAME" | tr '[:upper:] _' '[:lower:]--')
  PUB_MODE=$(echo "$ENV" | jq -r '.publish.mode')
  DEP_MECH=$(echo "$ENV" | jq -r '.deploy.mechanism')
  PUB_TARGET=$(echo "$ENV" | jq -r '.publish.target // .publish.trigger // ""')
  CI_PIPELINE=$(echo "$ENV" | jq -r '.publish.pipeline // ""')
  DEP_TARGET=$(echo "$ENV" | jq -r '.deploy.target // ""')

  case "$PUB_MODE" in
    promote)     PUB_SRC="TEMPLATE_claude/templates/environments/publish-promote.md" ;;
    rebuild-ci)  PUB_SRC="TEMPLATE_claude/templates/environments/publish-rebuild-ci.md" ;;
  esac
  case "$DEP_MECH" in
    docker-compose)        DEP_SRC="TEMPLATE_claude/templates/environments/deploy-docker-compose.md" ;;
    kubernetes|helm)       DEP_SRC="TEMPLATE_claude/templates/environments/deploy-kubernetes-helm.md" ;;
    serverless)            DEP_SRC="TEMPLATE_claude/templates/environments/deploy-serverless.md" ;;
    vps)                   DEP_SRC="TEMPLATE_claude/templates/environments/deploy-vps.md" ;;
    paas)                  DEP_SRC="TEMPLATE_claude/templates/environments/deploy-paas.md" ;;
    cloud-run)             DEP_SRC="TEMPLATE_claude/templates/environments/deploy-cloud-run.md" ;;
  esac

  sed -e "s|{ENV_NAME_LOWER}|${ENV_LOWER}|g" -e "s|{ENV_NAME}|${NAME}|g" \
      -e "s|{PUBLISH_TARGET}|${PUB_TARGET}|g" -e "s|{CI_PIPELINE}|${CI_PIPELINE}|g" \
      "$PUB_SRC" > ".claude/agents/environments/publish.${ENV_LOWER}.template.md"
  sed -e "s|{ENV_NAME_LOWER}|${ENV_LOWER}|g" -e "s|{ENV_NAME}|${NAME}|g" \
      -e "s|{PUBLISH_TARGET}|${PUB_TARGET}|g" -e "s|{DEPLOY_TARGET}|${DEP_TARGET}|g" \
      "$DEP_SRC" > ".claude/agents/environments/deploy.${ENV_LOWER}.template.md"
  echo "  ✓ fichiers d'environnement generes pour $NAME (publish: $PUB_MODE, deploy: $DEP_MECH)"

  # Variables attendues (union publish + deploy) -> <env>.env.example, committe, valeurs vides.
  # Le <env>.env reel (valeurs completees) n'est jamais genere automatiquement — voir
  # agents/deploy.md section "Fichiers d'Environnement".
  case "$PUB_MODE" in
    promote)     PUB_VARS="REGISTRY_USER REGISTRY_PASSWORD" ;;
    rebuild-ci)  PUB_VARS="" ;;  # secrets geres cote CI, pas dans .env local
  esac
  case "$DEP_MECH" in
    docker-compose) DEP_VARS="REGISTRY_USER REGISTRY_PASSWORD SSH_KEY_PATH" ;;
    kubernetes|helm) DEP_VARS="KUBE_CONTEXT KUBECONFIG" ;;
    serverless)     DEP_VARS="AWS_PROFILE VERCEL_TOKEN NETLIFY_AUTH_TOKEN" ;;
    vps)            DEP_VARS="SSH_KEY_PATH" ;;
    paas)           DEP_VARS="HEROKU_API_KEY RAILWAY_TOKEN RENDER_API_KEY" ;;
    cloud-run)      DEP_VARS="GCP_PROJECT GOOGLE_APPLICATION_CREDENTIALS" ;;
  esac

  ENV_EXAMPLE=".claude/agents/environments/${ENV_LOWER}.env.example"
  {
    echo "# Variables specifiques a $NAME — voir agents/deploy.md section \"Fichiers d'Environnement\"."
    echo "# Copier en ${ENV_LOWER}.env (jamais commite) et completer les valeurs."
    for v in $(echo "$PUB_VARS $DEP_VARS" | tr ' ' '\n' | sort -u); do
      [ -n "$v" ] && echo "$v="
    done
  } > "$ENV_EXAMPLE"
  echo "  ✓ $ENV_EXAMPLE genere"

  # Detection par environnement — fichiers dont le nom contient <env> (docker-compose.prod.yml,
  # values-prod.yaml...), meme regles que la detection globale ci-dessus.
  PER_ENV_FILES=$(find . -maxdepth 3 \( -iname "*${ENV_LOWER}*.yml" -o -iname "*${ENV_LOWER}*.yaml" \) \
    -not -path "*/node_modules/*" -not -path "*/TEMPLATE_claude/*" -not -path "*/.git/*" \
    -not -path "*/build/*" 2>/dev/null)
  PER_ENV_VARS=$(scan_var_names $PER_ENV_FILES)
  if [ -n "$PER_ENV_VARS" ]; then
    N=$(merge_env_example "$ENV_EXAMPLE" \
      "# Variables detectees dans les fichiers existants pour $NAME — a verifier manuellement, aucune valeur lue ni copiee :" \
      $PER_ENV_VARS)
    [ "$N" -gt 0 ] && echo "  ✓ $ENV_EXAMPLE enrichi de $N variable(s) detectee(s) pour $NAME"
  fi
done
```

> Compagnons optionnels `publish.<env>.md` / `deploy.<env>.md` : jamais generes automatiquement,
> crees manuellement par le projet pour ses propres adaptations (memes conventions que les
> compagnons d'agents, voir § precedent).
>
> `<env>.env.example` est committe (noms de variables, valeurs vides) ; `<env>.env` (valeurs
> reelles) est gitignore et n'est jamais cree automatiquement — le projet le cree manuellement
> a partir du `.example`. Meme convention pour `.env`/`.env.example` a la racine (variables
> globales, applicatif ET infra, communes a tous les environnements) : `/init-project` le
> cree/enrichit uniquement s'il detecte des variables referencees dans des fichiers existants
> (ci-dessus) — jamais un fichier vide invente sans raison — et ne gere jamais `.env` (valeurs
> reelles), a la charge du projet.

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
COVERAGE_CMD=$(jq -r '.commands.coverage // ""'      .claude/project-config.json)
PLUGIN_PLATFORM=$(jq -r '.stack.plugin.platform // ""' .claude/project-config.json)
SRC_DIR=$(jq -r '.src_dir // ""'                     .claude/project-config.json)
VERSION_FILE=$(jq -r '.version_file // ""'           .claude/project-config.json)
REPO_URL=$(git remote get-url origin 2>/dev/null | sed -E 's#^git@([^:]+):#https://\1/#; s#\.git$##')
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
COVERAGE_CMD_ESC=$(escape_sed "$COVERAGE_CMD")
```

Appliquer la substitution sur les fichiers deployes (commandes + agents generiques + contextes partages) :

```bash
for f in .claude/commands/*.md .claude/agents/*.template.md .claude/commands/context/*.template.md .claude/agents/context/*.template.md .claude/agents/environments/*.template.md; do
  [[ -f "$f" ]] || continue
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
    -e "s|{COVERAGE_CMD}|${COVERAGE_CMD_ESC}|g"  \
    -e "s|{PLUGIN_PLATFORM}|${PLUGIN_PLATFORM}|g" \
    -e "s|{SRC_DIR}|${SRC_DIR}|g"                \
    -e "s|{VERSION_FILE}|${VERSION_FILE}|g"      \
    -e "s|{REPO_URL}|${REPO_URL}|g"              \
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
- Backend      : <BACKEND>
- Frontend     : <FRONTEND>
- Database     : <DATABASE>
- CI/CD        : <CICD>
- Environnements : <ENVIRONMENTS>  (ex: QUALIF -> PROD)

Agents generes :
- .claude/agents/dev-backend.template.md
- .claude/agents/dev-frontend.template.md

Commandes disponibles :
- /feature, /bugfix, /hotfix, /refactor
- /review, /qa, /secu
- /build, /publish qualif|prod, /deploy qualif|prod
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

#### Etape d1c — Migration : renommer les contextes legacy `context/X.md` → `context/X.template.md`

Même migration que l'étape M1c (v1/v2 → v3), applicable ici à un projet déjà en v3 mais synchronisé
avant l'introduction du pattern template/compagnon pour `context/`. Sans effet si déjà migré.

```bash
for f in .claude/agents/context/*.md .claude/commands/context/*.md; do
  [[ -f "$f" ]] || continue
  dest="${f%.md}.template.md"
  [[ -f "$dest" ]] && continue   # déjà migré
  mv "$f" "$dest"
  echo "  ✓ migration contexte : $(basename $f) → $(basename $dest)"
done
```

#### Etape d1d — Vérification des fichiers d'environnement

> ⚠ **SCOPE STRICT** : cette étape **crée uniquement les fichiers manquants** — comme
> `dev-backend.template.md` (jamais resynchronisé automatiquement par l'option d, voir note
> d3b), les fichiers `.claude/agents/environments/*.template.md` déjà présents ne sont **jamais**
> modifiés ici, même si leur source `TEMPLATE_claude/templates/environments/` a changé. C'est un
> filet de sécurité (éviter qu'`agents/deploy.md` référence un fichier inexistant), pas une
> synchronisation complète — limitation assumée, à signaler dans le rapport si applicable.

```bash
if jq -e '.infrastructure.environments' .claude/project-config.json >/dev/null 2>&1; then
  MISSING_ENV_FILES=()
  jq -c '.infrastructure.environments[]' .claude/project-config.json | while read -r ENV; do
    NAME=$(echo "$ENV" | jq -r '.name')
    ENV_LOWER=$(echo "$NAME" | tr '[:upper:] _' '[:lower:]--')
    for TASK in publish deploy; do
      f=".claude/agents/environments/${TASK}.${ENV_LOWER}.template.md"
      [[ -f "$f" ]] || MISSING_ENV_FILES+=("$TASK.$ENV_LOWER")
    done
  done
fi
```

Si `MISSING_ENV_FILES[]` non vide → exécuter pour ces seules entrées manquantes la génération
décrite en section "3bis. Fichiers d'environnement" (Génération de la Configuration), puis
informer :

```
⚠ Fichiers d'environnement manquants générés : publish.qualif, deploy.qualif
  (project-config.json déclare ces environnements mais les fichiers n'existaient pas encore —
   vérifier/ajuster les cibles générées, voir "Fichiers d'Environnement" dans agents/deploy.md)
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

# Contextes partagés (agents/context/ et commands/context/) — déployés en *.template.md
# Comparer chaque fichier source avec le fichier déployé
for src in TEMPLATE_claude/agents/context/*.md TEMPLATE_claude/commands/context/*.md; do
  [ -f "$src" ] || continue
  subdir=$(echo "$src" | grep -o 'agents/context\|commands/context')
  dest=".claude/${subdir}/$(basename $src .md).template.md"
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

#### Etape d3b — Comparer la table "Agents Disponibles" (teammates)

> ⚠ **SCOPE STRICT** : cette comparaison (et sa mise à jour en d5d) ne porte que sur le
> *texte* de la table `## Agents Disponibles` dans `CLAUDE.md`. Elle ne crée, ne supprime
> et ne modifie **jamais** `.claude/agents/*.md` ni `.claude/agents/*.template.md` — la liste
> réelle des agents déployés pour le projet n'est jamais changée par cette étape. Un
> `RELIQUAT` ou un `NOUVEAU` ici signale un écart de *documentation*, pas une action sur
> les agents eux-mêmes.

**Calculer les lignes attendues**, selon la stack déjà configurée dans `project-config.json`
(les agents génériques sont toujours attendus, les agents `dev-*` seulement si la stack
correspondante est configurée) :

```bash
BACKEND_LANG=$(jq -r '.stack.backend.language // ""'   .claude/project-config.json)
FRONTEND_LANG=$(jq -r '.stack.frontend.language // ""' .claude/project-config.json)
FRONTEND_FW=$(jq -r '.stack.frontend.framework // ""'  .claude/project-config.json)
PLUGIN_PLATFORM=$(jq -r '.stack.plugin.platform // ""' .claude/project-config.json)
BACKEND_TECH="$BACKEND_LANG"
FRONTEND_TECH="${FRONTEND_FW:-$FRONTEND_LANG}"

# Toujours attendus, independants de la stack
EXPECTED_TEAMMATES=(planner test-writer code-reviewer qa doc-updater deployer security infra)
[[ -n "$BACKEND_LANG"    ]] && EXPECTED_TEAMMATES+=(dev-backend)
[[ -n "$FRONTEND_LANG"   ]] && EXPECTED_TEAMMATES+=(dev-frontend)
[[ -n "$PLUGIN_PLATFORM" ]] && EXPECTED_TEAMMATES+=(dev-plugin)
```

**Lire la table courante** dans le `CLAUDE.md` du projet (section entre `## Agents Disponibles`
et le prochain `---`) et la table source dans `TEMPLATE_claude/CLAUDE_TEMPLATE.md` (même
section, avec `{BACKEND_TECH}`/`{FRONTEND_TECH}`/`{PLUGIN_PLATFORM}` substitués par les
valeurs ci-dessus).

Pour chaque agent, determiner le statut :

| Statut | Critere |
|--------|---------|
| `NOUVEAU` | Dans `EXPECTED_TEAMMATES`, absent de la table du projet |
| `MODIFIE` | Present dans les deux, mais Rôle/Fichier/Spawn different de l'attendu |
| `INCHANGE` | Present dans les deux, ligne identique |
| `RELIQUAT` | Present dans la table du projet, absent de `EXPECTED_TEAMMATES` (stack retiree, ou agent qui n'existe plus dans le template) |

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

  Teammates (table "Agents Disponibles" — documentation uniquement, agents non touchés) :
  [+] dev-plugin                          ← nouveau (stack plugin configurée, absente de la table)
  [~] dev-backend  — tech passée de Node.js à Go
  [!] dev-firmware                        ← RELIQUAT (stack firmware retirée du projet)
  [=] planner, test-writer, code-reviewer, qa, doc-updater, deployer, security, infra (8 inchangés)

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

# Contextes partagés — copier fichier par fichier pour reporter les changements, déployé en *.template.md
for src in TEMPLATE_claude/agents/context/*.md TEMPLATE_claude/commands/context/*.md; do
  [ -f "$src" ] || continue
  subdir=$(echo "$src" | grep -o 'agents/context\|commands/context')
  dest=".claude/${subdir}/$(basename $src .md).template.md"
  mkdir -p ".claude/${subdir}"
  if ! cmp -s "$src" "$dest" 2>/dev/null; then
    cp "$src" "$dest"
    echo "  ✓ ${subdir}/$(basename $dest) mis a jour"
  fi
done
```

**Etape systematique — Appliquer les placeholders sur TOUS les fichiers deployes :**

Scanner l'integralite de `.claude/commands/*.md`, `.claude/commands/context/*.template.md`, `.claude/agents/*.template.md` et `.claude/agents/context/*.template.md` et appliquer
la procedure "Application des placeholders" (section 4 ci-dessus) sur tous les fichiers,
en lisant les valeurs depuis `.claude/project-config.json` existant. Les compagnons `context/*.md`
et `agents/*.md` ne sont pas scannés — ce sont des fichiers projet sans `{VAR}` a substituer.

> **Exclure `init-project.md`** de cette substitution (contient des `{VAR}` d'exemple
> dans ses blocs de code — les remplacer le corromprait).

**Etape additionnelle — Synchroniser init-project.md lui-même :**

`init-project.md` n'est pas dans `TEMPLATE_claude/commands/` (c'est le bootstrapper).
Le fetcher depuis la racine du repo GitHub pour que les projets existants reçoivent
les mises à jour (Message de Fin, corrections de bugs, etc.) :

```bash
gh api "repos/$TEMPLATE_REPO/contents/init-project.md?ref=$LATEST_COMMIT" \
  --jq '.content' | base64 -d > .claude/commands/init-project.md
echo "  ✓ .claude/commands/init-project.md mis à jour (depuis le tag $LATEST_TAG)"
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

#### Etape d5b — Détection de doublons (règles identiques ou couvertes)

Exécutée à **chaque sync** sur les fichiers compagnons `*.md`. Détecte les règles d'un
compagnon devenues redondantes avec le template — un fichier entier ou certaines règles
seulement — pour proposer leur retrait. Ne traite que la redondance ; les règles qui
disent autre chose que le template sur un même sujet relèvent de l'étape d5c (conflits).

**Détection des fichiers compagnons :**

```bash
COMPANIONS=()
for tmpl in .claude/agents/*.template.md .claude/agents/context/*.template.md .claude/commands/context/*.template.md; do
  [[ -f "$tmpl" ]] || continue
  base=$(basename "$tmpl" .template.md)
  dir=$(dirname "$tmpl")
  companion="$dir/$base.md"
  [[ -f "$companion" ]] && COMPANIONS+=("$companion")
done
```

Si `COMPANIONS` est vide → sauter d5b et d5c silencieusement.

**Analyse de chaque fichier compagnon, à la granularité de la règle/section :**

Pour chaque `xxx.md`, comparer avec `xxx.template.md` :

| Statut | Critère | Signal |
|--------|---------|--------|
| `IDENTIQUE` | `xxx.md` quasiment identique au template | Duplication inutile — peut être supprimé |
| `DERIVE-TEMPLATE` | Contenu de `xxx.md` couvert par le nouveau template | Template a rattrapé le projet — simplification possible |
| `MIXTE` | `xxx.md` mélange des règles désormais couvertes par le template et des règles propres au projet | Retirer uniquement les règles redondantes, conserver le reste |
| `PROPRE` | `xxx.md` contient uniquement du contenu spécifique, sans overlap | Rien à retirer ici — passer à l'étape d5c |

> La détection opère à la granularité de la règle/section, pas seulement du fichier
> entier : dès qu'une règle présente dans `xxx.md` se retrouve (littéralement ou en
> substance, **en disant la même chose**) dans `xxx.template.md`, elle peut être retirée
> du compagnon — même si le reste du fichier reste `PROPRE`. Une règle qui traite du même
> sujet mais dit autre chose n'est **pas** un doublon : c'est un conflit potentiel,
> laissé à l'étape d5c.

**Rapport (affiché uniquement si au moins un fichier non-PROPRE) :**

```
Analyse doublons template/projet :

  Commandes :
  [=] feature.md      — identique au template → peut être supprimé
  [↓] bugfix.md       — le template couvre maintenant "règle X" → simplification possible

  Agents :
  [↓] cdp.md          — le template couvre maintenant "phase CLARIFICATION" → simplification possible
  [~] marketing-release.md — règle "diff origin/gh-pages" désormais couverte, reste propre sinon

  Contextes (agents/context/ et commands/context/) :
  [*] COMMON.md (commands) — propre (rien de redondant)

  [=] N identiques  [↓] N simplifiables  [~] N mixtes  [*] N propres (→ d5c)
```

**Actions proposées :**

```
  [N] Nettoyer automatiquement (supprimer IDENTIQUES, retirer les règles couvertes des DERIVE-TEMPLATE/MIXTE)
  [I] Inspecter fichier par fichier
  [S] Ignorer — continuer sans modification
```

**Option N — Nettoyage automatique :**

Pour chaque fichier `IDENTIQUE` :
```bash
rm "$companion"
echo "  ✗ $(basename $companion) supprimé (identique au template)"
```

Pour chaque fichier `DERIVE-TEMPLATE` ou `MIXTE` :
- Lire `xxx.md` et `xxx.template.md`
- Identifier les règles de `xxx.md` qui disent la même chose qu'une règle du template
- Retirer uniquement ces règles redondantes, conserver le reste tel quel
- Confirmer : `"  ✓ $(basename $companion) — N règles redondantes retirées"`

**Option I — Fichier par fichier :**

Pour chaque fichier non-PROPRE, afficher le diff annoté et proposer l'action :
```
[xxx.md] — doublons détectés

  [↓] Section "Règle X" — couverte par le template mis à jour → retirer ?

  [R] Retirer les redondances  [C] Conserver tel quel  [E] Editer manuellement
```

> Le système fonctionne correctement quelle que soit l'action choisie.
> Un doublon est un signal de maintenance, pas une erreur bloquante.

#### Etape d5c — Détection de conflits (règles incohérentes)

Exécutée après d5b, sur le contenu restant des compagnons (fichiers `PROPRE` en entier,
et la part non redondante des fichiers `DERIVE-TEMPLATE`/`MIXTE` une fois les doublons
retirés). Contrairement à d5b (même règle en double), d5c cherche des règles qui portent
sur le **même sujet** que le template mais disent **autre chose** — une incohérence, pas
une simple règle spécifique au projet.

**Analyse de chaque règle restante :**

Pour chaque règle du compagnon, chercher si le template contient une règle sur le même
sujet (même commande, même mécanisme, même chemin, même convention...) :

| Statut | Critère | Signal |
|--------|---------|--------|
| `CONFLIT` | Le compagnon et le template traitent du même sujet mais se contredisent | Incohérence — nécessite un arbitrage |
| `COHERENT` | Le compagnon ajoute du contenu spécifique sans contredire le template | Aucune action requise |

> Exemple de `CONFLIT` : le template fixe le chemin qualif à `build/qualif_v<X.Y.Z>/`,
> le compagnon documente encore `build/qualif/<X.Y.Z>/`. Exemple de `COHERENT` : le
> compagnon ajoute une règle de nommage de branche propre au projet, absente du template
> et qui ne le contredit pas.

**Rapport (affiché uniquement si au moins un conflit détecté) :**

```
Analyse conflits template/projet :

  [X] deploy.md — chemin qualif : template "build/qualif_v<ver>/" vs compagnon "build/qualif/<ver>/"
  [X] cdp.md     — ordonnancement : template "QA parallèle à Review" vs compagnon "QA après Review"

  2 conflits détectés — nécessitent un arbitrage.
```

Si aucun conflit → passer directement à l'étape d5d, sans afficher de rapport.

**Arbitrage — un conflit à la fois :**

```
[deploy.md] Conflit détecté :

  Template  (nouveau)  : "build/qualif_v<X.Y.Z>/"
  Compagnon (projet)   : "build/qualif/<X.Y.Z>/"

  Lequel fait foi pour ce projet ?
  [T] Le template — adapter/retirer la règle du compagnon
  [P] Le compagnon — dérogation projet assumée, conserver telle quelle
  [E] Éditer manuellement
```

- **[T]** : retirer ou réécrire la règle du compagnon pour qu'elle ne contredise plus le
  template — le template s'applique alors sans override.
- **[P]** : conserver la règle du compagnon telle quelle — dérogation projet volontaire,
  pas une erreur. Elle sera re-signalée en `CONFLIT` aux prochaines sync tant qu'elle
  diffère du template ; c'est attendu pour une dérogation assumée.
- **[E]** : ouvrir le fichier pour édition manuelle, ne rien appliquer automatiquement.

Confirmer chaque arbitrage : `"  ✓ $(basename $companion) — conflit '<sujet>' résolu ([T]/[P]/[E])"`

> Un conflit non tranché (utilisateur ferme sans choisir) reste en l'état — il sera
> re-signalé à la prochaine sync.

#### Etape d5d — Mettre à jour la table "Agents Disponibles" dans CLAUDE.md

> ⚠ **SCOPE STRICT** : identique au rappel de l'étape d3b — cette étape réécrit uniquement
> le texte de la table `## Agents Disponibles` dans `CLAUDE.md`. Elle ne touche jamais
> `.claude/agents/*.md` ni `.claude/agents/*.template.md`.

Exécutée quand l'utilisateur choisit **A** ou **B** en d4 (jamais sous **C — Annuler**),
à partir du statut calculé en d3b :

- `NOUVEAU` → ajouter la ligne dans la table (Rôle/Fichier/Spawn attendus)
- `MODIFIE` → remplacer la ligne existante par la version attendue
- `INCHANGE` → ne rien faire
- `RELIQUAT` → retirer la ligne uniquement sous **Option A** ; la conserver sous **Option B**

```bash
# Extraire la table courante (entre le titre et le prochain "---")
awk '/^## Agents Disponibles/,/^---$/' CLAUDE.md > .claude/.teammates-table.tmp
```

Reconstruire la table ligne par ligne selon les statuts ci-dessus, puis réinjecter le
résultat dans `CLAUDE.md` à la même position (même mécanique que l'étape d6 pour le bloc
`TEAMLEADER_PROTOCOL`, mais bornée par le titre `## Agents Disponibles` et le `---` suivant
plutôt que par des marqueurs HTML) :

```bash
rm -f .claude/.teammates-table.tmp
echo "✓ CLAUDE.md — table Agents Disponibles mise à jour"
```

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
  Doublons compagnons retires       : N (etape d5b)
  Conflits compagnons arbitres      : N (etape d5c)
  CLAUDE.md bloc TEAMLEADER_PROTOCOL : mis à jour
  CLAUDE.md table Agents Disponibles : mis à jour (N lignes — documentation uniquement)
  Labels GitHub                     : vérifiés (PLANNING, EN COURS, EN REVIEW, EN QA, DONE)
  Schema infrastructure             : [convertit vers environments[] | deja a jour | inchange (refuse)]

  Fichiers PROJET preserves (non touches) :
    ✓ CLAUDE.md (hors bloc TEAMLEADER_PROTOCOL et hors table Agents Disponibles)
    ✓ .claude/project-config.json (hors migration ponctuelle du schema `infrastructure`, voir
      section "Migration du schema `infrastructure`" — champs projet non touches sinon)
    ✓ .claude/memory/
    ✓ .claude/agents/dev-*.md   (jamais modifiés par la sync de la table Agents Disponibles)
```
