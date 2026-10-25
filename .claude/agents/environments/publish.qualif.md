# publish.qualif — adaptations GhostDrive

Complete `publish.qualif.template.md` ; en cas de divergence explicite ci-dessous, ce fichier fait foi.

## Artefact promu

L'artefact candidat GhostDrive n'est pas `app-$VERSION.tar.gz` : c'est le contenu complet de `$BUILD_DIR`
(exe `ghostdrive-v<ver>-windows-amd64.exe`, `.exe-build-method`, plugins `*.ghdp`). La promotion copie
**tous ces fichiers au meme niveau** (le loader cherche `*.ghdp` a cote de l'exe) :

```bash
TARGET_DIR="$REPO_ROOT/build/qualif/$DIR_VERSION"
mkdir -p "$TARGET_DIR"
cp -f "$BUILD_DIR"/* "$TARGET_DIR"/
ls -la "$TARGET_DIR"
```
