# Plan d'Implémentation : POC Python EC4+1 MooseFS (issue #113)

> **Version** : v1.8  
> **Date** : 2026-05-16  
> **Issue** : #113  
> **Fichier** : `poc/ec4_reader.py`

---

## Résumé

Script Python standalone (~150 lignes) qui lit des fichiers EC 4+1 sur un vrai serveur
MooseFS via le protocole TCP, reconstruit les données par XOR, valide le checksum, et
génère des fixtures JSON pour les tests unitaires Go.

Le POC implémente strictement le même protocole que le code Go existant
(`plugins/moosefs/internal/mfsclient/`) — frame format identique, opcodes identiques,
pas de bibliothèque MooseFS externe.

---

## Critères d'Acceptation

- [ ] Lit `small_ec.bin` (32 MB, 1 chunk) via DF0–DF3, reconstruit le fichier complet, vérifie XOR contre CF0
- [ ] Lit `large_ec.bin` (128 MB, 2 chunks), reconstruit les 2 chunks, concatène, vérifie XOR par chunk
- [ ] Checksum MD5 du fichier reconstruit comparé à une valeur de référence
- [ ] Fixtures JSON générées dans `tests/fixtures/ec/ec4/` (format utilisable par les tests Go)
- [ ] Accès MooseFS en lecture seule uniquement — aucune écriture ni modification de config
- [ ] Script autonome (`python3 poc/ec4_reader.py`) sans dépendances externes (stdlib seulement)

---

## Protocole MooseFS — Référence Technique

### Trame générique (master ET chunkserver)

```
[cmd:32 BE][payloadLen:32 BE][payload:payloadLen bytes]
```

Toutes les valeurs numériques en Big Endian. Référence Go : `WriteFrame` / `ReadFrame`
dans `plugins/moosefs/internal/mfsclient/protocol.go`.

---

## Composants Impactés

- **POC Python** : `poc/ec4_reader.py` — nouveau fichier (script standalone)
- **Fixtures JSON** : `tests/fixtures/ec/ec4/` — nouveau répertoire + fichiers
- **Backend** : aucune modification (lecture seule, POC hors du moteur principal)
- **Frontend** : aucun
- **Database** : aucune

---

## Tâches

### Phase 1 : Protocole Master TCP

#### 1.1 — Primitives réseau Python

- Fichier : `poc/ec4_reader.py`
- Implémenter `write_frame(sock, cmd, payload)` : encode et envoie la trame
- Implémenter `read_frame(sock)` : lit les 8 octets de header, puis le payload

```python
import struct, socket

def write_frame(sock, cmd: int, payload: bytes):
    hdr = struct.pack(">II", cmd, len(payload))
    sock.sendall(hdr + payload)

def read_frame(sock) -> tuple[int, bytes]:
    hdr = _recv_exact(sock, 8)
    cmd, length = struct.unpack(">II", hdr)
    payload = _recv_exact(sock, length) if length > 0 else b""
    return cmd, payload

def _recv_exact(sock, n: int) -> bytes:
    buf = bytearray()
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise EOFError(f"Connection closed (expected {n} bytes, got {len(buf)})")
        buf.extend(chunk)
    return bytes(buf)
```

#### 1.2 — Register (opcode 400 → 401)

- Opcode request : `CLTOM_FUSE_REGISTER = 400`
- Opcode réponse : `MATOCL_FUSE_REGISTER = 401`
- Payload envoyé (RegisterNewSession = rcode 2) :

```
[blob:64B]["DjI1GAQDULI5d2YjA26ypc3ovkhjvhciTQVx3CS4nYgtBoUcsljiVpsErJENHaw0"]
[rcode:8 = 2]
[version:32 = 0x04_3A_08]   ← VERSION2INT(4, 58, 4) = (4<<16)|(58<<8)|(4*2) = 277000 = 0x043A08
[ileng:32 = 0]              ← instance name vide (accepté par tous les masters 4.x)
[pleng:32 = 2]              ← longueur du mount path incluant le null byte
[path: "/" + "\x00"]        ← mount path null-terminé
```

- Réponse succès (len ≥ 8) : `[version:32][sessionId:32][...]`
- Réponse erreur (len == 1) : `[status:8]` → lever exception

```python
FUSE_REGISTER_BLOB = b"DjI1GAQDULI5d2YjA26ypc3ovkhjvhciTQVx3CS4nYgtBoUcsljiVpsErJENHaw0"
MFS_CLIENT_VERSION = (4 << 16) | (58 << 8) | (4 * 2)   # VERSION2INT(4,58,4) = 277000

CLTOM_FUSE_REGISTER = 400
MATOCL_FUSE_REGISTER = 401
REGISTER_NEWSESSION  = 2

def register(sock) -> int:
    payload = (
        FUSE_REGISTER_BLOB
        + struct.pack(">B", REGISTER_NEWSESSION)
        + struct.pack(">I", MFS_CLIENT_VERSION)
        + struct.pack(">I", 0)          # ileng = 0
        + struct.pack(">I", 2)          # pleng = 2
        + b"/\x00"                      # path null-terminé
    )
    write_frame(sock, CLTOM_FUSE_REGISTER, payload)
    cmd, ans = read_frame(sock)
    # discards NOP (cmd=0) keepalive
    while cmd == 0:
        cmd, ans = read_frame(sock)
    assert cmd == MATOCL_FUSE_REGISTER
    if len(ans) == 1:
        raise RuntimeError(f"Register failed: status=0x{ans[0]:02x}")
    session_id = struct.unpack(">I", ans[4:8])[0]
    return session_id
```

#### 1.3 — Lookup (opcode 406 → 407)

- Objectif : trouver l'inode d'un fichier depuis son nom dans le répertoire parent
- Opcode request : `CLTOM_FUSE_LOOKUP = 406`
- Opcode réponse : `MATOCL_FUSE_LOOKUP = 407`
- Payload : `[msgid:32=0][parent:32][namelen:8][name][uid:32=0][gcnt:32=1][gid:32=0]`
- Réponse succès (len ≥ 39) : `[msgid:32][inode:32][attrs:35]`
- Réponse erreur (len == 5) : `[msgid:32][status:8]`

```python
CLTOM_FUSE_LOOKUP  = 406
MATOCL_FUSE_LOOKUP = 407
ROOT_NODE_ID       = 1

def lookup(sock, parent_id: int, name: str) -> int:
    name_b = name.encode()
    payload = (
        struct.pack(">I", 0)            # msgid
        + struct.pack(">I", parent_id)
        + struct.pack(">B", len(name_b))
        + name_b
        + struct.pack(">I", 0)          # uid
        + struct.pack(">I", 1)          # gcnt
        + struct.pack(">I", 0)          # gid
    )
    write_frame(sock, CLTOM_FUSE_LOOKUP, payload)
    cmd, ans = read_frame(sock)
    assert cmd == MATOCL_FUSE_LOOKUP
    if len(ans) == 5:
        raise FileNotFoundError(f"Lookup({name!r}): status=0x{ans[4]:02x}")
    inode = struct.unpack(">I", ans[4:8])[0]
    return inode
```

#### 1.4 — ReadChunk EC (opcode 432 → 433, proto=3)

- Opcode request : `CLTOM_FUSE_READ_CHUNK = 432`
- Opcode réponse : `MATOCL_FUSE_READ_CHUNK = 433`
- Payload : `[msgid:32=0][inode:32][chunkindx:32]`
- Réponse proto=3 (EC) :

```
[msgid:32]
[protocolid:8 = 3]          ← discriminant EC (vs 2 = réplication standard)
[file_length:64]            ← taille totale du fichier (pour calcul chunk_data_size)
[chunkid:64]                ← chunkID du chunk EC (même ID pour tous les shards)
[version:32]
5 × [ip:32][port:16][cs_ver:32][labelmask:32]   ← 14 bytes/entry
    index 0 = DF0, index 1 = DF1, index 2 = DF2, index 3 = DF3, index 4 = CF0
```

- Taille totale de la réponse : 4+1+8+8+4 + 5×14 = 95 bytes

```python
CLTOM_FUSE_READ_CHUNK  = 432
MATOCL_FUSE_READ_CHUNK = 433
CHUNK_SIZE             = 64 * 1024 * 1024   # 64 MiB

def read_chunk_ec(sock, inode: int, chunk_index: int) -> dict:
    """
    Returns {
        "chunk_id": int,
        "version": int,
        "file_length": int,
        "servers": [{"ip": "x.x.x.x", "port": int}, ...]  # 5 entries: DF0-DF3, CF0
    }
    """
    payload = struct.pack(">I", 0) + struct.pack(">I", inode) + struct.pack(">I", chunk_index)
    write_frame(sock, CLTOM_FUSE_READ_CHUNK, payload)
    cmd, ans = read_frame(sock)
    assert cmd == MATOCL_FUSE_READ_CHUNK

    if len(ans) == 5:
        raise RuntimeError(f"ReadChunk EC ({inode}, {chunk_index}): status=0x{ans[4]:02x}")

    off = 4   # skip msgid
    proto_id = ans[off]; off += 1
    assert proto_id == 3, f"Expected proto=3 (EC), got proto={proto_id}"

    file_length = struct.unpack_from(">Q", ans, off)[0]; off += 8
    chunk_id    = struct.unpack_from(">Q", ans, off)[0]; off += 8
    version     = struct.unpack_from(">I", ans, off)[0]; off += 4

    servers = []
    while off + 14 <= len(ans):
        ip_raw = struct.unpack_from(">I", ans, off)[0]; off += 4
        port   = struct.unpack_from(">H", ans, off)[0]; off += 2
        _cs_ver = struct.unpack_from(">I", ans, off)[0]; off += 4   # cs_ver (unused)
        _label  = struct.unpack_from(">I", ans, off)[0]; off += 4   # labelmask (unused)
        ip_str = f"{(ip_raw>>24)&0xFF}.{(ip_raw>>16)&0xFF}.{(ip_raw>>8)&0xFF}.{ip_raw&0xFF}"
        servers.append({"ip": ip_str, "port": port})

    assert len(servers) == 5, f"Expected 5 EC servers (4 DF + 1 CF), got {len(servers)}"
    return {"chunk_id": chunk_id, "version": version, "file_length": file_length, "servers": servers}
```

---

### Phase 2 : Protocole ChunkServer TCP — Lecture d'un shard EC

**Réponse à la question clé : même opcode que proto=2**

L'opcode `CLTOCS_READ = 200` est identique pour les chunks EC et les chunks répliqués.
La différence est que :
- Pour proto=2 (réplication) : le CS héberge le chunk complet (64 MiB) — on lit avec `size = chunk_data_size`
- Pour proto=3 (EC4+1) : le CS héberge uniquement son shard (¼ du chunk) — on lit avec `size = shard_size`

Le chunkID utilisé dans la requête CS est le même chunkID retourné par le master (partagé par tous les shards).

#### 2.1 — Calcul de la taille du shard

```
chunk_data_size = min(file_length - chunk_index * CHUNK_SIZE, CHUNK_SIZE)
shard_size      = math.ceil(chunk_data_size / 4)
```

Contrainte : si `chunk_data_size` n'est pas un multiple de 4, le dernier octet
de données est dans le dernier DF, et les DFs sont paddés à zéro jusqu'au prochain
multiple de 4. La taille du shard envoyée dans la requête CS est `shard_size`
(octets effectifs stockés, arrondi au supérieur).

#### 2.2 — Lecture d'un shard (opcode 200 → 202 + 201)

- Request `CLTOCS_READ (200)` : `[chunkId:64][version:32][offset:32][size:32]`
  - `offset = 0` (lire depuis le début du shard)
  - `size = shard_size`
- Response DATA `CSTOCL_READ_DATA (202)` (zéro ou plusieurs fois) :
  `[chunkId:64][blocknum:16][blockOffset:16][size:32][crc:32][data:size]`
  - Header = 20 bytes, data commence à offset 20
  - Vérifier CRC32-IEEE de `data` contre le champ `crc`
- Response STATUS `CSTOCL_READ_STATUS (201)` :
  `[chunkId:64][status:8]` — status=0 → succès

```python
import math, zlib

CLTOCS_READ           = 200
CSTOCL_READ_STATUS    = 201
CSTOCL_READ_DATA      = 202
ANTOAN_NOP            = 0

def read_shard(ip: str, port: int, chunk_id: int, version: int, shard_size: int) -> bytes:
    """
    Lit un shard EC depuis un chunkserver.
    Retourne les données brutes du shard (shard_size bytes).
    """
    with socket.create_connection((ip, port), timeout=10) as cs:
        payload = (
            struct.pack(">Q", chunk_id)
            + struct.pack(">I", version)
            + struct.pack(">I", 0)           # offset dans le shard
            + struct.pack(">I", shard_size)  # taille à lire
        )
        write_frame(cs, CLTOCS_READ, payload)

        result = bytearray()
        while True:
            cmd, data = read_frame(cs)
            if cmd == ANTOAN_NOP:
                continue
            if cmd == CSTOCL_READ_DATA:
                # Header : [chunkId:64][blocknum:16][blockOffset:16][size:32][crc:32]
                if len(data) < 20:
                    raise RuntimeError(f"READ_DATA too short: {len(data)} bytes")
                block_size = struct.unpack_from(">I", data, 12)[0]
                frame_crc  = struct.unpack_from(">I", data, 16)[0]
                block      = data[20:20 + block_size]
                got_crc    = zlib.crc32(block) & 0xFFFFFFFF
                if got_crc != frame_crc:
                    raise RuntimeError(f"CRC mismatch: got 0x{got_crc:08x}, want 0x{frame_crc:08x}")
                result.extend(block)
            elif cmd == CSTOCL_READ_STATUS:
                status = data[8]
                if status != 0:
                    raise RuntimeError(f"CS READ_STATUS error: 0x{status:02x}")
                return bytes(result)
            else:
                raise RuntimeError(f"Unexpected CS cmd: {cmd}")
```

---

### Phase 3 : Reconstruction XOR et Vérification

#### 3.1 — Reconstruction des données d'un chunk

```python
def reconstruct_chunk(dfs: list[bytes], chunk_data_size: int) -> bytes:
    """
    dfs = [DF0, DF1, DF2, DF3] (chacun de taille shard_size)
    chunk_data_size = taille réelle des données dans ce chunk
    Retourne les données reconstruites (chunk_data_size bytes)
    """
    # Concaténer les 4 data fragments dans l'ordre
    data = b"".join(dfs)
    # Tronquer au nombre d'octets réels (suppression du padding)
    return data[:chunk_data_size]
```

#### 3.2 — Vérification XOR contre CF0

```python
def xor_verify(dfs: list[bytes], cf0: bytes) -> bool:
    """
    Vérifie que XOR(DF0, DF1, DF2, DF3) == CF0.
    Les shards sont de même taille (shard_size). CF0 peut être plus court
    si file_length % 4 != 0, dans ce cas tronquer les DFs à len(cf0) pour la comparaison.
    """
    n = len(cf0)
    parity = bytearray(n)
    for df in dfs:
        for i in range(n):
            parity[i] ^= df[i]
    return bytes(parity) == cf0
```

**Note sur le padding** : CF0 = XOR(DF0[:shard_size], DF1[:shard_size], DF2[:shard_size], DF3[:shard_size]).
Les octets de padding (au-delà de `chunk_data_size`) sont inclus dans le XOR. Si CF0 est stocké avec la
même taille shard_size, la vérification porte sur tous les octets y compris le padding.

---

### Phase 4 : Layout Stripe et Multi-Chunk

#### 4.1 — Layout stripe EC4+1

```
Fichier d'origine : [AAAAAAAAAA BBBBBBBBBB CCCCCCCCCC DDDDDDDDDD ...]
                    |<-------- chunk 0 (64 MiB max) ------->|<-- chunk 1...

Pour un chunk de chunk_data_size octets :
    shard_size = ceil(chunk_data_size / 4)

    DF0 = octets [0 .. shard_size-1]
    DF1 = octets [shard_size .. 2*shard_size-1]
    DF2 = octets [2*shard_size .. 3*shard_size-1]
    DF3 = octets [3*shard_size .. 4*shard_size-1]
    CF0 = XOR(DF0, DF1, DF2, DF3)

Si chunk_data_size % 4 != 0 :
    Le dernier DF contient les octets réels + zéros de padding jusqu'à shard_size.
    CF0 inclut les zéros de padding dans le XOR.
```

#### 4.2 — Multi-chunk (large_ec.bin : 2 chunks)

```python
def read_file_ec(master_ip: str, master_port: int, filename: str) -> bytes:
    """Lit un fichier EC complet depuis MooseFS."""
    with socket.create_connection((master_ip, master_port), timeout=10) as master:
        register(master)
        inode = lookup(master, ROOT_NODE_ID, filename)

        file_data = bytearray()
        chunk_index = 0

        while True:
            try:
                info = read_chunk_ec(master, inode, chunk_index)
            except RuntimeError as e:
                if "EOF" in str(e) or "status=0x00" in str(e):
                    break   # fin du fichier
                raise

            file_length     = info["file_length"]
            chunk_data_size = min(file_length - chunk_index * CHUNK_SIZE, CHUNK_SIZE)
            if chunk_data_size <= 0:
                break

            shard_size = math.ceil(chunk_data_size / 4)
            servers    = info["servers"]   # [DF0, DF1, DF2, DF3, CF0]
            chunk_id   = info["chunk_id"]
            version    = info["version"]

            # Lire les 4 data fragments en parallèle (optionnel pour le POC)
            dfs = []
            for i in range(4):
                srv = servers[i]
                shard = read_shard(srv["ip"], srv["port"], chunk_id, version, shard_size)
                dfs.append(shard)

            # Lire CF0 pour vérification
            cf0_srv = servers[4]
            cf0 = read_shard(cf0_srv["ip"], cf0_srv["port"], chunk_id, version, shard_size)

            # Vérifier XOR
            if not xor_verify(dfs, cf0):
                raise RuntimeError(f"XOR parity mismatch on chunk {chunk_index}")

            # Reconstruire les données
            chunk_data = reconstruct_chunk(dfs, chunk_data_size)
            file_data.extend(chunk_data)
            chunk_index += 1

        return bytes(file_data)
```

---

### Phase 5 : Validation Checksum

#### 5.1 — MD5 du fichier reconstruit

```python
import hashlib

def validate_checksum(data: bytes, expected_md5: str) -> bool:
    got = hashlib.md5(data).hexdigest()
    print(f"MD5 reconstruit : {got}")
    if expected_md5:
        ok = got == expected_md5
        print(f"Comparaison : {'OK' if ok else 'ECHEC'} (attendu : {expected_md5})")
        return ok
    return True
```

**Obtention du MD5 de référence** (sur le serveur MooseFS, via ssh) :
```bash
md5sum /mnt/mfs/small_ec.bin
md5sum /mnt/mfs/large_ec.bin
```
Ces valeurs seront intégrées comme constantes dans le script ou passées en argument.

---

### Phase 6 : Génération des Fixtures JSON

#### 6.1 — Format des fixtures

Objectif : produire des fixtures JSON utilisables par les tests Go pour valider
l'implémentation EC en Go sans connexion réseau réelle.

Répertoire : `tests/fixtures/ec/ec4/`

```
tests/fixtures/ec/ec4/
├── small_ec_chunk0.json       # Métadonnées + shards bruts en base64 (32 MB → trop lourd)
├── small_ec_chunk0_meta.json  # Métadonnées uniquement (sans data) pour les tests de parsing
├── small_ec_chunk0_shards.json  # Premier 4 KiB de chaque shard (boundaries) pour XOR tests
└── large_ec_chunk0_meta.json  # Idem pour chunk 0 de large_ec.bin
```

**Format `*_meta.json`** :

```json
{
  "filename": "small_ec.bin",
  "file_size": 33554432,
  "chunk_index": 0,
  "chunk_id": "0x012EB689",
  "version": 1,
  "proto": 3,
  "chunk_data_size": 33554432,
  "shard_size": 8388608,
  "servers": [
    {"role": "DF0", "ip": "192.168.2.218", "port": 9423},
    {"role": "DF1", "ip": "192.168.2.216", "port": 9424},
    {"role": "DF2", "ip": "192.168.2.217", "port": 9423},
    {"role": "DF3", "ip": "192.168.2.211", "port": 9423},
    {"role": "CF0", "ip": "192.168.2.100", "port": 9423}
  ],
  "md5_reconstructed": "<md5sum du fichier reconstruit>"
}
```

**Format `*_shards.json`** (pour tests XOR unitaires — sous-ensemble de données) :

```json
{
  "chunk_id": "0x012EB689",
  "version": 1,
  "shard_size": 8388608,
  "sample_offset": 0,
  "sample_size": 4096,
  "df0_sample_b64": "<base64 des 4096 premiers octets de DF0>",
  "df1_sample_b64": "<base64>",
  "df2_sample_b64": "<base64>",
  "df3_sample_b64": "<base64>",
  "cf0_sample_b64": "<base64>",
  "xor_verified": true
}
```

```python
import json, base64, os

def generate_fixtures(filename: str, file_size: int, chunk_index: int,
                      info: dict, dfs: list[bytes], cf0: bytes,
                      chunk_data_size: int, shard_size: int,
                      md5_hash: str, out_dir: str):
    os.makedirs(out_dir, exist_ok=True)
    stem = filename.replace(".", "_")

    # Fichier meta (toujours généré)
    meta = {
        "filename": filename,
        "file_size": file_size,
        "chunk_index": chunk_index,
        "chunk_id": f"0x{info['chunk_id']:08X}",
        "version": info["version"],
        "proto": 3,
        "chunk_data_size": chunk_data_size,
        "shard_size": shard_size,
        "servers": [
            {"role": role, "ip": srv["ip"], "port": srv["port"]}
            for role, srv in zip(["DF0","DF1","DF2","DF3","CF0"], info["servers"])
        ],
        "md5_reconstructed": md5_hash,
    }
    with open(os.path.join(out_dir, f"{stem}_chunk{chunk_index}_meta.json"), "w") as f:
        json.dump(meta, f, indent=2)

    # Fichier shards (échantillon de 4096 premiers octets pour XOR tests)
    sample = 4096
    shards = {
        "chunk_id": f"0x{info['chunk_id']:08X}",
        "version": info["version"],
        "shard_size": shard_size,
        "sample_offset": 0,
        "sample_size": sample,
        "xor_verified": True,
    }
    for i, (role, shard) in enumerate(zip(["df0","df1","df2","df3"], dfs)):
        shards[f"{role}_sample_b64"] = base64.b64encode(shard[:sample]).decode()
    shards["cf0_sample_b64"] = base64.b64encode(cf0[:sample]).decode()

    with open(os.path.join(out_dir, f"{stem}_chunk{chunk_index}_shards.json"), "w") as f:
        json.dump(shards, f, indent=2)
```

---

### Phase 7 : Script principal et point d'entrée

#### 7.1 — Structure finale du script

Fichier : `poc/ec4_reader.py`

```
poc/ec4_reader.py
├── Imports (struct, socket, math, zlib, hashlib, json, base64, os, argparse)
├── Constantes protocole (opcodes, CHUNK_SIZE, FUSE_REGISTER_BLOB, MFS_CLIENT_VERSION)
├── Primitives réseau (_recv_exact, write_frame, read_frame)
├── Master API (register, lookup, read_chunk_ec)
├── CS API (read_shard)
├── EC logic (reconstruct_chunk, xor_verify)
├── Fixtures (generate_fixtures)
├── Orchestration (read_file_ec)
└── main() avec argparse
```

#### 7.2 — Interface CLI

```bash
# Lire small_ec.bin et générer fixtures
python3 poc/ec4_reader.py \
    --master 192.168.1.231:9421 \
    --file small_ec.bin \
    --expected-md5 <md5> \
    --fixtures tests/fixtures/ec/ec4/

# Lire large_ec.bin
python3 poc/ec4_reader.py \
    --master 192.168.1.231:9421 \
    --file large_ec.bin \
    --fixtures tests/fixtures/ec/ec4/
```

---

## Tests Requis

- [ ] **Unitaires Python** (`poc/test_ec4.py`) :
  - `test_write_read_frame()` : round-trip sur socket mock
  - `test_reconstruct_chunk_aligned()` : chunk_data_size multiple de 4
  - `test_reconstruct_chunk_unaligned()` : chunk_data_size non-multiple de 4
  - `test_xor_verify_correct()` : XOR valide
  - `test_xor_verify_corrupt()` : XOR invalide (corruption simulée)
- [ ] **Tests fixtures JSON** : script `poc/validate_fixtures.py` lit les `*_shards.json` et vérifie le XOR sur les échantillons
- [ ] **Tests Go** (phase ultérieure, issue #114) : les fixtures JSON de `tests/fixtures/ec/ec4/` serviront de données de référence

---

## Risques et Mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| Shard_size incorrect (arrondi) | Moyenne | Élevé | Tester les 2 cas : 32MB (non-multiple de 4×8MB) et 64MB (multiple) |
| CS retourne READ_STATUS avant données | Faible | Moyen | La boucle read_frame gère déjà l'ordre variable |
| Timeout connexion CS (réseau) | Faible | Moyen | `socket.create_connection` avec timeout=10s |
| Proto=3 non reçu (master version ancienne) | Faible | Élevé | Assertion explicite sur proto_id + message d'erreur clair |
| Fixtures trop volumineuses pour Git | Moyenne | Faible | Stocker uniquement échantillons 4096B — pas les shards complets |
| Padding zéro CF0 incorrect | Faible | Moyen | Test dédié avec chunk non-multiple de 4 octets |

---

## Points d'Attention

### Ambiguïté résolue : opcode CS pour les shards EC

Le même opcode `CLTOCS_READ = 200` est utilisé pour lire les chunks EC et les chunks répliqués.
La différence est dans la **sémantique** :
- Proto=2 (réplication) : le CS héberge le chunk complet → lire avec `size = chunk_data_size`
- Proto=3 (EC) : le CS héberge un shard de taille `ceil(chunk_data_size/4)` → lire avec `size = shard_size`

Le chunkID dans la requête CS est le même chunkID retourné par le master (partagé par tous les shards).

### Ordre des shards dans la réponse master

L'ordre des 5 serveurs dans la réponse `MATOCL_FUSE_READ_CHUNK` (proto=3) détermine
le rôle de chaque shard : index 0=DF0, 1=DF1, 2=DF2, 3=DF3, 4=CF0.
Cet ordre est fixe et garanti par le protocole MooseFS.

### NOP keepalives

Le master peut envoyer des trames NOP (cmd=0) après la connexion TCP. Les ignorer
silencieusement dans tous les `read_frame` loops (identique au comportement Go).

### Accès lecture seule

Le POC ne doit jamais écrire sur le serveur MooseFS. Uniquement :
- Register (NewSession, pas de droits write)
- Lookup
- ReadChunk
- DialCS + ReadChunk CS

Aucun `Mknod`, `Write`, `WriteChunkEnd` ou modification de configuration.

---

## Estimation

- **Complexité** : Moyenne
- **Nombre de fichiers** : ~3 fichiers Python (`ec4_reader.py`, `test_ec4.py`, `validate_fixtures.py`) + N fixtures JSON
- **Durée estimée** : 2–3h de développement (1h protocol, 30min EC logic, 30min fixtures, 30min tests)
- **Dépendances Python** : stdlib uniquement (`struct`, `socket`, `hashlib`, `zlib`, `json`, `base64`, `argparse`, `math`)

---

## Notes Supplémentaires

### Vérification des données de référence (mfsfileinfo)

Les données `mfsfileinfo` fournies dans l'ordre confirment les assignments DF/CF :
```
small_ec.bin chunk 0 (ID 0x012EB689):
  DF0 → 192.168.2.218:9423  (server[0])
  DF1 → 192.168.2.216:9424  (server[1])
  DF2 → 192.168.2.217:9423  (server[2])
  DF3 → 192.168.2.211:9423  (server[3])
  CF0 → 192.168.2.100:9423  (server[4])
```

Ces adresses correspondent exactement à l'ordre des servers dans la réponse proto=3 du master.

### Calcul exact des tailles pour les fichiers de test

**small_ec.bin (32 MB = 33 554 432 bytes, 1 chunk)** :
- chunk_data_size = 33 554 432
- shard_size = ceil(33 554 432 / 4) = 8 388 608 (= 8 MiB exactement, car 32MB = 4 × 8MB)
- Reconstruction : DF0‖DF1‖DF2‖DF3 = 4 × 8 MiB = 32 MiB ✓

**large_ec.bin (128 MB = 134 217 728 bytes, 2 chunks)** :
- chunk 0 : chunk_data_size = min(134 217 728 - 0, 67 108 864) = 67 108 864 (64 MiB)
  - shard_size = 67 108 864 / 4 = 16 777 216 (16 MiB, multiple exact)
- chunk 1 : chunk_data_size = min(134 217 728 - 67 108 864, 67 108 864) = 67 108 864 (64 MiB)
  - shard_size = 16 777 216 (idem)
- Reconstruction totale : 2 × 64 MiB = 128 MiB ✓
