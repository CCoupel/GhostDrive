# Contrat — États de Synchronisation et Icônes

> **Version** : 0.4.0
> **Créé** : 2026-04-25
> **Issues liées** : #14 (icônes explorateur Windows, v1.2.0)

---

## 1. États de synchronisation par fichier

| État | Identifiant | Description |
|------|-------------|-------------|
| Synchronisé | `synced` | Fichier identique local et distant |
| Upload en attente | `pending_upload` | Modifié localement, pas encore envoyé |
| Download en attente | `pending_download` | Modifié distantement, pas encore reçu |
| Upload en cours | `uploading` | Transfert local → backend en cours |
| Download en cours | `downloading` | Transfert backend → local en cours |
| Conflit | `conflict` | Versions locale et distante divergentes |
| Erreur | `error` | Échec de synchronisation |
| Placeholder | `placeholder` | Fichier distant uniquement, non téléchargé (Files On-Demand — v1.2.0) |
| Exclu | `excluded` | Hors des règles de synchronisation |
| Hors ligne | `offline` | Backend inaccessible |

---

## 2. Icônes — UI GhostDrive (cards et vue statut)

Affichage dans l'interface Wails (cards backend, SyncStatus.tsx) :

| État | Icône (Lucide) | Couleur | Label affiché |
|------|---------------|---------|---------------|
| `synced` | `CheckCircle2` | `#22c55e` (vert) | "Synchronisé" |
| `pending_upload` | `CloudUpload` | `#f59e0b` (ambre) | "En attente d'envoi" |
| `pending_download` | `CloudDownload` | `#f59e0b` (ambre) | "En attente de réception" |
| `uploading` | `Upload` (animé) | `#3b82f6` (bleu) | "Envoi en cours…" |
| `downloading` | `Download` (animé) | `#3b82f6` (bleu) | "Téléchargement…" |
| `conflict` | `AlertTriangle` | `#ef4444` (rouge) | "Conflit" |
| `error` | `XCircle` | `#ef4444` (rouge) | "Erreur" |
| `placeholder` | `Cloud` | `#94a3b8` (gris) | "Disponible en ligne" |
| `excluded` | `MinusCircle` | `#94a3b8` (gris) | "Exclu" |
| `offline` | `WifiOff` | `#6b7280` (gris foncé) | "Hors ligne" |

> **Animation** : les états `uploading` et `downloading` utilisent une animation CSS `spin` sur l'icône.

---

## 3. Icônes — Explorateur Windows (Cloud Filter API — v1.2.0)

Overlay icons affichés sur les fichiers dans l'explorateur Windows via Cloud Filter API.

| État | Overlay | Correspondance Cloud Filter |
|------|---------|---------------------------|
| `synced` | Coche verte ✓ | `CF_IN_SYNC_STATE_IN_SYNC` |
| `placeholder` | Nuage gris ☁ | `CF_IN_SYNC_STATE_NOT_IN_SYNC` + `CF_PIN_STATE_UNSPECIFIED` |
| `downloading` | Flèche bas bleue ↓ | Hydratation en cours |
| `uploading` | Flèche haut bleue ↑ | Déhydratation / upload en cours |
| `pending_upload` | Horloge ⏱ | En attente d'envoi |
| `conflict` | Point d'exclamation rouge ⚠ | Conflit détecté |
| `error` | Croix rouge ✗ | Erreur de sync |
| `excluded` | Barre grise — | Exclu des règles |

> **Note v1.2.0** : Les overlay icons Windows requièrent l'enregistrement d'un Shell Icon Overlay Handler. WinFsp gère le montage du volume virtuel ; Cloud Filter API gère les états de synchronisation. Ces deux mécanismes sont indépendants.

---

## 4. États du backend (carte résumée)

Distincts des états fichier — reflètent la connexion au backend :

| État | Icône (Lucide) | Couleur | Label |
|------|---------------|---------|-------|
| Connecté + synchronisé | `CheckCircle2` | `#22c55e` | "Connecté" |
| Connecté + syncing | `RefreshCw` (animé) | `#3b82f6` | "Synchronisation…" |
| Connecté + en pause | `PauseCircle` | `#f59e0b` | "En pause" |
| Connecté + erreur | `AlertCircle` | `#ef4444` | "Erreur" |
| Déconnecté | `PlugZap` | `#6b7280` | "Déconnecté" |

---

## 5. Priorité d'affichage (agrégation)

Quand plusieurs fichiers ont des états différents dans un dossier, le badge du dossier reflète l'état le plus critique :

```
error > conflict > uploading | downloading > pending_upload | pending_download > synced > placeholder > excluded
```

---

## 6. Règles d'implémentation

- Les états `uploading` / `downloading` sont émis via l'événement Wails `sync:progress` (voir `contracts/sync-state.md`)
- Les états fichier sont stockés en mémoire (pas en base) — reconstruits depuis `sync:state-changed`
- L'état `placeholder` n'existe qu'avec Cloud Filter API (v1.2.0) — en v0.x, tous les fichiers sont soit `synced` soit `pending_*`
- Les icônes Lucide sont importées depuis `lucide-react` — ne pas utiliser d'autres librairies d'icônes
