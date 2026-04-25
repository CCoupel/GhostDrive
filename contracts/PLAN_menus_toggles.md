# Plan d'Implémentation : Menus simplifiés + Toggles Enabled/AutoSync (BackendConfigCard)

> **Date** : 2026-04-25
> **Branche** : `feat/v0.4.0-plugin-local`
> **Type** : FEATURE — Frontend + Backend

---

## Contrats à créer / mettre à jour

- [ ] `contracts/wails-bindings.md` — Ajouter `SetBackendEnabled` + `SetAutoSync`
- [ ] `contracts/backend-config.md` — Documenter le champ `AutoSync bool`

---

## Résumé

Ce plan couvre deux changements distincts mais livrés ensemble :

1. **Simplification des menus** : La vue principale (`App.tsx`) passe de 2 tabs ("État" | "Paramètres") à 2 tabs ("Configuration" | "À propos"). Les 3 sub-tabs de `SettingsPage` (Backends / Préférences / Cache) sont supprimés — la page devient plate : liste de backends + préférences essentielles condensées.

2. **Toggles BackendConfigCard** : Deux nouveaux boutons sur chaque carte backend :
   - **Activer/Désactiver** (`Enabled`) — coupe la connexion et la sync au démarrage
   - **Sync automatique** (`AutoSync`) — active/désactive le démarrage automatique de la sync

Les deux toggles **persistent dans `config.json`** (les champs Go existent déjà pour `Enabled`; `AutoSync` est à ajouter).

---

## Critères d'Acceptation

- [ ] La vue principale a exactement 2 menus : "Configuration" et "À propos"
- [ ] Aucun sub-tab dans la page Configuration (liste plates de backends)
- [ ] Toggle Enabled sur chaque BackendConfigCard : persiste au redémarrage
- [ ] Toggle AutoSync sur chaque BackendConfigCard : persiste au redémarrage
- [ ] Un backend désactivé (`Enabled=false`) s'affiche avec badge "Désactivé" (gris), pas "Erreur"
- [ ] `AutoSync=false` → engine de sync ne démarre pas automatiquement ; "Force sync" reste fonctionnel
- [ ] `AutoSync=true` → sync démarre automatiquement à la connexion du backend
- [ ] `go build ./...` passe
- [ ] `wails build` passe

---

## Composants Impactés

- **Backend Go** : `plugins/plugin.go`, `internal/app/app.go`
- **Frontend** : `App.tsx`, `BackendConfig.tsx`, `SettingsPage.tsx`, `ghostdrive.ts`, `wails.ts`, `useBackends.ts`
- **Wails bindings** : `frontend/wailsjs/go/app/App.d.ts`, `frontend/wailsjs/go/app/App.js`
- **Contracts** : `contracts/wails-bindings.md`, `contracts/backend-config.md`

---

## Analyse de l'Existant

### Structure actuelle

```
App.tsx
├── NavTab "État"       → SyncStatusPanel (vue sync globale)
└── NavTab "Paramètres" → SettingsPage
                             ├── sub-tab "Backends"    → liste BackendConfigCard
                             ├── sub-tab "Préférences" → autoStart, startMinimized, cache
                             └── sub-tab "Cache"       → stats + vider cache
```

### Structure cible

```
App.tsx
├── NavTab "Configuration" → page plate :
│                             ├── Chaque BackendConfigCard (avec 2 nouveaux toggles)
│                             └── Bouton "Ajouter un backend"
└── NavTab "À propos"      → page informative :
                              ├── Version GhostDrive
                              ├── Préférences système (autoStart, startMinimized)
                              └── Section cache (stats + vider)
```

**Supprimé** :
- NavTab "État" (SyncStatusPanel) — les informations de sync par backend sont visibles dans chaque BackendConfigCard
- Les 3 sub-tabs de SettingsPage (Backends / Préférences / Cache)

### BackendConfig — état des champs

| Champ | Go (`plugins/plugin.go`) | TypeScript (`ghostdrive.ts`) | État |
|-------|--------------------------|------------------------------|------|
| `Enabled` | ✅ existe | ✅ existe | — |
| `AutoSync` | ❌ absent | ❌ absent | **À ajouter** |

### Startup loop — comportement Enabled (déjà implémenté)

```go
// internal/app/app.go — Startup()
for _, bc := range cfg.Backends {
    if bc.Enabled {          // ← déjà présent
        manager.Add(bc)
    }
}
```

Le comportement `Enabled=false` au démarrage est **déjà géré**. Il faut seulement ajouter la logique `AutoSync` et les bindings `SetBackendEnabled` / `SetAutoSync`.

---

## Tâches

### Phase 1 — Contrats (avant tout code)

#### Tâche 1.1 — Documenter les nouveaux bindings dans `contracts/wails-bindings.md`

Fichier modifié : `contracts/wails-bindings.md`

Ajouter dans la section "Backends" :

```
### SetBackendEnabled

Signature : SetBackendEnabled(id string, enabled bool) error
Frontend  : window.go.App.SetBackendEnabled(id, enabled)
Retour    : null en cas de succès
Erreur    : "not found: <id>"

Comportement :
- enabled=false : StopSync(id) → manager.Remove(id) → cfg.Backends[i].Enabled=false → Save
- enabled=true  : cfg.Backends[i].Enabled=true → Save → manager.Add(bc)
                  → si AutoSync=true : StartSync(id)
- Émet backend:status-changed

### SetAutoSync

Signature : SetAutoSync(id string, autoSync bool) error
Frontend  : window.go.App.SetAutoSync(id, autoSync)
Retour    : null en cas de succès
Erreur    : "not found: <id>"

Comportement :
- autoSync=false : cfg.Backends[i].AutoSync=false → Save → StopSync(id) si engine actif
- autoSync=true  : cfg.Backends[i].AutoSync=true → Save → StartSync(id) si backend connecté
- Émet sync:state-changed
```

#### Tâche 1.2 — Documenter `AutoSync` dans `contracts/backend-config.md`

Fichier modifié : `contracts/backend-config.md`

Ajouter dans le modèle `BackendConfig` :

```
| `AutoSync` | bool | — | `true` = la sync démarre automatiquement à la connexion ; `false` = mode manuel (défaut: false) |
```

---

### Phase 2 — Backend Go

#### Tâche 2.1 — Ajouter `AutoSync` dans `plugins/plugin.go`

Fichier modifié : `plugins/plugin.go`

Ajouter dans la struct `BackendConfig`, après le champ `Enabled` :

```go
// AutoSync controls whether the sync engine starts automatically when the
// backend connects. When false, the user must trigger sync manually via
// ForceSync. Default: false (opt-in).
AutoSync bool `json:"autoSync"`
```

**Note** : Pas de valeur par défaut en Go (bool zero-value = false). Les backends existants dans config.json qui n'ont pas `autoSync` liront `false` → comportement inchangé.

#### Tâche 2.2 — Ajouter `SetBackendEnabled` dans `internal/app/app.go`

Fichier modifié : `internal/app/app.go`

```go
// SetBackendEnabled enables or disables a backend by ID.
// Disabling stops the sync engine and disconnects the backend.
// Enabling reconnects and optionally starts auto-sync.
func (a *App) SetBackendEnabled(id string, enabled bool) error {
    // 1. Find backend config
    a.mu.Lock()
    idx := -1
    for i, bc := range a.cfg.Backends {
        if bc.ID == id { idx = i; break }
    }
    if idx == -1 { a.mu.Unlock(); return fmt.Errorf("not found: %s", id) }

    a.cfg.Backends[idx].Enabled = enabled
    bc := a.cfg.Backends[idx]
    cfg := a.cfg
    path := a.cfgPath
    a.mu.Unlock()

    // 2. Save config
    if err := config.Save(cfg, path); err != nil { return err }

    if !enabled {
        // 3a. Disable: stop sync + disconnect
        _ = a.StopSync(id)
        _ = a.manager.Remove(id)
    } else {
        // 3b. Enable: reconnect
        if err := a.manager.Add(bc); err != nil {
            return fmt.Errorf("reconnect: %w", err)
        }
        // Auto-start sync if configured
        if bc.AutoSync {
            _ = a.StartSync(id)
        }
    }

    a.emit("backend:status-changed", types.BackendStatus{
        BackendID: id,
        Connected: enabled && a.manager.isConnected(id),
    })
    return nil
}
```

#### Tâche 2.3 — Ajouter `SetAutoSync` dans `internal/app/app.go`

Fichier modifié : `internal/app/app.go`

```go
// SetAutoSync enables or disables automatic sync for a backend.
// When autoSync=false, any running engine is stopped (manual mode).
// When autoSync=true, the engine is started immediately if the backend is connected.
func (a *App) SetAutoSync(id string, autoSync bool) error {
    a.mu.Lock()
    idx := -1
    for i, bc := range a.cfg.Backends {
        if bc.ID == id { idx = i; break }
    }
    if idx == -1 { a.mu.Unlock(); return fmt.Errorf("not found: %s", id) }

    a.cfg.Backends[idx].AutoSync = autoSync
    cfg := a.cfg
    path := a.cfgPath
    a.mu.Unlock()

    if err := config.Save(cfg, path); err != nil { return err }

    if !autoSync {
        _ = a.StopSync(id)
    } else {
        b, ok := a.manager.Get(id)
        if ok && b.IsConnected() {
            _ = a.StartSync(id)
        }
    }

    a.emitSyncState()
    return nil
}
```

#### Tâche 2.4 — Ajouter logique AutoSync dans `Startup()`

Fichier modifié : `internal/app/app.go`

Dans la loop de reconnexion au démarrage, après `manager.Add(bc)` :

```go
for _, bc := range cfg.Backends {
    if bc.Enabled {
        if err := a.manager.Add(bc); err != nil {
            a.emitError(...)
            continue
        }
        // ← NOUVEAU : démarrer la sync si AutoSync activé
        if bc.AutoSync {
            if err := a.StartSync(bc.ID); err != nil {
                a.emitError(fmt.Sprintf("app: auto-start sync %s: %v", bc.Name, err))
            }
        }
    }
}
```

---

### Phase 3 — Frontend TypeScript

#### Tâche 3.1 — Ajouter `autoSync` dans `frontend/src/types/ghostdrive.ts`

Fichier modifié : `frontend/src/types/ghostdrive.ts`

Dans `interface BackendConfig`, après `enabled: boolean` :

```typescript
/** Si true, la sync démarre automatiquement à la connexion du backend (défaut: false) */
autoSync: boolean;
```

Mettre à jour `DEFAULT_CONFIG` dans `App.tsx` si nécessaire (backends: [], pas de changement).

#### Tâche 3.2 — Ajouter les bindings dans `frontend/src/services/wails.ts`

Fichier modifié : `frontend/src/services/wails.ts`

```typescript
setBackendEnabled: (backendId: string, enabled: boolean): Promise<void> =>
  (App as any).SetBackendEnabled(backendId, enabled) as Promise<void>,

setAutoSync: (backendId: string, autoSync: boolean): Promise<void> =>
  (App as any).SetAutoSync(backendId, autoSync) as Promise<void>,
```

#### Tâche 3.3 — Ajouter les callbacks dans `frontend/src/hooks/useBackends.ts`

Fichier modifié : `frontend/src/hooks/useBackends.ts`

```typescript
const setEnabled = useCallback(async (backendId: string, enabled: boolean) => {
  await ghostdriveApi.setBackendEnabled(backendId, enabled);
  setState(s => ({
    ...s,
    configs: s.configs.map(c => c.id === backendId ? { ...c, enabled } : c),
  }));
}, []);

const setAutoSync = useCallback(async (backendId: string, autoSync: boolean) => {
  await ghostdriveApi.setAutoSync(backendId, autoSync);
  setState(s => ({
    ...s,
    configs: s.configs.map(c => c.id === backendId ? { ...c, autoSync } : c),
  }));
}, []);
```

Exporter les deux depuis le hook.

#### Tâche 3.4 — Mettre à jour `frontend/wailsjs/go/app/App.d.ts`

Fichier modifié : `frontend/wailsjs/go/app/App.d.ts`

Ajouter les déclarations manuellement (le fichier est normalement autogénéré mais doit être mis à jour pour le développement) :

```typescript
export function SetBackendEnabled(arg1:string,arg2:boolean):Promise<void>;
export function SetAutoSync(arg1:string,arg2:boolean):Promise<void>;
```

Même ajout dans `frontend/wailsjs/go/app/App.js`.

---

### Phase 4 — Composant BackendConfigCard (2 toggles)

#### Tâche 4.1 — Ajouter les 2 toggles dans `frontend/src/components/settings/BackendConfig.tsx`

Fichier modifié : `frontend/src/components/settings/BackendConfig.tsx`

**Props à ajouter** : `onToggleEnabled` et `onToggleAutoSync` callbacks.

```typescript
interface BackendConfigCardProps {
  config: BackendConfig;
  status?: BackendStatus;
  syncState?: BackendSyncState;
  onRemove: (id: string) => void;
  onToggleEnabled: (id: string, enabled: boolean) => Promise<void>;  // nouveau
  onToggleAutoSync: (id: string, autoSync: boolean) => Promise<void>; // nouveau
}
```

**Logique** :
- `config.enabled = false` → badge "Désactivé" (gris), status-dot gris (pas rouge), boutons sync désactivés
- `config.enabled = true` + `!status?.connected` → status-dot rouge + erreur (comportement actuel)
- `config.autoSync = false` → badge "Manuel" sur la zone sync, bouton "Force sync" reste actif

**Badge "Désactivé"** : remplacer le dot `status-dot-error` par une logique à 3 états :
```typescript
const dotClass = !config.enabled
  ? 'status-dot bg-gray-300'           // désactivé
  : isConnected
  ? 'status-dot status-dot-idle'       // connecté
  : 'status-dot status-dot-error';     // erreur connexion
```

**Icônes toggle** (Lucide React) :
- Enabled : `Power` (on) / `PowerOff` (off)
- AutoSync : `RefreshCw` (auto) / `RefreshCcw` ou `Clock` (manuel)

**Rendu des boutons toggle** (en haut à droite de la carte, à côté du dot Wifi) :
```tsx
{/* Toggle Enabled */}
<button
  onClick={() => onToggleEnabled(config.id, !config.enabled)}
  className={`p-1 rounded transition-colors ${config.enabled ? 'text-status-idle' : 'text-gray-300'}`}
  title={config.enabled ? 'Désactiver ce backend' : 'Activer ce backend'}
  disabled={busy}
>
  {config.enabled ? <Power size={14} /> : <PowerOff size={14} />}
</button>

{/* Toggle AutoSync */}
<button
  onClick={() => onToggleAutoSync(config.id, !config.autoSync)}
  className={`p-1 rounded transition-colors ${config.autoSync ? 'text-brand' : 'text-gray-300'}`}
  title={config.autoSync ? 'Désactiver sync auto' : 'Activer sync auto'}
  disabled={busy || !config.enabled}
>
  <RefreshCw size={14} className={config.autoSync ? '' : 'opacity-40'} />
</button>
```

---

### Phase 5 — Simplification des menus (App.tsx + SettingsPage)

#### Tâche 5.1 — Restructurer `frontend/src/App.tsx`

Fichier modifié : `frontend/src/App.tsx`

- Remplacer `type View = 'status' | 'settings'` par `type View = 'configuration' | 'about'`
- Renommer les NavTabs : "État" → "Configuration", "Paramètres" → "À propos"
- Supprimer l'import et l'usage de `SyncStatusPanel` (ou le garder en widget compact en haut de Configuration)
- Passer `onToggleEnabled` et `onToggleAutoSync` (depuis `useBackends`) vers `SettingsPage`

#### Tâche 5.2 — Simplifier `frontend/src/components/settings/SettingsPage.tsx`

Fichier modifié : `frontend/src/components/settings/SettingsPage.tsx`

**Supprimer** :
- Le type `Tab = 'backends' | 'prefs' | 'cache'`
- Le composant de navigation par sous-onglets (`TabButton` + les 3 onglets)
- `CachePanel` → déplacé dans `AboutPage`
- `PrefsPanel` → déplacé dans `AboutPage`

**Garder** :
- La liste de `BackendConfigCard` (flat, sans tab)
- Le bouton "Ajouter un backend" + Modal `SyncPointForm`
- `onToggleEnabled` + `onToggleAutoSync` propagés aux cartes

**Résultat** : `SettingsPage` devient une page plate nommée `ConfigPage` (ou renommée `SettingsPage` simplifiée) affichant directement la liste des backends.

#### Tâche 5.3 — Créer `frontend/src/components/about/AboutPage.tsx` (ou inline dans App.tsx)

Fichier créé : `frontend/src/components/about/AboutPage.tsx`

Contenu :
```tsx
// Sections :
// 1. "GhostDrive" — version + lien GitHub
// 2. "Démarrage" — toggles autoStart + startMinimized (repris de PrefsPanel)
// 3. "Cache" — stats + bouton vider (repris de CachePanel)
```

---

## Tests Requis

| Scope | Critère |
|-------|---------|
| Go build | `go build ./...` — PASS |
| Go tests | `go test ./... -v` — VERT (pas de régression) |
| Wails build | `wails build` — PASS |
| Manuel — Enabled toggle | Toggle OFF → backend marqué "Désactivé" (gris) → redémarrer → état persisté |
| Manuel — AutoSync toggle | Toggle ON → sync démarre automatiquement au prochain redémarrage |
| Manuel — ForceSync manuel | `AutoSync=false` → ForceSync reste fonctionnel |
| Manuel — menus | 2 tabs uniquement : "Configuration" et "À propos" |

---

## Fichiers Impactés — Récapitulatif

| Fichier | Action |
|---------|--------|
| `plugins/plugin.go` | modifié — `AutoSync bool` dans BackendConfig |
| `internal/app/app.go` | modifié — `SetBackendEnabled`, `SetAutoSync`, Startup autoSync |
| `frontend/src/types/ghostdrive.ts` | modifié — `autoSync: boolean` dans BackendConfig |
| `frontend/src/services/wails.ts` | modifié — `setBackendEnabled`, `setAutoSync` |
| `frontend/src/hooks/useBackends.ts` | modifié — `setEnabled`, `setAutoSync` callbacks |
| `frontend/wailsjs/go/app/App.d.ts` | modifié — déclarations `SetBackendEnabled`, `SetAutoSync` |
| `frontend/wailsjs/go/app/App.js` | modifié — idem JS |
| `frontend/src/components/settings/BackendConfig.tsx` | modifié — 2 toggles + états disabled |
| `frontend/src/components/settings/SettingsPage.tsx` | modifié — suppression sub-tabs, aplati |
| `frontend/src/App.tsx` | modifié — 2 new NavTabs "Configuration" + "À propos" |
| `frontend/src/components/about/AboutPage.tsx` | **CRÉÉ** — prefs + cache + version |
| `contracts/wails-bindings.md` | modifié — `SetBackendEnabled`, `SetAutoSync` |
| `contracts/backend-config.md` | modifié — champ `AutoSync` |

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| `SetBackendEnabled(false)` pendant une sync active | Moyen | Moyen | Appeler `StopSync` avant `manager.Remove` — séquence dans tâche 2.2 |
| Race condition : Enable + AutoSync simultanés | Faible | Moyen | `a.mu.Lock()` dans chaque méthode — les deux sont indépendantes et atomiques |
| Backward compat `autoSync` manquant en config.json | Faible | Faible | Zero-value Go = `false` → comportement inchangé pour configs existantes |
| `SetBackendEnabled(true)` avec connect qui échoue | Moyen | Moyen | Retourner l'erreur de connect sans modifier Enabled en mémoire — annuler |
| Suppression de SyncStatusPanel → perte d'info | Moyen | Faible | Les infos sync sont visibles par BackendConfigCard (currentFile, pending, syncStatus badge) |
| App.d.ts/App.js modifié manuellement → désync avec wails generate | Faible | Faible | Note dans le plan : régénérer avec `wails generate` après build ou mettre à jour manuellement avec précision |

---

## Estimation

- **Complexité** : Moyenne
- **Nombre de fichiers** : 13 (1 créé, 12 modifiés)
- **Phases** : 5 (contrats → backend → types/services → composant → menus)
- **Dépendances** : Phase 1 → Phase 2 (backend) et Phase 3 (frontend) en parallèle → Phase 4 (utilise Phase 3) → Phase 5 (utilise Phase 4)

---

## Notes Architecturales

1. **`SetBackendEnabled` vs `SaveConfig`** : On préfère des bindings dédiés plutôt que passer par `SaveConfig` pour garantir les effets de bord (disconnect/reconnect, stop/start sync). `SaveConfig` est trop générique.

2. **`AutoSync` default = false** : Choix conservateur pour préserver le comportement actuel (sync manuelle). Les backends existants continueront à ne pas auto-syncer. Les nouveaux backends cochent AutoSync à la création si souhaité.

3. **Disabled ≠ Error** : L'état "désactivé" doit visuellement être distingué de l'état "erreur" (rouge). Utiliser gris + badge "Désactivé" au lieu du dot rouge.

4. **SyncStatusPanel supprimé** : La vue globale d'état sync disparaît du main nav. Les infos par-backend (currentFile, pending, progress) sont déjà dans BackendConfigCard. Si une vue agrégée est souhaitée dans le futur, elle peut être ajoutée comme widget dans "À propos" ou réintroduite.

5. **App.d.ts / App.js** : Ces fichiers sont normalement générés par `wails generate`. En DEV, les mettre à jour manuellement pour que le TypeScript compile. En PROD, `wails build` les régénère automatiquement.
