# Plan d'Implémentation : v0.5.0 — GhD: Mount (WinFsp + Files On-Demand)

> **Version** : 0.5.0  
> **Créé** : 2026-04-26  
> **Issues** : #11, #52, #54, #55, #56, #57, #58  
> **Milestone** : v0.5.0 — GhD-mount  
> **Stack** : Go 1.22 + Wails v2 + React/TypeScript — Windows uniquement

---

## Contrats API à créer/modifier

- [ ] `contracts/winfsp-bindings.md` — nouveaux bindings Wails : MountDrive, UnmountDrive, GetDriveStatus + événements drive:*
- [ ] `contracts/wails-bindings.md` — ajouter les 3 bindings WinFsp dans la section Système
- [ ] `contracts/models.md` — ajouter DriveStatus, MountPoint

---

## Résumé

v0.5.0 fait de GhostDrive un vrai drive Windows : le dossier `GhD:` est monté via WinFsp comme un volume virtuel. Les backends connectés apparaissent comme sous-dossiers (`GhD:\MonNAS\`). Les fichiers distants sont listés à la demande via `StorageBackend.List()` et téléchargés à l'ouverture via `StorageBackend.Download()`. En parallèle, deux quick-wins complètent l'expérience : création automatique du dossier `C:\GhostDrive\` au démarrage (#58) et menu systray complet (#54).

---

## Critères d'Acceptation

- [ ] `C:\GhostDrive\` est créé automatiquement au démarrage de l'application (#58)
- [ ] L'icône systray reflète les 4 états (idle/syncing/paused/error) + le menu contextuel contient les 5 entrées définies dans `tray-menu.md` (#54)
- [ ] La lettre de lecteur `GhD:` est montée via WinFsp quand au moins un backend est connecté (#11)
- [ ] L'explorateur Windows affiche les sous-dossiers de chaque backend connecté sous `GhD:\` (#55)
- [ ] Double-clic sur un fichier dans `GhD:` déclenche son téléchargement via `StorageBackend.Download()` (#56)
- [ ] L'arrêt de l'application démonte proprement `GhD:` avant de quitter (#57)
- [ ] `go test ./...` passe à ≥ 70% couverture
- [ ] `wails build -platform windows/amd64` produit un binaire fonctionnel

---

## Composants Impactés

- **Backend** : `internal/placeholder/` (nouveau package), `internal/app/app.go` (Startup, Shutdown, nouveaux bindings), `go.mod` (cgofuse)
- **Frontend** : `tray_windows.go` (menu systray), `frontend/src/pages/FileBrowserPage.tsx` (nouveau), `frontend/src/components/status/FileList.tsx` (progress download)
- **Contracts** : `contracts/winfsp-bindings.md` (nouveau), `contracts/wails-bindings.md` (+ 3 bindings), `contracts/models.md` (+ 2 types)
- **Infrastructure** : Pas de changements CI requis — `CGO_ENABLED=1` + MinGW déjà présents

---

## Dépendances entre issues

```
#58 (auto-create C:\GhostDrive\) ──── indépendant ──── Phase 1
#54 (tray menu complet)          ──── indépendant ──── Phase 1
#11 (WinFsp mount GhD:)          ──── Phase 2
  └─► #55 (List dans GhD:)       ──── dépend de #11 ── Phase 3
  └─► #56 (Download on-demand)   ──── dépend de #11 ── Phase 3
  └─► #57 (unmount propre)       ──── dépend de #11 ── Phase 3
#52 (EPIC validation)             ──── Phase 4
```

---

## Tâches

### Phase 1 : Quick Wins — #58 + #54

#### 1.1 — Contrats API (à faire EN PREMIER)

1. [ ] Créer `contracts/winfsp-bindings.md`
   - Fichier : `contracts/winfsp-bindings.md`
   - Description : Définir les 3 bindings Wails (MountDrive, UnmountDrive, GetDriveStatus), les 3 événements (drive:mounted, drive:unmounted, drive:error) et le type DriveStatus

2. [ ] Mettre à jour `contracts/wails-bindings.md`
   - Fichier : `contracts/wails-bindings.md`
   - Description : Ajouter la section "Drive Virtuel" avec MountDrive, UnmountDrive, GetDriveStatus

3. [ ] Mettre à jour `contracts/models.md`
   - Fichier : `contracts/models.md`
   - Description : Ajouter DriveStatus { Mounted bool, DriveLetter string, BackendPaths map[string]string } et MountStatus par backend

#### 1.2 — #58 : Auto-création `C:\GhostDrive\`

4. [ ] Créer le dossier racine GhostDrive au démarrage
   - Fichier : `internal/app/app.go` — méthode `Startup()`
   - Description : Après chargement de config, appeler `os.MkdirAll(a.GetGhostDriveRoot(), 0755)` avant la reconnexion des backends. Logger l'erreur mais ne pas bloquer.

5. [ ] Test unitaire auto-création
   - Fichier : `internal/app/app_test.go`
   - Description : Vérifier que Startup() crée le dossier défini dans `cfg.GhostDriveRoot` (utiliser `t.TempDir()`)

#### 1.3 — #54 : Menu systray complet + icônes

6. [ ] Enrichir le menu systray
   - Fichier : `tray_windows.go` — fonction `onSystrayReady()`
   - Description : Ajouter les 3 entrées manquantes per `tray-menu.md` : `mSync` ("Synchroniser maintenant"), `mPause` ("Pause / Reprendre"), `mSettings` ("Paramètres"). Ajouter les séparateurs. Connecter les callbacks :
     - `mSync` → appelle `ghostApp.ForceSync()` sur tous les backends actifs
     - `mPause` → toggle `PauseSync`/`StartSync` selon état courant
     - `mSettings` → émet `"tray:open-settings"` via `ghostApp.Emit()`
   - Les 4 icônes et le state-watcher 3s EXISTENT DÉJÀ — ne pas toucher

7. [ ] Stub tray pour non-Windows
   - Fichier : `tray_other.go`
   - Description : Vérifier que le fichier contient bien un `runSystray()` no-op — sinon le créer

---

### Phase 2 : WinFsp Infrastructure — #11

**Pré-requis** : Contrats Phase 1 validés.

#### 2.1 — Ajouter cgofuse au module Go

8. [ ] Ajouter la dépendance cgofuse
   - Fichier : `go.mod`, `go.sum`
   - Description : Exécuter `go get github.com/billziss-gh/cgofuse@latest`. cgofuse fournit les headers WinFsp vendorisés — pas d'installation SDK requise au build. La DLL WinFsp est nécessaire au runtime.

#### 2.2 — Interface Go VirtualDrive

9. [ ] Créer l'interface VirtualDrive et les types communs
   - Fichier : `internal/placeholder/placeholder.go` (nouveau)
   - Description : Package `placeholder`. Définir :
     ```go
     type VirtualDrive interface {
         Mount(driveLetter string, backends []MountedBackend) error
         Unmount() error
         IsMounted() bool
         Status() DriveStatus
     }
     type MountedBackend struct {
         ID     string
         Name   string
         Backend plugins.StorageBackend
         Config  plugins.BackendConfig
     }
     type DriveStatus struct {
         Mounted      bool
         DriveLetter  string
         BackendPaths map[string]string // ID → chemin sous le drive
     }
     ```

10. [ ] Créer le stub VirtualDrive pour les plateformes non-Windows
    - Fichier : `internal/placeholder/placeholder_other.go` (nouveau, `//go:build !windows`)
    - Description : `NullDrive` implémentant `VirtualDrive` — toutes les méthodes retournent `ErrNotSupported`. Permet la compilation cross-platform.

#### 2.3 — Filesystem GhostDrive (cgofuse)

11. [ ] Implémenter le GhostFileSystem (fuse.FileSystemInterface)
    - Fichier : `internal/placeholder/filesystem_windows.go` (nouveau, `//go:build windows`)
    - Description : Struct `GhostFileSystem` qui implémente `fuse.FileSystemBase`. Les méthodes clés :
      - `Getattr(path, ...)` → `backend.Stat()` — attributs fichier (size, mode, timestamps)
      - `Readdir(path, ...)` → `backend.List()` — listing répertoire
      - `Read(path, buf, off, ...)` → `backend.Download()` vers un fichier temp + lecture au offset (lazy download avec cache par fichier ouvert)
      - `Create/Write` → `backend.Upload()` (écriture)
      - `Unlink` → `backend.Delete()`
      - `Rename` → `backend.Move()`
      - `Mkdir` → `backend.CreateDir()`
    - Le chemin FUSE est de la forme `/<BackendName>/path/to/file` — router vers le bon backend par nom

12. [ ] Implémenter le router multi-backends
    - Fichier : `internal/placeholder/router_windows.go` (nouveau, `//go:build windows`)
    - Description : Le filesystem `GhD:` expose N backends comme sous-dossiers. Chaque path `/BackendName/...` est résolu vers le backend correspondant + chemin relatif. La racine `/` liste les backends disponibles.

#### 2.4 — Mount/Unmount Windows

13. [ ] Implémenter WinFspDrive (Mount + Unmount)
    - Fichier : `internal/placeholder/mount_windows.go` (nouveau, `//go:build windows`)
    - Description :
      - `WinFspDrive.Mount(driveLetter, backends)` : instancie `GhostFileSystem`, crée un `fuse.FileSystemHost`, appelle `host.Mount(driveLetter, args)` dans une goroutine dédiée (bloquant)
      - `WinFspDrive.Unmount()` : appelle `host.Unmount()` + attend la fin de la goroutine avec timeout 5s
      - `WinFspDrive.IsMounted()` et `WinFspDrive.Status()` : accès thread-safe via mutex

14. [ ] Test stub + interface
    - Fichier : `internal/placeholder/placeholder_test.go`
    - Description : Tester que `NullDrive` implémente bien `VirtualDrive`, que `Mount()` retourne `ErrNotSupported`. Mocker un backend pour vérifier le routing de chemins.

#### 2.5 — Intégration dans App

15. [ ] Ajouter le champ drive dans App + méthodes Wails
    - Fichier : `internal/app/app.go`
    - Description :
      - Ajouter `drive placeholder.VirtualDrive` dans `App`
      - Initialiser dans `NewApp()` : `a.drive = placeholder.New()` (factory qui retourne WinFspDrive sur Windows, NullDrive sinon)
      - Ajouter méthode `MountDrive() error` : collecte les backends connectés → `a.drive.Mount("G:", backends)`
      - Ajouter méthode `UnmountDrive() error` : `a.drive.Unmount()`
      - Ajouter méthode `GetDriveStatus() placeholder.DriveStatus`
      - Dans `Startup()` : après reconnexion backends, si ≥ 1 backend connecté → auto-mount → émettre `drive:mounted`
      - Dans `Shutdown()` : avant fermeture engines → `a.drive.Unmount()` (#57)

16. [ ] Émettre les événements drive
    - Fichier : `internal/app/app.go`
    - Description : Émettre `drive:mounted` avec payload `DriveStatus` après mount réussi, `drive:unmounted` à l'arrêt, `drive:error` en cas d'échec.

17. [ ] Factory `placeholder.New()`
    - Fichier : `internal/placeholder/new.go` (nouveau)
    - Description : Fichier buildtagué : `new_windows.go` retourne `&WinFspDrive{}`, `new_other.go` retourne `&NullDrive{}`

---

### Phase 3 : Files On-Demand UI — #55 + #56

**Pré-requis** : Phase 2 mergée.

#### 3.1 — #55 : Navigation contenu distant (frontend)

> Le binding `ListFiles(backendID, path)` existe déjà dans `app.go` et `wails-bindings.md`.

18. [ ] Créer la page FileBrowser
    - Fichier : `frontend/src/pages/FileBrowserPage.tsx` (nouveau)
    - Description : Page "Drive" dans la navigation 3-onglets (ou 4e onglet). Sélection d'un backend → appel `window.go.App.ListFiles(backendId, path)` → affichage liste fichiers avec icônes (dossier/fichier, taille, date). Navigation en profondeur (breadcrumb).

19. [ ] Composant RemoteFileList
    - Fichier : `frontend/src/components/status/RemoteFileList.tsx` (nouveau)
    - Description : Liste les `FileInfo[]` avec colonnes Nom / Taille / Date. Clic sur dossier → navigation. Clic sur fichier → déclenche DownloadFile. Afficher `IsPlaceholder` avec icône nuage gris.

20. [ ] Ajouter l'onglet "Drive" dans la navigation
    - Fichier : `frontend/src/App.tsx`
    - Description : Ajouter `type View = 'backends' | 'configuration' | 'about' | 'drive'`. Ajouter onglet "GhD:" dans la `<nav>`. Afficher `FileBrowserPage` si `view === 'drive'`.

21. [ ] Écouter l'événement drive:mounted dans le frontend
    - Fichier : `frontend/src/hooks/useDriveStatus.ts` (nouveau)
    - Description : Hook qui écoute `drive:mounted` / `drive:unmounted` / `drive:error`. Expose `{ mounted, driveLetter, error }`. Utilisé par le TrayStatus pour afficher "GhD: monté".

#### 3.2 — #56 : Téléchargement à la demande (progress)

> Le binding `DownloadFile(backendID, remotePath)` existe déjà.  
> Amélioration : progress reporting.

22. [ ] Améliorer DownloadFile avec progress
    - Fichier : `internal/app/app.go` — méthode `DownloadFile()`
    - Description : Passer un `ProgressCallback` à `b.Download()` qui émet `sync:progress` à chaque appel. Le localPath doit préserver la structure de répertoire distante (pas uniquement `filepath.Base`).

23. [ ] Afficher la progression dans RemoteFileList
    - Fichier : `frontend/src/components/status/RemoteFileList.tsx`
    - Description : Écouter `sync:progress` via `onEvent`. Pour le fichier en cours de téléchargement, afficher une progress bar inline ou une animation. Après completion, rafraîchir `IsPlaceholder` sur l'entrée.

---

### Phase 4 : Validation EPIC — #52

24. [ ] Tests d'intégration end-to-end
    - Fichier : `internal/placeholder/filesystem_test.go` (Windows uniquement — `//go:build windows`)
    - Description : Test avec un `StorageBackend` mock (in-memory). Vérifier Readdir, Getattr, Read sur un fichier simple.

25. [ ] Tests app.go — mount/unmount cycle
    - Fichier : `internal/app/app_test.go`
    - Description : Vérifier que `MountDrive()` et `UnmountDrive()` passent sur `NullDrive` (non-Windows). Vérifier que `Shutdown()` appelle `Unmount()`.

26. [ ] Vérification TypeScript types
    - Fichier : `contracts/typescript-types.ts`
    - Description : Ajouter `DriveStatus` et `MountStatus` dans les types TypeScript exportés.

27. [ ] Mise à jour CHANGELOG + README
    - Fichier : `CHANGELOG.md`, `README.md`
    - Description : Section v0.5.0 avec les 6 issues fermées, prérequis WinFsp runtime.

---

## Contrats à Créer (détail)

### `contracts/winfsp-bindings.md` (nouveau)

```markdown
## MountDrive
Signature : MountDrive() error
Frontend  : window.go.App.MountDrive()
Comportement : Monte GhD: avec tous les backends connectés. No-op si déjà monté.
Erreur : "winfsp: driver not found" | "winfsp: drive letter in use: G:"

## UnmountDrive
Signature : UnmountDrive() error
Frontend  : window.go.App.UnmountDrive()
Comportement : Démonte GhD: proprement. No-op si non monté.

## GetDriveStatus
Signature : GetDriveStatus() DriveStatus
Frontend  : window.go.App.GetDriveStatus()
Retour    : DriveStatus { Mounted bool, DriveLetter string, BackendPaths map[string]string }

## Événements
drive:mounted   → payload DriveStatus — émis après mount réussi
drive:unmounted → payload {} — émis après unmount
drive:error     → payload { message string } — erreur mount/unmount
```

### Additions à `contracts/models.md`

```go
type DriveStatus struct {
    Mounted      bool              `json:"mounted"`
    DriveLetter  string            `json:"driveLetter"`  // ex: "G:"
    BackendPaths map[string]string `json:"backendPaths"` // backendID → "G:\NomBackend\"
}
```

---

## Fichiers à Créer (Go)

| Fichier | Taille | Description |
|---------|--------|-------------|
| `internal/placeholder/placeholder.go` | ~50 lignes | Interface VirtualDrive + types |
| `internal/placeholder/placeholder_other.go` | ~30 lignes | NullDrive stub (!windows) |
| `internal/placeholder/filesystem_windows.go` | ~300 lignes | GhostFileSystem cgofuse |
| `internal/placeholder/router_windows.go` | ~100 lignes | Routage multi-backends |
| `internal/placeholder/mount_windows.go` | ~80 lignes | WinFspDrive Mount/Unmount |
| `internal/placeholder/new.go` + `new_other.go` | ~20 lignes | Factory |
| `internal/placeholder/placeholder_test.go` | ~100 lignes | Tests stub + routing |

## Fichiers à Modifier (Go)

| Fichier | Changement |
|---------|------------|
| `internal/app/app.go` | +Startup (MkdirAll), +Shutdown (Unmount), +MountDrive, +UnmountDrive, +GetDriveStatus, +drive field |
| `internal/app/app_test.go` | +Test auto-create root, +Test mount cycle |
| `tray_windows.go` | +3 items menu systray (Sync, Pause, Paramètres) |
| `go.mod` + `go.sum` | +cgofuse |

## Fichiers à Créer (Frontend)

| Fichier | Description |
|---------|-------------|
| `frontend/src/pages/FileBrowserPage.tsx` | Page navigation GhD: |
| `frontend/src/components/status/RemoteFileList.tsx` | Liste fichiers distants + progress |
| `frontend/src/hooks/useDriveStatus.ts` | Hook état drive monté |

## Fichiers à Modifier (Frontend)

| Fichier | Changement |
|---------|------------|
| `frontend/src/App.tsx` | +onglet "GhD:" + Vue drive |
| `contracts/typescript-types.ts` | +DriveStatus |

---

## Tests Requis

- [ ] **Tests unitaires** : `internal/placeholder/placeholder_test.go` — NullDrive, routing de chemins, parsing `/<BackendName>/path`
- [ ] **Tests unitaires** : `internal/app/app_test.go` — auto-create GhostDriveRoot, MountDrive/UnmountDrive sur NullDrive, Shutdown appelle Unmount
- [ ] **Tests intégration** : `internal/placeholder/filesystem_test.go` (Windows uniquement) — Readdir, Getattr, Read via mock StorageBackend
- [ ] **Tests frontend** : `RemoteFileList.test.tsx` — rendu avec FileInfo[], navigation dans dossiers, event progress

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| cgofuse/WinFsp : incompatibilité headers MinGW cross-compilation | Moyen | Élevé | Tester `go build -tags windows` en CI dès le début. cgofuse vendorise les headers WinFsp — normalement pas d'installation SDK requise |
| Lettre `G:` déjà utilisée sur la machine de l'utilisateur | Moyen | Moyen | Rendre la lettre configurable dans `AppConfig.DriveLetter` (défaut: "G"). Essayer la prochaine lettre libre si conflit |
| WinFsp DLL absente au runtime (utilisateur sans WinFsp installé) | Élevé | Élevé | Vérifier la présence DLL au démarrage + afficher un message d'erreur clair dans le frontend avec lien https://winfsp.dev. Dégradation gracieuse : l'app fonctionne sans GhD: |
| Download on-demand bloquant le thread FUSE | Moyen | Moyen | Utiliser un channel buffered + goroutine par Read(). Timeout 30s sur les downloads FUSE |
| Test Windows en CI (cgofuse ne peut pas s'exécuter cross-OS) | Élevé | Faible | Tests `filesystem_windows.go` taggués `//go:build windows` — exécutés uniquement si `GOOS=windows`. Tests stub compilent partout |

---

## Estimation

- **Complexité** : Élevée (CGo + WinFsp kernel interface)
- **Nombre de fichiers modifiés/créés** : ~15 (Go) + ~5 (frontend)
- **Phases critiques** : Phase 2 (WinFsp) — isoler dans une branche dédiée
- **Quick wins livrables indépendamment** : Phase 1 (#58 + #54) — peuvent merger séparément avant Phase 2

---

## Notes Techniques

### cgofuse vs Cloud Filter API
- `cgofuse` + WinFsp → virtual drive letter (`GhD:`) — **v0.5.0**
- Cloud Filter API → overlay icons dans l'explorateur + Files On-Demand avancé — **v1.2.0**
- Les deux mécanismes sont indépendants et peuvent coexister

### Architecture GhD: multi-backends
```
GhD:\
  ├── MonNAS\          ← backend "MonNAS" (local plugin)
  │   ├── Documents\
  │   └── Photos\
  └── WebDAV-Pro\      ← backend "WebDAV-Pro"
      └── backup\
```
Chaque sous-dossier est routé vers le `StorageBackend` correspondant via son `Name`.

### Téléchargement lazy (Read FUSE)
Le callback `Read()` de cgofuse est synchrone. Pour éviter de bloquer :
1. Au premier `Open()` d'un fichier → `Download()` complet vers `os.TempDir()/ghostdrive/<hash>/filename`
2. `Read()` lit le fichier temp depuis l'offset demandé
3. `Release()` — le fichier temp est conservé en cache (TTL 1h ou configurable)

### Prérequis runtime WinFsp
L'utilisateur doit installer WinFsp : https://winfsp.dev/rel/
Version minimale : WinFsp 2.0 (2023+)
Detecter via `winfsp.dll` dans `%SystemRoot%\System32\` ou via registry `HKLM\SOFTWARE\WinFsp`.
