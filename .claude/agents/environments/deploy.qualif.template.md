# Deploy — QUALIF (mecanisme : binary-releases)

> Procedure dediee GhostDrive (aucun template `deploy-*` ne couvre un client Windows distribue en
> binaire) — selectionnee car `infrastructure.environments[].deploy.mechanism = "binary-releases"`
> pour QUALIF dans `.claude/project-config.json`. Generee par `/init-project` (sync v3.27.0).
> Chargee par `agents/deploy.md`, Tache DEPLOY QUALIF, etape [2. MECANISME].
> Adaptations projet : compagnon `deploy.qualif.md` (jamais ce fichier).

QUALIF = livraison pour **test manuel Windows** : rien n'est installe sur un serveur. DEPLOY verifie
le contenu publie par PUBLISH QUALIF, le rapporte, et lance l'exe depuis WSL (non bloquant).
Aucun build, aucune publication ici (principe BORE, `agents/infra.md` section 3).

## Variables attendues

Aucune (poste local WSL, pas d'acces distant).

```bash
# 1. Verification — dossier publie par PUBLISH QUALIF
TARGET_DIR="$REPO_ROOT/build/qualif/$DIR_VERSION"
EXE=$(ls "$TARGET_DIR"/ghostdrive-v*-windows-amd64.exe 2>/dev/null | head -1)
[ -n "$EXE" ] || { echo "Aucune publication trouvee pour QUALIF — executer /publish qualif d'abord"; exit 1; }
# Les plugins (.ghdp) doivent etre au meme niveau que l'exe (decouverte : filepath.Dir(exe) + *.ghdp)
ls "$TARGET_DIR"/*.ghdp >/dev/null 2>&1 || echo "⚠ aucun plugin .ghdp a cote de l'exe"

# 2. Rapport de livraison (contenu exact + tailles + chemin Windows)
ls -la "$TARGET_DIR"
echo "Chemin Windows : $(wslpath -w "$TARGET_DIR" 2>/dev/null || echo "$TARGET_DIR")"

# 3. Verification post-deploy — smoke projet si defini, sinon lancement de l'exe (non bloquant)
SMOKE_CMD=$(jq -r '.commands.smoke // empty' .claude/project-config.json)
if [ -n "$SMOKE_CMD" ]; then bash -c "$SMOKE_CMD"
else cmd.exe /c start "" "$(wslpath -w "$EXE")" 2>/dev/null || echo "⚠ lancement auto impossible — test manuel"; fi

# 4. Notification
echo "Deploiement QUALIF termine - $VERSION -> $TARGET_DIR (test manuel Windows)"
```

## Echec

Exe ou plugins absents → PUBLISH QUALIF incomplet, relancer `/publish qualif` (agent responsable :
`deploy`/`dev` selon le Protocole d'echec BUILD). Echec de lancement → warning seulement, le test
manuel reste possible. Rapport au teamleader, jamais de correction autonome.

## Rollback

Aucun etat durable : la version precedente reste disponible dans son propre
`build/qualif/<X.Y.Z>/` — la redonner au testeur.
