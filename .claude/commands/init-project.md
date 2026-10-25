# Commande /init-project

Initialisation interactive du projet pour configurer l'environnement Claude Code.

> **Documentation complete** : `TEMPLATE_claude/INITIALIZATION.md`

## Declenchement

- **Automatique** : Si `.claude/project-config.json` n'existe pas au demarrage
- **Manuel** : Commande `/init-project` pour reinitialiser ou modifier

## Convention d'interaction

Toute sollicitation de l'utilisateur ci-dessous est posee avec l'outil **`AskUserQuestion`** —
jamais du texte brut listant des lettres dans le chat :

- **2 a 4 options** (contrainte de l'outil), chacune avec un **label court** ET une **description**
  qui explique la consequence/le contexte du choix — jamais juste un mot.
- Une option marquee **"(Recommande)"** dans son libelle quand un defaut raisonnable existe.
- Jamais d'option "Autre" ajoutee manuellement : l'outil la propose deja automatiquement en saisie
  libre. Quand une liste naturelle depasse 4 reponses (ex. choix d'une techno), ne garder que les
  3-4 plus frequentes en options explicites et laisser "Autre" couvrir le reste.
- Checklist a choix multiples → `multiSelect: true` (toujours dans la limite de 4 options ; au-dela,
  scinder en plusieurs questions du meme appel — jusqu'a 4 questions groupees par appel).
- Les questions de decouverte du workshop (Option a, phases 1-6) sont **aussi** posees via `AskUserQuestion`,
  jamais en texte dans le chat : 2 a 4 options pertinentes deduites du contexte detecte (README, manifestes,
  reponses precedentes), la saisie libre passant par "Autre". Jusqu'a 4 questions groupees par appel (une phase
  = 1 a 2 appels). Les listes numerotees des phases ci-dessous sont le **contenu** des questions, pas un
  format d'affichage. Avec un brief (Option a-bis), seules les precisions manquantes sont posees ; la
  reception du brief lui-meme (texte long colle dans le message suivant) est la seule saisie libre admise.
- Toute confirmation (« Confirmer ou modifier ? », validation d'un plan, d'un diff, d'un pipeline) suit la meme
  regle : jamais `[O/n]`, `(o/n)` ni « dis-moi » en texte.

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
    |-- Projet vide --> Workshop, brief (texte de presentation) ou questionnaire
    |
    v
[CONFIGURATION] --> Questions ou deduction
    |
    v
[GENERATION] --> project-config.json + agents dev-* + registre des agents
    |
    v
[PLACEHOLDERS] --> Substituer {VAR} dans commandes et agents deployes
    |
    v
[FINALISATION] --> CLAUDE.md + .gitignore + CI/CD workflow + labels GitHub
    |              + milestone v0.1.0 et issues de base (nouveau projet avec remote GitHub)
    v
[AUDIT CONTEXTE] --> commit d'init puis /context-audit (audit complet)
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
| **AGENTS TEMPLATE** | `.claude/agents/*.template.md` | Depuis `TEMPLATE_claude/agents/*.md` (agents communs) ou `TEMPLATE_claude/templates/dev-*.md` (agents de stack), déployé en `*.template.md` avec un en-tete de provenance — gitignore. Chaque copie est inscrite au **registre des agents** (`project-config.json` → `agents.registry[]`, voir section dediee) |
| **CONTEXTES PARTAGES** | `.claude/{commands,agents}/context/*.template.md` + compagnon `.claude/{commands,agents}/context/*.md` optionnel | Depuis `TEMPLATE_claude/{commands,agents}/context/*.md` — meme convention template/compagnon que les agents |
| **PROJET** | `.claude/CLAUDE.md`, `project-config.json` (dont le registre des agents), `memory/`, compagnons `agents/dev-*.md`, specifications `agents/generic.<nom>.md`, compagnons `agents/*.md` et `context/*.md` | Trackes dans git, jamais ecrases |

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
- **Pre-menu de reinitialisation** : toujours (silencieusement, avant l'analyse) — **etapes 1 a 3
  uniquement** : rien n'est deploye dans `.claude/` ni le registre des agents modifie avant le choix de
  l'utilisateur (sinon le diff calcule ensuite serait toujours vide)
- **Reinitialisation option d)** : appliquer les changements detectes (fetch deja fait au pre-menu) —
  deploiement (d5) puis etape 5 ci-dessous

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

#### 2. Verifier si une mise a jour est disponible, et determiner la cible du fetch

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

# FETCH_TAG/FETCH_COMMIT = cible reellement fetchee par l'etape 3 ci-dessous. Par defaut
# la derniere version taggee (init, sync/option d). La section "Reinstallation des fichiers
# template" (fichiers manquants sur un projet deja v3) peut les faire pointer a la place vers
# le commit fige dans project-config.json.template_version, pour restaurer a l'identique sans
# upgrade implicite.
FETCH_TAG="$LATEST_TAG"
FETCH_COMMIT="$LATEST_COMMIT"
```

#### 3. Fetcher TEMPLATE_claude/ depuis GitHub

```bash
# Fetch au commit cible resolu a l'etape precedente (pas la branche) : les
# fichiers deployes viennent toujours d'une version stabilisee et taggee.
gh api repos/$TEMPLATE_REPO/git/trees/$FETCH_COMMIT?recursive=1 \
  --jq '.tree[] | select(.type=="blob") | .path' \
  | grep -E '^TEMPLATE_claude/' \
  | while read FILE; do
      mkdir -p "$(dirname $FILE)"
      gh api "repos/$TEMPLATE_REPO/contents/$FILE?ref=$FETCH_COMMIT" \
        --jq '.content' | base64 -d > "$FILE"
      echo "  ✓ $FILE"
    done
```

#### 4. Deployer dans .claude/

Les commandes sont déployées en `*.md` — directement invocables comme `/xxx`, jamais éditées manuellement.
Les agents sont déployés en `*.template.md` — les adaptations projet vont dans des fichiers `*.md` compagnons (voir COMMON.md §13).
Le déploiement des agents passe **toujours** par les fonctions de la section "Registre des agents" (en-tête de
provenance + inscription au registre) — jamais par un `cp` direct.
Les contextes partagés (`context/COMMON.md`, `context/GITHUB.md`...) suivent la même convention que les agents : déployés en `*.template.md`, avec un compagnon `*.md` optionnel pour les adaptations projet (voir COMMON.md §13).

```bash
mkdir -p .claude/commands .claude/agents .claude/agents/context .claude/commands/context

# Commandes : déployé en *.md (invocables directement comme /xxx)
for src in TEMPLATE_claude/commands/*.md; do
  dest=".claude/commands/$(basename $src)"
  cp "$src" "$dest"
  echo "  ✓ $dest"
done

# Agents : déployé en *.template.md, avec en-tête de provenance (section "Registre des agents")
if jq -e '.agents.registry' .claude/project-config.json >/dev/null 2>&1; then
  # Projet deja initialise (reinstallation) : chaque agent du registre, agents de stack compris.
  # AGENTS_MODE=restore → version enregistree (reinstallation a l'identique, epingles compris) ;
  # AGENTS_MODE=sync    → derniere version (hors epingles), registre mis a jour.
  [ "${AGENTS_MODE:-restore}" = "sync" ] && registry_sync || registry_restore
else
  # Premiere initialisation : agents communs seulement — les agents de stack (dev-*) sont deployes
  # et l'ensemble inscrit au registre a la "Generation de la Configuration" (section 2)
  for src in TEMPLATE_claude/agents/*.md; do
    deploy_agent "$(basename "$src" .md)" "agents/$(basename "$src")"
  done
fi

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

# Mods Claude Code (panneau milestone…) : chargés automatiquement depuis .claude/skills/<mod>/.
# Copie complète, générée — gitignorée comme le reste des fichiers template.
for src in TEMPLATE_claude/mods/*/; do
  dest=".claude/skills/$(basename $src)"
  mkdir -p "$dest" && cp -r "$src". "$dest/"
  echo "  ✓ $dest"
done
```

#### 5. Mettre a jour TEMPLATE_claude/.template-source.json et project-config.json

```bash
TODAY=$(date +%Y-%m-%d)
cat > TEMPLATE_claude/.template-source.json <<EOF
{
  "repo": "$TEMPLATE_REPO",
  "branch": "$TEMPLATE_BRANCH",
  "tag": "$FETCH_TAG",
  "commit": "$FETCH_COMMIT",
  "synced_at": "$TODAY"
}
EOF
echo "✓ TEMPLATE_claude/.template-source.json mis a jour ($FETCH_TAG - $FETCH_COMMIT)"

# Copie durable (trackee git) — TEMPLATE_claude/.template-source.json ci-dessus est gitignore
# avec le reste de TEMPLATE_claude/ et disparait sur un clone frais ; project-config.json est
# le seul endroit ou cette info survit. Ne s'applique qu'a une mise a jour d'un projet deja
# initialise — a la premiere initialisation, le champ est deja inclus a la creation du fichier
# (section "Generation de la Configuration").
if [ -f .claude/project-config.json ]; then
  mkdir -p _work/tmp
  jq --arg tag "$FETCH_TAG" --arg commit "$FETCH_COMMIT" \
    '.template_version = { "tag": $tag, "commit": $commit }' \
    .claude/project-config.json > _work/tmp/project-config.json.tmp \
    && mv _work/tmp/project-config.json.tmp .claude/project-config.json
  echo "✓ project-config.json : template_version mis a jour ($FETCH_TAG - $FETCH_COMMIT)"
fi
```

---

## Registre des agents (provenance et version)

Chaque agent deploye (`.claude/agents/<nom>.template.md`) est une **copie** d'un template source de
`TEMPLATE_claude/` — agent commun (`agents/<nom>.md`) ou agent de stack (`templates/dev-*.md`). Pour que
chaque copie soit surveillee, resynchronisee et sa derive de version visible, elle est :

1. **inscrite au registre** `agents.registry[]` de `project-config.json` (tracke git — la source de verite,
   qui survit a un clone frais contrairement aux copies gitignorees) :

   ```json
   "agents": {
     "registry": [
       { "name": "dev-firmware-capteur", "source": "templates/dev-firmware-esp32.md",
         "tag": "v3.22.0", "commit": "<sha du commit>", "sha256": "<empreinte de la source>",
         "role": "Firmware capteur (ESP32)", "paths": ["firmware/sensor/**"] },
       { "name": "dev-firmware-passerelle", "source": "templates/dev-firmware-esp32.md", "…": "…",
         "role": "Firmware passerelle (ESP32)", "paths": ["firmware/gateway/**"] },
       { "name": "dev-backend", "source": "templates/dev-generic.md", "…": "…",
         "role": "Backend (Rust/Axum)" },
       { "name": "qa", "source": "agents/qa.md", "tag": "v3.22.0", "commit": "…", "sha256": "…",
         "pinned": true }
     ]
   }
   ```

   | Champ | Sens |
   |-------|------|
   | `name` | Nom du fichier deploye `.claude/agents/<name>.template.md` (= nom canonique de l'agent, sauf `generic`) |
   | `source` | Chemin du template source, relatif a `TEMPLATE_claude/` |
   | `tag` / `commit` | Version du template d'ou provient la copie deployee |
   | `sha256` | Empreinte du contenu **brut** de la source (avant en-tete et placeholders). Detecte la derive par agent : seul un agent dont la source a reellement change est propose en mise a jour, pas chaque agent a chaque release. Vide = version inconnue (projet anterieur au registre) |
   | `role` | Agents de stack : role affiche dans la table `## Agents Disponibles` (defaut : libelle de la categorie + techno) |
   | `paths` | Optionnel — **perimetre** de l'agent (globs). Obligatoire pour les instances multiples d'une meme categorie ; repris dans le compagnon `## Perimetre` et utilise par le planner pour assigner les taches |
   | `pinned` | Optionnel, pose **a la main** par l'utilisateur : `true` = agent fige a sa version, jamais mis a jour par la synchronisation (exception assumee — les versions restent alignees par defaut) |

2. **marquee d'un en-tete de provenance** insere juste apres le frontmatter de la copie (diagnostic
   lisible dans le fichier lui-meme ; un commentaire HTML ne perturbe ni le frontmatter `model:`/`effort:`
   ni la lecture de l'agent) :

   ```
   <!-- template-source: templates/dev-firmware-esp32.md @ v3.22.0 (1a2b3c4) sha256:5d6e7f8a9b0c — copie generee par /init-project, ne pas editer : adapter dans le compagnon dev-firmware.md -->
   ```

**Agents attendus** = tous les agents communs de `TEMPLATE_claude/agents/*.md` (un nouvel agent du
template est donc ajoute automatiquement) + les agents de stack inscrits au registre (choisis a la
"Generation de la Configuration", section 2, ou a la migration ci-dessous selon la table suivante).

**Correspondance stack → agent de stack** (unique table de reference — utilisee a l'init, a la
reconfiguration et a la migration) :

| Agent (`name`) | Condition dans `project-config.json` | `source` |
|----------------|--------------------------------------|----------|
| `dev-backend` | `stack.backend.language` = `go` | `templates/dev-backend-go.md` |
| `dev-backend` | `stack.backend.language` = `javascript` / `typescript` / Node.js | `templates/dev-backend-node.md` |
| `dev-backend` | `stack.backend.language` = `python` | `templates/dev-backend-python.md` |
| `dev-frontend` | `stack.frontend.framework` = `react` / `next` | `templates/dev-frontend-react.md` |
| `dev-frontend` | `stack.frontend.framework` = `vue` / `nuxt` | `templates/dev-frontend-vue.md` |
| `dev-firmware` | `stack.firmware` non nul et ESP32 | `templates/dev-firmware-esp32.md` |
| `dev-plugin` | `stack.plugin` non nul (toute plateforme) | `templates/dev-plugin.md` |
| `dev-<categorie>` | Stack renseignee **sans template dedie** (reponse « Autre » : Rust, Java, STM32, Raspberry Pi, Flutter, natif mobile...) — y compris `stack.mobile` → `dev-mobile` | `templates/dev-generic.md` |

`dev-generic` ne contient aucune techno : son **compagnon** `<name>.md` est **obligatoire** et porte la stack
(`## Stack`, `## Toolchain et commandes`, `## Perimetre`, `## Conventions`) — cree par `ensure_companion` a partir
des reponses de l'Etape 5d ; sans lui, l'agent repond `BLOQUE`.

**Instances multiples** : une categorie presente plusieurs fois (deux firmwares, deux backends...) donne une
entree par composant, nommee `dev-<categorie>-<suffixe>` (kebab-case, ex. `dev-firmware-capteur`), toutes
avec la `source` de la categorie ci-dessus, un `role` et un perimetre `paths` disjoints. Il n'y a alors **pas**
d'entree `dev-<categorie>` seule. Chaque instance a sa propre copie `<name>.template.md` (frontmatter `name:`
reecrit au nom de l'instance — c'est son `subagent_type` au spawn) et son propre compagnon : spawn, modele/effort
et synchronisation sont ceux de n'importe quel agent, sans cas particulier.

### Fonctions

Definies une fois, utilisees par l'etape 4 du fetch, la "Generation de la Configuration", la
reinstallation et l'option d. `TEMPLATE_REPO`, `FETCH_TAG` et `FETCH_COMMIT` viennent de la procedure de fetch.

```bash
CFG=.claude/project-config.json
mkdir -p _work/tmp/registry

# Empreinte d'une source : sha256 du contenu brut du template
src_sha() { sha256sum "$1" | cut -d' ' -f1; }

# deploy_agent NAME SOURCE [FICHIER] [TAG] [COMMIT] — copie + en-tete de provenance (n'ecrit pas le registre)
deploy_agent() {
  local name="$1" source="$2" file="${3:-TEMPLATE_claude/$2}" tag="${4:-$FETCH_TAG}" commit="${5:-$FETCH_COMMIT}"
  local sha; sha=$(src_sha "$file")
  local h="<!-- template-source: ${source} @ ${tag} (${commit:0:7}) sha256:${sha:0:12} — copie generee par /init-project, ne pas editer : adapter dans le compagnon ${name}.md -->"
  # En-tete apres le frontmatter (2e ligne "---"), ou en premiere ligne si pas de frontmatter
  # Frontmatter : "name:" reecrit au nom deploye (instances multiples : chaque copie = son propre subagent_type)
  awk -v h="$h" -v n="$name" '
    NR==1 && $0!="---" {print h; done=1}
    NR>1 && !done && /^name:/ {print "name: " n; next}
    {print}
    NR>1 && $0=="---" && !done {print h; done=1}' "$file" > ".claude/agents/${name}.template.md"
  echo "  ✓ .claude/agents/${name}.template.md  ← ${source} @ ${tag}"
}

# registry_set NAME SOURCE SHA TAG COMMIT — ajoute/met a jour l'entree (conserve "pinned"), registre trie
registry_set() {
  jq --arg n "$1" --arg s "$2" --arg h "$3" --arg t "$4" --arg c "$5" '
    (.agents.registry // []) as $r
    | .agents.registry = ( ($r | map(select(.name != $n)))
        + [ (($r | map(select(.name == $n)) | .[0]) // {}) + {name:$n, source:$s, tag:$t, commit:$c, sha256:$h} ]
        | sort_by(.name) )' "$CFG" > _work/tmp/registry/cfg.tmp && mv _work/tmp/registry/cfg.tmp "$CFG"
}
registry_del() {
  jq --arg n "$1" '.agents.registry |= map(select(.name != $n))' "$CFG" > _work/tmp/registry/cfg.tmp \
    && mv _work/tmp/registry/cfg.tmp "$CFG"
}

# registry_meta NAME ROLE [PATHS_JSON] — role et perimetre d'un agent de stack (ex. '["firmware/sensor/**"]')
registry_meta() {
  jq --arg n "$1" --arg r "$2" --argjson p "${3:-null}" '
    .agents.registry |= map(if .name == $n then . + {role: $r} + (if $p == null then {} else {paths: $p} end) else . end)' \
    "$CFG" > _work/tmp/registry/cfg.tmp && mv _work/tmp/registry/cfg.tmp "$CFG"
}

# ensure_companion NAME — cree le squelette du compagnon .claude/agents/NAME.md s'il est absent (jamais ecrase).
# Requis pour dev-generic (stack) et pour les instances avec perimetre ; Claude remplit ensuite les sections
# avec les reponses de l'Etape 5d (un squelette vide fait repondre BLOQUE a dev-generic).
ensure_companion() {
  local f=".claude/agents/$1.md" e source role paths
  [ -f "$f" ] && return
  e=$(jq -c --arg n "$1" '.agents.registry[] | select(.name==$n)' "$CFG")
  source=$(jq -r '.source' <<<"$e"); role=$(jq -r '.role // ""' <<<"$e")
  paths=$(jq -r '(.paths // []) | map("- `" + . + "`") | join("\n")' <<<"$e")
  [ "$source" = "templates/dev-generic.md" ] || [ -n "$paths" ] || return
  { echo "# $1 — ${role}"; echo
    if [ "$source" = "templates/dev-generic.md" ]; then
      echo "## Stack"; echo; echo "## Toolchain et commandes"; echo; fi
    echo "## Perimetre"; echo; [ -n "$paths" ] && { echo "$paths"; echo; }
    echo "## Conventions"; echo
  } > "$f"
  echo "  ✓ $f cree (a completer — compagnon tracke git)"
}

# deploy_and_register NAME SOURCE — derniere version (TEMPLATE_claude/ fetche) + registre
deploy_and_register() {
  deploy_agent "$1" "$2" && registry_set "$1" "$2" "$(src_sha "TEMPLATE_claude/$2")" "$FETCH_TAG" "$FETCH_COMMIT"
}

# restore_one NAME — redeploie exactement la version enregistree. Source locale si son empreinte
# correspond, sinon fetch du fichier au commit enregistre. Version inconnue (sha256 vide) → derniere.
restore_one() {
  local e source tag commit sha file
  e=$(jq -c --arg n "$1" '.agents.registry[] | select(.name==$n)' "$CFG")
  source=$(jq -r '.source' <<<"$e"); tag=$(jq -r '.tag' <<<"$e")
  commit=$(jq -r '.commit' <<<"$e"); sha=$(jq -r '.sha256 // ""' <<<"$e")
  [ -z "$sha" ] && { deploy_and_register "$1" "$source"; return; }
  file="TEMPLATE_claude/$source"
  if [ ! -f "$file" ] || [ "$(src_sha "$file")" != "$sha" ]; then
    file="_work/tmp/registry/$1.md"
    gh api "repos/$TEMPLATE_REPO/contents/TEMPLATE_claude/$source?ref=$commit" --jq '.content' | base64 -d > "$file"
  fi
  deploy_agent "$1" "$source" "$file" "$tag" "$commit"
}
registry_restore() { for n in $(jq -r '.agents.registry[]?.name' "$CFG"); do restore_one "$n"; done; }

# registry_status — une ligne "STATUT NAME SOURCE VERSION_ENREGISTREE" par agent
registry_status() {
  local name source e reg_sha reg_tag pinned f n
  { for f in TEMPLATE_claude/agents/*.md; do echo "$(basename "$f" .md) agents/$(basename "$f")"; done
    jq -r '.agents.registry[]? | "\(.name) \(.source)"' "$CFG"
  } | sort -u -k1,1 | while read -r name source; do
    e=$(jq -c --arg n "$name" '.agents.registry[]? | select(.name==$n)' "$CFG")
    [ -n "$e" ] || e='{}'
    reg_sha=$(jq -r '.sha256 // ""' <<<"$e"); reg_tag=$(jq -r '.tag // "-"' <<<"$e")
    pinned=$(jq -r '.pinned // false' <<<"$e")
    if   [ ! -f "TEMPLATE_claude/$source" ]; then echo "RELIQUAT $name $source $reg_tag"   # source retiree du template
    elif [ "$e" = "{}" ]; then echo "NOUVEAU $name $source -"
    elif [ ! -f ".claude/agents/${name}.template.md" ]; then echo "MANQUANT $name $source $reg_tag"
    elif [ "$(src_sha "TEMPLATE_claude/$source")" = "$reg_sha" ]; then echo "INCHANGE $name $source $reg_tag"
    elif [ "$pinned" = "true" ]; then echo "EPINGLE $name $source $reg_tag"
    else echo "MODIFIE $name $source ${reg_tag:-inconnue}"
    fi
  done
  # Copies deployees ni agent commun ni inscrites au registre (ex. agent d'une stack retiree)
  for f in .claude/agents/*.template.md; do
    [ -f "$f" ] || continue; n=$(basename "$f" .template.md)
    [ -f "TEMPLATE_claude/agents/$n.md" ] && continue
    jq -e --arg n "$n" '.agents.registry[]? | select(.name==$n)' "$CFG" >/dev/null || echo "RELIQUAT $n - hors-registre"
  done
}

# registry_sync [--reliquats] — applique les statuts : NOUVEAU/MODIFIE → derniere version ;
# MANQUANT → derniere version, ou version enregistree si epingle ; EPINGLE → rien ;
# RELIQUAT → supprime (copie + entree) uniquement avec --reliquats
registry_sync() {
  local all st name source info
  all=$(registry_status)   # calcule AVANT toute ecriture du registre
  while read -r st name source info; do
    case "$st" in
      NOUVEAU|MODIFIE) deploy_and_register "$name" "$source" ;;
      MANQUANT) if [ "$(jq -r --arg n "$name" '.agents.registry[] | select(.name==$n) | .pinned // false' "$CFG")" = "true" ]
                then restore_one "$name"; else deploy_and_register "$name" "$source"; fi ;;
      EPINGLE)  echo "  📌 $name epingle a $info — non mis a jour (retirer \"pinned\" pour le resynchroniser)" ;;
      RELIQUAT) [ "$1" = "--reliquats" ] && [[ "$source" != templates/* ]] && { rm -f ".claude/agents/${name}.template.md"; registry_del "$name"
                  echo "  ✗ .claude/agents/${name}.template.md supprime (reliquat)"; } ;;
    esac
  done <<<"$all"
}

# registry_report — tableau final des teammates : version deployee, etat, compagnon
registry_report() {
  local name source tag pinned sha teammate etat comp n gtag
  echo "| Teammate | Fichier | Source | Version | Etat | Compagnon |"
  echo "|----------|---------|--------|---------|------|-----------|"
  jq -r '.agents.registry[]? | [.name, .source, .tag, (.pinned // false), .sha256] | @tsv' "$CFG" |
  while IFS=$'\t' read -r name source tag pinned sha; do
    # Nom canonique (SendMessage) lu dans la table "Agents Disponibles" de CLAUDE.md
    # (a defaut — agent hors table, ex. teamleader — le nom du fichier ; generic = template commun des instances)
    teammate=$(grep '^| ' CLAUDE.md 2>/dev/null | grep -F ".claude/agents/${name}.template.md" \
      | head -1 | cut -d'|' -f2 | tr -d ' `')
    [ "$name" = "generic" ] && teammate="(commun aux instances)"
    # Version = version de la copie deployee ; la cible (FETCH_TAG) n'est affichee que si elle differe en contenu
    if [ -f "TEMPLATE_claude/$source" ] && [ "$(src_sha "TEMPLATE_claude/$source")" = "$sha" ]; then etat="🟢 a jour"
    elif [ "$pinned" = "true" ]; then etat="📌 epingle (${FETCH_TAG} dispo)"
    else etat="⚠ en retard → ${FETCH_TAG}"; fi
    [ "$pinned" = "true" ] && [ "$etat" = "🟢 a jour" ] && etat="🟢 a jour 📌"
    if [ -f ".claude/agents/${name}.md" ]; then comp="✓ ${name}.md"
    # Compagnon obligatoire : dev-generic (stack) et instances avec perimetre
    elif [ "$source" = "templates/dev-generic.md" ] || jq -e --arg n "$name" '.agents.registry[] | select(.name==$n) | .paths' "$CFG" >/dev/null; then
      comp="✗ ${name}.md manquant (obligatoire)"
    else comp="—"; fi
    echo "| ${teammate:-$name} | ${name}.template.md | ${source} | ${tag} | ${etat} | ${comp} |"
  done
  # Instances d'agents generiques : template commun generic + specification (compagnon obligatoire)
  gtag=$(jq -r '.agents.registry[]? | select(.name=="generic") | .tag' "$CFG")
  jq -r '.agents.generic[]?.name' "$CFG" | while read -r n; do
    comp=$([ -s ".claude/agents/generic.${n}.md" ] && echo "✓ generic.${n}.md (spec)" || echo "✗ generic.${n}.md manquante")
    echo "| ${n} | generic.template.md | agents/generic.md | ${gtag:-—} | (voir generic) | ${comp} |"
  done
}
```

Le **rapport final** de chaque chemin (init, option d, reinstallation, migration v1/v2) se termine par
`registry_report` : un tableau des teammates avec la version **actuelle** de chaque agent (celle de la copie
deployee — un agent dont le template n'a pas change depuis reste a sa version de deploiement tout en etant a
jour), son etat (`🟢 a jour` ; `⚠ en retard → <cible>` — ex. sync partielle ou annulee ; `📌 epingle (<cible>
dispo)`, la cible etant la version fetchee) et la presence d'un compagnon `<nom>.md`
(adaptations projet ; pour une instance generique, sa specification `generic.<nom>.md`).

> Un `RELIQUAT` d'agent de **stack** (`source` = `templates/...` retire du template, ex. template renomme)
> n'est jamais supprime sans confirmation explicite : le signaler et demander (re-pointer vers le nouveau
> template de la table ci-dessus, ou supprimer). Les placeholders sont appliques apres le deploiement,
> comme pour tous les `*.template.md` (section "4. Application des placeholders").

### Migration — projet anterieur au registre

Declenchee des que `HAS_CONFIG=yes` et que `agents.registry` est absent de `project-config.json`
(projet initialise avant le registre), **avant tout deploiement d'agent** : a l'entree de l'option d
(avant d2), avant l'etape 4 de la reinstallation, et en fin de migration v1/v2 → v3. Au pre-menu, rien
n'est ecrit : le rapport annonce seulement `[⇪] registre des agents absent — sera cree (N agents, dont dev-*)`.

```bash
# Agents communs : version de la copie deployee inconnue → sha256 vide = MODIFIE, redeployes une fois
for f in TEMPLATE_claude/agents/*.md; do
  registry_set "$(basename "$f" .md)" "agents/$(basename "$f")" "" "inconnue" ""
done
```

Agents de stack : pour chaque categorie configuree dans `stack`, determiner la `source` avec la table de
correspondance ci-dessus et l'inscrire de meme (`registry_set dev-<cat> templates/<source> "" inconnue ""`).
Si la valeur de stack est ambigue mais qu'une copie `.claude/agents/dev-<cat>.template.md` existe,
l'identifier en comparant son frontmatter (`name`/`description`) aux `TEMPLATE_claude/templates/dev-*.md` ;
si l'ambiguite persiste → AskUserQuestion (une option par template candidat). Corrige au passage les
agents de stack jamais resynchronises jusqu'ici (ex. `dev-firmware`) : ils sont desormais suivis comme
les autres, et redeployes a la version courante par la synchronisation qui suit.

---

## Detection de version et migration

**Avant tout**, detecter la version du projet :

```bash
HAS_CONFIG=$([ -f .claude/project-config.json ] && echo "yes" || echo "no")
HAS_TEMPLATE_DIR=$([ -d TEMPLATE_claude ] && echo "yes" || echo "no")
HAS_OLD_SOURCE=$([ -f .claude/.template-source.json ] && echo "yes" || echo "no")
HAS_TEMPLATE_VERSION=$([ "$HAS_CONFIG" = "yes" ] && \
  jq -e '.template_version.commit' .claude/project-config.json >/dev/null 2>&1 && \
  echo "yes" || echo "no")
```

| `project-config.json` | `TEMPLATE_claude/` | `template_version` (dans `project-config.json`) | `.claude/.template-source.json` | Diagnostic |
|-----------------------|--------------------|--------------------------------------------------|--------------------------------|------------|
| absent | absent | - | absent | Nouveau projet → flux normal |
| present | present | - | - | Projet v3 → Reinitialisation |
| present | absent | present | - | **Projet v3, fichiers template manquants (clone frais) → Reinstallation a l'identique** (voir section dediee) |
| present | absent | absent | present | **Projet v2 → Migration v3** |
| present | absent | absent | absent | **Projet v1 → Migration v3** |

> Le cas "fichiers manquants" (ligne 3) n'est **pas** une migration : le projet est deja en
> architecture v3, seuls les fichiers gitignores (`TEMPLATE_claude/`, commandes/agents deployes)
> sont absents — situation normale apres un `git clone`/`git pull` sur un projet deja initialise.
> Le distinguer de la migration v1/v2 evite de rejouer inutilement tout le flux de conversion de
> schema sur un projet qui n'en a pas besoin.

---

## Reinstallation des fichiers template (projet v3, fichiers manquants)

Declenche quand `HAS_CONFIG=yes`, `HAS_TEMPLATE_DIR=no` et `HAS_TEMPLATE_VERSION=yes` (voir
tableau de detection ci-dessus) — typiquement apres un `git clone`/`git pull` d'un projet deja
initialise en v3, dont `TEMPLATE_claude/` et les commandes/agents deployes sont gitignores.

```bash
PINNED_TAG=$(jq -r '.template_version.tag' .claude/project-config.json)
PINNED_COMMIT=$(jq -r '.template_version.commit' .claude/project-config.json)

# Resoudre la derniere version disponible (etape 2 de la procedure de fetch ci-dessus,
# sans encore fixer FETCH_TAG/FETCH_COMMIT)
LATEST_TAG=$(gh api repos/$TEMPLATE_REPO/tags --jq '.[0].name // empty')
LATEST_COMMIT=$(gh api repos/$TEMPLATE_REPO/tags --jq '.[0].commit.sha // empty')
```

Si `PINNED_COMMIT = LATEST_COMMIT` → aucun choix reel a proposer (reinstaller a l'identique
*est* la derniere version) : fixer directement `FETCH_TAG=$PINNED_TAG`, `FETCH_COMMIT=$PINNED_COMMIT`
et enchainer sur les etapes 3-5 de la procedure de fetch, sans poser de question.

Sinon :

```
AskUserQuestion : "Les fichiers template (TEMPLATE_claude/, commandes, agents) sont absents de
ce clone — ils sont gitignores et ne se recuperent pas avec git. Comment les restaurer ?"
- Reinstaller a partir des templates <PINNED_TAG> (Recommande) — restaure exactement la version
  deja utilisee par ce projet (celle enregistree dans project-config.json), aucun changement de
  comportement, rien a revoir avant de continuer a travailler
- Profiter pour mettre a jour les templates vers <LATEST_TAG> (derniere version) — equivalent a
  une synchronisation complete (option "Appliquer les mises a jour detectees"), peut introduire
  des changements de comportement a relire avant de continuer
```

- **Reinstaller a l'identique** → `FETCH_TAG=$PINNED_TAG`, `FETCH_COMMIT=$PINNED_COMMIT`, puis
  etapes 3-5 de la procedure de fetch avec `AGENTS_MODE=restore`. `template_version` dans
  `project-config.json` ne change pas (deja a cette valeur), le registre des agents non plus : chaque
  agent — agents de stack `dev-*` et agents epingles compris — est restaure a la version qu'il enregistre.
- **Mettre a jour** → `FETCH_TAG=$LATEST_TAG`, `FETCH_COMMIT=$LATEST_COMMIT`, puis etapes 3-5 avec
  `AGENTS_MODE=sync` — strictement equivalent a une reinitialisation normale (Option d),
  `template_version` et le registre des agents sont mis a jour vers la nouvelle version (hors epingles).

Avant l'etape 4, si `agents.registry` est absent → section "Registre des agents", "Migration — projet
anterieur au registre" (les agents de stack, gitignores, sont alors redeployes depuis la stack configuree).

Dans les deux cas, enchainer ensuite sur le deploiement standard (etape 4 de la procedure de
fetch) et la detection de doublons/conflits (d5b/d5c) si des fichiers compagnons `*.md` existent
deja localement (customisations projet preservees, jamais ecrasees). Terminer par le tableau des
teammates (`registry_report`).

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

AskUserQuestion : "Convertir maintenant vers le nouveau modele ?"
- Oui, convertir automatiquement (Recommande) — applique QUALIF=promote / PROD=rebuild-ci /
  deploy.mechanism="<MECH>" ; /build, /publish, /deploy fonctionnent immediatement apres
- Non, laisser en l'etat — infrastructure.deploy reste tel quel ; /build, /publish <env>,
  /deploy <env> nouvellement synchronisees ne fonctionneront pas correctement tant que la
  conversion n'est pas faite
```

Si confirme :

```bash
mkdir -p _work/tmp
mkdir -p _work/tmp
jq --arg mech "$MECH" '
  .infrastructure.environments = [
    { "name": "QUALIF", "order": 1,
      "publish": { "mode": "promote", "target": "build/qualif_v{X.Y.Z}/" },
      "deploy":  { "mechanism": $mech } },
    { "name": "PROD", "order": 2,
      "publish": { "mode": "rebuild-ci", "trigger": "git-tag", "pipeline": ".github/workflows/release.yml" },
      "deploy":  { "mechanism": $mech } }
  ] | del(.infrastructure.deploy)
' .claude/project-config.json > _work/tmp/project-config.json.tmp \
  && mv _work/tmp/project-config.json.tmp .claude/project-config.json

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

AskUserQuestion : "Migration vers l'architecture v3 requise — continuer ?"
- Oui, migrer maintenant (Recommande) — deplace les fichiers template vers TEMPLATE_claude/
  gitignore ; .claude/ ne garde que les fichiers projet
- Non, annuler l'initialisation — le projet reste sur l'architecture v1/v2, aucune commande
  synchronisee depuis TEMPLATE_claude/ ne sera disponible
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
# Fusion non destructive : conserver les lignes du projet, ajouter uniquement les lignes template absentes
if [ -f .gitignore ]; then
  grep -vE '^\s*(#|$)' TEMPLATE_claude/gitignore-for-projects | while IFS= read -r l; do
    grep -qxF "$l" .gitignore || echo "$l" >> .gitignore
  done
else
  cp TEMPLATE_claude/gitignore-for-projects .gitignore
fi
```

Puis appliquer la section **"Site marketing — worktree `gh-pages`"** (migration d'un `MARKETING/` suivi).

### Etape M3b — Registre des agents

Appliquer la section "Registre des agents (provenance et version)", "Migration — projet anterieur au
registre", puis `registry_sync` : agents communs et agents de stack (`dev-*`) sont redeployes a la version
fetchee en M1, avec leur en-tete de provenance, et inscrits au registre de `project-config.json`.

### Etape M4 — Commiter la migration

M2 a désindexé les `*.md` agents via `git rm --cached`. Re-tracker les fichiers compagnons agents
et contextes qui existent sur disque (customisations projet à préserver) avant de commiter.

```bash
git add .gitignore TEMPLATE_claude/.template-source.json .claude/project-config.json
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
- Agent and context companion files re-tracked after cache cleanup
- Agents registry (agents.registry[]) created in project-config.json"
```

### Etape M5 — Rapport

```
Migration → v3 terminee.

  TEMPLATE_claude/ fetche depuis CCoupel/claude_project_template
  Commandes deployees dans .claude/commands/
  Agents template deployes dans .claude/agents/ (en-tete de provenance, dev-* compris)
  Registre des agents cree : project-config.json → agents.registry[] (N agents, <tag>)
  Contextes partages deployes dans .claude/agents/context/ et .claude/commands/context/

  Fichiers PROJET preserves :
    ✓ .claude/CLAUDE.md
    ✓ .claude/project-config.json (hors migration ponctuelle du schema `infrastructure`, voir
      section "Migration du schema `infrastructure`" — appliquee avant cette etape si besoin)
    ✓ .claude/memory/
    ✓ .claude/agents/dev-*.md (si presents)
    ✓ .claude/agents/*.md et context/*.md compagnons (si presents)

  Teammates : tableau registry_report (version, etat, compagnon)
```

---

## Site marketing — worktree `gh-pages`

Regle : le site marketing vit **uniquement sur `gh-pages`**, jamais commite sur la branche de code (`main`,
`milestone/*`, `hotfix/*`). `MARKETING/` est le **worktree git de `gh-pages`**, exclu par le `.gitignore`
(ligne `MARKETING/` de `gitignore-for-projects`, fusionnee sans ecraser les lignes du projet — voir
Finalisation). Ce n'est pas une publication de `main`.

Applique a l'init, a la migration v1/v2 → v3 et a la reinitialisation (option d) :

```bash
# Garde-fou : exclusion presente meme si le .gitignore du projet est personnalise
grep -qxF 'MARKETING/' .gitignore 2>/dev/null || echo 'MARKETING/' >> .gitignore

# Detecter un MARKETING/ suivi par la branche de code (doublon avec gh-pages)
TRACKED=$(git ls-files MARKETING | wc -l)
```

- `TRACKED` = 0 → rien a migrer. Le worktree sera cree par l'agent `marketing` (`git worktree add MARKETING gh-pages`)
  quand il en aura besoin.
- `TRACKED` > 0 → AskUserQuestion : "`MARKETING/` est suivi par la branche de code (N fichiers) alors que le site
  doit vivre uniquement sur `gh-pages`. Migrer ?"
  - Migrer (Recommande) — comparer d'abord `MARKETING/` a `origin/gh-pages` (fichier par fichier :
    `git show origin/gh-pages:<fichier> | diff - MARKETING/<fichier>`) ; si le contenu local diverge de
    `gh-pages`, l'afficher et demander lequel garder AVANT toute suppression. Puis :
    `git rm -r --cached MARKETING/` (les fichiers restent sur disque), deplacer le dossier
    (`mkdir -p _work/tmp && mv MARKETING _work/tmp/MARKETING.bak`), `git worktree add MARKETING gh-pages`, recopier les eventuelles
    differences retenues dans le worktree et les commiter **sur `gh-pages`** (`git -C MARKETING ...`).
    Commit de migration sur la branche de code : `chore(site): MARKETING/ devient le worktree de gh-pages`
    (contient uniquement le `git rm --cached` et le `.gitignore`).
  - Ignorer — ne rien changer (le doublon subsiste ; l'agent `marketing` refusera de publier tant que
    `MARKETING/` est suivi).
  - Annuler.

Les release notes et posts ne font pas partie du site : ils restent sur la branche de code, dans `docs/releases/`.

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

AskUserQuestion : "Comment veux-tu initialiser le projet ?"
- Initialiser avec cette configuration (Recommande) — utilise directement les technologies
  detectees ci-dessus, aucune question supplementaire
- Initialiser avec cette configuration et cadrer depuis le brief — stack detectee + cadrage
  produit deduit du brief disponible (Option a-bis, phases 1-4 et 6 seulement)   ← uniquement
  si un brief est disponible (voir "Detection d'un brief")
- Initialiser manuellement — reprend chaque technologie via le questionnaire complet (etapes
  2 a 10), utile si une detection est incorrecte ou incomplete
- Annuler — n'initialise rien, quitte /init-project
```

**Si projet vide :**

```
Ce projet ne contient pas encore de code.

Comment souhaitez-vous initialiser le projet ?

AskUserQuestion : "Comment souhaites-tu initialiser le projet ?"
- Brief de projet — fournir (ou utiliser) un texte de presentation du projet ; le cadrage et la
  stack en sont deduits, seules les precisions manquantes sont demandees (Option a-bis)
  → "(Recommande)" si un brief est disponible (voir ci-dessous), sinon sur le workshop
- Workshop de cadrage — entretien guide en 6 phases pour definir vision, objectifs, stack et
  contraintes, sans texte prealable (Option a)
- Questionnaire rapide — questions directes sur la stack technique uniquement, sans phase de
  cadrage produit
- Annuler — n'initialise rien, quitte /init-project
```

### Detection d'un brief

Un **brief** est un texte de presentation ou de preparation du projet (note d'intention, cahier des charges
sommaire, compte rendu de reunion, fiche d'idee). Il est **disponible** si l'une de ces sources existe, dans
cet ordre de priorite :

1. le texte passe avec la commande (`/init-project <texte ou chemin de fichier>`) ;
2. un texte colle dans la conversation **avant** `/init-project` (message de l'utilisateur de plus de ~5 lignes
   decrivant le projet) ;
3. un fichier `BRIEF.md`, `docs/BRIEF.md` ou `docs/CADRAGE.md` (section « Brief d'origine ») a la racine ;
4. sur un projet vide, un `README.md` qui decrit le projet (plus qu'un titre).

Brief disponible → l'option correspondante est marquee "(Recommande)" et sa description cite la source
(« brief detecte : docs/BRIEF.md, 42 lignes »). Jamais de deduction silencieuse : le brief n'est exploite
qu'apres ce choix explicite.

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
8. Solutions existantes et differenciateur — RECHERCHE A LA CHARGE DE CLAUDE, pas une question :
   voir "Recherche des solutions existantes" ci-dessous. L'utilisateur ne fait que valider le
   differenciateur propose.
```

> Conserver les reponses 2, 5, 6 et 7 : elles alimentent les issues de base du milestone `v0.1.0`
> (section "Premier milestone v0.1.0 et issues de base" de la finalisation) — une issue par cas d'usage v1.

**Recherche des solutions existantes (q8)** — apres les reponses 2, 5 et 6, avant la phase 3 :

1. **Chercher** (outil `WebSearch`, 2 a 4 requetes construites depuis le probleme central, les cas d'usage v1
   et le public cible ; en francais puis en anglais) les solutions qui repondent deja au besoin : produits du
   marche, services SaaS, projets open source, fonctions d'outils generalistes (tableur, formulaires, messagerie).
   Retenir 3 a 5 solutions au plus, les plus proches du besoin — jamais une liste exhaustive.
2. **Presenter** dans le chat un tableau `Solution | Type (SaaS / open source / outil generaliste) | Couvre les
   cas d'usage v1 (lesquels) | Limite par rapport au besoin | Modele (gratuit / payant / inconnu)`, suivi d'une
   ligne par solution citant sa source (URL). Pas d'affirmation sans source : une caracteristique non verifiee
   est marquee « a verifier ». Pas de prix chiffre sans page consultee.
3. **Deduire le differenciateur** : ce que le projet apporte que les solutions retenues ne couvrent pas (ou
   couvrent mal) — formule en une phrase a partir du tableau, jamais invente. Si une solution couvre deja
   l'ensemble des cas d'usage v1 au meme public, le dire explicitement : c'est une information de cadrage
   majeure, pas un echec.
4. **Faire valider** via `AskUserQuestion` « Differenciateur du projet ? » : le differenciateur deduit en option
   "(Recommande)", 1 a 2 alternatives tirees du tableau (ex. « meme couverture mais gratuit / auto-heberge »,
   « integration a <outil existant> »), et, si une solution couvre tout, l'option « Adopter <solution> plutot que
   developper » — a transmettre telle quelle dans la fiche si elle est choisie (le projet peut s'arreter la).
5. **Consigner** le tableau et le differenciateur valide dans `docs/CADRAGE.md`, section « 2. Solution »,
   sous-section « Solutions existantes » — le marketing s'en sert pour le positionnement.

Sans acces reseau (`WebSearch` indisponible ou vide) : le dire en une ligne, poser la question 8 a
l'utilisateur sous sa forme classique (solutions connues + differenciateur, via `AskUserQuestion` avec
« Autre »), et marquer la sous-section « Solutions existantes — recherche non effectuee (hors ligne) ».

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

A la fin du workshop, ecrire la **fiche de cadrage** (`docs/CADRAGE.md`, format ci-dessous), puis generer
`CLAUDE.md` complet, `project-config.json`, et les agents.

### Option a-bis : Brief de projet

Meme resultat que le workshop (les 21 reponses des phases 1-6 + la stack des etapes 1-10), obtenu en
**trois temps** : lecture du brief → **proposition de cadrage** deduite → **questionnaires de precision**
limites a ce que le brief ne dit pas. L'utilisateur confirme ou corrige, il ne redige pas.

**1. Obtenir le brief**

- Source detectee (voir "Detection d'un brief") → la lire, confirmer en une ligne (« Brief : docs/BRIEF.md,
  42 lignes »). Plusieurs sources → AskUserQuestion « Quel brief utiliser ? » (une option par source, 4 max).
- Aucune source → AskUserQuestion : "Comment fournir le brief ?"
  - Le coller dans le prochain message (Recommande) — attendre le message suivant de l'utilisateur, qui est
    le brief integral (seule exception a la regle « jamais de texte libre » : un texte long ne tient pas
    dans l'option « Autre »)
  - Indiquer un chemin de fichier — via « Autre » ; fichier absent ou vide → reposer la question
  - Revenir au workshop — Option a
- Conserver le texte **verbatim** : il devient la section « Brief d'origine » de `docs/CADRAGE.md`
  (`mkdir -p docs`, ne jamais ecraser une section « Brief d'origine » existante : en ajouter une datee).

**2. Proposition de cadrage**

Pour chacune des 21 questions des phases 1-6 et pour chaque etape de stack (1 a 10), deduire une valeur
et lui attribuer un statut :

| Statut | Regle |
|--------|-------|
| `deduit` | Dit explicitement par le brief (reformulation permise, pas d'ajout) |
| `propose` | Inference raisonnable a partir du brief (ex. « acces par lien » → application web ; « benevoles » → utilisateurs peu techniques) — toujours a confirmer |
| `manquant` | Le brief ne permet rien de solide ; **ne jamais inventer** un chiffre, un nom, une date, une contrainte reglementaire ou une techno |

Regles de deduction :
- Les 3 cas d'usage v1 (q6) sont les actions concretes decrites par le brief, dans son ordre ; au-dela de 3
  explicites, garder les 3 premiers et proposer les suivants en « roadmap » (issues `roadmap` seulement).
- Le hors-perimetre (q7) ne contient que ce que le brief exclut ou reporte explicitement ; sinon `manquant`.
- Les solutions existantes et le differenciateur (q8) ne sont **jamais** deduits du brief seul : appliquer la
  « Recherche des solutions existantes » de l'Option a (WebSearch, tableau, validation du differenciateur). Les
  concurrents que le brief nomme sont ajoutes au tableau, verifies comme les autres ; le differenciateur que le
  brief avance est l'option "(Recommande)" s'il tient face au tableau, sinon il est reformule et le brief est cite.
- La stack (etapes 2-10) : `deduit` uniquement si nommee ; une suggestion du brief (« plutot Node ») est
  `propose` ; absente → `manquant`, et c'est le questionnaire standard qui tranche.
- Contraintes reglementaires (q16) : `propose` RGPD des que des donnees de personnes physiques sont manipulees ;
  tout autre regime (HDS, PCI-DSS...) seulement s'il est nomme.

Afficher la proposition en un tableau par phase (`Question | Valeur | Statut`) dans le chat — c'est le seul
affichage texte de cette option ; il precede les questions et sert de reference pendant les precisions.

**3. Questionnaires de precision**

Un appel `AskUserQuestion` par phase (jusqu'a 4 questions par appel, scinder si plus), construit ainsi :

- Elements `manquant` → une question chacun, 2 a 4 options plausibles tirees du contexte du brief (jamais une
  option vide ou « je ne sais pas » : la saisie libre est « Autre »), sans "(Recommande)" sauf defaut evident.
- Elements `propose` → une question chacun, la valeur deduite en option "(Recommande)" + 1 a 3 alternatives.
- Elements `deduit` → **pas de question individuelle** : une seule question de cloture par phase
  « Confirmer les elements deduits de la phase N ? » avec les options « Confirmer (Recommande) » et
  « Corriger un point » (le point corrige est saisi via « Autre », puis la question est reposee).
- Une phase entierement `deduit` tient donc en une question ; une phase entierement `manquant` revient au
  workshop pour cette phase.
- Phase 5 (stack) : enchainer les etapes 1 a 10 **normalement**, en placant la valeur `deduit`/`propose` en
  option "(Recommande)" et en sautant une etape seulement si sa valeur est `deduit` et confirmee dans la
  question de cloture de la phase 5.
- Code existant + brief (Etape 0, deuxieme option) : phases 1, 2, 3, 4 et 6 seulement ; la phase 5 est la
  configuration detectee.

A la fin : mettre a jour `docs/CADRAGE.md` (section « Fiche de cadrage », une sous-section par phase avec les
valeurs finales et, pour chaque valeur, sa provenance : `brief`, `precision` ou `workshop`), puis generer
`CLAUDE.md` complet, `project-config.json` (`initialized_from` = `brief`) et les agents. Les reponses 2, 5, 6
et 7 alimentent les issues de base du milestone `v0.1.0` exactement comme pour le workshop.

### Fiche de cadrage — `docs/CADRAGE.md`

Produite par les options a et a-bis (jamais par le questionnaire rapide), trackee git, relue par le planner et
le marketing. Distincte de `MARKETING/CADRAGE.md` (cadrage du site, sur `gh-pages`).

```markdown
# Cadrage — <PROJECT_NAME>

> Produit par /init-project (<workshop|brief>, <date>). Source des issues de base du milestone v0.1.0.

## Brief d'origine                      ← option a-bis uniquement, texte verbatim

## Fiche de cadrage
### 1. Probleme        (q1-q4)
### 2. Solution        (q5-q8 — cas d'usage v1 numerotes, hors perimetre, differenciateur valide)
#### Solutions existantes   (tableau de la recherche q8 + sources ; ou « recherche non effectuee (hors ligne) »)
### 3. Parties prenantes (q9-q12)
### 4. Contraintes et risques (q13-q17)
### 5. Stack et architecture (resume des etapes 1-10)
### 6. Conventions d'equipe (q18-q21)
```

Chaque ligne de la fiche porte sa provenance entre crochets : `[brief]`, `[precision]`, `[workshop]` ou
`[recherche]` (solutions existantes et differenciateur, avec leurs sources).

---

## Etape 1 : Informations Generales

```
1. Quel est le nom du projet ?
   [Detecte: nom depuis package.json/go.mod] → AskUserQuestion : option « <nom detecte> (Recommande) » +
   « Autre » pour saisir un autre nom

2. Decris brievement le projet (1-2 phrases) :

3. Repo GitHub ?
   [Detecte: `git remote get-url origin`] → rien a demander, afficher "Remote GitHub : <owner>/<repo>"
   Sinon AskUserQuestion : "Quel repo GitHub pour ce projet ?"
   - Aucun pour l'instant — pas de remote : labels de phase, milestone v0.1.0 et issues de base ne
     seront pas crees (relancer /init-project apres `git remote add origin ...`, ou `/milestone new v0.1`)
   - Autre : `owner/repo` ou URL → `gh repo view owner/repo` ;
       existe     → `git remote add origin https://github.com/owner/repo.git`
       n'existe pas → AskUserQuestion "Creer le repo GitHub owner/repo (prive) ?" :
                      Creer (Recommande) → `gh repo create owner/repo --private --source=. --remote=origin`
                      Ne pas creer → comme "Aucun pour l'instant"
```

> `org` / `project` de `project-config.json` sont extraits du remote (section "Generation de la Configuration") ;
> sans remote, ils restent vides et les etapes GitHub de la finalisation sont sautees.

---

## Etape 2 : Stack Backend

```
AskUserQuestion : "Quelle technologie backend utilises-tu ?"
- Node.js (JavaScript/TypeScript) — Express, Fastify, NestJS... genere les agents et templates
  dev-backend-node
- Python (FastAPI/Django/Flask) — genere les agents et templates dev-backend-python
- Go — genere les agents et templates dev-backend-go
- Aucun backend — projet frontend/mobile/firmware seul, pas d'agent dev-backend genere
(Autre technologie — Java/Kotlin, C#/.NET, PHP, Ruby, Rust... — saisie libre via "Autre")
```

---

## Etape 3 : Stack Frontend

```
AskUserQuestion : "Quelle technologie frontend utilises-tu ?"
- React (Vite/CRA) — SPA classique, genere les agents et templates dev-frontend-react
- React (Next.js) — SSR/routing integre, genere les agents et templates dev-frontend-react
  (variante Next.js)
- Vue.js (Vite ou Nuxt — preciser lequel via "Autre" si besoin) — genere les agents et
  templates dev-frontend-vue
- Aucun frontend — backend/API seul ou mobile/firmware seul, pas d'agent dev-frontend genere
(Autre technologie — Angular, Svelte/SvelteKit, HTML/CSS/JS vanilla... — saisie libre via "Autre")
```

---

## Etape 4 : Mobile (optionnel)

```
AskUserQuestion : "As-tu une application mobile ?"
- React Native — partage du code avec le frontend React eventuel
- Flutter — stack Dart independante, cross-platform
- Natif (iOS Swift/SwiftUI ou Android Kotlin — preciser lequel via "Autre" si besoin)
- Pas de mobile — aucun agent mobile genere
(Autre techno — Capacitor/Ionic... — saisie libre via "Autre")
```

---

## Etape 5 : Firmware/Hardware (optionnel)

```
AskUserQuestion : "As-tu du code firmware ou embarque ?"
- ESP32 (Arduino/PlatformIO) — genere l'agent dev-firmware-esp32
- Raspberry Pi — carte complete, souvent Linux embarque plutot que firmware bas niveau
- Pas de firmware — aucun agent firmware genere
(Autre carte/microcontroleur — ESP8266, Arduino AVR, STM32... — saisie libre via "Autre")
```

---

## Etape 5b : Plugin (optionnel)

```
AskUserQuestion : "Ton projet inclut-il un plugin pour une plateforme existante ?"
- VS Code Extension — genere les agents/templates plugin cibles VS Code
- Browser Extension (Chrome/Firefox) — genere les agents/templates plugin navigateur
- Pas de plugin — aucun agent plugin genere
(Autre plateforme — Obsidian, WordPress, plugin applicatif maison... — preciser via "Autre")
```

---

## Etape 5c : Agents generiques — projets non-dev (optionnel)

Pour les taches hors developpement (redaction de presentation, documents metier, analyses...). Chaque
instance = un agent `generic` (template commun `agents/generic.md`) + une **specification** propre
`.claude/agents/generic.<nom>.md`. Plusieurs instances possibles, chacune avec sa specification.
Un projet purement non-dev peut repondre « Pas de ... » aux etapes 2 a 6 et ne declarer que des agents generiques.

```
AskUserQuestion : "Ton projet a-t-il des taches hors developpement a confier a un agent specialise ?"
- Oui — declarer un agent generique (ex: redacteur de PowerPoint, redacteur de documentation metier)
- Non — aucun agent generique
```

Si **Oui**, pour chaque instance (boucler tant que l'utilisateur en ajoute) :

```
AskUserQuestion (texte libre via "Autre") :
- Nom canonique (kebab-case, unique dans la team, ex: redacteur-pptx) — jamais un nom d'agent existant
  (planner, qa, deployer, dev-*...) ni `generic`
- Role en une ligne
- Specification : perimetre, entrees, livrables (formats/chemins), outils/charte, criteres de validation
- Spawn : permanent (defaut — spawne au /start-session) | ponctuel (spawne a la demande)
```

Puis « Ajouter une autre instance ? ». Les reponses alimentent `agents.generic[]` et la specification
(voir "2bis. Agents generiques").

---

## Etape 5d : Agents de developpement — instances multiples et stacks sans template

Recapituler les agents de stack deduits des etapes 2 a 5b avec la **table de correspondance stack → agent de
stack** (section "Registre des agents") et les afficher : `dev-backend ← templates/dev-backend-go.md`, ...
Une reponse « Autre » (Rust, STM32, Flutter...) donne un agent `dev-<categorie>` base sur `dev-generic`.

Pre-remplir depuis la detection de l'Etape 0 : plusieurs racines de composant d'une meme categorie (ex. deux
`platformio.ini`, deux `go.mod`, plusieurs `package.json` serveur) → proposer directement les instances.

```
AskUserQuestion : "Un meme type de composant est-il present plusieurs fois dans le projet ?"
- Non, un agent par categorie (Recommande si rien n'a ete detecte) — un seul dev-backend, dev-firmware...
- Oui — plusieurs composants d'une meme categorie (ex. firmware capteur + firmware passerelle) : un agent
  par composant, chacun avec son perimetre
```

Si **Oui**, pour chaque composant (boucler tant que l'utilisateur en ajoute, puis « Ajouter un autre composant ? ») :

```
AskUserQuestion (texte libre via "Autre") :
- Categorie et template — options issues de la table de correspondance (ex. "firmware — dev-firmware-esp32")
- Suffixe du nom (kebab-case) → agent dev-<categorie>-<suffixe>, unique dans la team (ex. dev-firmware-capteur)
- Role en une ligne (ex. "Firmware capteur — acquisition et envoi LoRa")
- Perimetre : dossiers/globs dont l'agent est responsable (ex. firmware/sensor/**) — disjoints entre instances
```

Pour chaque agent base sur **`dev-generic`** (stack « Autre »), completer sa specification de stack — deduite
du code detecte a l'Etape 0 si possible, sinon demandee (texte libre via "Autre") :

```
- Stack : langage, version, framework, cible (ex. "Rust 1.80, Axum, Linux x86_64")
- Toolchain et commandes : build, tests (complets / rapides / cibles), lint, format, audit — commandes exactes
- Conventions : structure du projet, style, gestion d'erreurs (optionnel)
```

Les reponses alimentent `agents.registry[]` (`name`, `source`, `role`, `paths`) et les compagnons `<name>.md`
(voir "2. Agents dev-* et registre des agents").

---

## Etape 6 : Base de Donnees

```
AskUserQuestion : "Quelle base de donnees utilises-tu ?"
- PostgreSQL — relationnel, choix par defaut pour la plupart des stacks backend generees
- MySQL / MariaDB — relationnel, alternative a PostgreSQL
- MongoDB — document, pour un modele de donnees non relationnel
- Aucune — pas de persistance geree par le template
(Autre — SQLite, Redis, Firebase/Firestore, Supabase, ou plusieurs bases combinees —
preciser via "Autre")
```

---

## Etape 7 : CI/CD

```
AskUserQuestion : "Quel systeme CI/CD utilises-tu ?"
- GitHub Actions — audite le workflow de release existant, sinon le genere selon la stack et les livrables
- GitLab CI — meme demarche avec `.gitlab-ci.yml`
- Aucun — pas de pipeline CI/CD genere, /build et /publish resteront manuels
(Autre — Jenkins, CircleCI, Azure DevOps, Bitbucket Pipelines... — preciser via "Autre")
```

---

## Etape 8 : Environnements et Deploiement

```
AskUserQuestion : "Comment deploies-tu ton application ?" (mecanisme d'installation par defaut)
- Docker / Docker Compose — genere le template environments/docker-compose
- Kubernetes / Helm — genere le template environments/kubernetes-helm
- Serverless (AWS Lambda, Vercel, Netlify) — genere le template environments/serverless
- VPS / Bare metal — genere le template environments/vps
(Autre mecanisme — PaaS type Heroku/Railway/Render, Cloud Run/App Engine... — preciser via
"Autre")

AskUserQuestion : "Quels environnements de release utilises-tu, dans l'ordre de promotion ?"
- QUALIF puis PROD (Recommande) — chaine standard, seule cablee dans l'orchestration CDP
  automatisee (GATE 1 a 4)
- DEV puis QUALIF puis PROD — ajoute un environnement DEV manuel (hors flux CDP automatise)
  avant QUALIF
- QUALIF puis PRE-PROD puis PROD — ajoute un environnement PRE-PROD manuel (hors flux CDP
  automatise) entre QUALIF et PROD
(Autre chaine personnalisee — lister les noms dans l'ordre de promotion — via "Autre")
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

### Etape 8b : Acces aux environnements (collecte + validation)

Declarer un mecanisme ne suffit pas : un environnement injoignable ou mal configure (mauvais hote, cle
refusee, runtime absent, droits insuffisants) doit etre decouvert **ici, au cadrage**, pas au premier
`/deploy`. Pour **chaque** entree d'`infrastructure.environments[]` (Etape 8), collecter les elements
d'acces **puis les tester**. Cette etape est conduite avec le **role de l'agent `infra`** : lire
`TEMPLATE_claude/agents/infra.md` section « Mode Acces (cadrage) » (grille de collecte, sondes, verdicts)
et l'appliquer — meme demarche que l'audit CI/CD (§ 3), `infra` n'etant pas un teammate actif pendant
l'init. L'utilisateur peut aussi deleguer la verification a un acces qu'il n'a pas lui-meme
(administrateur de la plateforme) : dans ce cas, produire la liste exacte des commandes a lui faire
executer et l'attendre.

**1. Collecter** (AskUserQuestion, une serie par environnement, selon `deploy.mechanism` et `publish.mode`) :

| Mecanisme | Elements a demander | Variables secretes (noms seulement → `<env>.env.example`) |
|-----------|--------------------|------------------|
| `docker-compose` / `vps` | hote (vide = deploiement local), utilisateur, port SSH, **runtime** (`docker` ou `podman`, compose inclus), repertoire cible et droits d'ecriture, registre d'images, **services a deployer** (liste ; vide = tous — ecrit dans `deploy.services`, les autres services du compose ne sont jamais touches) | `SSH_KEY_PATH`, `REGISTRY_USER`, `REGISTRY_PASSWORD` |
| `kubernetes` / `helm` | contexte kube, namespace, registre, **deployments a mettre a jour** (`deploy.services`, vide = `app`) | `KUBE_CONTEXT`, `KUBECONFIG` |
| `serverless` / `paas` / `cloud-run` | **plateforme** (`access.platform` : aws/vercel/netlify, heroku/railway/render, gcp), compte/projet (`access.project`), region (`access.region`), CLI de la plateforme | jetons de la section 3bis |
| `publish.mode = promote` | registre ou depot de stockage de l'artefact et droit de push | `REGISTRY_USER`, `REGISTRY_PASSWORD` |
| `publish.mode = rebuild-ci` | droit de pousser un tag sur le depot, secrets configures cote CI | — |

Regles : **jamais de valeur secrete** dans `project-config.json`, la conversation ou un fichier commite —
uniquement des **noms** de variables (valeurs dans `<env>.env`, gitignore, que l'utilisateur renseigne
lui-meme avant le test ; s'il n'est pas pret, passer au point 3 « Reporter »). Un acces
« je ne sais pas encore » est un resultat valide : il est enregistre `unknown`, pas devine.

**2. Valider** — sondes **lecture seule**, `<env>.env` charge dans le shell du test (ordre de
`agents/deploy.md` « Variables et secrets »), un resultat par sonde :

```bash
# vps / docker-compose / podman — joignabilite SSH sans mot de passe, runtime, droits sur la cible
ssh -o BatchMode=yes -o ConnectTimeout=10 -i "$SSH_KEY_PATH" -p "$PORT" "$USER@$HOST" 'true'
ssh ... "command -v $RUNTIME && $RUNTIME info >/dev/null && $RUNTIME compose version"
ssh ... "test -w '$TARGET_DIR' || mkdir -p '$TARGET_DIR'"        # le repertoire est-il ecrivable ?
printf '%s' "$REGISTRY_PASSWORD" | $RUNTIME login "$REGISTRY" -u "$REGISTRY_USER" --password-stdin  # push/pull
# kubernetes / helm
kubectl --context "$KUBE_CONTEXT" auth can-i create deployments -n "$NAMESPACE"
# serverless / paas / cloud-run : commande « whoami » de la CLI (aws sts get-caller-identity,
# vercel whoami, heroku auth:whoami, gcloud auth list...)
# rebuild-ci : droits du jeton sur le depot
gh api "repos/<owner>/<repo>" --jq '.permissions.push'
```

Aucune sonde ne cree de ressource, ne deploie ni ne modifie l'etat distant (le `mkdir` du repertoire cible
est le seul effet, propose a l'utilisateur et sans importance si le repertoire existe deja).

**3. Verdict par environnement** — afficher le tableau (✓ OK / ✗ ECHEC avec l'erreur brute / ? NON TESTE),
puis pour tout environnement non `ok`, AskUserQuestion :
- Corriger et retester (Recommande) — `infra` propose le correctif precis (cle a installer, utilisateur
  a ajouter au groupe `docker`, port a ouvrir...), l'utilisateur l'applique, on relance les sondes
- Basculer sur un autre mecanisme/hote — retour a l'Etape 8 pour cet environnement
- Reporter — l'environnement est enregistre `verified.status: "skipped"` ; l'ecart est reporte dans le
  rapport final et bloquera `/deploy <env>` (preflight, `agents/deploy.md`) jusqu'a validation

Jamais de passage silencieux : un environnement non valide ne doit apparaitre nulle part comme « pret ».

**4. Enregistrer** dans `project-config.json` (schema ci-dessous, section « 1. project-config.json »),
sans secret : `infrastructure.environments[].access` — coordonnees non sensibles, **noms** des variables
requises (`credentials_env`) et `verified` (date, statut, sondes jouees). Reporter ces memes noms dans
`<env>.env.example` (section 3bis).

---

## Etape 9 : Tests

```
10. Quels frameworks de tests utilises-tu ? (propositions par defaut selon la stack choisie
    aux etapes 2/3 — preciser si different)
    a) Tests unitaires backend : [defaut deduit, ex. `go test` pour Go, Jest/Vitest pour Node]
    b) Tests unitaires frontend : [defaut deduit, ex. Vitest/Jest pour React/Vue]
    c) Tests E2E : Playwright (recommande)
```

---

## Etape 10 : Securite

```
AskUserQuestion (`multiSelect: true`) : "Quels aspects securite sont importants pour ce projet ?"
- Authentification utilisateurs — active les checks de gestion de session/mots de passe pour
  l'agent security
- Donnees sensibles (RGPD, sante, finance) — active les checks de protection des donnees
  personnelles/sensibles
- Paiements (PCI-DSS) — active les checks specifiques au traitement de paiements
- Aucun aspect particulier — n'active aucun check securite specifique (base uniquement)
(Autre aspect — API publique, multi-tenant... — preciser via "Autre" ; plusieurs choix possibles)
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
  "initialized_from": "analysis|manual|workshop|brief",
  "template_version": { "tag": "<LATEST_TAG>", "commit": "<LATEST_COMMIT>" },
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
        "deploy":  { "mechanism": "docker-compose", "target": "docker-compose.qualif.yml", "services": ["api", "web"] },
        "access":  {
          "method": "ssh", "host": "qualif.example.com", "user": "deploy", "port": 22,
          "runtime": "podman", "registry": "registry.example.com",
          "credentials_env": ["SSH_KEY_PATH", "REGISTRY_USER", "REGISTRY_PASSWORD"],
          "verified": { "status": "ok", "at": "2026-10-22T11:30:00Z", "checks": ["ssh", "runtime", "target-dir", "registry"] }
        }
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
    "e2e": ["playwright"],
    "components": { "<composant>": ["<glob des sources du composant>"] },
    "regression_at_qa": "gated",
    "full_regression_at": "qualif",
    "coverage_min": 70,
    "lot_max_tests": 50,
    "perf": { "p95_ms": 200, "p99_ms": 500, "error_rate_max": 0.001 }
  },
  "security": {
    "concerns": ["auth", "api-public"]
  },
  "commands": {
    "build": "<BUILD_CMD>",
    "test": "<TEST_CMD>",
    "lint": "<LINT_CMD>",
    "audit": "<AUDIT_CMD>",
    "ci": "<CI_CMD : script unique de la CI, ex. scripts/ci.sh>",
    "typecheck": "<TYPECHECK_CMD>",
    "coverage": "<COVERAGE_CMD>",
    "test_fast": "<TEST_FAST_CMD>",
    "test_targeted": "<TEST_TARGETED_CMD avec {TARGETS}>",
    "smoke": "<SMOKE_CMD optionnel>"
  },
  "docs": {
    "mockup_dir": "docs/mockup"
  },
  "marketing": {
    "site": "auto"
  },
  "agents": {
    "idle_ttl_minutes": 15,
    "idle_warning_interval_minutes": 5,
    "generic": [
      { "name": "redacteur-pptx", "role": "Redaction de presentations PowerPoint", "spawn": "permanent" }
    ],
    "registry": []
  }
}
```

> `agents.registry` : registre des agents deployes (provenance et version de chaque copie) — cree vide ici,
> rempli par la section "2. Agents dev-*" ci-dessous. Format et fonctions : section "Registre des agents".

> `agents.generic` : instances d'agents generiques (Etape 5c) — `[]` ou absent si aucune. Chaque `name`
> est unique et correspond a un fichier `.claude/agents/generic.<name>.md`.

Valeurs a deriver si elles ne sont pas fournies explicitement :

| Champ | Derivation |
|-------|-----------|
| `template_version` | `{tag, commit}` du template effectivement deploye (`$LATEST_TAG`/`$LATEST_COMMIT` resolus en section "Fetch du Template depuis GitHub"). Ecrit/mis a jour a **chaque** fetch (init, sync, reinstallation) — seule copie durable (trackee git) de cette info, `TEMPLATE_claude/.template-source.json` etant gitignore avec le reste de `TEMPLATE_claude/`. Sert a detecter et reinstaller a l'identique si les fichiers template disparaissent (clone frais) sans forcer une mise a jour — voir section "Reinstallation des fichiers template" |
| `team_name` | `<PROJECT_NAME>-team` (minuscules, tirets) |
| `org` | `git remote get-url origin` → extraire l'organisation GitHub |
| `project` | `git remote get-url origin` → extraire le nom du repo (sans `.git`) |
| `commands.build` | Stack backend : `go build ./...` / `npm run build` / `python -m build` |
| `commands.test` | Stack : `go test ./...` / `npm test` / `pytest` |
| `commands.lint` | Stack : `golangci-lint run` / `npm run lint` / `ruff check .` |
| `commands.audit` | Stack : `govulncheck ./...` / `npm audit` / `pip-audit` |
| `commands.ci` | `CI_CMD` : **script unique** de la CI (defaut `scripts/ci.sh`, genere par l'etape "3. Workflow CI/CD" s'il n'existe pas) enchainant build, lint/vet, tests (+ detecteur de course si la stack en a un), `commands.audit`, build frontend. Appele par les workflows (release et pre-validation), `qa` et `deploy` — voir `agents/infra.md` §3bis C11 |
| `commands.typecheck` | Frontend TS : `npm run typecheck` / `tsc --noEmit` — vide sinon |
| `commands.coverage` | Stack : `go test -cover ./...` / `npm run test -- --coverage` / `pytest --cov` |
| `marketing.site` | Defaut `"auto"` : un site marketing est attendu (branche `gh-pages`, uniquement — `MARKETING/` en est le worktree git, jamais un dossier de la branche de code) ; s'il n'existe pas, l'agent marketing declenche une initialisation (questions de cadrage + maquette). `false` = ordre direct de ne pas avoir de site (le CDP dispatche `PREPARE ... — SANS SITE`) |
| `docs.mockup_dir` | Defaut `docs/mockup` (dossier des maquettes validees — voir `context/COMMON.md` §14) |
| `commands.test_fast` | Boucle DEV : tests hors tag `slow`. Stack : `go test -short ./...` / `npx vitest run --exclude "**/*.slow.*"` / `pytest -m "not slow"` — vide sinon (les dev-* retombent sur `commands.test_targeted`) |
| `commands.test_targeted` | Tests d'un sous-ensemble, `{TARGETS}` = fichiers ou dossiers. Stack : `go test {TARGETS}` / `npx vitest run {TARGETS}` / `pytest {TARGETS}` |
| `commands.smoke` | Optionnel : tests tagues `smoke` (verification post-deploy). Absent → `curl /health` |
| `testing.components` | Composant → globs de sources (ex. `"http_server": ["server-go/internal/server/**"]`) — sert a selectionner les NR impactees (`context/COMMON.md` 15.3). Derive de l'arborescence detectee (Etape 0) ; a defaut, un composant par dossier de premier niveau de `src_dir` |
| `testing.regression_at_qa` | Defaut `gated` (`gated` \| `parallel` \| `none`) — `context/COMMON.md` 15.3 |
| `testing.full_regression_at` | Defaut `qualif` (`qualif` \| `build` \| `prod`) — `context/COMMON.md` 15.4 |
| `testing.coverage_min` | Defaut `70` (seuil minimal de couverture, en %) |
| `testing.lot_max_tests` | Defaut `50` (nombre max de cas de test par lot ; QA execute et rapporte lot par lot) |
| `src_dir` | Detection Etape 0 (repertoire source principal) ou stack par defaut : `src`, `cmd`... |
| `version_file` | Fichier source de verite de la version (ex: `package.json`, `config.json`, `VERSION`) |
| `infrastructure.environments` | Defaut `[QUALIF, PROD]` (Etape 8, question 9bis) ; `publish.mode` = `promote` pour tous sauf le dernier (`rebuild-ci`) ; `deploy.mechanism` reprend la reponse a la question 9 pour chaque environnement, sauf personnalisation explicite |
| `infrastructure.environments[].access` | Etape 8b : coordonnees non sensibles d'acces (`method`, `host`, `user`, `port`, `runtime`, `registry`/`context`...), **noms** des variables secretes (`credentials_env`) et resultat des sondes (`verified.status` : `ok` / `failed` / `skipped` / `unknown`). Jamais de valeur secrete. Lu par le preflight de `agents/deploy.md` et par les fichiers `deploy.<env>` (runtime, hote, port). `deploy.services` (optionnel) : seuls ces services/unites sont deployes |
| Nom de fichier des environnements | `infrastructure.environments[].name` normalise : minuscules, espaces/underscores → tirets (ex. `PRE-PROD` → `pre-prod`) — utilise pour `.claude/agents/environments/{publish,deploy}.<nom>.template.md` (section 3bis) |

### 2. Agents dev-* et registre des agents

Les agents de stack sont choisis avec la **table de correspondance stack → agent de stack** de la section
"Registre des agents" (`dev-backend`, `dev-frontend`, `dev-firmware`, `dev-plugin` — une stack sans template
est signalee `[?]`, aucun agent genere). Chacun est deploye en `.claude/agents/<name>.template.md`, avec un
compagnon `.md` optionnel pour les adaptations projet, et inscrit au registre ; les agents communs, deja
deployes a l'etape 4 du fetch, y sont inscrits aussi :

```bash
# Agents communs (deja deployes avec leur en-tete a l'etape 4 du fetch) → registre
for f in TEMPLATE_claude/agents/*.md; do
  registry_set "$(basename "$f" .md)" "agents/$(basename "$f")" "$(src_sha "$f")" "$FETCH_TAG" "$FETCH_COMMIT"
done
# Agents de stack : une ligne par agent retenu (Etape 5d), puis role, perimetre et compagnon, ex. :
deploy_and_register dev-backend templates/dev-generic.md
registry_meta       dev-backend "Backend (Rust/Axum)"
deploy_and_register dev-firmware-capteur templates/dev-firmware-esp32.md
registry_meta       dev-firmware-capteur "Firmware capteur (ESP32)" '["firmware/sensor/**"]'
deploy_and_register dev-firmware-passerelle templates/dev-firmware-esp32.md
registry_meta       dev-firmware-passerelle "Firmware passerelle (ESP32)" '["firmware/gateway/**"]'
for n in $(jq -r '.agents.registry[] | select(.source | startswith("templates/")) | .name' "$CFG"); do
  ensure_companion "$n"
done
```

Remplir ensuite chaque compagnon cree avec les reponses de l'Etape 5d (`## Stack` et `## Toolchain et commandes`
**non vides** pour `dev-generic`, `## Perimetre` pour une instance) — trackes git, jamais ecrases par la sync.
Ajouter une ligne par agent de stack dans la table `## Agents Disponibles` de `CLAUDE.md` :
`| <name> | <role> | .claude/agents/<name>.template.md | permanent |` (la ligne `dev-<categorie>` du template est
retiree quand la categorie a des instances).

En **reconfiguration** (stack modifiee), meme procedure : un agent de stack dont la `source` change (ex.
Node.js → Go) est redeploye avec `deploy_and_register` ; un agent de stack retire de la stack est supprime
(`rm .claude/agents/<name>.template.md` puis `registry_del <name>`), son compagnon eventuel etant signale
comme orphelin (d1e).

### 2bis. Agents generiques (projets non-dev)

Pour chaque entree de `agents.generic[]` (Etape 5c) — idempotent, **ne jamais ecraser** une specification existante :

```bash
# Le template commun generic.template.md est deja deploye avec les autres agents (etape 4 du fetch)
for NAME in $(jq -r '.agents.generic[]?.name' .claude/project-config.json); do
  SPEC=".claude/agents/generic.${NAME}.md"
  ROLE=$(jq -r --arg n "$NAME" '.agents.generic[] | select(.name==$n) | .role' .claude/project-config.json)
  [ -f "$SPEC" ] || cat > "$SPEC" <<SPEC_EOF
# ${NAME} — ${ROLE}

## Role et perimetre
## Entrees
## Livrables
## Outils et conventions
## Criteres de validation
SPEC_EOF
done
```

Remplir chaque section avec les reponses de l'Etape 5c (specification **non vide** — un agent sans
specification repond `BLOQUE` au demarrage). Le fichier est **tracke git**, jamais ecrase par la sync
(le compagnon `generic.md` sans nom reste, lui, un compagnon classique du template `generic.template.md`).
Si l'instance justifie un autre modele ou effort que `generic.template.md` (`sonnet` / `medium`), ajouter en tete
de la specification un frontmatter ne contenant que `model:` et/ou `effort:` (ex. `opus` pour un redacteur de
documents longs) — resolu au spawn, voir `agents/context/TEAMMATES_PROTOCOL.md` section 7.

Ajouter une ligne par instance dans la table `## Agents Disponibles` de `CLAUDE.md` :
`| <name> | <role> | .claude/agents/generic.template.md + .claude/agents/generic.<name>.md | <spawn> |`.
Le CDP les connait via cette table et `agents.generic[]` (voir `agents/cdp.md`).

### 3. Workflow CI/CD

Le pipeline de release n'est plus copie d'un modele : il est **audite s'il existe, genere sinon**, selon le
contrat de `TEMPLATE_claude/agents/infra.md` section 3bis (exigences C1 a C11). Si l'utilisateur a repondu
« Aucun » a l'Etape 7, ne rien faire.

1. **Detecter** : `.github/workflows/*.y*ml`, `.gitlab-ci.yml`.
2. **Pipeline existant → audit** : evaluer C1-C11 (verdict `CONFORME` / `ECART` / `NON APPLICABLE`, fichier et ligne
   cites), afficher le tableau, puis AskUserQuestion :
   - Corriger les ecarts (Recommande) — patch minimal, diff affiche et valide avant ecriture
   - Garder tel quel — ecarts consignes dans le rapport final (un ecart C1/C5 reste signale comme bloquant
     pour le mode `rebuild-ci`)
   - Regenerer — remplace le pipeline apres sauvegarde (`.bak`)
   Ne jamais ecraser ni modifier sans reponse.
3. **Aucun pipeline → generation** : ecrire d'abord le script `CI_CMD` (`commands.ci`, C11) s'il n'existe pas, puis
   generer **deux workflows** qui n'y font que `run: $CI_CMD` : pre-validation `.github/workflows/ci-branch.yml`
   (C10 : push `milestone/*` + `workflow_dispatch`, lecture seule, sans publier) et release. Composer le pipeline a partir de `project-config.json` (stack, livrables,
   `version_file`, `commands.*`) en suivant le contrat, l'afficher, le faire valider (AskUserQuestion :
   Ecrire / Modifier / Annuler), l'ecrire a `infrastructure.environments[].publish.pipeline`
   (par defaut `.github/workflows/release.yml`), puis le valider syntaxiquement (`actionlint` si present).
   `TEMPLATE_claude/templates/workflows/release-go-react.yml` sert de **reference de niveau**, pas de modele.
4. Les evolutions ulterieures (nouveau livrable, changement de stack) passent par l'agent `infra`
   (Mode Modification) ; son Mode Validation rejoue l'audit avant `PUBLISH PROD`.

Placeholders de l'exemple de reference (informatif) :

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

# .gitignore projet — jamais ecrase : s'il existe deja, fusion non destructive
# (les lignes ajoutees par le projet sont conservees, seules les lignes template absentes sont ajoutees)
if [ -f .gitignore ]; then
  grep -vE '^\s*(#|$)' TEMPLATE_claude/gitignore-for-projects | while IFS= read -r l; do
    grep -qxF "$l" .gitignore || echo "$l" >> .gitignore
  done
else
  cp TEMPLATE_claude/gitignore-for-projects .gitignore
fi
```

Puis appliquer la section **"Site marketing — worktree `gh-pages`"** (detection d'un `MARKETING/` deja suivi).

#### Dossier des maquettes

Creer le squelette du dossier des maquettes (idempotent — ne jamais ecraser un fichier existant ;
egalement execute a la reinitialisation d'un projet existant qui n'a pas encore `docs.mockup_dir`
ni `INDEX.md`) :

```bash
MOCKUP_DIR=$(jq -r '.docs.mockup_dir // "docs/mockup"' .claude/project-config.json)
mkdir -p "$MOCKUP_DIR"

[ -f "$MOCKUP_DIR/INDEX.md" ] || cat > "$MOCKUP_DIR/INDEX.md" <<'INDEX_EOF'
# Index des maquettes

> Tenu exclusivement par le CDP — convention : `context/COMMON.md` section 14.

## Actives
| Composant | Feature | Fichier | Version | Relation |
|-----------|---------|---------|---------|----------|

## Obsolètes
| Fichier | Remplacée par | Version |
|---------|---------------|---------|
INDEX_EOF

[ -f "$MOCKUP_DIR/DECISIONS.md" ] || cat > "$MOCKUP_DIR/DECISIONS.md" <<'DECISIONS_EOF'
# Contraintes de conception

> Contraintes durables issues des refus/corrections de l'utilisateur, par composant.
> Le planner les respecte, QA les vérifie. Tenu par le CDP.
DECISIONS_EOF
```

Si `docs.mockup_dir` est absent de `project-config.json` (projet existant), l'ajouter avec la valeur par defaut
(migration additive, sans autre modification du fichier).

#### Index des tests

Creer `tests/INDEX.md` et `tests/METRICS.md` (idempotent — ne jamais ecraser un fichier existant ;
egalement execute a la reinitialisation d'un projet existant) :

```bash
mkdir -p tests

[ -f tests/INDEX.md ] || cat > tests/INDEX.md <<'TESTS_INDEX_EOF'
# Index des tests

> Tests de specification ecrits par le test-writer, ranges en lots `<famille>/<theme>/<lot>/` ; statuts tenus par le CDP.
> Le chemin porte l'identite ; cet index ne porte que l'etat : une ligne par lot (chemin termine par `/`),
> plus des lignes fichier pour les exceptions (quarantaine). Lot sans ligne = `feature`.
> Convention : `context/COMMON.md` section 15. Statuts : `feature` | `regression` | `quarantaine`.
> Tags : `smoke`, `critical`, `slow`.

| Chemin | Niveau | Composant | Feature | Statut | Tags |
|--------|--------|-----------|---------|--------|------|
TESTS_INDEX_EOF

[ -f tests/METRICS.md ] || cat > tests/METRICS.md <<'TESTS_METRICS_EOF'
# Metriques de tests

> Une ligne par verdict QA (tenu par le CDP) — sert a mesurer le taux de retours dus a la regression.

| Date | Milestone | Feature | Cycle | Verdict | feature | regression | quarantaine | environnement | flaky |
|------|-----------|---------|-------|---------|---------|------------|-------------|---------------|-------|
TESTS_METRICS_EOF
```

Si `testing.regression_at_qa`, `testing.full_regression_at`, `testing.coverage_min` ou `testing.lot_max_tests` sont absents de
`project-config.json` (projet existant), les ajouter avec leurs valeurs par defaut (migration additive).

#### Labels GitHub de suivi de phase

Créer les labels de phase sur le repo GitHub (idempotent — `--force` met à jour si déjà existant) :

```bash
gh label create "PLANNING"  --color "0075ca" --description "Plan à élaborer ou à valider"      --force
gh label create "PRET"      --color "bfdadc" --description "Plan validé, développement pas encore lancé" --force
gh label create "EN COURS"  --color "e4e669" --description "Issue en cours de développement" --force
gh label create "EN REVIEW" --color "d876e3" --description "Issue en cours de revue de code"  --force
gh label create "EN QA"     --color "f9d0c4" --description "Issue en cours de tests QA"       --force
gh label create "DONE"      --color "0e8a16" --description "Issue livrée et validée"           --force
```

> Si le repo n'a pas encore de remote GitHub configuré, sauter cette étape et noter dans
> le Message de Fin : "Labels GitHub à créer manuellement ou relancer /init-project après `git remote add`."

#### Premier milestone v0.1.0 et issues de base (nouveau projet)

Un projet qui demarre n'a ni milestone ni backlog : le cycle (`/feature`, planner, panneau milestone) n'a rien a
piloter. Creer le premier milestone `v0.1.0` (aligne sur `project-config.json` → `version: 0.1.0`) et ses issues de
socle, pour que la premiere session commence directement par `/feature #<n>`.

**Conditions (toutes requises, sinon sauter l'etape sans question et le dire dans le Message de Fin) :**

- Etape 0 : **projet vide** (aucun code detecte → `initialized_from` = `workshop`, `brief` ou `manual`). Code existant →
  ne rien creer : le projet a deja une histoire, `/milestone new` reste la voie.
- Remote GitHub configure (Etape 1, question 3) et `gh auth status` OK.
- Aucun milestone (ouvert ou ferme) et aucun tag `v*` (local ou distant) : sinon `v0.1.0` n'est pas le premier
  milestone, et ne serait pas posterieur a la derniere version livree (`/milestone new` Etape 2d).
- Etape d'**initialisation uniquement** : jamais executee par la reinitialisation / mise a jour d'un projet deja
  initialise (section "Reinitialisation (projet v3)").

```bash
REPO=$(gh repo view --json owner,name --jq '.owner.login + "/" + .name')
MILESTONES=$(gh api "repos/$REPO/milestones?state=all" --jq 'length')
TAGS=$(( $(git tag --list 'v*' | wc -l) + $(git ls-remote --tags origin 'v*' | wc -l) ))
[ "$MILESTONES" = "0" ] && [ "$TAGS" = "0" ] || echo "Milestones ou tags existants : milestone v0.1.0 non cree"
```

**Issues de base** — composees depuis les reponses de l'init (stack, CI/CD, environnements, workshop). Label de
**type** uniquement (`feature`, `context/GITHUB.md` §8.2) ; **aucun label de phase** : elles entrent dans le cycle
(`PLANNING`) au premier `/feature #<n>`. Toutes rattachees au milestone `v0.1.0`.

| # | Titre | Corps (resume) | Condition |
|---|-------|----------------|-----------|
| 1 | `Squelette du projet` | Arborescence `<SRC_DIR>`, `<BUILD_CMD>` et `<TEST_CMD>` qui passent (meme a vide), lint, `<VERSION_FILE>` a `0.1.0` | toujours |
| 2 | `Pipeline CI/CD de release` | Premier run vert du pipeline genere/audite (contrat `agents/infra.md` §3bis, C1-C11), script `commands.ci` | CI/CD ≠ "Aucun" (Etape 7) |
| 3 | `Publication et deploiement <ENV>` | `publish.mode` et `deploy.mechanism` du premier environnement (`infrastructure.environments[0]`, ex. QUALIF) operationnels de bout en bout | au moins un environnement (Etape 8) |
| 4 | `Documentation initiale (README)` | Objectif, installation, lancement, tests, contribution | toujours |
| 5+ | Un cas d'usage v1 par issue | Titre = cas d'usage (q6, fiche `docs/CADRAGE.md`) ; corps = probleme central (q2), solution (q5), hors perimetre (q7) ; labels `feature` + `roadmap` | `initialized_from` = `workshop` ou `brief` (questionnaire rapide : les ajouter ensuite via `/backlog`) |

Afficher la liste (titres, labels) puis AskUserQuestion :
`"Creer le milestone v0.1.0 avec ces N issues sur https://github.com/<REPO> ?"`
- Creer le milestone et les issues (Recommande)
- Creer le milestone seul — issues a ajouter ensuite via `/backlog`
- Ne rien creer — `/milestone new v0.1` plus tard

```bash
# Labels de type — absents d'un repo neuf, et `gh issue create` echoue sur un label inconnu
gh label create "feature" --color "a2eeef" --description "Nouvelle fonctionnalite"       --force
gh label create "roadmap" --color "fbca04" --description "Visible sur le site marketing" --force

# Milestone — titre = version seule (context/COMMON.md §5.7 : le titre fixe X.Y.Z pour tout le cycle)
gh api "repos/$REPO/milestones" --method POST -f title="v0.1.0" -f description="Release v0.1.0 — socle du projet"

# Issues — idempotent : un titre deja present dans le milestone (ouvert ou ferme) n'est pas recree
EXISTING=$(gh issue list --milestone "v0.1.0" --state all --json title --jq '.[].title')
for each issue (titre, labels, fichier corps) :
  grep -qxF "<titre>" <<< "$EXISTING" && continue
  gh issue create --title "<titre>" --label "<labels>" --milestone "v0.1.0" --body-file <fichier>
```

Corps d'issue (meme structure pour toutes, fichier temporaire dans le scratchpad) :

```markdown
## Objectif
<une phrase>

## Perimetre
- <element 1>
- <element 2>

## Critere de validation
- [ ] <verifiable : commande qui passe, URL qui repond, fichier present>
```

A reporter dans le Message de Fin : titre du milestone, URL (`https://github.com/<REPO>/milestone/<numero>`),
nombre d'issues creees — ou "non cree" avec la raison (code existant, pas de remote, milestones/tags existants,
refus de l'utilisateur).

#### Settings Claude Code

Les règles critiques du teamleader sont dans `CLAUDE.md` (bloc `TEAMLEADER_PROTOCOL`) — elles survivent aux compactages nativement via le chargement natif de CLAUDE.md.

Déployer `TEMPLATE_claude/settings.json` dans `.claude/settings.json` si absent, puis y écrire le **modèle et
l'effort du teamleader** résolus depuis les frontmatters (`.claude/agents/teamleader.md` compagnon s'il en a un,
sinon `.claude/agents/teamleader.template.md` — règle `agents/context/TEAMMATES_PROTOCOL.md` section 7). Les
autres clés de `settings.json` ne sont jamais touchées ; `model` et `effortLevel` sont **dérivées** (toute édition
manuelle est écrasée à la prochaine sync — la surcharge se fait dans le compagnon) :

```bash
[ -f .claude/settings.json ] || { cp TEMPLATE_claude/settings.json .claude/settings.json; echo "✓ .claude/settings.json créé"; }

# Frontmatter : lignes entre le premier '---' et le suivant ; valeur d'une clé (vide si absente)
fm_val() { awk -v k="$2" 'NR==1&&$0!="---"{exit} NR>1&&$0=="---"{exit} NR>1&&$1==k":"{print $2; exit}' "$1" 2>/dev/null; }
TL_MODEL=$(fm_val .claude/agents/teamleader.md model);   [ -n "$TL_MODEL" ]  || TL_MODEL=$(fm_val .claude/agents/teamleader.template.md model)
TL_EFFORT=$(fm_val .claude/agents/teamleader.md effort); [ -n "$TL_EFFORT" ] || TL_EFFORT=$(fm_val .claude/agents/teamleader.template.md effort)

if [ "$TL_EFFORT" = "max" ]; then
  # effortLevel n'accepte pas max : retirer la clé, l'utilisateur tape /effort max au lancement
  jq --arg m "$TL_MODEL" '.model=$m | del(.effortLevel)' .claude/settings.json > .claude/settings.json.tmp
  echo "  effort max : taper /effort max au lancement de chaque session"
else
  jq --arg m "$TL_MODEL" --arg e "$TL_EFFORT" '.model=$m | .effortLevel=$e' .claude/settings.json > .claude/settings.json.tmp
fi
mv .claude/settings.json.tmp .claude/settings.json
echo "✓ .claude/settings.json — teamleader : $TL_MODEL / $TL_EFFORT"
```

> Les teammates n'ont rien ici : leur modèle/effort (frontmatter du template, surchargé par le compagnon) est résolu
> et passé explicitement au spawn par `/start-session`.

#### Memoire projet et squelettes lus au demarrage (derniere etape)

`/start-session` lit `.claude/memory/MEMORY.md` (source de verite unique) et le CDP lit `contracts/CHANGELOG.md`.
Sans ces fichiers, le premier `/start-session` echoue. Les creer a la **toute fin** de l'init
(apres `project-config.json`, pour en tirer les valeurs) — idempotent, ne jamais ecraser un fichier existant ;
egalement execute a la reinitialisation d'un projet existant (option d) :

```bash
mkdir -p .claude/memory _work/handoff _work/reports contracts

PROJECT_NAME=$(jq -r '.name // ""' .claude/project-config.json)
VERSION=$(jq -r '.version // "0.1.0"' .claude/project-config.json)
BRANCH=$(git branch --show-current 2>/dev/null); BRANCH=${BRANCH:-main}

[ -f .claude/memory/MEMORY.md ] || cat > .claude/memory/MEMORY.md <<MEMORY_EOF
# Memoire projet — ${PROJECT_NAME}

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : ${VERSION}
- **Branche** : ${BRANCH}
- **Travail en cours** : aucun — projet initialise par /init-project
- **Issues actives** : aucune
- **Cadrage produit** : docs/CADRAGE.md (si present — workshop ou brief)

## Regles critiques

(aucune pour le moment)

## Corrections de comportement

(aucune pour le moment)
MEMORY_EOF

[ -f contracts/CHANGELOG.md ] || cat > contracts/CHANGELOG.md <<'CHANGELOG_EOF'
# Changelog des contrats

> Changements de contrats (API, schemas) : `BREAKING` | `CHANGED` | `ADDED`. Lu par le CDP au GATE 2.
CHANGELOG_EOF
```

> `_work/` est gitignore (purge a chaque `/start-session`, selective si un workflow interrompu est repris).
> `.claude/workflow-state.json` et `.claude/workflow/` (copie du plan valide, gitignore) ne sont **pas** crees ici :
> ils sont crees par le CDP au premier workflow. `.claude/memory/` est tracke — l'inclure dans le commit d'init.

---

### 6. Audit du contexte (`/context-audit`)

Derniere etape avant le message de fin : un init genere beaucoup de fichiers par substitution de placeholders
(commandes, agents, `environments/*`, `CLAUDE.md`, `project-config.json`) — c'est la que naissent les `{VARIABLE}`
non remplaces, les references vers un agent non genere et les incoherences entre `project-config.json`,
`CLAUDE.md` et les agents. `/context-audit` les detecte deja : on le reutilise tel quel, sans variante.

1. **Commiter l'etat initial AVANT l'audit** (fichiers d'init suivis par git, `.claude/memory/` inclus) pour que le
   diff de l'audit soit isole et facile a relire ou annuler :

   ```bash
   git add -A && git commit -m "chore(claude): init-project" || true   # || true : rien a commiter = ok
   ```

2. **Lancer `/context-audit`** en audit complet (sans scope), en suivant `commands/context-audit.md`. Les
   resolutions `Auto` (doublons, references cassees, optimisations) sont appliquees sans question, les `❓`
   passent par `AskUserQuestion`, comme d'habitude. Apres un init, `MEMORY.md` est vide : la migration
   MEMORY → docs (2.4) n'a rien a signaler.
3. **Presenter le rapport "post-init" en tete du message de fin** : ecarts corriges en Auto, ecarts arbitres, ecarts
   laisses en l'etat. Ne pas committer les corrections de l'audit : l'utilisateur relit `git diff` (le commit
   d'init sert de point de retour).

Si `/context-audit` est absent de `.claude/commands/` (deploiement incomplet), le signaler dans le rapport et
continuer — ne jamais bloquer la fin de l'init.

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

Memoire projet : .claude/memory/MEMORY.md initialisee (si absente)
Teamleader : <modele> / <effort> ecrits dans .claude/settings.json (surcharge : compagnon .claude/agents/teamleader.md)
Cadrage : docs/CADRAGE.md — <workshop|brief : N elements deduits, N precises>   (si workshop ou brief)
Agents generiques : <NOM> (.claude/agents/generic.<NOM>.md), ...   (si declares)

Audit contexte (/context-audit) : N ecarts corriges (Auto), N arbitres, N laisses — voir `git diff`
  (commit d'init = point de retour)

Milestone v0.1.0 : N issues de base — https://github.com/<org>/<repo>/milestone/<numero>
  (nouveau projet avec remote GitHub ; sinon "non cree — <raison>")

Agents generes (registre : project-config.json → agents.registry[], <tag>) :
- .claude/agents/dev-backend.template.md   ← templates/dev-backend-<stack>.md
- .claude/agents/dev-frontend.template.md  ← templates/dev-frontend-<stack>.md
- [?] dev-<categorie> — <techno> : aucun template de stack   (si stack « Autre »)

Commandes disponibles :
- /feature, /bugfix, /hotfix, /refactor
- /review, /qa, /secu
- /build, /publish qualif|prod, /deploy qualif|prod
- /milestone new/status/close
- /backlog, /marketing
- /progression, /context-audit
- /start-session, /end-session
- /team-status

Prochaine etape : /start-session, puis /feature #<n> sur la premiere issue du milestone v0.1.0

Bonne utilisation de Claude Code !
```

Puis afficher le tableau des teammates (`registry_report`, section "Registre des agents") : version deployee,
etat et compagnon de chaque agent.

---

## Reinitialisation (projet v3)

Si `project-config.json` + `TEMPLATE_claude/` existent :

### Etape pre-menu — Fetch GitHub + Calcul automatique des changements

Avant d'afficher le menu :

**1. Fetcher silencieusement le template depuis GitHub** (procedure "Fetch du Template depuis GitHub",
etapes 1 a 3 seulement — aucun deploiement avant le choix de l'utilisateur).
Si le fetch echoue (pas de reseau, gh non auth) → continuer avec le template local en place,
afficher un avertissement discret : `⚠ Fetch GitHub impossible — analyse basee sur le template local (sync du <date>).`

**2. Calculer les changements** entre `TEMPLATE_claude/` (maintenant a jour) et les fichiers deployes
(`.claude/commands/`, `.claude/agents/`). Meme logique que l'etape d3 ci-dessous — pour les agents,
`registry_status` (section "Registre des agents") : la version enregistree de chaque agent est affichee
(`[~] qa — v3.18.0 → <tag> : <explication>`, `[📌] dev-firmware — epingle v3.18.0`).

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

AskUserQuestion : "Le projet est deja initialise — que veux-tu faire ?"
- Appliquer les mises a jour detectees (Recommande si seuls des [+]/[~]/[!] sont listes) —
  synchronise commandes/agents/contextes depuis TEMPLATE_claude/, conserve project-config.json
- Reconfigurer completement — relance tout le questionnaire/workshop (ou l'Option a-bis si un brief est
  disponible), ecrase project-config.json existant
- Re-analyser le code — relance la detection automatique de stack pour rafraichir le diagnostic
  avant de choisir
- Annuler — ne modifie rien
(Pour modifier un seul parametre precis sans tout reconfigurer — preciser via "Autre". Ex : ajouter une
instance d'agent generique → Etape 5c puis "2bis. Agents generiques" uniquement, sans toucher au reste ;
ajouter un agent de developpement, ou scinder un agent dev en instances (ex. dev-firmware → dev-firmware-capteur +
dev-firmware-passerelle) → Etape 5d puis "2. Agents dev-* et registre des agents" uniquement : l'agent scinde est
retire (`rm .claude/agents/<name>.template.md`, `registry_del <name>`), son compagnon eventuel est propose comme
base des compagnons des instances puis supprime apres confirmation ;
cadrer ou recadrer le produit depuis un brief → Option a-bis seule, phases 1-4 et 6, stack inchangee :
`docs/CADRAGE.md` cree ou mis a jour, et les cas d'usage v1 nouveaux proposes comme issues via `/backlog`,
jamais de milestone `v0.1.0` cree ici)
```

### Option d : Appliquer les mises a jour detectees

> Le fetch GitHub a deja ete effectue au pre-menu — `TEMPLATE_claude/` est a jour.
> Cette option calcule le diff precis et deploie les changements dans `.claude/`.

#### Etape d0 — Registre des agents

Si `agents.registry` est absent de `project-config.json` → section "Registre des agents", "Migration —
projet anterieur au registre", avant toute autre etape. Le registre est ensuite la reference des etapes
d2, d3, d3b et d5 pour les agents.

#### Etape d1c — Audit du pipeline CI/CD de release

Appliquer l'etape "3. Workflow CI/CD" (sections Generation de la Configuration) en mode audit : detecter le
pipeline, evaluer C1-C11 (`agents/infra.md` section 3bis), proposer corrections ou generation si absent. Ne rien
modifier sans reponse de l'utilisateur ; ne pas re-poser l'Etape 7 si le systeme CI/CD est deja connu.

**Migration CI_CMD / pre-validation (projet deja initialise)** — a faire si `commands.ci` est absent de
`project-config.json` ou si C10/C11 sont en `ECART` :

1. **Config** : ajouter `commands.ci` (cle absente des configs anterieures ; `project-config.json` est par
   ailleurs conserve tel quel). Valeur proposee : `scripts/ci.sh` s'il existe, sinon ce chemin a creer.
2. **Script `CI_CMD`** : s'il n'existe pas, le deriver des etapes du workflow de release existant
   (build, vet/lint, tests, audit, build web — plus `commands.build/test/lint/audit`), l'afficher, puis
   AskUserQuestion : Ecrire (Recommande) / Modifier / Passer. Ne rien ecrire sans reponse ; ne jamais
   inventer une etape absente du workflow ni de la config.
3. **Workflow de pre-validation** : s'il n'existe pas, generer `.github/workflows/ci-branch.yml` (C10 : push
   `milestone/*` + `workflow_dispatch`, lecture seule, `run: $CI_CMD`), meme validation utilisateur.
4. **Workflow de release existant** : proposer (jamais impose, diff affiche, sauvegarde `.bak`) de reduire
   ses etapes de CI a `run: $CI_CMD` pour supprimer l'ecart local/CI ; « garder tel quel » est consigne dans
   le rapport (ecart C11).
5. **Fichier d'environnement `rebuild-ci`** : voir d1d (la barriere de pre-validation y vit).

#### Etape d1a — Site marketing : worktree `gh-pages`

Appliquer la section **"Site marketing — worktree `gh-pages`"** (ajout de `MARKETING/` au `.gitignore` et
proposition de migration si `MARKETING/` est suivi par la branche de code).

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

> ⚠ **SCOPE STRICT** : cette étape **crée uniquement les fichiers manquants** — contrairement aux
> agents (y compris `dev-*`, resynchronisés via le registre des agents), les fichiers
> `.claude/agents/environments/*.template.md` déjà présents ne sont **jamais**
> modifiés ici, même si leur source `TEMPLATE_claude/templates/environments/` a changé. C'est un
> filet de sécurité (éviter qu'`agents/deploy.md` référence un fichier inexistant), pas une
> synchronisation complète — limitation assumée, à signaler dans le rapport si applicable.
> Seule exception : la migration de la barriere de pre-validation (`rebuild-ci`), ci-dessous.

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

**Exception — migration de la barriere de pre-validation** : pour chaque environnement `publish.mode =
"rebuild-ci"`, si `publish.<env>.template.md` ne contient pas la section « BARRIERE » (`grep -q BARRIERE`),
il date d'avant la pre-validation CI. AskUserQuestion : Regenerer depuis
`TEMPLATE_claude/templates/environments/publish-rebuild-ci.md` (Recommande — ancien fichier sauvegarde en
`.bak`, diff affiche, cibles/variables relues dans `project-config.json`) / Garder tel quel (ecart consigne
au rapport : `deploy` applique alors seulement la regle generique de `agents/deploy.md`, sans commande de
barriere). Jamais d'ecrasement sans reponse ; les autres modes (`promote`, etc.) ne sont pas concernes.

Si `MISSING_ENV_FILES[]` non vide → exécuter pour ces seules entrées manquantes la génération
décrite en section "3bis. Fichiers d'environnement" (Génération de la Configuration), puis
informer :

```
⚠ Fichiers d'environnement manquants générés : publish.qualif, deploy.qualif
  (project-config.json déclare ces environnements mais les fichiers n'existaient pas encore —
   vérifier/ajuster les cibles générées, voir "Fichiers d'Environnement" dans agents/deploy.md)
```

#### Etape d1e — Audit du graphe de dépendances des fichiers de définition locaux

Exécutée **avant** tout déploiement (état de référence). Construit le graphe des fichiers de définition
du projet, puis détecte les **liens cassés** et les **orphelins**. Lecture seule : aucune modification ici —
les constats alimentent le rapport d4 et sont traités à l'étape d5 / vérifiés à l'étape d5e.

**Noeuds** : `CLAUDE.md`, `.claude/project-config.json`, `.claude/settings.json`, `.claude/memory/*`,
`.claude/agents/*.md` (compagnons, `dev-*`, `generic.<nom>.md`), `.claude/agents/*.template.md`,
`.claude/agents/context/*`, `.claude/commands/*` + `context/*`, `.claude/agents/environments/*`,
`docs/mockup/INDEX.md`, `docs/tests/INDEX.md` (s'ils existent).

**Racines** (points d'entrée, jamais orphelins) : `CLAUDE.md` (tables « Agents Disponibles » et « Commandes
Disponibles »), `project-config.json` (`agents.registry[]` → `<name>.template.md`, `agents.generic[]` → `generic.<nom>.md`,
`infrastructure.environments[]` → `environments/{publish,deploy}.<env>.*`, chemins `version_file`,
`pipeline`, `commands.*`), `settings.json` (hooks → scripts), fichiers template déployés
(`*.template.md`, `commands/*.md`) tant que leur source existe dans `TEMPLATE_claude/`.

**Arêtes** (lien = référence d'un noeud vers un autre) : colonne `Fichier` des tables de `CLAUDE.md` ;
`@import X` ; chemins `.claude/...`, `context/X.md`, `docs/...` (liens markdown ou entre backticks) ;
renvois « voir X section N » / ancres ; commandes de hooks (`settings.json`) ; pairage compagnon ↔ template
(`xxx.md` ↔ `xxx.template.md`).

```bash
# Extraction des références (à compléter par la lecture du contenu pour les renvois de section)
for f in CLAUDE.md .claude/agents/*.md .claude/agents/context/*.md .claude/commands/*.md \
         .claude/commands/context/*.md .claude/agents/environments/*.md; do
  [ -f "$f" ] || continue
  grep -oE '(@import +[A-Za-z0-9_./-]+\.md|(\.claude|docs|context|agents|commands)/[A-Za-z0-9_./*-]+\.(md|json|sh|env|ya?ml))' "$f" \
    | sed "s|^|$f -> |"
done
```

**Liens cassés** (`BROKEN`) — pour chaque arête :
- la cible n'existe pas (fichier absent, hors motifs `*` / `<...>` / `{...}` volontairement génériques) ;
- la section ou l'ancre citée n'existe pas dans la cible ;
- un `{PLACEHOLDER}` reste non substitué dans un fichier déployé ;
- une ligne de table `Agents Disponibles` / `Commandes Disponibles` pointe vers un fichier absent.

**Orphelins** (`ORPHAN`) — fichier non atteignable depuis une racine et non pairé à un template :
- compagnon `xxx.md` (agents, `agents/context/`, `commands/context/`) dont `xxx.template.md` n'existe plus
  ni dans `.claude/` ni dans `TEMPLATE_claude/` (template supprimé ou renommé — **à relier au nouveau nom si
  un renommage est détectable**) ;
- `generic.<nom>.md` dont `<nom>` n'est plus dans `agents.generic[]` ; `dev-*.md` dont l'agent n'est plus
  inscrit au registre des agents ;
- `environments/*.<env>.*` dont `<env>` n'est plus dans `infrastructure.environments[]` ;
- agent déployé (`.template.md`) ou fichier de `agents/` absent de la table `Agents Disponibles` ;
- `*.template.md` déployé dont la source a disparu de `TEMPLATE_claude/` (reliquat, voir d3).

Le résultat (liste `BROKEN[]` et `ORPHAN[]` avec fichier, cible/raison, correction suggérée) est conservé
pour d4 (rapport) et d5e (revérification). Aucun fichier de `TEMPLATE_claude/` n'est jamais candidat à
la suppression ou à la modification : **le template fait toujours foi**.

#### Etape d1f — Acces aux environnements (validation)

Un projet initialise avant l'Etape 8b n'a pas d'`access` dans `infrastructure.environments[]` ; un acces
`failed` / `skipped` / `unknown`, ou verifie il y a longtemps, reste un risque au prochain `/deploy`.
Lecture seule ici (rien n'est modifie avant d5) :

```bash
jq -r '.infrastructure.environments[]? | "\(.name)\t\(.access.verified.status // "absent")\t\(.access.verified.at // "-")"' \
  .claude/project-config.json
```

Pour tout environnement dont le statut n'est pas `ok` : l'inclure au rapport d4 (`[⚠] <ENV> — acces non
valide (<statut>)`), puis a l'etape d5 AskUserQuestion : Collecter et valider maintenant (Recommande —
rejouer l'Etape 8b pour ces seuls environnements, avec le role `infra`) / Plus tard (l'ecart reste affiche
a chaque sync et `/deploy <env>` refusera de partir tant que l'acces n'est pas valide).

#### Etape d2 — Calculer les noms deployes attendus

```bash
# Noms attendus pour les commandes — tous les *.md du top-level (glob non récursif)
# NB : context-audit.md est une commande normale, pas un fichier du sous-répertoire context/
EXPECTED_COMMANDS=$(for f in TEMPLATE_claude/commands/*.md; do
  basename "$f" .md
done)

```

Agents : pas de liste a calculer ici — les agents attendus (communs + agents de stack inscrits) et
leur statut viennent de `registry_status` (etape d3).

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

# Agents : statut par agent depuis le registre (section "Registre des agents") — agents communs ET
# agents de stack (dev-*), comparaison par empreinte de la source (insensible aux placeholders)
AGENTS_STATUS=$(registry_status)

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

# Contextes RELIQUAT : *.template.md déployé dont la source n'existe plus dans TEMPLATE_claude/
for dest in .claude/agents/context/*.template.md .claude/commands/context/*.template.md; do
  [ -f "$dest" ] || continue
  subdir=$(echo "$dest" | grep -o 'agents/context\|commands/context')
  [ -f "TEMPLATE_claude/${subdir}/$(basename $dest .template.md).md" ] || echo "RELIQUAT $dest"
done
```

Pour chaque fichier compare, determiner le statut :

| Statut | Critere |
|--------|---------|
| `NOUVEAU` | Present dans EXPECTED, absent de DEPLOYED |
| `MODIFIE` | Present dans les deux, contenu different |
| `INCHANGE` | Present dans les deux, contenu identique |
| `RELIQUAT` | Present dans DEPLOYED, absent de EXPECTED |

Agents — statuts de `registry_status` : `NOUVEAU` (agent commun ajoute au template), `MODIFIE` (source
changee depuis la version enregistree, ou version inconnue), `MANQUANT` (copie absente — redeployee),
`EPINGLE` (source changee mais agent fige par `pinned`, non mis a jour), `INCHANGE`, `RELIQUAT` (source
retiree du template, ou copie deployee hors registre).

Agents de stack **manquants** : pour chaque categorie renseignee dans `stack` (`backend`, `frontend`, `mobile`,
`firmware`, `plugin`) sans aucun agent `dev-<categorie>` ni `dev-<categorie>-*` au registre (typiquement une
stack « Autre » initialisee avant le repli `dev-generic`) → signaler `[?]` dans le rapport d4 et proposer
l'Etape 5d pour cette categorie seule (jamais d'ajout automatique : le compagnon de stack demande des reponses).

#### Etape d3b — Comparer la table "Agents Disponibles" (teammates)

> ⚠ **SCOPE STRICT** : cette comparaison (et sa mise à jour en d5d) ne porte que sur le
> *texte* de la table `## Agents Disponibles` dans `CLAUDE.md`. Elle ne crée, ne supprime
> et ne modifie **jamais** `.claude/agents/*.md` ni `.claude/agents/*.template.md` — la liste
> réelle des agents déployés pour le projet n'est jamais changée par cette étape. Un
> `RELIQUAT` ou un `NOUVEAU` ici signale un écart de *documentation*, pas une action sur
> les agents eux-mêmes.

**Calculer les lignes attendues**, selon la stack déjà configurée dans `project-config.json`
(les agents communs sont toujours attendus, les agents `dev-*` seulement s'ils sont inscrits au
registre des agents) :

```bash
BACKEND_LANG=$(jq -r '.stack.backend.language // ""'   .claude/project-config.json)
FRONTEND_LANG=$(jq -r '.stack.frontend.language // ""' .claude/project-config.json)
FRONTEND_FW=$(jq -r '.stack.frontend.framework // ""'  .claude/project-config.json)
PLUGIN_PLATFORM=$(jq -r '.stack.plugin.platform // ""' .claude/project-config.json)
BACKEND_TECH="$BACKEND_LANG"
FRONTEND_TECH="${FRONTEND_FW:-$FRONTEND_LANG}"

# Toujours attendus, independants de la stack
EXPECTED_TEAMMATES=(planner test-writer code-reviewer qa doc-updater deployer security infra)
# Agents de stack : ceux inscrits au registre (dev-backend, dev-frontend, dev-firmware, dev-plugin...)
while read -r n; do [[ -n "$n" ]] && EXPECTED_TEAMMATES+=("$n"); done < <(jq -r '.agents.registry[]? | select(.source | startswith("templates/")) | .name' .claude/project-config.json)
# Instances d'agents generiques declarees dans project-config.json (ligne attendue :
# Role = .role, Fichier = generic.template.md + generic.<nom>.md, Spawn = .spawn)
while read -r n; do [[ -n "$n" ]] && EXPECTED_TEAMMATES+=("$n"); done < <(jq -r '.agents.generic[]?.name' .claude/project-config.json)
```

Ligne attendue d'un agent de stack (registre) : `| <name> | <role> | .claude/agents/<name>.template.md | permanent |`,
`<role>` = `agents.registry[].role` (a defaut, le role de la ligne `dev-<categorie>` du template) — une ligne par
instance.

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

  Agents (registre — version enregistree → version fetchee) :
  [~] cdp          v3.18.0 → v3.22.0 — <explication courte>   ← modifie
  [~] dev-firmware v3.15.1 → v3.22.0 — <explication courte>   ← modifie (agent de stack)
  [+] new-agent    — → v3.22.0                                ← nouveau
  [↺] security     v3.22.0                                    ← manquant (copie restauree)
  [📌] qa          epingle v3.18.0 (source modifiee depuis)    ← non mis a jour
  [=] code-reviewer                      ← inchange (x7...)
  [!] old-agent                          ← RELIQUAT (absent du nouveau template)
  [?] dev-backend  — Rust : aucun agent de stack → repli dev-generic disponible (Etape 5d)

  Contextes (agents/context/ et commands/context/) :
  [~] TEAMMATES_PROTOCOL — <explication courte>   ← modifie
  [=] COMMON, DEV_COMMON, GITHUB, VALIDATION_COMMON, CDP_WORKFLOWS, DEVELOPMENT, QUALITY (7 inchangés)

  Teammates (table "Agents Disponibles" — documentation uniquement, agents non touchés) :
  [+] dev-plugin                          ← nouveau (stack plugin configurée, absente de la table)
  [~] dev-backend  — tech passée de Node.js à Go
  [!] dev-firmware                        ← RELIQUAT (stack firmware retirée du projet)
  [=] planner, test-writer, code-reviewer, qa, doc-updater, deployer, security, infra (8 inchangés)

  Graphe des fichiers de définition locaux (etape d1e, etat avant mise a jour) :
  [⛓] CLAUDE.md:42 → .claude/agents/old.md          ← lien casse (cible absente)
  [○] .claude/agents/context/FOO.md                 ← orphelin (template FOO supprime/renomme)

  Nouveaux   : N
  Modifies   : N
  Inchanges  : N
  Reliquats  : N  ← a supprimer (commandes, agents ET contextes)
  Liens casses : N   Orphelins : N  ← traites a l'etape d5 (confirmation) puis reverifies en d5e

AskUserQuestion : "Comment appliquer cette synchronisation ?"
- Tout appliquer et supprimer les reliquats (Recommande) — deploie nouveaux/modifies, supprime
  les fichiers reliquats listes ci-dessus
- Appliquer uniquement les nouveaux et modifies — deploie sans toucher aux reliquats (a nettoyer
  manuellement plus tard)
- Annuler — ne deploie rien, la synchronisation s'arrete ici
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

# Agents (communs ET de stack) : NOUVEAU/MODIFIE/MANQUANT deployes avec en-tete de provenance et
# inscrits au registre, EPINGLE laisses a leur version — section "Registre des agents"
registry_sync

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

Puis **etape 5 de la procedure de fetch** (`.template-source.json` et `template_version`), non executee au
pre-menu.

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

# Supprimer les agents reliquats (copie + entree du registre). Un reliquat d'agent de STACK
# (source templates/... retiree du template) n'est supprime qu'apres confirmation — voir "Registre des agents"
grep '^RELIQUAT' <<<"$AGENTS_STATUS" | while read -r _ name source info; do
  [[ "$source" == templates/* ]] && { echo "  ? ${name} : template de stack ${source} retire — confirmation requise"; continue; }
  rm -f ".claude/agents/${name}.template.md"; registry_del "$name"
  echo "  ✗ .claude/agents/${name}.template.md supprime (reliquat)"
done

# Supprimer les contextes reliquats (source disparue de TEMPLATE_claude/)
for dest in .claude/agents/context/*.template.md .claude/commands/context/*.template.md; do
  [ -f "$dest" ] || continue
  subdir=$(echo "$dest" | grep -o 'agents/context\|commands/context')
  if [ ! -f "TEMPLATE_claude/${subdir}/$(basename $dest .template.md).md" ]; then
    rm "$dest"
    echo "  ✗ ${subdir}/$(basename $dest) supprime (reliquat)"
  fi
done
```

**Orphelins et liens cassés (d1e) — après le déploiement :** recalculer `BROKEN[]`/`ORPHAN[]` (la suppression
des reliquats ci-dessus peut en créer : compagnon dont le template vient de disparaître). Les fichiers
`.template.md` et `TEMPLATE_claude/` ne sont jamais touchés ; seuls les fichiers PROJET le sont, **toujours
après confirmation** :

```
AskUserQuestion : "N orphelins / M liens cassés détectés dans les fichiers de définition du projet — que faire ?"
- Corriger automatiquement (Recommandé) — lien cassé : repointé vers le fichier renommé/déplacé s'il est
  identifiable sans ambiguïté, sinon la référence est retirée ; orphelin : relié à son nouveau template si un
  renommage est détecté, sinon supprimé s'il n'a plus de racine
- Inspecter un par un — décision au cas par cas (corriger / supprimer / conserver)
- Ignorer — rien n'est modifié, ils seront re-signalés à la prochaine sync
```

Un fichier suivi par git est retiré avec `git rm` (jamais `rm` seul, pour que la suppression soit commitée).

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
AskUserQuestion : "Comment traiter les doublons détectés entre template et fichiers projet ?"
- Nettoyer automatiquement (Recommandé) — supprime les fichiers IDENTIQUES, retire les règles
  couvertes des fichiers DERIVE-TEMPLATE/MIXTE, conserve le reste tel quel
- Inspecter fichier par fichier — décide au cas par cas, fichier par fichier (voir Option I)
- Ignorer — continue sans rien modifier, les doublons resteront signalés à la prochaine sync
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

AskUserQuestion : "Cette section est couverte par le template mis à jour — que faire ?"
- Retirer les redondances (Recommandé) — supprime la section du compagnon, le template
  s'applique seul désormais
- Conserver tel quel — garde la section dans le compagnon malgré le recouvrement (sera
  re-signalée aux prochaines sync)
- Éditer manuellement — n'applique rien automatiquement, ouvre le fichier pour édition
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

AskUserQuestion : "Lequel fait foi pour ce projet ?"
- Le template (nouveau) — adapte/retire la règle du compagnon, le template s'applique seul
- Le compagnon (projet) — dérogation projet assumée, conserve la règle telle quelle (sera
  re-signalée en CONFLIT aux prochaines sync, c'est attendu)
- Éditer manuellement — n'applique rien automatiquement, ouvre le fichier pour édition
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

#### Etape d5e — Vérification post-nettoyage (doublons et graphe)

Exécutée **après** le déploiement des templates (d5), le nettoyage des doublons (d5b), l'arbitrage des
conflits (d5c) et la mise à jour de la table (d5d). Objectif : prouver que les templates mis à jour sont
**seuls maîtres** et qu'il ne reste ni doublon ni lien cassé non assumé. Lecture seule, sauf proposition finale.

**1. Doublons résiduels — contrôle déterministe** (en plus de l'analyse sémantique de d5b) :

```bash
for tmpl in .claude/agents/*.template.md .claude/agents/context/*.template.md .claude/commands/context/*.template.md; do
  [[ -f "$tmpl" ]] || continue
  companion="$(dirname "$tmpl")/$(basename "$tmpl" .template.md).md"
  [[ -f "$companion" ]] || continue
  # a) compagnon identique au template -> doublon complet
  cmp -s "$tmpl" "$companion" && echo "DOUBLON-IDENTIQUE $companion"
  # b) paragraphes du compagnon repris mot pour mot dans le template (hors lignes vides et titres)
  norm() { grep -v '^\s*$' "$1" | grep -v '^#' | sed 's/[[:space:]]\+/ /g;s/^ //;s/ $//'; }
  norm "$companion" | while IFS= read -r l; do
    [[ ${#l} -ge 40 ]] && grep -qxF -- "$l" <(norm "$tmpl") && echo "DOUBLON-LIGNE $companion : ${l:0:60}..."
  done
done
# c) commandes « customisées » (d1b) devenues identiques au template
for name in "${CUSTOMIZED_COMMANDS[@]}"; do
  cmp -s "TEMPLATE_claude/commands/${name}.md" ".claude/commands/${name}.md" && echo "CUSTOM-INUTILE $name"
done
```

Relancer ensuite la classification sémantique de d5b sur l'état courant. Résultat attendu : **plus aucun
fichier `IDENTIQUE`/`DERIVE-TEMPLATE`/`MIXTE`**, hors éléments explicitement ignorés (« Ignorer » / « Conserver
tel quel ») ou arbitrés `[P]` en d5c — ceux-là sont listés comme **doublons assumés**, pas comme erreurs.

**2. Graphe** : relancer l'extraction de d1e. Résultat attendu : `BROKEN[]` et `ORPHAN[]` vides, hors éléments
ignorés par l'utilisateur. Vérifier en particulier qu'**aucun nettoyage de d5b/d5c n'a cassé un lien** (une
section retirée d'un compagnon alors qu'un fichier la référence, un compagnon supprimé encore cité dans
`CLAUDE.md`) et qu'aucun `{PLACEHOLDER}` ne subsiste dans les fichiers déployés.

**3. Rapport et reprise** :

```
Vérification post-nettoyage :
  Doublons résiduels  : N  (dont M assumés : ignorés/[P])
  Liens cassés        : N
  Orphelins           : N
```

Si N > 0 hors éléments assumés → `AskUserQuestion` : « Des doublons ou liens cassés subsistent — que faire ? »
(**Les traiter maintenant** (Recommandé) — relance d5b/d5c/d5 sur ces seuls éléments, puis d5e / **Laisser en
l'état** — signalés à la prochaine sync). Maximum 2 passes de reprise, pour éviter toute boucle ; au-delà,
lister les éléments restants dans le rapport final d8.

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

Appliquer la sous-section **"Settings Claude Code"** (section Finalisation) : création de `.claude/settings.json`
s'il est absent, puis écriture des clés dérivées `model` / `effortLevel` du teamleader (compagnon
`teamleader.md` > `teamleader.template.md`). Les autres clés sont conservées. Afficher l'ancienne et la nouvelle
valeur si elles diffèrent — un changement ici n'est effectif qu'au prochain lancement de la session (le rappeler
dans le rapport).

Puis appliquer la sous-section **"Memoire projet et squelettes lus au demarrage"** (creation idempotente de
`.claude/memory/MEMORY.md` et `contracts/CHANGELOG.md` s'ils sont absents).

Puis fusionner le `.gitignore` projet avec `TEMPLATE_claude/gitignore-for-projects` (meme bloc non destructif que la
Finalisation : seules les lignes template absentes sont ajoutees, ex. `.claude/workflow/`). Lister les lignes ajoutees
dans le rapport.

#### Etape d7 — Vérifier et créer les labels GitHub de phase

S'assurer que les labels de suivi existent sur le repo (même commande que l'init, idempotent). Le milestone initial
`v0.1.0` et ses issues de base ne sont **pas** concernés : ils ne sont créés qu'à la première initialisation d'un
projet vide (section "Premier milestone v0.1.0 et issues de base"), jamais en mise à jour.

```bash
gh label create "PLANNING"  --color "0075ca" --description "Plan à élaborer ou à valider"      --force
gh label create "PRET"      --color "bfdadc" --description "Plan validé, développement pas encore lancé" --force
gh label create "EN COURS"  --color "e4e669" --description "Issue en cours de développement" --force
gh label create "EN REVIEW" --color "d876e3" --description "Issue en cours de revue de code"  --force
gh label create "EN QA"     --color "f9d0c4" --description "Issue en cours de tests QA"       --force
gh label create "DONE"      --color "0e8a16" --description "Issue livrée et validée"           --force
```

#### Etape d7b — Audit du contexte (`/context-audit`)

Meme etape que « 6. Audit du contexte » de l'init : commiter l'etat synchronise (point de retour), puis lancer
`/context-audit` en audit complet. Les verifications d1e / d5e (graphe, doublons) restent la verification
specifique a la synchronisation ; `/context-audit` y ajoute les incoherences et references cassees transverses
(commandes, agents, contextes). Reporter le resultat dans le rapport d8.

#### Etape d8 — Rapport final

```
Synchronisation terminee.

  Commandes mises a jour            : N
  Agents mis a jour                 : N (dont N agents de stack dev-*) — registre agents.registry[] a <tag>
  Agents epingles (non mis a jour)  : N (<nom> @ <tag>, ...)
  Reliquats supprimes               : N
  Doublons compagnons retires       : N (etape d5b)
  Conflits compagnons arbitres      : N (etape d5c)
  Liens casses / orphelins corriges : N / N (etapes d1e, d5)
  Verification post-nettoyage       : N doublons residuels (dont N assumes), N liens casses, N orphelins (etape d5e)
  CLAUDE.md bloc TEAMLEADER_PROTOCOL : mis à jour
  CLAUDE.md table Agents Disponibles : mis à jour (N lignes — documentation uniquement)
  Labels GitHub                     : vérifiés (PLANNING, PRET, EN COURS, EN REVIEW, EN QA, DONE)
  Audit contexte (/context-audit)   : N corriges (Auto), N arbitres, N laisses (etape d7b)
  Schema infrastructure             : [convertit vers environments[] | deja a jour | inchange (refuse)]
  Acces aux environnements          : [<ENV>: ok | <ENV>: non valide (statut) | valides a l'instant (etape d1f)]

  Fichiers PROJET preserves (non touches) :
    ✓ CLAUDE.md (hors bloc TEAMLEADER_PROTOCOL et hors table Agents Disponibles)
    ✓ .claude/project-config.json (hors migration ponctuelle du schema `infrastructure`, voir
      section "Migration du schema `infrastructure`" — champs projet non touches sinon)
    ✓ .claude/memory/
    ✓ .claude/agents/dev-*.md   (compagnons — jamais modifiés par la sync ; les copies dev-*.template.md sont,
                                  elles, resynchronisées via le registre comme les autres agents)

  Teammates (registre des agents — sortie de registry_report) :
  | Teammate     | Fichier                  | Source                          | Version | Etat                        | Compagnon   |
  |--------------|--------------------------|---------------------------------|---------|-----------------------------|-------------|
  | planner      | implementation-planner.… | agents/implementation-planner.md| v3.20.0 | 🟢 a jour                   | —           |
  | dev-firmware | dev-firmware.template.md | templates/dev-firmware-esp32.md | v3.22.0 | 🟢 a jour                   | ✓ dev-firmware.md |
  | qa           | qa.template.md           | agents/qa.md                    | v3.18.0 | 📌 epingle (v3.22.0 dispo)  | ✓ qa.md     |
  | ...
```

Le tableau des teammates est **toujours affiche en dernier** (sortie de `registry_report`, section "Registre
des agents"), meme si aucun agent n'a change — c'est l'etat de reference de l'equipe apres la synchronisation.
