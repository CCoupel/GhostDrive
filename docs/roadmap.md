# Roadmap GhostDrive

> **Mise à jour** : 2026-05-22  
> **Version courante** : v2.1.0 (Files On-Demand)

---

## Vue d'ensemble

GhostDrive suit une roadmap ambitieuse vers une synchronisation complète type OneDrive, avec support multi-client et chiffrement côté client à long terme.

---

## Versions et Milestones

| Version | Milestone | Contenu | Statut | Date |
|---------|-----------|---------|--------|------|
| **v1.x** | — | Sync bidirectionnelle, WebDAV, MooseFS, Cache local | ✅ Livré | 2026-04 |
| **v2.0** | — | VFS Foundation, WinFsp + Cloud Filter API, ReadAt/ChunkSize | ✅ Livré | 2026-05-17 |
| **v2.1** | **v2.1 - Files On-Demand** | CF API foundation, placeholders, hydratation progressive, 8 bugs CF | ✅ Livré | 2026-05-22 |
| **v2.2** | **v2.2 - Workflow Objets** | Rename/move natif, Copier état complet, bugs data-loss (Supprimer L, Désépingler L, Épingler transfert), avertissement Conflict (#139–#144) | 🔜 En cours | Q3 2026 |
| **v2.3** | **v2.3 - Sync bidirectionnelle** | Badges ☁️ (sparse MSIX), UI conflits, cache états, retry exponentiel (#129, #135–#138) | 🔜 Planifié | Q3 2026 |
| **v2.4** | **v2.4 - Cache avancé & Offline** | (à détailler) | 🔜 Planifié | Q4 2026 |
| **V2** | — | Multi-client synchronisé | 🔜 Planifié | 2027 |
| **V3** | — | Chiffrement client + Versioning | 🔜 Planifié | 2027+ |

---

## Détail des Versions Livrées

### V1.x — Synchronisation Bidirectionnelle (✅ Livré)

**Points clés** :
- Synchronisation bidirectionnelle : local → remote et remote → local
- Deux backends inclus : **WebDAV** et **MooseFS**
- Cache local activable par point de sync
- Interface tray Windows avec menu natif

**Version courante** : v1.7.0

---

### V2.0 — VFS Foundation (✅ Livré 2026-05-17)

**Points clés** :
- **Architecture WinFsp unifiée** : un seul drive virtuel `GhD:` au lieu de E:, F:, G:...
- **Cloud Filter API** : placeholders Windows, Files On-Demand
- **ReadAt & ChunkSize** : interface StorageBackend enrichie pour lectures par plage optimisées
- **Breaking change** : tous les plugins externes doivent implémenter ReadAt() et ChunkSize()

**Issues fermées** : #120, #121, #132

**Version** : v2.0.0

---

### V2.1 — Files On-Demand (✅ Livré 2026-05-22)

**Points clés** :
- **Cloud Filter API foundation** : `CfRegisterSyncRoot`, `CfConnectSyncRoot`
- **Placeholder creation** : `CfCreatePlaceholders` avec `FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS`
- **Hydratation progressive** : chunks de 4 MiB, fichier accessible dès le premier chunk
- **Cache chunks BoltDB** : TTL configurable, invalidation ETag/mtime, éviction LRU
- **Badges shell Windows** : états CF natifs ☁️ ✓✓ ⟳ ⚡
- **8 CF API bugs fixes** : normalisation chemins, BaseDirectoryPath, ALREADY_EXISTS, CfConvertToPlaceholder

**Issues fermées** : #122, #123, #124, #133

**Issues ouvertes** : #129 (overlay badges partiels, reste v2.3)

**Version** : v2.1.0

---

## Roadmap Futures

### V2.2 — Workflow Objets (🔜 Q3 2026)

**Objectif** : gérer les opérations d'exploration Windows (rename, move, copier) de manière sûre et cohérente.

**Points clés** :
- **Rename/Move natifs** : directement sur le placeholder, sans téléchargement complet
- **Copier état complet** : preserve les états (L → nouvelle entrée P, R → hydrate + copie, etc.)
- **Bugs data-loss fixes** :
  - Supprimer L → annule upload
  - Désépingler L → upload obligatoire avant transformation en R
  - Épingler transfert → confirme après upload
- **Avertissement Conflict** : affiche popup ⚠️ avant opération en état C

**Issues liées** : #139–#144

---

### V2.3 — Sync Bidirectionnelle Complète (🔜 Q3 2026)

**Objectif** : boucle de synchronisation bidirectionnelle fiable avec gestion des conflits et badges complets.

**Points clés** :
- **Badges ☁️ complets** : sparse MSIX package pour support badges multiples
- **UI Conflits** : interface pour choisir version locale / remote / merge
- **Cache états** : session persistante des états fichier
- **Retry exponentiel** : backoff adaptatif pour connexions instables

**Issues liées** : #129 (overlay badges complets), #135–#138 (retry, cache états)

---

### V2.4 — Cache Avancé & Offline (🔜 Q4 2026)

**Objectif** : mode offline complet avec cache intelligent et synchronisation opportuniste.

**Points clés** :
- Cache granulaire par backend
- Résumé de changements lors de reconnexion
- Synchronisation opportuniste (lors de ralentissements réseau)

*Détails à venir.*

---

## Versions Majeures Futures

### V2 — Multi-client (🔜 2027)

Synchronisation de plusieurs clients Windows vers le même backend avec résolution des conflits.

### V3 — Chiffrement & Versioning (🔜 2027+)

- **Chiffrement côté client** (zero-knowledge)
- **Versioning des fichiers** (historique local)

---

## Convention de Versioning

GhostDrive suit [Semantic Versioning](https://semver.org/) :
- **MAJOR** : architecture incompatible (V1 → V2, V2 → V3)
- **MINOR** : feature compatible (v2.0 → v2.1 → v2.2)
- **PATCH** : bugfix compatible (v2.1.0 → v2.1.1)

---

## Comment Contribuer

Vous avez une idée pour une future version ? Consultez [CLAUDE.md](../CLAUDE.md) pour les conventions de développement et ouvrez une issue GitHub.
