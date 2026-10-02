# Plan d'Implémentation : EC4+1 MooseFS Go Plugin — issue #114

**Version** : v1.8  
**Date** : 2026-05-16  
**Auteur** : Planner (basé sur POC Python issue #113, SHA 878bcd9)

---

## Résumé

Implémenter la lecture des chunks erasure-coded EC4+1 dans le plugin MooseFS Go.  
Le protocole a été validé par le POC Python (issue #113) : chaque shard est lu via
`CLTOCS_READ (opcode 200)` avec un **physical chunk ID** dérivé du logical chunk ID
retourné par le master. La reconstruction consiste à concaténer DF0‖DF1‖DF2‖DF3.

Aucun changement d'interface publique (`StorageBackend`). Aucune régression sur les
chunks normaux (proto=0/1/2).

---

## Protocole EC découvert (POC #113)

```
physical_chunk_id[part] = logical_chunk_id + 0x1000000000000000 + part × 0x0100000000000000

DF0 : part=0, physical = logical + 0x1000000000000000
DF1 : part=1, physical = logical + 0x1100000000000000
DF2 : part=2, physical = logical + 0x1200000000000000
DF3 : part=3, physical = logical + 0x1300000000000000
CF0 : part=4, physical = logical + 0x1400000000000000  (parity, hors scope lecture)
```

Opcode : `CLTOCS_READ (200)` — identique chunks normaux, seul le chunk_id change.  
Master proto=3 → retourne 4 servers (DF0-DF3) en lecture normale.  
Shard `i` contient les octets logiques `[i×shardSize, (i+1)×shardSize)`.  
`shardSize = ceil(chunkDataSize / 4)` arrondi au bloc 65536.

---

## Critères d'Acceptation

- [ ] Téléchargement d'un fichier EC4+1 depuis MooseFS Pro 4.x sans erreur
- [ ] Pas de régression : fichiers sur chunks normaux (proto=0/1/2) inchangés
- [ ] `ECPhysicalChunkID(logicalID, partIdx)` — formule conforme au POC
- [ ] `parseChunkInfo` proto=3 : plus d'erreur, `ChunkInfo.ECParts = 4` positionné
- [ ] Tests unitaires : 100% des cas nominaux + 3 cas d'erreur, mock CS uniquement
- [ ] Tests `TestParseChunkInfo_Proto3` existants mis à jour (comportement change)
- [ ] Coverage MooseFS plugin ≥ 70% conservée

---

## Composants Impactés

- **Backend Go** : `mfsclient/protocol.go`, `mfsclient/client.go`, nouveau `mfsclient/ecclient.go`
- **Frontend** : aucun
- **Database** : aucune
- **Contrats Wails** : aucun changement (EC transparent pour l'appelant)

---

## Contrats internes (fonctions Go)

### `ECPhysicalChunkID` (nouveau, `protocol.go`)

```go
// ECPhysicalChunkID retourne le physical chunk ID pour le shard EC donné.
// partIdx : 0=DF0, 1=DF1, 2=DF2, 3=DF3, 4=CF0 (parité).
// Source : MooseFS CE hddspacemgr.c::hdd_int_split() — identique en Pro.
func ECPhysicalChunkID(logicalID uint64, partIdx int) uint64 {
    return logicalID + 0x1000000000000000 + uint64(partIdx)*0x0100000000000000
}
```

### Constantes EC (nouveau, `protocol.go`)

```go
const (
    EC4ECIDStart uint64 = 0x1000000000000000 // base DF0
    EC4ECIDStep  uint64 = 0x0100000000000000 // pas par part
    EC8ECIDStart uint64 = 0x2000000000000000 // réservé issue #115
)
```

### Champ `ECParts` dans `ChunkInfo` (modifié, `protocol.go`)

```go
type ChunkInfo struct {
    ChunkID uint64
    Version uint32
    Length  uint64       // longueur totale du fichier
    Servers []ChunkServer
    LockID  uint32
    // ECParts > 0 indique un chunk erasure-coded (proto=3).
    // 4 = EC4+1, 8 = EC8+2 (futur #115).
    // Servers contient uniquement les data-shards (DF0..DFn-1).
    ECParts int
}
```

### `readEC4At` (nouveau, `ecclient.go`)

```go
// readEC4At lit un chunk EC4+1 en mode shard-granulaire.
// Pour chaque appel de 64 KiB, seul 1 CS sur 4 est contacté.
//
// Paramètres :
//   info       — ChunkInfo avec ECParts==4 et Servers[0..3]
//   chunkIndex — index du chunk dans le fichier (offset / ChunkSize)
//   chunkOffset — offset DANS le chunk (0..ChunkSize-1)
//   size        — taille demandée en octets (typiquement 64 KiB)
//
// Retourne les octets lus, ou une erreur.
func (c *Client) readEC4At(
    info *ChunkInfo,
    chunkIndex uint32,
    chunkOffset uint32,
    size uint32,
) ([]byte, error)
```

Algorithme interne de `readEC4At` :

```
1. Calculer chunkDataSize = min(info.Length - uint64(chunkIndex)*ChunkSize, ChunkSize)
2. shardSize = alignToBlock(divCeil(chunkDataSize, 4), 65536)
3. shardIdx = chunkOffset / shardSize   → quel shard (0..3)
4. offsetInShard = chunkOffset % shardSize
5. Vérifier shardIdx < len(info.Servers)  → erreur si insuffisant
6. srv = info.Servers[shardIdx]
7. physicalID = ECPhysicalChunkID(info.ChunkID, shardIdx)
8. Pool.Get(srv.IP, srv.Port)  → cs net.Conn
9. ReadChunk(cs, physicalID, info.Version, offsetInShard, size)
10. Retry-once sur stale connection (même logique que Client.Read normal)
11. Pool.Put(cs, ...) si succès
```

---

## Tâches

### Phase 1 — Constantes EC et struct ChunkInfo (protocol.go)

1. [ ] Ajouter constantes `EC4ECIDStart`, `EC4ECIDStep`, `EC8ECIDStart`
   - Fichier : `plugins/moosefs/internal/mfsclient/protocol.go`
   - Après la section `// ─── Chunk-server status codes ───`

2. [ ] Ajouter fonction `ECPhysicalChunkID(logicalID uint64, partIdx int) uint64`
   - Fichier : `plugins/moosefs/internal/mfsclient/protocol.go`
   - Juste après les constantes EC

3. [ ] Ajouter champ `ECParts int` à `ChunkInfo`
   - Fichier : `plugins/moosefs/internal/mfsclient/protocol.go`
   - Struct `ChunkInfo` — après champ `LockID`
   - Commentaire GoDoc complet

### Phase 2 — parseChunkInfo : proto=3 sans erreur (client.go)

4. [ ] Modifier `parseChunkInfo` — ligne 1172 environ
   - Fichier : `plugins/moosefs/internal/mfsclient/client.go`
   - **Supprimer** : `return nil, fmt.Errorf("parseChunkInfo: proto=3 erasure-coded chunk...")` 
   - **Remplacer** par : `info.ECParts = len(info.Servers)` (setté APRÈS la boucle de parsing CS)
   - NB : le log debug existant peut être conservé (utile pour diagnostic)
   - Garder la protection nCS=0 (inchangée — déjà dans `Client.Read()`)

5. [ ] Mettre à jour les tests `TestParseChunkInfo_Proto3` existants
   - Fichier : `plugins/moosefs/internal/mfsclient/client_test.go`
   - Subtest `four_part_ec_chunk_returns_error` → renommer `four_part_ec_chunk_sets_ecparts`
     - Retirer `require.Error`
     - Ajouter `require.NoError` + `assert.Equal(t, 4, info.ECParts)`
   - Subtest `eight_part_ec_chunk_returns_error` → renommer `eight_part_ec_chunk_sets_ecparts`
     - Même logique, vérifier `ECParts == 8`
   - Subtest `raw_bytes_from_issue_114` : vérifier `ECParts == 0` (payload tronqué → 0 servers)
   - Subtest `proto3_zero_servers_no_error` : vérifier `ECParts == 0`

### Phase 3 — Client.Read() : routage EC4 (client.go)

6. [ ] Ajouter branchement EC4 dans `Client.Read()`
   - Fichier : `plugins/moosefs/internal/mfsclient/client.go`
   - Après le bloc `if info == nil { return nil, nil }` (ligne ~984)
   - AVANT la section `// Phase 2: I/O chunk server`
   - Code :
     ```go
     if info.ECParts == 4 {
         return c.readEC4At(info, index, chunkOffset, size)
     }
     if info.ECParts != 0 {
         return nil, fmt.Errorf("mfsclient: Read(%d): EC%d+%d not supported (only EC4+1 implemented)",
             nodeID, info.ECParts, info.ECParts/4)
     }
     ```
   - Note : `index` est déjà calculé en ligne 934

### Phase 4 — ecclient.go : logique lecture EC4 (nouveau fichier)

7. [ ] Créer `plugins/moosefs/internal/mfsclient/ecclient.go`
   - En-tête de package + commentaire décrivant EC4+1
   - Fonction `divCeil(a, b uint32) uint32` — division plafond
   - Fonction `alignToBlock(n, blockSize uint32) uint32` — alignement supérieur
   - Méthode `(c *Client) readEC4At(info *ChunkInfo, chunkIndex, chunkOffset, size uint32) ([]byte, error)`
   - Imports : `fmt`, `github.com/CCoupel/GhostDrive/internal/logger`

### Phase 5 — Tests unitaires EC4 (nouveau fichier)

8. [ ] Créer `plugins/moosefs/internal/mfsclient/ecclient_test.go`

   **Tests à implémenter** (dans le même package `mfsclient`) :

   | Test | Description |
   |------|-------------|
   | `TestECPhysicalChunkID` | Vérifie la formule pour parts 0..4 avec logical=0 et logical arbitraire |
   | `TestReadEC4Basic` | 4 fakeCSServers, shards de 8 KiB (chunk de 32 KiB), reconstruction correcte |
   | `TestReadEC4FullChunk` | 4 fakeCSServers, shards de 4 MiB (chunk de 16 MiB), 64 reads séquentiels de 64 KiB |
   | `TestReadEC4PartialLastShard` | chunk dont la taille n'est pas un multiple de 4 (dernier shard tronqué) |
   | `TestReadEC4InsufficientServers` | `info.Servers` = 3 entrées → erreur "shardIdx out of range" |
   | `TestReadEC4ShardReadError` | CS du shard 1 ferme la connexion → erreur propagée |
   | `TestReadEC4StaleConnection` | Pool retourne connexion stale → retry automatique → succès |
   | `TestReadEC4Via_ClientRead` | `Client.Read()` + fake master retournant proto=3 → données correctes |

   **Structure de test `TestReadEC4Basic`** :
   ```
   - 4 fakeCSServer instanciés, chacun contient 1 shard (8 KiB)
   - ChunkInfo{ECParts:4, ChunkID: logicalID, Length: 32KiB, Servers: [cs0..cs3]}
   - Appels : readEC4At(info, 0, 0, 8192) → 8 KiB depuis CS0
   -           readEC4At(info, 0, 8192, 8192) → 8 KiB depuis CS1
   -           readEC4At(info, 0, 16384, 8192) → 8 KiB depuis CS2
   -           readEC4At(info, 0, 24576, 8192) → 8 KiB depuis CS3
   - Vérification : octets reçus == shard respectif
   ```

   **Réutilisation de `fakeCSServer`** :
   - `fakeCSServer` est défini dans `csclient_test.go` (même package)
   - Utiliser `s.SetChunkData(physicalID, shardBytes)` pour pré-semer chaque shard
   - `physicalID = ECPhysicalChunkID(logicalID, partIdx)`

### Phase 6 — Vérification no-regression

9. [ ] Exécuter la suite de tests complète MooseFS
   - Commande : `go test ./plugins/moosefs/... -v -race -count=1`
   - Vérifier : 0 erreurs de race, tous les tests existants passent
   - Vérifier coverage : `go test ./plugins/moosefs/... -cover`

---

## Tests Requis

- [ ] **Tests unitaires EC** : `ecclient_test.go` — 8 tests (mock CS, aucun vrai serveur)
- [ ] **Tests parseChunkInfo** : 4 subtests mis à jour (client_test.go)
- [ ] **Tests intégration** : test smoke manuel sur cluster MooseFS Pro 4.x (hors CI)
- [ ] **No-regression** : `go test ./plugins/moosefs/... -race` — 0 failures

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| shardSize mal calculé pour chunks non-multiples de 4 | Moyen | Élevé | Test `TestReadEC4PartialLastShard` dédié ; clamp sur `info.Length` |
| Lecture sur mauvais CS (shardIdx off-by-one) | Faible | Élevé | Test `TestReadEC4Basic` vérifie quel CS est contacté via `connCount` |
| Régression proto=0/1/2 via modification parseChunkInfo | Faible | Élevé | Tests existants (≥340) couvrent ces paths ; `go test -race` en phase 6 |
| Pools de connexions EC partagés avec chunks normaux | Faible | Faible | `csPool` est agnostique au chunk type — comportement identique |
| EC8+2 (#115) accidentellement activé | Faible | Moyen | Guard `info.ECParts != 0 && info.ECParts != 4` → erreur explicite |

---

## Estimation

- Complexité : **Moyenne**
- Nombre de fichiers : 4 modifiés + 1 créé = **5 fichiers**
  - `protocol.go` : +25 lignes
  - `client.go` : +10 lignes (routage) — 8 lignes supprimées (erreur proto=3)
  - `client_test.go` : ~20 lignes modifiées (4 subtests)
  - `ecclient.go` : ~120 lignes (nouveau)
  - `ecclient_test.go` : ~250 lignes (nouveau)

---

## Notes

- **XOR / CF0** : hors scope issue #114. Le master ne retourne CF0 qu'en mode recovery.
  À planifier séparément (issue #116 ou dans #115).
- **EC8+2** : hors scope (issue #115). `EC8ECIDStart = 0x2000000000000000` documenté
  dans les constantes pour préparer le terrain.
- **Concurrence** : `readEC4At` utilise `c.pool` (thread-safe). Plusieurs goroutines
  peuvent lire des shards différents du même fichier EC sans conflit.
- **Fixtures Python** : les fixtures JSON dans `/tmp/ec_fixtures/` (POC #113) peuvent
  servir d'oracle pour valider les physicalIDs en test.
- **Version CS** : `info.Version` passé tel quel à `ReadChunk` — les CS acceptent 0 et 1
  pour les shards EC (confirmé POC).
