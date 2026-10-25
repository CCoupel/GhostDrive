# deploy — adaptations GhostDrive

Complete .claude/agents/deploy.template.md ; en cas de divergence explicite ci-dessous, ce fichier fait foi.

> **Le flux du template fait foi** : BUILD / PUBLISH <env> / DEPLOY <env>, adresse `team-lead`, format des
> rapports, merge PROD en worktree detache + barriere `ci-branch.yml` avant tag, suivi CI, rollback et
> cloture du milestone. Ce fichier ne precise que **l'etape de compilation de la Tache BUILD** (GhostDrive =
> client Windows Wails + plugins `.ghdp`, pas une archive `app-$VERSION.tar.gz`) et la maintenance des pins CI.
> Les tests ne sont pas rejoues au BUILD (deja valides par QA, `scripts/ci.sh`).
>
> **Chemin QUALIF (derogation projet)** : PUBLISH QUALIF promeut le contenu de `$BUILD_DIR` vers
> `build/qualif/<X.Y.Z>/` (tous les fichiers au meme niveau — le loader cherche `*.ghdp` a cote de l'exe),
> comme `scripts/qualif.sh`. Cible configuree : `infrastructure.environments[QUALIF].publish.target`.

## BUILD — compilation GhostDrive

> **Important** : GhostDrive utilise Wails v2 qui embed le frontend React dans le binaire.
> Un `go build` nu produit ~1.5 MB (backend seul) — NON valide pour QUALIF.
> Le binaire QUALIF doit peser ≥ 10 MB (frontend inclus).
>
> **Ne jamais marquer un exe comme KEPT sans avoir tenté les deux méthodes** (wails puis
> fallback CGO). KEPT est un dernier recours, pas un comportement par défaut.
>
> **Parallélisme obligatoire** : le build Wails (CGO, lent ~2-4 min) et les builds plugins
> (pure Go, rapides ~10-20 s) sont indépendants — les lancer en arrière-plan simultanément
> avec `&` + `PIDS` array, puis `wait` sur tous les PIDs.

> **Toujours** rebuilder l'exe ET tous les plugins à chaque QUALIF — même si seul
> un plugin a changé. Le build est reproductible et garantit la cohérence de l'ensemble.
> **Ne jamais réutiliser un artefact existant** : supprimer exe et .ghdp avant de builder.

```bash
# ── Environnement (variables $REPO_ROOT, $VERSION, $BUILD_DIR etablies par la Tache BUILD du template) ──
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin:"/mnt/c/Program Files/nodejs"
COMMIT=$(git rev-parse --short HEAD)
PROJ="$REPO_ROOT"
OUT_DIR="$BUILD_DIR"        # artefact candidat : exe + .ghdp + .exe-build-method
mkdir -p "$OUT_DIR"
cd "$PROJ"
```

```bash
# ── 2. WinFsp headers (requis pour CGO cross-compile Linux → Windows) ────────
# Télécharger dans /tmp si /usr/local/include/winfsp/ absent (pas de sudo requis)
WINFSP_DIR=/tmp/winfsp-headers
if [ ! -f "$WINFSP_DIR/fuse.h" ]; then
  mkdir -p $WINFSP_DIR
  BASE="https://raw.githubusercontent.com/winfsp/winfsp/v2.0/inc/fuse"
  for h in fuse.h fuse_common.h fuse_opt.h winfsp_fuse.h; do
    wget -q "${BASE}/${h}" -O "$WINFSP_DIR/${h}"
  done
fi
export CGO_CFLAGS="-I$WINFSP_DIR"
```

```bash
# ── 3. Build parallèle : Wails exe + tous les plugins ────────────────────────
# RÈGLE : toujours rebuilder — ne jamais réutiliser un artefact existant.
# Supprimer l'exe et les .ghdp avant de lancer les builds.
BIN_NAME="ghostdrive-v${VERSION}-windows-amd64.exe"
rm -f "$OUT_DIR/$BIN_NAME" "$PROJ/build/bin/$BIN_NAME"
rm -f "$OUT_DIR/"*.ghdp
PIDS=()

# 3a. Exe Windows/amd64 — deux méthodes, dans l'ordre :
#
#   Méthode A (wails) : binaire complet avec frontend React embed (~17 MB).
#     Requis pour QUALIF finale. Nécessite wails + mingw installés.
#   Méthode B (CGO fallback) : backend seul sans embed frontend (~7 MB).
#     Suffisant pour valider les fixes dans internal/ (ex: filesystem_windows.go).
#     NON valide pour release PROD — indique clairement dans le rapport.
#   KEPT : seulement si les deux méthodes échouent (noter la raison explicitement).
#
# Prérequis cross-build (vérifier/installer si absent) :
#   which x86_64-w64-mingw32-gcc || sudo apt-get install -y gcc-mingw-w64-x86-64
#   which $HOME/go/bin/wails      || go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
#   # Frontend (pour méthode A) :
#   [ -d frontend/dist ] || (cd frontend && npm ci && npm run build)
(
  EXE_METHOD="KEPT"
  EXE_NOTE=""

  # --- Méthode A : wails build (frontend embed) ---
  if command -v $HOME/go/bin/wails >/dev/null 2>&1 && \
     command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
    echo "[wails] tentative build Windows/amd64..."
    CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
      $HOME/go/bin/wails build -platform windows/amd64 \
        -ldflags "-X 'github.com/CCoupel/GhostDrive/internal/app.GitCommit=${COMMIT}' -X 'github.com/CCoupel/GhostDrive/internal/app.AppVersion=${VERSION}'" \
        -o "$OUT_DIR/$BIN_NAME" 2>&1 | sed 's/^/[wails] /'
    # wails peut ignorer -o avec chemin absolu → copier depuis build/bin/
    [ -f "$OUT_DIR/$BIN_NAME" ] || \
      cp "$PROJ/build/bin/$BIN_NAME" "$OUT_DIR/$BIN_NAME" 2>/dev/null || true
    if [ -f "$OUT_DIR/$BIN_NAME" ]; then
      SIZE=$(du -m "$OUT_DIR/$BIN_NAME" | cut -f1)
      if [ "$SIZE" -ge 10 ]; then
        EXE_METHOD="WAILS"
        EXE_NOTE="frontend embed, ${SIZE} MB"
        echo "[wails] ✅ $BIN_NAME (${SIZE} MB)"
      else
        echo "[wails] ⚠️ binaire trop petit (${SIZE} MB) — frontend non inclus, tentative fallback"
        rm -f "$OUT_DIR/$BIN_NAME"
      fi
    else
      echo "[wails] ❌ binaire absent après build"
    fi
  else
    echo "[wails] ⚠️ wails ou mingw absent — passage direct au fallback CGO"
  fi

  # --- Méthode B : CGO cross-compile sans frontend (fallback) ---
  if [ "$EXE_METHOD" = "KEPT" ]; then
    if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
      echo "[cgo] tentative cross-compile Windows/amd64 (backend seul)..."
      GOOS=windows GOARCH=amd64 \
        CC=x86_64-w64-mingw32-gcc \
        CGO_ENABLED=1 \
        go build -ldflags="-s -w -X 'github.com/CCoupel/GhostDrive/internal/app.GitCommit=${COMMIT}' -X 'github.com/CCoupel/GhostDrive/internal/app.AppVersion=${VERSION}'" \
        -o "$OUT_DIR/$BIN_NAME" \
        . 2>&1 | sed 's/^/[cgo] /'
      if [ -f "$OUT_DIR/$BIN_NAME" ]; then
        SIZE=$(du -m "$OUT_DIR/$BIN_NAME" | cut -f1)
        EXE_METHOD="CGO_FALLBACK"
        EXE_NOTE="backend seul sans frontend embed, ${SIZE} MB — NON valide pour release PROD"
        echo "[cgo] ✅ $BIN_NAME (${SIZE} MB) — fallback CGO (pas de frontend embed)"
      else
        echo "[cgo] ❌ cross-compile échoué"
      fi
    else
      echo "[cgo] ❌ mingw absent (x86_64-w64-mingw32-gcc) — installer : sudo apt-get install -y gcc-mingw-w64-x86-64"
    fi
  fi

  # --- Résultat final ---
  if [ "$EXE_METHOD" = "KEPT" ]; then
    echo "[exe] ❌ KEPT — wails et CGO fallback ont tous deux échoué (voir logs ci-dessus)"
    echo "[exe] ⚠️  Les fixes dans internal/ NE SONT PAS testables sur Windows avant le CI PROD"
  fi
  echo "EXE_METHOD=$EXE_METHOD" > "$OUT_DIR/.exe-build-method"
  echo "EXE_NOTE=$EXE_NOTE"    >> "$OUT_DIR/.exe-build-method"
  echo "[exe] done (method=$EXE_METHOD)"
) &
PIDS+=($!)

# 3b+3c. Plugins Linux + Windows (pure Go, CGO_ENABLED=0) — rapides (~10-20 s)
# Lancés en parallèle du build Wails (indépendants). Auto-découverte : plugins/*/cmd/
for cmd_dir in plugins/*/cmd; do
  [ -d "$cmd_dir" ] || continue
  plugin_name=$(basename "$(dirname "$cmd_dir")")

  (
    PKG_PATH="github.com/CCoupel/GhostDrive/plugins/${plugin_name}"
    PLUGIN_LDFLAGS="-s -w -X '${PKG_PATH}.Version=${VERSION}'"

    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
      go build -ldflags="$PLUGIN_LDFLAGS" \
      -o "$OUT_DIR/ghostdrive-${plugin_name}-v${VERSION}-linux-amd64.ghdp" \
      "./$cmd_dir/" && echo "✓ $plugin_name linux"

    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
      go build -ldflags="$PLUGIN_LDFLAGS" \
      -o "$OUT_DIR/ghostdrive-${plugin_name}-v${VERSION}-windows-amd64.ghdp" \
      "./$cmd_dir/" && echo "✓ $plugin_name windows"
  ) &
  PIDS+=($!)
done

# Attendre tous les builds (exe + tous les plugins)
FAILED=0
for pid in "${PIDS[@]}"; do
  wait "$pid" || FAILED=$((FAILED + 1))
done
[ "$FAILED" -eq 0 ] || { echo "ERREUR : $FAILED build(s) ont échoué"; exit 1; }
```

## Maintenance `.ci-versions.env` (apres BUILD QUALIF)


```bash
# ── 5. Maintenance .ci-versions.env ──────────────────────────────────────────
# Vérifier les versions réelles des outils et mettre à jour le fichier si écart.
echo ""
echo "=== VÉRIFICATION .ci-versions.env ==="

CI_ENV_FILE="$PROJ/.ci-versions.env"
UPDATED=0

# Versions réelles
REAL_GO=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+\.[0-9]+' | head -1)
REAL_WAILS=$( { $HOME/go/bin/wails version 2>/dev/null || echo "not installed"; } | grep -oP '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
REAL_NODE=$(node --version 2>/dev/null | tr -d 'v' || echo "not installed")
REAL_NPM=$(npm --version 2>/dev/null || echo "not installed")

echo "go      : $REAL_GO"
echo "wails   : ${REAL_WAILS:-not installed}"
echo "node    : $REAL_NODE"
echo "npm     : $REAL_NPM"

# Lire les valeurs pinnées actuelles (défaut vide si non présentes)
PIN_GO=$(grep '^GO_PIN=' "$CI_ENV_FILE" 2>/dev/null | cut -d= -f2)
PIN_WAILS=$(grep '^WAILS_PIN=' "$CI_ENV_FILE" 2>/dev/null | cut -d= -f2)
PIN_NODE=$(grep '^NODE_PIN=' "$CI_ENV_FILE" 2>/dev/null | cut -d= -f2)
PIN_NPM=$(grep '^NPM_PIN=' "$CI_ENV_FILE" 2>/dev/null | cut -d= -f2)

update_pin() {
  local KEY="$1" VAL="$2" FILE="$3"
  if grep -q "^${KEY}=" "$FILE" 2>/dev/null; then
    sed -i "s|^${KEY}=.*|${KEY}=${VAL}|" "$FILE"
  else
    echo "${KEY}=${VAL}" >> "$FILE"
  fi
}

[ "$REAL_GO" != "$PIN_GO" ] && [ -n "$REAL_GO" ] && \
  { echo "⚠️  go : $PIN_GO → $REAL_GO"; update_pin GO_PIN "$REAL_GO" "$CI_ENV_FILE"; UPDATED=$((UPDATED+1)); }
[ -n "$REAL_WAILS" ] && [ "$REAL_WAILS" != "$PIN_WAILS" ] && \
  { echo "⚠️  wails : $PIN_WAILS → $REAL_WAILS"; update_pin WAILS_PIN "$REAL_WAILS" "$CI_ENV_FILE"; UPDATED=$((UPDATED+1)); }
[ "$REAL_NODE" != "$PIN_NODE" ] && [ "$REAL_NODE" != "not installed" ] && \
  { echo "⚠️  node : $PIN_NODE → $REAL_NODE"; update_pin NODE_PIN "$REAL_NODE" "$CI_ENV_FILE"; UPDATED=$((UPDATED+1)); }
[ "$REAL_NPM" != "$PIN_NPM" ] && [ "$REAL_NPM" != "not installed" ] && \
  { echo "⚠️  npm : $PIN_NPM → $REAL_NPM"; update_pin NPM_PIN "$REAL_NPM" "$CI_ENV_FILE"; UPDATED=$((UPDATED+1)); }

if [ "$UPDATED" -gt 0 ]; then
  cd "$PROJ"
  git add .ci-versions.env
  git commit -m "chore(ci): bump PROD pins après QUALIF v${VERSION}"
  echo "✅ .ci-versions.env mis à jour et committé ($UPDATED variable(s) modifiée(s))"
else
  echo "✅ .ci-versions.env à jour — aucun écart détecté"
fi
echo "======================================"
```

### Structure de sortie (publiee par PUBLISH QUALIF dans `build/qualif/<X.Y.Z>/`)

```
build/qualif/<version>/
├── ghostdrive-v<version>-windows-amd64.exe              # ≥ 10 MB (Wails + frontend)
├── .exe-build-method                                    # WAILS | CGO_FALLBACK | KEPT
├── ghostdrive-moosefs-v<version>-linux-amd64.ghdp
├── ghostdrive-moosefs-v<version>-windows-amd64.ghdp
├── ghostdrive-webdav-v<version>-linux-amd64.ghdp
└── ghostdrive-webdav-v<version>-windows-amd64.ghdp
```

> **Test manuel Windows** : ce dossier est la livraison pour qualification manuelle.
> L'utilisateur copie son contenu sur la machine Windows pour tester.
> Chemin Windows (WSL) : `\\wsl$\Ubuntu\home\cyril\GITHUB\GhostDrive\build\qualif\<version>\`

**Chemin exact de l'exe à transmettre dans le rapport** :
- Linux/WSL : `/home/cyril/GITHUB/GhostDrive/build/qualif/<version>/ghostdrive-v<version>-windows-amd64.exe`
- Windows (via WSL) : `\\wsl$\Ubuntu\home\cyril\GITHUB\GhostDrive\build\qualif\<version>\ghostdrive-v<version>-windows-amd64.exe`
- Si KEPT (build échoué depuis Linux) : fournir la commande Windows à lancer manuellement :
  ```
  wails build -platform windows/amd64
  # Binaire produit dans : build\bin\ghostdrive.exe
  ```

### Outils requis (vérification préalable)

| Outil | Chemin | Requis pour | Commande d'installation |
|-------|--------|------------|------------------------|
| Go | `/usr/local/go/bin/go` | tout | — (présent) |
| MinGW gcc | `/usr/bin/x86_64-w64-mingw32-gcc` | exe Windows (méthode A + B) | `sudo apt-get install -y gcc-mingw-w64-x86-64` |
| Wails | `$HOME/go/bin/wails` | exe Windows méthode A (≥10 MB) | `go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0` |
| npm | `/mnt/c/Program Files/nodejs/npm` | frontend (méthode A) | — (présent, ajouté au PATH) |
| WinFsp headers | `/tmp/winfsp-headers/` (auto-téléchargé si absent) | CGO Windows | voir étape 2 |

> MinGW est le prérequis minimal pour tout build Windows. Sans MinGW, l'exe est KEPT.
> Sans wails, la méthode B (CGO fallback) produit un exe valide pour tester les fixes `internal/`
> mais sans frontend embed — suffisant pour QUALIF de bugfixes, insuffisant pour release PROD.


## Checklist Pre-Deploiement

### QUALIF

- [ ] Verdict QA VALIDATED (tests + couverture ≥ 70 % via `scripts/ci.sh`) — non rejoues au BUILD
- [ ] WinFsp headers présents (`/tmp/winfsp-headers/` ou `/usr/local/include/winfsp/`)
- [ ] MinGW présent (`which x86_64-w64-mingw32-gcc`) — sinon exe marqué KEPT
- [ ] **`wails build -platform windows/amd64`** exécuté (méthode A obligatoire pour QUALIF finale)
- [ ] Exe Windows présent dans `build/qualif/<version>/` — méthode documentée dans `.exe-build-method` :
  - `WAILS` : ≥ 10 MB, frontend embed — valide pour QUALIF et PROD
  - `CGO_FALLBACK` : backend seul — valide pour tester fixes `internal/`, NON valide release PROD
  - `KEPT` : les deux méthodes ont échoué — **fournir commande Windows manuelle** dans le rapport
- [ ] **Rapport QUALIF** généré : contenu exact de `build/qualif/<version>/` listé avec tailles (exe + plugins)
- [ ] **Chemin Windows (WSL)** du dossier fourni dans le rapport — l'utilisateur copie ce dossier sur sa machine Windows
- [ ] Plugin(s) `.ghdp` (linux + windows) présents dans `build/qualif/<version>/`
- [ ] **`.ci-versions.env` vérifié** — go/wails/node/npm comparés aux pins actuels, écarts commités
- [ ] **Binaire lancé automatiquement depuis WSL** (DEPLOY QUALIF, `cmd.exe /c start`, non-bloquant) ; si échec → lancement manuel requis avant `/deploy prod`

