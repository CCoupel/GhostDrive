# Rapport QUALIF — GhostDrive v1.5.0 (bugfix #96/#97)

**Date** : 2026-05-09
**Environnement** : QUALIF
**Branche** : feature/v1.5.x
**SHA HEAD** : a6cccc3

## Commits déployés

| SHA | Message |
|-----|---------|
| a6cccc3 | docs(changelog): document fixes #96/#97 |
| 4822369 | test(bugfix): add manual QA procedure #96/#97 |
| 693cacd | test(placeholder): add Getattr non-regression #97 |
| cb21f8b | fix(logging): upgrade plugin proxy logs INFO→ERROR/WARN (#96) |
| 720b6d3 | docs(changelog): rename WinFsp fix (#97) |
| 1025ea7 | fix(placeholder): errors.Is for ENOENT in Getattr (#97) |
| 3e91a9d | fix(placeholder): implement Rename3 FUSE3/WinFsp (#97) |

## Résultats Build

| Artefact | Statut | Taille | Notes |
|----------|--------|--------|-------|
| ghostdrive-v1.5.0-linux-amd64 | ✅ REBUILT | 14 Mo | CGO_ENABLED=0 |
| ghostdrive-v1.5.0-windows-amd64.exe | ⚠️ KEPT | 18 Mo | Wails non dispo localement |
| ghostdrive-webdav-v1.5.0-linux-amd64.ghdp | ✅ REBUILT | 13 Mo | CGO_ENABLED=0 |
| ghostdrive-webdav-v1.5.0-windows-amd64.ghdp | ✅ REBUILT | 14 Mo | CGO_ENABLED=0 |
| ghostdrive-moosefs-v1.5.0-linux-amd64.ghdp | ✅ REBUILT | 13 Mo | CGO_ENABLED=0 |
| ghostdrive-moosefs-v1.5.0-windows-amd64.ghdp | ✅ REBUILT | 13 Mo | CGO_ENABLED=0 |

## Tests

| Scope | Résultat |
|-------|----------|
| Suite complète (go test ./...) | ✅ 22/22 packages OK |
| internal/placeholder (fixes #97) | ✅ 15/15 PASS |
| internal/logging (fixes #96) | ✅ PASS |
| plugins/loader | ✅ PASS |

## Smoke tests

| Test | Résultat |
|------|----------|
| Linux binary self-check | ✅ (Wails runtime msg attendu) |
| webdav plugin self-check | ✅ ("not meant to be executed directly") |
| moosefs plugin self-check | ✅ ("not meant to be executed directly") |

## Artefacts QUALIF

Dossier : `build/qualif/1.5.0/`

```
ghostdrive-v1.5.0-linux-amd64         14M  (rebuilt 2026-05-09)
ghostdrive-v1.5.0-windows-amd64.exe   18M  (kept, Wails build)
ghostdrive-webdav-v1.5.0-linux-amd64.ghdp    13M  (rebuilt)
ghostdrive-webdav-v1.5.0-windows-amd64.ghdp  14M  (rebuilt)
ghostdrive-moosefs-v1.5.0-linux-amd64.ghdp   13M  (rebuilt)
ghostdrive-moosefs-v1.5.0-windows-amd64.ghdp 13M  (rebuilt)
```

## Statut

**QUALIF VALIDÉE** ✅

Note : Le binaire Windows .exe (wails) n'a pas pu être recompilé localement (wails + mingw requis).
Les corrections #96/#97 sont dans `internal/placeholder` et `internal/logging` — validées via tests et build Linux.
Pour la PROD, la CI/CD GitHub Actions recompilera l'exe complet.
