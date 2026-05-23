# Rapport QUALIF — GhostDrive v1.5.0 (bugfix #99 Rmdir)

**Date** : 2026-05-09 18:02
**Environnement** : QUALIF
**Branche** : bug/delete-rmdir
**SHA HEAD** : afddcc07cb9540afda02330ec62f418b55d1e6c4

## Commits déployés

| SHA | Message |
|-----|---------|
| afddcc0 | test(placeholder): add regression tests for Rmdir/Unlink delete behaviour (#99) |
| dc05b00 | fix(placeholder): return ENOTEMPTY instead of EIO for non-empty directory removal |
| cd0f8fd | fix(placeholder): implement Rmdir + add ENTER logs to Unlink/Delete for delete diagnostics (#99) |
| 7e4c064 | release: GhostDrive v1.5.0 |

## Tests

| Scope | Résultat | Couverture |
|-------|----------|------------|
| Suite complète (go test ./...) | ✅ PASS | — |
| internal/placeholder (fix #99) | ✅ PASS | 88.1% |
| internal/sync | ✅ PASS | 76.4% |
| internal/backends | ✅ PASS | 79.1% |
| plugins/webdav | ✅ PASS | 80.6% |
| plugins/moosefs | ✅ PASS | 68.3% (pré-existant) |
| internal/app | ✅ PASS | 46.7% (infra, pré-existant) |

## WinFsp Headers

Headers téléchargés depuis GitHub winfsp/v2.0 dans /tmp/winfsp-headers/ (sudo indisponible).
`CGO_CFLAGS=-I/tmp/winfsp-headers` utilisé pour le build Wails + MinGW cross-compilation.

## Résultats Build

| Artefact | Statut | Taille | SHA |
|----------|--------|--------|-----|
| ghostdrive-v1.5.0-windows-amd64.exe | ✅ REBUILT | 18 Mo (18,140,672 B) | afddcc0 |
| ghostdrive-webdav-v1.5.0-windows-amd64.ghdp | ✅ REBUILT | 14 Mo (14,069,760 B) | afddcc0 |
| ghostdrive-moosefs-v1.5.0-windows-amd64.ghdp | ✅ REBUILT | 13 Mo (13,363,200 B) | afddcc0 |

**Méthode** : `EXE_METHOD=WAILS_CROSS_CGO_MINGW`

## Artefacts QUALIF

Dossier : `build/qualif/1.5.0/`

```
ghostdrive-v1.5.0-windows-amd64.exe          18M  2026-05-09 18:02
ghostdrive-webdav-v1.5.0-windows-amd64.ghdp  14M  2026-05-09 18:01
ghostdrive-moosefs-v1.5.0-windows-amd64.ghdp 13M  2026-05-09 18:02
.exe-build-method                             (EXE_METHOD=WAILS_CROSS_CGO_MINGW)
ghostdrive-v1.5.0-windows-amd64.exe.bak      18M  2026-05-09 16:53  (pré-bugfix, GhostDrive tournait)
ghostdrive-moosefs-v1.5.0-windows-amd64.ghdp.bak 13M 2026-05-09 16:53 (pré-bugfix, GhostDrive tournait)
```

> Note : Les fichiers `.bak` sont les anciens artifacts pré-bugfix (16:53). Ils peuvent être supprimés
> manuellement quand GhostDrive est fermé.

## Note technique — Artifacts verrouillés

L'instance GhostDrive v1.5.0 en cours d'exécution (Windows) maintenait l'ancien exe et le plugin
moosefs verrouillés (open file handles). Résolution via `PowerShell Rename-Item` (Windows permet de
renommer un fichier ouvert) → nommés `.bak` → nouveaux artifacts copiés aux noms corrects.

## Statut

**QUALIF VALIDÉE ✅**

Tous les artifacts contiennent le bugfix #99 (Rmdir FUSE + ENOTEMPTY).
SHA HEAD vérifié : afddcc07cb9540afda02330ec62f418b55d1e6c4
