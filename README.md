# GhostDrive

> Client de synchronisation Windows vers backends de stockage prives — l'equivalent OneDrive pour votre NAS.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://golang.org)
[![Wails](https://img.shields.io/badge/Wails-v2-FF3E00)](https://wails.io)
[![GitHub release](https://img.shields.io/github/v/release/CCoupel/GhostDrive)](https://github.com/CCoupel/GhostDrive/releases)

---

## Le probleme

Aujourd'hui, un utilisateur Windows qui veut etendre son stockage est limite a Google Drive, OneDrive ou iDrive. Ces solutions :

- Sont **vendor-locked** — vos fichiers sont sur leurs serveurs
- Sont **limitees en espace** — payantes au-dela du quota gratuit
- **Ne supportent pas les backends prives** — NAS, CephFS, MooseFS, S3 auto-heberge

Pourtant, de nombreux utilisateurs disposent deja d'une infrastructure de stockage maison (NAS Synology, TrueNAS, serveur MooseFS...) qui reste inexploitee depuis Windows.

## La solution

GhostDrive est un client Windows libre qui transforme n'importe quel backend de stockage prive en drive cloud, avec la meme experience qu'OneDrive :

- **Placeholders Files On-Demand** — les fichiers apparaissent dans l'explorateur sans occuper d'espace local
- **Synchronisation bidirectionnelle** — les modifications locales se propagent vers le backend, et vice-versa
- **Cache local configurable** — gardez vos fichiers les plus utilises disponibles hors ligne
- **Architecture plugin** — connectez n'importe quel backend via une interface standardisee

---

## Fonctionnalites

### V1 — ✅ Livré (v1.7.0)
- Synchronisation bidirectionnelle de dossiers locaux vers backends distants
- Placeholders Windows (Cloud Filter API) — Files On-Demand
- Cache local activable par point de sync
- Plugins inclus : **WebDAV** et **MooseFS**
- Interface tray Windows avec menu natif — lancement au demarrage, fenetre cachee au lieu de quittee
- Configuration backends WebDAV et MooseFS avec validation et test de connexion en direct
- Points de synchronisation configurables (dossier local ↔ chemin distant) par backend
- Vue etat de synchronisation en temps reel — barres de progression par fichier, erreurs, historique

### V2.0 — ✅ Livré (2026-05-17)
- **VFS Foundation** : architecture WinFsp unifiée, un seul drive GhD: pour tous les backends
- **ReadAt() & ChunkSize()** : lectures par plage optimisées pour hydratation progressive
- Breaking change : interface StorageBackend requiert ReadAt() et ChunkSize()

### V2.1 — ✅ Livré (2026-05-22)
- **Files On-Demand** : Cloud Filter API foundation, placeholders non hydratés, hydratation progressive par chunks
- **Cache chunks BoltDB** : TTL configurable, invalidation ETag/mtime, éviction LRU
- **Badges shell Windows** : états CF natifs ☁️ ✓✓ ⟳ ⚡ via CfSetInSyncState/CfSetPinState
- **8 CF API bugs fixes** : normalisation chemins, BaseDirectoryPath, ALREADY_EXISTS, CfConvertToPlaceholder

### V2.2/V2.3/V2.4 — 🔜 Roadmap
- **V2.2** : Workflow Objets (rename/move natif, copier état complet, data-loss bugs, avertissement Conflict)
- **V2.3** : Sync bidirectionnelle complète (badges complets avec sparse MSIX, UI conflits, cache états)
- **V2.4** : Cache avancé & Offline (à détailler)

Pour la roadmap détaillée, voir [`docs/roadmap.md`](docs/roadmap.md).

### V2 (Multi-client) — 🔜 Roadmap
- Plusieurs machines synchronisées vers le même backend

### V3 — 🔜 Roadmap
- Chiffrement côté client (zero-knowledge)
- Versioning des fichiers

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  Windows Explorer                   │
│         (placeholders via Cloud Filter API)         │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│              GhostDrive (Wails App)                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ │
│  │  Sync Engine │  │   Cache Mgr  │  │  Tray UI   │ │
│  └──────┬──────┘  └─────────────┘  └─────────────┘ │
│         │                                           │
│  ┌──────▼──────────────────────────────────────┐   │
│  │         StorageBackend Interface (plugin)    │   │
│  └──────┬──────────────────┬───────────────────┘   │
│         │                  │                        │
│  ┌──────▼──────┐  ┌────────▼────────┐              │
│  │  WebDAV     │  │   MooseFS       │  [+ plugins] │
│  └─────────────┘  └─────────────────┘              │
└─────────────────────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────┐
        │              │              │
   ┌────▼────┐   ┌─────▼────┐  ┌─────▼─────┐
   │  NAS    │   │ MooseFS  │  │ S3/MinIO  │
   │ WebDAV  │   │ Cluster  │  │ (V2+)     │
   └─────────┘   └──────────┘  └───────────┘
```

### Stack Technique

| Composant | Technologie |
|-----------|-------------|
| Application | Go 1.21 + Wails v2 |
| UI | React + TypeScript |
| Placeholders Windows | Cloud Filter API + WinFsp |
| Architecture plugin | Interface Go (compile) |
| CI/CD | GitHub Actions |
| Distribution | Binaires GitHub Releases |

### États & Synchronisation

Pour comprendre les différents états des fichiers et les actions possibles selon l'état, consultez la **matrice états × actions** :

📖 **[Voir : docs/sync-states-actions.md](docs/sync-states-actions.md)** — matrice complète (9 états × 10 actions)

---

## Installation

### Depuis les releases binaires (recommande)

Telecharger le dernier binaire depuis les [Releases GitHub](https://github.com/CCoupel/GhostDrive/releases) :

| Plateforme | Fichier |
|------------|---------|
| Windows (x64) | `ghostdrive-vX.Y.Z-windows-amd64.exe` |

**Windows** : executer le binaire, GhostDrive se lance automatiquement au demarrage.

**Prerequis Windows** : [WinFsp 2.0+](https://winfsp.dev/rel/) doit etre installe pour les placeholders Files On-Demand et le drive virtuel GhD:

### Depuis les sources

**Prerequis** :
- Go 1.21+
- Node.js 18+
- [Wails v2](https://wails.io/docs/gettingstarted/installation) (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
- [WinFsp 2.0+](https://winfsp.dev/rel/) (Windows uniquement — requis pour placeholders Files On-Demand et drive GhD:)

```bash
git clone https://github.com/CCoupel/GhostDrive.git
cd GhostDrive

# Build Windows depuis WSL/Linux
wails build -platform windows/amd64
```

---

## Configuration

GhostDrive se configure via l'interface tray :

1. Cliquer sur l'icone GhostDrive dans la barre des taches
2. **Backends** → Ajouter un backend (WebDAV ou MooseFS)
3. **Sync** → Configurer les points de synchronisation (dossier local ↔ dossier distant)
4. **Cache** → Activer le cache local si souhaite

### Exemple de configuration WebDAV

```
URL      : https://mon-nas.local/webdav
Username : mon_utilisateur
Password : ****
```

---

## Developper un Plugin

GhostDrive utilise une architecture plugin basee sur une interface Go.
Pour ajouter le support d'un nouveau backend, implementez l'interface `StorageBackend` :

```go
type StorageBackend interface {
    Name() string
    Connect(config BackendConfig) error
    Disconnect() error
    Upload(ctx context.Context, local, remote string, progress ProgressCallback) error
    Download(ctx context.Context, remote, local string, progress ProgressCallback) error
    Delete(ctx context.Context, remote string) error
    List(ctx context.Context, path string) ([]FileInfo, error)
    Stat(ctx context.Context, path string) (*FileInfo, error)
    Watch(ctx context.Context, path string) (<-chan FileEvent, error)
    CreateDir(ctx context.Context, path string) error
}
```

Consultez la [documentation plugin](docs/plugin-development.md) pour le guide complet.

---

## Contribuer

Les contributions sont les bienvenues !

1. Fork le depot
2. Creer une branche : `feat/<description>` ou `bug/<description>`
3. Implementer avec des tests
4. Ouvrir une Pull Request

Les issues sont gerees sur [GitHub Issues](https://github.com/CCoupel/GhostDrive/issues).
Les features sont organisees en Epics avec des Milestones (V1, V2, V3).

---

## Comparaison

| Solution | Backends prives | Libre | Files On-Demand | Multi-client |
|----------|:--------------:|:-----:|:---------------:|:------------:|
| OneDrive | Non | Non | Oui | Oui |
| Google Drive | Non | Non | Oui | Oui |
| Rclone mount | Oui | Oui | Non | Non |
| Mountain Duck | Oui | Non | Partiel | Non |
| **GhostDrive** | **Oui** | **Oui** | **Oui (V1)** | **V2** |

---

## Licence

MIT — voir [LICENSE](LICENSE)
