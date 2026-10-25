# Deploy — PROD (mecanisme : binary-releases)

> Procedure dediee GhostDrive — selectionnee car `infrastructure.environments[].deploy.mechanism =
> "binary-releases"` pour PROD dans `.claude/project-config.json`. Generee par `/init-project`
> (sync v3.27.0). Chargee par `agents/deploy.md`, Tache DEPLOY PROD, etape [2. MECANISME].
> Adaptations projet : compagnon `deploy.prod.md` (jamais ce fichier).

La distribution PROD **est** la GitHub Release creee par la CI (`.github/workflows/build.yml`,
PUBLISH PROD) : les utilisateurs telechargent les binaires. DEPLOY n'installe rien ; il verifie
que la Release du tag est complete et publique. Aucun build, aucune publication ici (BORE).

## Variables attendues

Aucune — `gh` utilise l'authentification ambiante du poste.

```bash
mkdir -p _work/tmp
# 1. Verification — la Release du tag existe et n'est ni brouillon ni pre-release
gh release view "v$VERSION" --json isDraft,isPrerelease,assets,body > _work/tmp/release.json \
  || { echo "Release v$VERSION absente — PUBLISH PROD incomplet"; exit 1; }
jq -e '.isDraft == false and .isPrerelease == false' _work/tmp/release.json >/dev/null \
  || { echo "Release v$VERSION en brouillon/pre-release"; exit 1; }

# 2. Livrables attendus — exe Windows + au moins un plugin .ghdp, nommes sur la version du tag
jq -r '.assets[].name' _work/tmp/release.json
jq -e --arg v "$VERSION" '[.assets[].name] | any(. == "ghostdrive-v\($v)-windows-amd64.exe")' _work/tmp/release.json >/dev/null \
  || { echo "Binaire ghostdrive-v$VERSION-windows-amd64.exe absent de la Release"; exit 1; }
jq -e '[.assets[].name] | any(endswith(".ghdp"))' _work/tmp/release.json >/dev/null \
  || { echo "Aucun plugin .ghdp dans la Release"; exit 1; }
jq -e '.body | length > 0' _work/tmp/release.json >/dev/null || echo "⚠ notes de release vides"

# 3. Notification
echo "Deploiement PROD termine - v$VERSION : $(gh release view "v$VERSION" --json url --jq .url)"
```

## Echec

Release absente/incomplete → echec de PUBLISH PROD (CI), classer selon la table d'echec de
`publish.prod.template.md` (CODE / FLAKY / CONFIG / INFRA). Rapport au teamleader, jamais de
correction autonome.

## Rollback

Ne jamais supprimer le tag ni la Release sans ordre explicite de l'utilisateur. Retrait de la
distribution : `gh release edit "v$VERSION" --draft` (masque la Release), puis correctif via
`/hotfix`. Le revert du merge/tag releve du Rollback de `publish.prod.template.md`.
