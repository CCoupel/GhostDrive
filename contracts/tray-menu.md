# Contrat — Icône Systray et Menu Contextuel

> **Version** : 0.2.0
> **Issue** : #28 (icône systray + menu contextuel Wails)
> **Règle** : Configuration Wails dans main.go ; frontend consomme les événements de menu.

---

## Approche Wails v2 — Systray

Wails v2 expose un système de **menus natifs** et de **systray** via `options.Windows` et l'API `menu`.

### Comportement attendu

| Action | Résultat |
|--------|----------|
| Démarrage app | Fenêtre cachée si `StartMinimized: true`, icône tray visible |
| Clic icône tray | Affiche/cache la fenêtre principale |
| Clic droit icône tray | Menu contextuel natif |
| Fermeture fenêtre (×) | Cache la fenêtre (ne quitte PAS l'app) |
| "Quitter" dans le menu | Quitte l'application proprement |

---

## Configuration Wails (main.go)

```go
app := wails.Run(&options.App{
    Title:            "GhostDrive",
    Width:            480,
    Height:           640,
    MinWidth:         400,
    MinHeight:        500,
    AssetServer:      &assetserver.Options{Assets: assets},
    BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
    OnStartup:        ghostApp.startup,
    OnShutdown:       ghostApp.shutdown,
    Bind:             []interface{}{ghostApp},
    Windows: &windows.Options{
        WebviewIsTransparent: false,
        WindowIsTranslucent:  false,
        DisableWindowIcon:    false,
    },
    // Masquer dans la tray au lieu de quitter
    HideWindowOnClose: true,
    // Démarrer minimisé si configuré
    StartHidden: ghostApp.cfg.StartMinimized,
    // Icône systray (32x32 ICO)
    SystemTray: &options.SystemTray{
        Icon: icon,
        Menu: buildTrayMenu(ghostApp),
        Tooltip: "GhostDrive",
    },
})
```

---

## Menu Contextuel Tray — Structure

```go
func buildTrayMenu(app *App) *menu.Menu {
    return menu.NewMenuFromItems(
        menu.Text("Ouvrir GhostDrive", nil, openWindow),
        menu.Separator(),
        menu.Text("Synchroniser maintenant", nil, forceSyncAll),
        menu.Text("Pause / Reprendre", nil, togglePause),
        menu.Separator(),
        menu.Text("Paramètres", nil, openSettings),
        menu.Separator(),
        menu.Text("Quitter", nil, quit),
    )
}
```

### Actions Menu

| Libellé | Callback Go | Comportement |
|---------|------------|-------------|
| "Ouvrir GhostDrive" | `openWindow` | `runtime.WindowShow(ctx)` |
| "Synchroniser maintenant" | `forceSyncAll` | Appelle `ForceSync` sur tous les backends actifs |
| "Pause / Reprendre" | `togglePause` | Toggle `PauseSync`/`StartSync` selon état courant |
| "Paramètres" | `openSettings` | Affiche la fenêtre principale sur l'onglet Paramètres |
| "Quitter" | `quit` | `runtime.Quit(ctx)` |

---

## Tooltip Dynamique

Le tooltip de l'icône tray doit refléter l'état de sync :

| État | Tooltip |
|------|---------|
| `idle` | "GhostDrive — À jour" |
| `syncing` | "GhostDrive — Synchronisation en cours..." |
| `paused` | "GhostDrive — En pause" |
| `error` | "GhostDrive — Erreur de synchronisation" |

Mise à jour via `runtime.MenuSetLabel` ou re-création du menu lors de `sync:state-changed`.

---

## Icône Tray Dynamique

| État | Icône |
|------|-------|
| `idle` | `assets/tray-idle.ico` (grise/verte) |
| `syncing` | `assets/tray-syncing.ico` (animée ou bleue) |
| `paused` | `assets/tray-paused.ico` (jaune) |
| `error` | `assets/tray-error.ico` (rouge) |

Changement via `runtime.SystemTraySetIcon(ctx, icon)` à chaque `sync:state-changed`.

---

## Événements Wails émis vers Frontend (#28)

### tray:open-settings

```
Nom     : "tray:open-settings"
Payload : aucun
```

Émis quand l'utilisateur clique "Paramètres" dans le menu tray. Le frontend écoute et navigue vers l'onglet Settings.

### tray:action

```
Nom     : "tray:action"
Payload : { "action": "open" | "settings" | "pause" | "sync" | "quit" }
```

Événement générique pour toute action tray. Permet au frontend de réagir (ex: afficher une notification).

---

## Composants Frontend liés (#28)

### TrayStatus.tsx

Affiche l'état résumé dans la fenêtre principale (pas l'icône native — c'est Go qui gère ça).

```typescript
interface TrayStatusProps {
  syncState: SyncState;
}
```

Affiche : icône d'état + texte status + nombre de fichiers en attente.

### TrayMenu.tsx

Menu/barre d'actions en haut de la fenêtre principale (indépendant du menu tray natif).

```typescript
interface TrayMenuProps {
  onPause: () => void;
  onForceSync: () => void;
  onOpenSettings: () => void;
  onQuit: () => void;
}
```

---

## Fichiers à créer/modifier

| Fichier | Action |
|---------|--------|
| `cmd/ghostdrive/main.go` | Configuration Wails + menu tray |
| `assets/tray-idle.ico` | Icône tray état idle |
| `assets/tray-syncing.ico` | Icône tray état syncing |
| `assets/tray-paused.ico` | Icône tray état paused |
| `assets/tray-error.ico` | Icône tray état error |
| `frontend/src/components/tray/TrayMenu.tsx` | Câblage actions + événements Wails |
| `frontend/src/components/tray/TrayStatus.tsx` | Affichage état dans fenêtre principale |
