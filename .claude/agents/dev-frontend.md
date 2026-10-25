# dev-frontend — adaptations GhostDrive

Complete .claude/agents/dev-frontend.template.md ; en cas de divergence explicite ci-dessous, ce fichier fait foi.

## Regles projet

- Commits atomiques avec messages conventionnels (`feat(tray): ...`, `fix(settings): ...`)

## Contexte Wails

GhostDrive utilise **Wails v2** : le backend Go expose des methodes (`App`) qui sont
automatiquement generees comme fonctions TypeScript dans `frontend/wailsjs/go/`.

```typescript
// Auto-genere par Wails — NE PAS MODIFIER
// frontend/wailsjs/go/main/App.js
export function GetBackends(): Promise<Array<BackendInfo>>;
export function AddSyncPoint(localPath: string, remotePath: string, backendName: string): Promise<void>;
export function GetSyncStatus(): Promise<SyncStatus>;
```

**Regle absolue** : utiliser uniquement les fonctions disponibles dans `wailsjs/go/`.
Ne jamais appeler `fetch` ou `axios` — tout passe par les bindings Wails.

<!-- Derogation projet : structure projet (frontend/src GhostDrive vs src/ typique) -->
## Structure Projet

```
frontend/
├── src/
│   ├── components/
│   │   ├── tray/             # Icone systray et menu contextuel
│   │   │   ├── TrayMenu.tsx
│   │   │   └── TrayStatus.tsx
│   │   ├── settings/         # Configuration backends et points de sync
│   │   │   ├── BackendConfig.tsx
│   │   │   ├── SyncPointForm.tsx
│   │   │   └── SettingsPage.tsx
│   │   ├── status/           # Vue d'etat de synchronisation
│   │   │   ├── SyncStatus.tsx
│   │   │   ├── FileList.tsx
│   │   │   └── ProgressBar.tsx
│   │   └── ui/               # Composants generiques
│   │       ├── Button.tsx
│   │       ├── Input.tsx
│   │       └── Modal.tsx
│   ├── hooks/
│   │   ├── useBackends.ts    # Liste et etat des backends
│   │   ├── useSyncStatus.ts  # Polling/events de sync
│   │   └── useSyncPoints.ts  # Points de synchronisation
│   ├── services/
│   │   └── wails.ts          # Wrapper typé des bindings Wails
│   ├── types/
│   │   └── ghostdrive.ts     # Types partagés (BackendInfo, SyncPoint, etc.)
│   ├── styles/
│   │   └── globals.css       # Styles globaux (Tailwind)
│   └── App.tsx               # Composant racine + routing
├── wailsjs/                  # Auto-genere par Wails — ne pas modifier
│   ├── go/
│   │   └── main/App.js
│   └── runtime/
│       └── runtime.js
├── index.html
├── package.json
└── vite.config.ts
```

<!-- Derogation projet : couche services (wrapper bindings Wails vs fetch + VITE_API_URL) -->
## Service Wails (wrapper type)

```typescript
// services/wails.ts — wrapper typé sur les bindings auto-generés
import * as App from '../wailsjs/go/main/App';
import type { BackendInfo, SyncPoint, SyncStatus } from '../types/ghostdrive';

export const ghostdriveApi = {
  getBackends: (): Promise<BackendInfo[]> => App.GetBackends(),
  addSyncPoint: (local: string, remote: string, backend: string): Promise<void> =>
    App.AddSyncPoint(local, remote, backend),
  getSyncStatus: (): Promise<SyncStatus> => App.GetSyncStatus(),
  // ...
};
```

## Conventions

### Events Wails (runtime)

```tsx
// Pour les evenements pousses depuis Go
import { EventsOn } from '../wailsjs/runtime/runtime';

useEffect(() => {
  const unsub = EventsOn('sync:progress', (data: SyncProgress) => {
    setProgress(data);
  });
  return unsub;
}, []);
```

<!-- Derogation projet : commandes dev/build (wails dev/build vs npm run dev/build) -->
## Commandes

```bash
# Demarrer en mode dev (hot reload)
wails dev

# ou depuis le dossier frontend uniquement
npm run dev

# Build production (via Wails)
wails build

# Tests
npm run test
npm run test:coverage

# Linter
npm run lint
npm run lint:fix

# Type check
npm run typecheck
```

## Design System GhostDrive

L'UI doit etre **sobre et fonctionnelle** — similaire a OneDrive :
- Palette : blanc/gris clair (#F8F9FA) + accent bleu (#0078D4 — couleur Windows)
- Fonts : System UI (Segoe UI sur Windows)
- Icones : `lucide-react`
- Composants de base : Tailwind CSS utility classes

**Tray UI** : fenetre flottante compacte (400x300px max), sombre ou claire selon theme systeme.

<!-- Derogation projet : framework de test (vitest/vi.fn vs jest.fn) -->
## Tests (vitest + testing-library)

```tsx
// BackendCard.test.tsx
import { render, screen, fireEvent } from '@testing-library/react';

describe('BackendCard', () => {
  it('affiche le statut connecte', () => {
    const backend = { name: 'WebDAV NAS', status: 'connected' };
    render(<BackendCard backend={backend} />);
    expect(screen.getByText('connecte')).toBeInTheDocument();
  });

  it('appelle onConnect au clic', () => {
    const backend = { name: 'WebDAV NAS', status: 'disconnected' };
    const onConnect = vi.fn();
    render(<BackendCard backend={backend} onConnect={onConnect} />);
    fireEvent.click(screen.getByText('Connecter'));
    expect(onConnect).toHaveBeenCalledWith(backend);
  });
});
```

<!-- Derogation projet : checklist implementation (types/services Wails, vitest vs checklist generique) -->
## Checklist Implementation

- [ ] Types TypeScript dans `types/ghostdrive.ts`
- [ ] Wrapper dans `services/wails.ts` si nouveau binding
- [ ] Composant avec props typees
- [ ] Hook custom si logique reutilisable
- [ ] Gestion loading + error states
- [ ] Styles Tailwind coherents avec le design GhostDrive
- [ ] Tests composants avec vitest
- [ ] Accessibilite (labels ARIA sur les formulaires)
