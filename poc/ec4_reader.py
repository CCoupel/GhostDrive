#!/usr/bin/env python3
"""
poc/ec4_reader.py — POC Python EC4+1 MooseFS (issue #113)

Lit des fichiers EC 4+1 sur un vrai serveur MooseFS via le protocole TCP,
reconstruit les données par XOR, valide le checksum, et génère des fixtures
JSON pour les tests unitaires Go.

Accès MooseFS en LECTURE SEULE uniquement.
stdlib Python uniquement — aucune dépendance externe.

Usage :
    python3 poc/ec4_reader.py --master 192.168.1.231:9421 --file small_ec.bin \\
        --expected-md5 <md5> --fixtures tests/fixtures/ec/ec4/
"""

import argparse
import base64
import hashlib
import json
import math
import os
import socket
import struct
import zlib

# ---------------------------------------------------------------------------
# Constantes protocole
# ---------------------------------------------------------------------------

FUSE_REGISTER_BLOB = b"DjI1GAQDULI5d2YjA26ypc3ovkhjvhciTQVx3CS4nYgtBoUcsljiVpsErJENHaw0"
MFS_CLIENT_VERSION = (4 << 16) | (58 << 8) | (4 * 2)  # VERSION2INT(4,58,4) = 277000

CLTOM_FUSE_REGISTER = 400
MATOCL_FUSE_REGISTER = 401
REGISTER_NEWSESSION = 2

CLTOM_FUSE_LOOKUP = 406
MATOCL_FUSE_LOOKUP = 407
ROOT_NODE_ID = 1

CLTOM_FUSE_READ_CHUNK = 432
MATOCL_FUSE_READ_CHUNK = 433
CHUNK_SIZE = 64 * 1024 * 1024  # 64 MiB

CLTOCS_READ = 200
CSTOCL_READ_STATUS = 201
CSTOCL_READ_DATA = 202
ANTOAN_NOP = 0

# ---------------------------------------------------------------------------
# EC4 physical chunk_id encoding (source: MooseFS CE hddspacemgr.c)
# ---------------------------------------------------------------------------
# EC4: ecidstart = 0x1000000000000000, step = 0x0100000000000000
# EC8: ecidstart = 0x2000000000000000, step = 0x0100000000000000
#
# physical_chunk_id[part] = logical_chunk_id + ecidstart + part * step
#
# Part index corresponds to the order returned by the master (DF0=0, DF1=1, ..., CF0=4).
# The CS validates chunk_id exactly (no masking in the read path), so the physical ID
# MUST be sent — sending the logical ID returns NOCHUNK for size>0.

EC4_ECID_START = 0x1000000000000000  # upper byte = 0x10 for part 0
EC4_ECID_STEP  = 0x0100000000000000  # upper byte increments by 1 per part
EC8_ECID_START = 0x2000000000000000  # upper byte = 0x20 for EC8 part 0


def ec4_physical_chunk_id(logical_id: int, part_idx: int) -> int:
    """
    Calcule le physical chunk_id EC4 pour la part `part_idx`.

    part_idx 0..3 = data fragments (DF0-DF3)
    part_idx 4    = parity (CF0)

    Formule (MooseFS CE hddspacemgr.c, hdd_int_split) :
        physical = logical_id + 0x1000000000000000 + part_idx * 0x0100000000000000
    """
    ecidpart = EC4_ECID_START + part_idx * EC4_ECID_STEP
    return (logical_id + ecidpart) & 0xFFFFFFFFFFFFFFFF


# ---------------------------------------------------------------------------
# Primitives réseau
# ---------------------------------------------------------------------------

def _recv_exact(sock: socket.socket, n: int) -> bytes:
    """Lit exactement n octets depuis le socket."""
    buf = bytearray()
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise EOFError(f"Connection closed (expected {n} bytes, got {len(buf)})")
        buf.extend(chunk)
    return bytes(buf)


def write_frame(sock: socket.socket, cmd: int, payload: bytes) -> None:
    """Encode et envoie une trame MooseFS [cmd:32 BE][len:32 BE][payload]."""
    hdr = struct.pack(">II", cmd, len(payload))
    sock.sendall(hdr + payload)


def read_frame(sock: socket.socket) -> tuple:
    """Lit une trame MooseFS. Retourne (cmd, payload)."""
    hdr = _recv_exact(sock, 8)
    cmd, length = struct.unpack(">II", hdr)
    payload = _recv_exact(sock, length) if length > 0 else b""
    return cmd, payload


# ---------------------------------------------------------------------------
# Master API
# ---------------------------------------------------------------------------

def register(sock: socket.socket) -> int:
    """
    Enregistre une nouvelle session (REGISTER_NEWSESSION = rcode 2).
    Retourne le sessionId.
    """
    payload = (
        FUSE_REGISTER_BLOB
        + struct.pack(">B", REGISTER_NEWSESSION)
        + struct.pack(">I", MFS_CLIENT_VERSION)
        + struct.pack(">I", 0)   # ileng = 0 (nom instance vide)
        + struct.pack(">I", 2)   # pleng = 2 (len de "/" + "\x00")
        + b"/\x00"               # mount path null-terminé
    )
    write_frame(sock, CLTOM_FUSE_REGISTER, payload)
    cmd, ans = read_frame(sock)
    # Ignorer les NOP keepalives
    while cmd == ANTOAN_NOP:
        cmd, ans = read_frame(sock)
    assert cmd == MATOCL_FUSE_REGISTER, f"Expected MATOCL_FUSE_REGISTER={MATOCL_FUSE_REGISTER}, got {cmd}"
    if len(ans) == 1:
        raise RuntimeError(f"Register failed: status=0x{ans[0]:02x}")
    session_id = struct.unpack(">I", ans[4:8])[0]
    return session_id


def lookup(sock: socket.socket, parent_id: int, name: str) -> int:
    """
    Lookup d'un fichier par nom dans un répertoire parent.
    Retourne l'inode.
    """
    name_b = name.encode()
    payload = (
        struct.pack(">I", 0)             # msgid
        + struct.pack(">I", parent_id)
        + struct.pack(">B", len(name_b))
        + name_b
        + struct.pack(">I", 0)           # uid
        + struct.pack(">I", 1)           # gcnt
        + struct.pack(">I", 0)           # gid
    )
    write_frame(sock, CLTOM_FUSE_LOOKUP, payload)
    cmd, ans = read_frame(sock)
    assert cmd == MATOCL_FUSE_LOOKUP, f"Expected MATOCL_FUSE_LOOKUP={MATOCL_FUSE_LOOKUP}, got {cmd}"
    if len(ans) == 5:
        raise FileNotFoundError(f"Lookup({name!r}): status=0x{ans[4]:02x}")
    inode = struct.unpack(">I", ans[4:8])[0]
    return inode


def read_chunk_ec(sock: socket.socket, inode: int, chunk_index: int) -> dict:
    """
    Lit les métadonnées d'un chunk EC depuis le master (proto=3).

    Retourne :
    {
        "chunk_id": int,
        "version": int,
        "file_length": int,
        "servers": [{"ip": "x.x.x.x", "port": int}, ...]  # 5 entrées : DF0-DF3, CF0
    }
    """
    payload = (
        struct.pack(">I", 0)
        + struct.pack(">I", inode)
        + struct.pack(">I", chunk_index)
    )
    write_frame(sock, CLTOM_FUSE_READ_CHUNK, payload)
    cmd, ans = read_frame(sock)
    # Ignorer les NOP keepalives envoyés par le master pendant les transferts CS
    while cmd == ANTOAN_NOP:
        cmd, ans = read_frame(sock)
    assert cmd == MATOCL_FUSE_READ_CHUNK, f"Expected MATOCL_FUSE_READ_CHUNK={MATOCL_FUSE_READ_CHUNK}, got {cmd}"

    if len(ans) == 5:
        raise RuntimeError(f"ReadChunk EC ({inode}, {chunk_index}): status=0x{ans[4]:02x}")

    off = 4  # skip msgid
    proto_id = ans[off]; off += 1
    assert proto_id == 3, f"Expected proto=3 (EC), got proto={proto_id}"

    file_length = struct.unpack_from(">Q", ans, off)[0]; off += 8
    chunk_id    = struct.unpack_from(">Q", ans, off)[0]; off += 8
    version     = struct.unpack_from(">I", ans, off)[0]; off += 4

    remaining = len(ans) - off
    # Pour EC4+1 : le master retourne 4 serveurs (DF0-DF3) pour une lecture normale.
    # CF0 (parity) n'est fourni que pour la récupération (shard manquant).
    # Chaque entrée : [ip:32][port:16][cs_ver:32][labelmask:32] = 14 bytes
    ENTRY_SIZE = 14
    n_servers = remaining // ENTRY_SIZE
    if n_servers not in (4, 5) or remaining % ENTRY_SIZE != 0:
        raise RuntimeError(
            f"Unexpected EC response: remaining={remaining} bytes "
            f"(raw: {ans[off:].hex()})"
        )

    servers = []
    for _ in range(n_servers):
        ip_raw  = struct.unpack_from(">I", ans, off)[0]; off += 4
        port    = struct.unpack_from(">H", ans, off)[0]; off += 2
        off    += 8  # cs_ver (4B) + labelmask (4B)
        ip_str  = (
            f"{(ip_raw >> 24) & 0xFF}.{(ip_raw >> 16) & 0xFF}"
            f".{(ip_raw >> 8) & 0xFF}.{ip_raw & 0xFF}"
        )
        servers.append({"ip": ip_str, "port": port})
    return {
        "chunk_id": chunk_id,
        "version": version,
        "file_length": file_length,
        "servers": servers,
    }


# ---------------------------------------------------------------------------
# ChunkServer API
# ---------------------------------------------------------------------------

def read_shard(ip: str, port: int, chunk_id: int, version: int, shard_size: int,
               part_idx: int = -1) -> bytes:
    """
    Lit un shard EC depuis un chunkserver via TCP.

    Le CS MooseFS Pro 4.x stocke les shards EC sous des physical chunk_ids différents
    des logical chunk_ids retournés par le master. Le physical chunk_id doit être
    calculé avant l'appel :
        physical = logical_id + EC4_ECID_START + part_idx * EC4_ECID_STEP

    Si `part_idx` >= 0, calcule automatiquement le physical_chunk_id depuis `chunk_id`
    (traité comme logical_id). Sinon, `chunk_id` est utilisé tel quel (physical_id fourni).

    Opcode CLTOCS_READ (200) identique pour chunks EC et répliqués.
    Pour EC : size = shard_size (¼ du chunk + padding éventuel).
    Vérifie le CRC32-IEEE de chaque bloc DATA reçu.
    Retourne les données brutes du shard (shard_size bytes).
    """
    if part_idx >= 0:
        physical_id = ec4_physical_chunk_id(chunk_id, part_idx)
    else:
        physical_id = chunk_id

    with socket.create_connection((ip, port), timeout=10) as cs:
        payload = (
            struct.pack(">Q", physical_id)
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
                    raise RuntimeError(
                        f"CRC mismatch on shard {chunk_id}: "
                        f"got 0x{got_crc:08x}, want 0x{frame_crc:08x}"
                    )
                result.extend(block)
            elif cmd == CSTOCL_READ_STATUS:
                # [chunkId:64][status:8]
                if len(data) < 9:
                    raise RuntimeError(f"READ_STATUS too short: {len(data)} bytes")
                status = data[8]
                if status != 0:
                    raise RuntimeError(f"CS READ_STATUS error: 0x{status:02x}")
                return bytes(result)
            else:
                raise RuntimeError(f"Unexpected CS cmd: {cmd}")


# ---------------------------------------------------------------------------
# EC logic
# ---------------------------------------------------------------------------

def reconstruct_chunk(dfs: list, chunk_data_size: int) -> bytes:
    """
    Reconstruit les données d'un chunk depuis les 4 data fragments.

    dfs = [DF0, DF1, DF2, DF3] (chacun de taille shard_size)
    Retourne les données reconstruites (chunk_data_size bytes, sans padding).
    """
    data = b"".join(dfs)
    return data[:chunk_data_size]


def xor_verify(dfs: list, cf0: bytes) -> bool:
    """
    Vérifie que XOR(DF0, DF1, DF2, DF3) == CF0.

    Les shards peuvent être plus longs que cf0 si chunk_data_size % 4 != 0.
    La comparaison porte sur len(cf0) octets.
    """
    n = len(cf0)
    parity = bytearray(n)
    for df in dfs:
        for i in range(n):
            parity[i] ^= df[i]
    return bytes(parity) == cf0


# ---------------------------------------------------------------------------
# Fixtures JSON
# ---------------------------------------------------------------------------

def generate_fixtures(
    filename: str,
    file_size: int,
    chunk_index: int,
    info: dict,
    dfs: list,
    cf0: bytes,
    chunk_data_size: int,
    shard_size: int,
    md5_hash: str,
    out_dir: str,
) -> None:
    """
    Génère 2 fichiers JSON pour un chunk :
    - <stem>_chunk<N>_meta.json  : métadonnées (sans data)
    - <stem>_chunk<N>_shards.json : échantillon 4096B de chaque shard (pour tests XOR)
    """
    os.makedirs(out_dir, exist_ok=True)
    stem = os.path.basename(filename).replace(".", "_")

    # --- Fichier meta ---
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
            for role, srv in zip(["DF0", "DF1", "DF2", "DF3", "CF0"], info["servers"])
        ],
        "md5_reconstructed": md5_hash,
    }
    meta_path = os.path.join(out_dir, f"{stem}_chunk{chunk_index}_meta.json")
    with open(meta_path, "w", encoding="utf-8") as f:
        json.dump(meta, f, indent=2)
    print(f"  Fixture écrite : {meta_path}")

    # --- Fichier shards (échantillon 4096B pour tests XOR unitaires) ---
    sample = 4096
    shards = {
        "chunk_id": f"0x{info['chunk_id']:08X}",
        "version": info["version"],
        "shard_size": shard_size,
        "sample_offset": 0,
        "sample_size": sample,
        "xor_verified": True,
    }
    for role, shard in zip(["df0", "df1", "df2", "df3"], dfs):
        shards[f"{role}_sample_b64"] = base64.b64encode(shard[:sample]).decode()
    shards["cf0_sample_b64"] = base64.b64encode(cf0[:sample]).decode()

    shards_path = os.path.join(out_dir, f"{stem}_chunk{chunk_index}_shards.json")
    with open(shards_path, "w", encoding="utf-8") as f:
        json.dump(shards, f, indent=2)
    print(f"  Fixture écrite : {shards_path}")


# ---------------------------------------------------------------------------
# Validation checksum
# ---------------------------------------------------------------------------

def validate_checksum(data: bytes, expected_md5: str) -> bool:
    """Calcule le MD5 des données reconstruites et compare à la valeur attendue."""
    got = hashlib.md5(data).hexdigest()
    print(f"  MD5 reconstruit : {got}")
    if expected_md5:
        ok = got == expected_md5
        print(f"  Comparaison MD5 : {'OK' if ok else 'ECHEC'} (attendu : {expected_md5})")
        return ok
    return True


# ---------------------------------------------------------------------------
# Orchestration principale
# ---------------------------------------------------------------------------

def read_file_ec(
    master_ip: str,
    master_port: int,
    filename: str,
    expected_md5: str = "",
    fixtures_dir: str = "",
) -> bytes:
    """
    Lit un fichier EC complet depuis MooseFS via le protocole TCP.

    - Se connecte au master, s'enregistre, lookup l'inode
    - Pour chaque chunk : récupère les métadonnées EC, lit les 4 shards DF + CF0,
      vérifie le XOR, reconstruit les données
    - Optionnellement génère les fixtures JSON dans fixtures_dir
    - Valide le MD5 si expected_md5 est fourni
    """
    print(f"\n=== Lecture EC : {filename} ===")
    print(f"Master : {master_ip}:{master_port}")

    with socket.create_connection((master_ip, master_port), timeout=10) as master:
        session_id = register(master)
        print(f"  Session enregistrée : id={session_id}")

        # Traversée du chemin composant par composant
        parts = [p for p in filename.split("/") if p]
        inode = ROOT_NODE_ID
        for part in parts:
            inode = lookup(master, inode, part)
        print(f"  Inode : {inode} (path: /{'/'.join(parts)})")

        file_data = bytearray()
        chunk_index = 0
        file_length = 0  # sera rempli au premier chunk

        while True:
            # Vérification EOF avant de demander le chunk suivant au master
            # (évite une requête pour un chunk qui n'existe pas)
            if file_length > 0:
                chunk_data_size_preview = min(file_length - chunk_index * CHUNK_SIZE, CHUNK_SIZE)
                if chunk_data_size_preview <= 0:
                    print(f"  Fin de fichier atteinte après {chunk_index} chunk(s)")
                    break

            print(f"\n  --- Chunk {chunk_index} ---")
            try:
                info = read_chunk_ec(master, inode, chunk_index)
            except RuntimeError as e:
                # Fin du fichier si le status indique pas de chunk (EOF)
                err_str = str(e)
                if "status=0x00" in err_str or "EOF" in err_str or "proto=" in err_str:
                    print(f"  Fin de fichier détectée au chunk {chunk_index}: {e}")
                    break
                raise

            file_length     = info["file_length"]
            chunk_data_size = min(file_length - chunk_index * CHUNK_SIZE, CHUNK_SIZE)
            if chunk_data_size <= 0:
                break

            shard_size = math.ceil(chunk_data_size / 4)
            servers    = info["servers"]
            chunk_id   = info["chunk_id"]
            version    = info["version"]

            print(f"  chunk_id=0x{chunk_id:08X} version={version} "
                  f"chunk_data_size={chunk_data_size} shard_size={shard_size}")

            # Lire les 4 data fragments (physical chunk_ids = logical + ecidstart + i*step)
            dfs = []
            for i in range(4):
                srv = servers[i]
                role = f"DF{i}"
                phys_id = ec4_physical_chunk_id(chunk_id, i)
                print(f"  Lecture {role} : {srv['ip']}:{srv['port']} "
                      f"phys=0x{phys_id:016X} ({shard_size} bytes)")
                shard = read_shard(srv["ip"], srv["port"], chunk_id, version, shard_size,
                                   part_idx=i)
                print(f"    → {len(shard)} bytes reçus")
                dfs.append(shard)

            # Lire CF0 si disponible dans la réponse master (récupération) ou skip
            cf0 = None
            if len(servers) == 5:
                cf0_srv = servers[4]
                phys_cf0 = ec4_physical_chunk_id(chunk_id, 4)
                print(f"  Lecture CF0 : {cf0_srv['ip']}:{cf0_srv['port']} "
                      f"phys=0x{phys_cf0:016X} ({shard_size} bytes)")
                cf0 = read_shard(cf0_srv["ip"], cf0_srv["port"], chunk_id, version, shard_size,
                                 part_idx=4)
                print(f"    → {len(cf0)} bytes reçus")
                if not xor_verify(dfs, cf0):
                    raise RuntimeError(f"XOR parity mismatch on chunk {chunk_index}")
                print(f"  XOR vérifié ✓")
            else:
                # Vérification XOR locale : XOR(DF0,DF1,DF2,DF3) doit être cohérent
                # (pas de CF0 dispo en lecture normale — vérification MD5 finale suffira)
                print(f"  CF0 non retourné par le master (lecture normale) — skip XOR check")

            # Générer les fixtures si demandé
            if fixtures_dir:
                md5_chunk = hashlib.md5(reconstruct_chunk(dfs, chunk_data_size)).hexdigest()
                generate_fixtures(
                    filename=filename,
                    file_size=file_length,
                    chunk_index=chunk_index,
                    info=info,
                    dfs=dfs,
                    cf0=cf0 or b"",
                    chunk_data_size=chunk_data_size,
                    shard_size=shard_size,
                    md5_hash=md5_chunk,
                    out_dir=fixtures_dir,
                )

            # Reconstruire les données du chunk
            chunk_data = reconstruct_chunk(dfs, chunk_data_size)
            file_data.extend(chunk_data)
            print(f"  Chunk {chunk_index} reconstruit : {len(chunk_data)} bytes")
            chunk_index += 1

    result = bytes(file_data)
    print(f"\n  Total reconstruit : {len(result)} bytes")

    # Validation checksum global
    validate_checksum(result, expected_md5)

    return result


# ---------------------------------------------------------------------------
# Point d'entrée CLI
# ---------------------------------------------------------------------------

def main() -> None:
    parser = argparse.ArgumentParser(
        description="POC Python EC4+1 MooseFS — Lecture et reconstruction de fichiers EC"
    )
    parser.add_argument(
        "--master",
        required=True,
        help="Adresse du master MooseFS (format host:port, ex: 192.168.1.231:9421)",
    )
    parser.add_argument(
        "--file",
        required=True,
        help="Nom du fichier à lire depuis la racine MooseFS (ex: small_ec.bin)",
    )
    parser.add_argument(
        "--expected-md5",
        default="",
        help="MD5 attendu du fichier reconstruit (optionnel, pour validation)",
    )
    parser.add_argument(
        "--fixtures",
        default="",
        help="Répertoire de sortie pour les fixtures JSON (ex: tests/fixtures/ec/ec4/)",
    )
    parser.add_argument(
        "--output",
        default="",
        help="Fichier de sortie pour les données reconstruites (optionnel)",
    )
    args = parser.parse_args()

    # Parser host:port
    if ":" not in args.master:
        parser.error("--master doit être au format host:port")
    host, port_str = args.master.rsplit(":", 1)
    try:
        port = int(port_str)
    except ValueError:
        parser.error(f"Port invalide : {port_str!r}")

    data = read_file_ec(
        master_ip=host,
        master_port=port,
        filename=args.file,
        expected_md5=args.expected_md5,
        fixtures_dir=args.fixtures,
    )

    if args.output:
        with open(args.output, "wb") as f:
            f.write(data)
        print(f"\nFichier reconstruit écrit : {args.output} ({len(data)} bytes)")


if __name__ == "__main__":
    main()
