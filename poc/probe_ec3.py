#!/usr/bin/env python3
"""
poc/probe_ec3.py — Phase 3 : protocole EC MooseFS Pro 4.x

Découverte source MooseFS CE (hddspacemgr.c) :
  EC4 physical chunk_id = logical_chunk_id + ecidpart[i]
    ecidpart[0] = 0x1000000000000000  (part 0, DF0)
    ecidpart[1] = 0x1100000000000000  (part 1, DF1)
    ecidpart[2] = 0x1200000000000000  (part 2, DF2)
    ecidpart[3] = 0x1300000000000000  (part 3, DF3)
    ecidpart[4] = 0x1400000000000000  (part 4, CF0 / parity)
  EC8 physical chunk_id : ecidstart = 0x2000000000000000

  Le CS masque l'upper byte pour les lookups admin/info (0x00FFFFFFFFFFFFFF),
  mais PAS pour les reads → size=0 avec logical ID retourne OK (lookup masquée),
  size>0 retourne NOCHUNK (lookup exacte sur le physical ID).

Tests :
  A. Physical EC chunk_ids (hypothèse principale)
  B. Trivial check — chunk_id bogus size=0 → confirme que OK n'est pas trivial
  C. Master raw dump — vérifie le format exact de la réponse proto=3
  D. version=0 vs version=1 pour les physical chunk_ids
  E. Hello on connect — le CS envoie-t-il quelque chose ?
  F. Opcode scan 350-600 avec size=0 payload
  G. Raw bytes STATUS=OK pour size=0 avec logical chunk_id
  H. EC8 chunk_ids (ecidstart=0x2000000000000000) — au cas où
"""

import socket
import struct
import sys

# ─── Protocole master ─────────────────────────────────────────────────────────

FUSE_REGISTER_BLOB = b"DjI1GAQDULI5d2YjA26ypc3ovkhjvhciTQVx3CS4nYgtBoUcsljiVpsErJENHaw0"
MFS_CLIENT_VERSION = (4 << 16) | (58 << 8) | (4 * 2)
CLTOM_FUSE_REGISTER  = 400
MATOCL_FUSE_REGISTER = 401
CLTOM_FUSE_LOOKUP    = 406
MATOCL_FUSE_LOOKUP   = 407
CLTOM_FUSE_READ_CHUNK  = 432
MATOCL_FUSE_READ_CHUNK = 433
ANTOAN_NOP = 0
ROOT_NODE_ID = 1

# ─── Protocole CS ─────────────────────────────────────────────────────────────

CLTOCS_READ       = 200
CSTOCL_READ_STATUS = 201
CSTOCL_READ_DATA   = 202

# ─── Topologie ────────────────────────────────────────────────────────────────

MASTER = ("192.168.1.231", 9421)

# CS dans l'ordre retourné par le master (DF0 = part 0, DF1 = part 1, etc.)
CS = [
    ("DF0", "192.168.2.218", 9423, 0),  # part index 0
    ("DF1", "192.168.2.216", 9424, 1),  # part index 1
    ("DF2", "192.168.2.217", 9423, 2),  # part index 2
    ("DF3", "192.168.2.211", 9423, 3),  # part index 3
    ("CF0", "192.168.2.100", 9423, 4),  # part index 4 (parity)
]

LOGICAL_CHUNK_ID = 0x00000000012EB689
LOGICAL_VERSION  = 1
SMALL_READ       = 65536        # 64 KiB
SHARD_SIZE       = 8 * 1024 * 1024  # 8 MiB

# ─── EC4 physical chunk_id dérivation ─────────────────────────────────────────

EC4_ECID_START = 0x1000000000000000
EC4_ECID_STEP  = 0x0100000000000000
EC8_ECID_START = 0x2000000000000000


def ec4_physical_id(logical_id: int, part_idx: int) -> int:
    """Calcule le physical chunk_id pour la part EC4 `part_idx`."""
    ecidpart = EC4_ECID_START + part_idx * EC4_ECID_STEP
    return (logical_id + ecidpart) & 0xFFFFFFFFFFFFFFFF


def ec8_physical_id(logical_id: int, part_idx: int) -> int:
    """Calcule le physical chunk_id pour la part EC8 `part_idx`."""
    ecidpart = EC8_ECID_START + part_idx * EC4_ECID_STEP
    return (logical_id + ecidpart) & 0xFFFFFFFFFFFFFFFF


# ─── Helpers réseau ───────────────────────────────────────────────────────────

def _recv_exact(sock, n):
    buf = bytearray()
    while len(buf) < n:
        c = sock.recv(n - len(buf))
        if not c:
            raise EOFError(f"closed ({len(buf)}/{n})")
        buf.extend(c)
    return bytes(buf)


def write_frame(sock, cmd, payload):
    sock.sendall(struct.pack(">II", cmd, len(payload)) + payload)


def read_frame(sock):
    hdr = _recv_exact(sock, 8)
    cmd, length = struct.unpack(">II", hdr)
    data = _recv_exact(sock, length) if length > 0 else b""
    return cmd, data


STATUS_NAMES = {0: "OK", 0x0d: "NOCHUNK", 0x03: "ENOENT", 0x01: "EPERM",
                0x0a: "WRONGVERSION", 0x04: "NOTDONE"}


def status_name(s):
    return STATUS_NAMES.get(s, f"?0x{s:02x}")


# ─── Probe CS générique ───────────────────────────────────────────────────────

def probe_cs(ip, port, opcode, payload, timeout=5, label="", verbose=False):
    """Envoie opcode+payload, retourne string résultat."""
    try:
        with socket.create_connection((ip, port), timeout=timeout) as cs:
            cs.settimeout(timeout)
            write_frame(cs, opcode, payload)
            frames = []
            for _ in range(4):
                try:
                    cmd, data = read_frame(cs)
                    if cmd == CSTOCL_READ_STATUS:
                        s = data[8] if len(data) >= 9 else 0xFF
                        frames.append(f"STATUS={status_name(s)}({len(data)}b)")
                        if verbose:
                            print(f"    raw: {data.hex()}")
                        break
                    elif cmd == CSTOCL_READ_DATA:
                        bs = struct.unpack_from(">I", data, 12)[0] if len(data) >= 16 else 0
                        frames.append(f"DATA({bs}b)")
                    elif cmd == ANTOAN_NOP:
                        frames.append("NOP")
                    else:
                        frames.append(f"CMD{cmd}({len(data)}b)")
                        if verbose:
                            print(f"    CMD{cmd} raw: {data[:32].hex()}")
                except socket.timeout:
                    frames.append("TIMEOUT")
                    break
            result = " + ".join(frames) or "NO_RESP"
    except ConnectionResetError:
        result = "RESET"
    except ConnectionRefusedError:
        result = "REFUSED"
    except EOFError:
        result = "EOF"
    except Exception as e:
        result = f"ERR:{type(e).__name__}:{e}"
    if label:
        print(f"  {label:<76s} → {result}")
    return result


def read_full_shard(ip, port, chunk_id, version, shard_size, timeout=30):
    """Lit un shard complet depuis le CS. Retourne (bytes, status_str)."""
    import zlib
    try:
        with socket.create_connection((ip, port), timeout=timeout) as cs:
            cs.settimeout(timeout)
            payload = struct.pack(">QIII", chunk_id, version, 0, shard_size)
            write_frame(cs, CLTOCS_READ, payload)
            result = bytearray()
            for _ in range(500):
                cmd, data = read_frame(cs)
                if cmd == ANTOAN_NOP:
                    continue
                if cmd == CSTOCL_READ_DATA:
                    if len(data) < 20:
                        return None, f"DATA_TOO_SHORT({len(data)})"
                    block_size = struct.unpack_from(">I", data, 12)[0]
                    frame_crc  = struct.unpack_from(">I", data, 16)[0]
                    block = data[20:20 + block_size]
                    got_crc = zlib.crc32(block) & 0xFFFFFFFF
                    if got_crc != frame_crc:
                        return None, f"CRC_MISMATCH(got=0x{got_crc:08x},want=0x{frame_crc:08x})"
                    result.extend(block)
                elif cmd == CSTOCL_READ_STATUS:
                    s = data[8] if len(data) >= 9 else 0xFF
                    if s != 0:
                        return None, f"STATUS_ERR=0x{s:02x}({status_name(s)})"
                    return bytes(result), f"OK({len(result)}b)"
                else:
                    return None, f"UNEXPECTED_CMD({cmd})"
            return None, "TOO_MANY_FRAMES"
    except Exception as e:
        return None, f"ERR:{type(e).__name__}:{e}"


# ─── Master helpers ───────────────────────────────────────────────────────────

def master_connect():
    """Connecte au master et retourne un socket enregistré."""
    m = socket.create_connection(MASTER, timeout=10)
    m.settimeout(10)
    payload = (
        FUSE_REGISTER_BLOB
        + struct.pack(">B", 2)  # REGISTER_NEWSESSION
        + struct.pack(">I", MFS_CLIENT_VERSION)
        + struct.pack(">I", 0)
        + struct.pack(">I", 2)
        + b"/\x00"
    )
    write_frame(m, CLTOM_FUSE_REGISTER, payload)
    cmd, ans = read_frame(m)
    while cmd == ANTOAN_NOP:
        cmd, ans = read_frame(m)
    assert cmd == MATOCL_FUSE_REGISTER, f"Expected 401, got {cmd}"
    if len(ans) == 1:
        raise RuntimeError(f"Register failed: 0x{ans[0]:02x}")
    return m


def master_lookup(m, parent, name):
    name_b = name.encode()
    payload = (
        struct.pack(">I", 0)
        + struct.pack(">I", parent)
        + struct.pack(">B", len(name_b))
        + name_b
        + struct.pack(">I", 0)
        + struct.pack(">I", 1)
        + struct.pack(">I", 0)
    )
    write_frame(m, CLTOM_FUSE_LOOKUP, payload)
    cmd, ans = read_frame(m)
    assert cmd == MATOCL_FUSE_LOOKUP
    if len(ans) == 5:
        raise FileNotFoundError(f"Lookup {name!r}: 0x{ans[4]:02x}")
    return struct.unpack(">I", ans[4:8])[0]


# ═══════════════════════════════════════════════════════════════════════════════
# A. Physical EC chunk_ids — hypothèse principale
# ═══════════════════════════════════════════════════════════════════════════════

def test_A_physical_ec_ids():
    """
    Teste CLTOCS_READ avec les physical chunk_ids EC4.
    physical_id = logical_chunk_id + ecidstart + part_idx * ecid_step
    """
    print("\n" + "═" * 70)
    print("═══ A. Physical EC4 chunk_ids (hypothèse principale) ═══")
    print("═" * 70)

    print(f"\n  Formule: physical = logical(0x{LOGICAL_CHUNK_ID:016X}) + ecidstart(0x{EC4_ECID_START:016X}) + part*step")
    print(f"  {'Role':<6} {'Part':<5} {'Physical chunk_id':<22} {'IP:Port':<26} {'size=0':>12}  {'size=64K':>12}")
    print("  " + "-" * 80)

    for role, ip, port, part_idx in CS:
        phys = ec4_physical_id(LOGICAL_CHUNK_ID, part_idx)
        r0 = probe_cs(ip, port, CLTOCS_READ,
                      struct.pack(">QIII", phys, LOGICAL_VERSION, 0, 0),
                      timeout=4, label="", verbose=False)
        r64k = probe_cs(ip, port, CLTOCS_READ,
                        struct.pack(">QIII", phys, LOGICAL_VERSION, 0, SMALL_READ),
                        timeout=4, label="", verbose=False)
        print(f"  {role:<6} {part_idx:<5} 0x{phys:016X}    {ip}:{port:<6} {r0:>14}  {r64k:>14}")

    print()
    # Si on trouve des DATA, essayons de lire les shards complets
    print("  --- Tentative lecture complète des shards ---")
    shards = {}
    for role, ip, port, part_idx in CS:
        phys = ec4_physical_id(LOGICAL_CHUNK_ID, part_idx)
        data, status = read_full_shard(ip, port, phys, LOGICAL_VERSION, SHARD_SIZE, timeout=30)
        print(f"  {role}(part {part_idx}) phys=0x{phys:016X}: {status}")
        if data is not None:
            shards[role] = data
            print(f"    → {len(data)} bytes lus")
            print(f"    → premiers 32 bytes: {data[:32].hex()}")

    if len(shards) >= 4:
        print("\n  --- Vérification XOR EC4+1 ---")
        df_shards = [shards[f"DF{i}"] for i in range(4) if f"DF{i}" in shards]
        if len(df_shards) == 4 and all(len(s) == len(df_shards[0]) for s in df_shards):
            n = len(df_shards[0])
            parity = bytearray(n)
            for s in df_shards:
                for j in range(n):
                    parity[j] ^= s[j]
            nonzero = sum(1 for b in parity if b != 0)
            print(f"  XOR(DF0..DF3): {nonzero} bytes non-zero")
            if "CF0" in shards:
                match = bytes(parity) == shards["CF0"]
                print(f"  XOR == CF0: {match}")
            elif nonzero > 0:
                print(f"  XOR non-nul → parity XOR = {bytes(parity)[:32].hex()}")
        else:
            print(f"  Shards DF disponibles: {[k for k in shards if k.startswith('DF')]}")

    return shards


# ═══════════════════════════════════════════════════════════════════════════════
# B. Trivial check
# ═══════════════════════════════════════════════════════════════════════════════

def test_B_trivial():
    """Vérifie que STATUS=OK pour size=0 n'est PAS trivial."""
    print("\n═══ B. Trivial check ═══")
    ip, port = CS[0][1], CS[0][2]
    # Chunk_id totalement inconnu
    probe_cs(ip, port, CLTOCS_READ, struct.pack(">QIII", 0xFFFFFFFFFFFFFFFF, 1, 0, 0),
             label="bogus 0xFFFF... size=0")
    probe_cs(ip, port, CLTOCS_READ, struct.pack(">QIII", 0x0000000000000001, 1, 0, 0),
             label="chunk_id=1 size=0")
    probe_cs(ip, port, CLTOCS_READ, struct.pack(">QIII", 0, 0, 0, 0),
             label="chunk_id=0 ver=0 size=0")
    # Logical chunk_id pour référence
    probe_cs(ip, port, CLTOCS_READ, struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, 0),
             label=f"logical 0x{LOGICAL_CHUNK_ID:016X} size=0 (référence)", verbose=True)


# ═══════════════════════════════════════════════════════════════════════════════
# C. Master proto=3 raw dump
# ═══════════════════════════════════════════════════════════════════════════════

def test_C_master_raw():
    """Dump les raw bytes de la réponse MATOCL_FUSE_READ_CHUNK."""
    print("\n═══ C. Master proto=3 raw dump ═══")
    try:
        m = master_connect()
        inode = master_lookup(m, ROOT_NODE_ID, "home")
        inode = master_lookup(m, inode, "cyril")
        inode = master_lookup(m, inode, "ghostdrive_ec_test")
        inode = master_lookup(m, inode, "small_ec.bin")
        print(f"  Inode: {inode}")

        payload = struct.pack(">III", 0, inode, 0)
        write_frame(m, CLTOM_FUSE_READ_CHUNK, payload)
        cmd, ans = read_frame(m)
        while cmd == ANTOAN_NOP:
            cmd, ans = read_frame(m)
        m.close()

        print(f"  cmd={cmd} total_len={len(ans)}")
        print(f"  full hex: {ans.hex()}")

        off = 4  # skip msgid
        proto = ans[off]; off += 1
        file_length = struct.unpack_from(">Q", ans, off)[0]; off += 8
        chunk_id = struct.unpack_from(">Q", ans, off)[0]; off += 8
        version = struct.unpack_from(">I", ans, off)[0]; off += 4

        print(f"  proto={proto}")
        print(f"  file_length={file_length}")
        print(f"  chunk_id=0x{chunk_id:016X}")
        print(f"  version={version}")

        remaining = len(ans) - off
        print(f"  remaining after header: {remaining} bytes")
        print(f"  raw server entries: {ans[off:].hex()}")

        # Tester différentes tailles d'entrée
        for entry_sz in (14, 15, 16, 18, 20, 22, 26):
            if remaining % entry_sz == 0:
                n = remaining // entry_sz
                print(f"\n  → entry_size={entry_sz}: {n} entrées")
                off2 = off
                for i in range(n):
                    if off2 + entry_sz > len(ans):
                        break
                    ip_raw = struct.unpack_from(">I", ans, off2)[0]; off2 += 4
                    port   = struct.unpack_from(">H", ans, off2)[0]; off2 += 2
                    cs_ver = struct.unpack_from(">I", ans, off2)[0]; off2 += 4
                    lm     = struct.unpack_from(">I", ans, off2)[0]; off2 += 4
                    extra_bytes = ans[off2:off2 + (entry_sz - 14)]
                    off2 += (entry_sz - 14)
                    ip_str = f"{(ip_raw>>24)&0xFF}.{(ip_raw>>16)&0xFF}.{(ip_raw>>8)&0xFF}.{ip_raw&0xFF}"
                    extra = f" extra={extra_bytes.hex()}" if extra_bytes else ""
                    print(f"    [{i}] {ip_str}:{port} cs_ver=0x{cs_ver:08X} lm=0x{lm:08X}{extra}")

    except Exception as e:
        print(f"  ERR: {e}")
        import traceback; traceback.print_exc()


# ═══════════════════════════════════════════════════════════════════════════════
# D. version=0 pour les physical chunk_ids
# ═══════════════════════════════════════════════════════════════════════════════

def test_D_version_zero():
    """Teste version=0 avec les physical EC chunk_ids."""
    print("\n═══ D. version=0 avec physical EC chunk_ids ═══")
    for role, ip, port, part_idx in CS:
        phys = ec4_physical_id(LOGICAL_CHUNK_ID, part_idx)
        for ver in (0, 1):
            probe_cs(ip, port, CLTOCS_READ,
                     struct.pack(">QIII", phys, ver, 0, SMALL_READ),
                     label=f"{role}(part {part_idx}) phys=0x{phys:016X} ver={ver} size=64K")


# ═══════════════════════════════════════════════════════════════════════════════
# E. Hello on connect
# ═══════════════════════════════════════════════════════════════════════════════

def test_E_hello():
    """Vérifie si le CS envoie quelque chose à la connexion."""
    print("\n═══ E. Hello on connect ═══")
    for role, ip, port, _ in CS:
        try:
            with socket.create_connection((ip, port), timeout=3) as cs:
                cs.settimeout(2)
                try:
                    hdr = _recv_exact(cs, 8)
                    cmd, length = struct.unpack(">II", hdr)
                    data = _recv_exact(cs, length) if length > 0 else b""
                    print(f"  {role}: GREETING cmd={cmd} len={length} hex={data[:32].hex()}")
                except socket.timeout:
                    print(f"  {role}: pas de greeting (timeout 2s)")
                except EOFError:
                    print(f"  {role}: connexion fermée immédiatement")
        except Exception as e:
            print(f"  {role}: ERR {e}")


# ═══════════════════════════════════════════════════════════════════════════════
# F. Opcode scan 350-600
# ═══════════════════════════════════════════════════════════════════════════════

def test_F_opcode_scan():
    """Scan opcodes 350-600 avec payload size=0 et size=64K."""
    print("\n═══ F. Opcode scan 350-600 ═══")
    ip, port = CS[0][1], CS[0][2]  # DF0
    phys_df0 = ec4_physical_id(LOGICAL_CHUNK_ID, 0)
    payload_z = struct.pack(">QIII", phys_df0, LOGICAL_VERSION, 0, 0)
    payload_r = struct.pack(">QIII", phys_df0, LOGICAL_VERSION, 0, SMALL_READ)

    hits = []
    for opcode in range(350, 601):
        for payload, desc in [(payload_z, "sz0"), (payload_r, "sz64K")]:
            r = probe_cs(ip, port, opcode, payload, timeout=1, label="", verbose=False)
            if r not in ("RESET", "EOF", "NO_RESP", "TIMEOUT") and not r.startswith("ERR"):
                hits.append((opcode, desc, r))
                print(f"  *** opcode={opcode} {desc} → {r}")

    if not hits:
        print("  Aucun opcode 350-600 ne répond.")


# ═══════════════════════════════════════════════════════════════════════════════
# G. STATUS=OK raw bytes pour size=0 avec logical chunk_id
# ═══════════════════════════════════════════════════════════════════════════════

def test_G_status_ok_raw():
    """Capture les raw bytes complets de STATUS=OK avec logical chunk_id size=0."""
    print("\n═══ G. STATUS=OK raw bytes (logical chunk_id, size=0) ═══")
    for role, ip, port, _ in CS:
        try:
            with socket.create_connection((ip, port), timeout=5) as cs:
                cs.settimeout(5)
                payload = struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, 0)
                write_frame(cs, CLTOCS_READ, payload)
                frames = []
                for _ in range(5):
                    try:
                        cmd, data = read_frame(cs)
                        frames.append((cmd, data))
                        if cmd == CSTOCL_READ_STATUS:
                            break
                    except (socket.timeout, EOFError):
                        break
                for cmd, data in frames:
                    s = ""
                    if cmd == CSTOCL_READ_STATUS and len(data) >= 9:
                        s = f" status={status_name(data[8])}"
                    print(f"  {role}: cmd={cmd} len={len(data)}{s} hex={data.hex()}")
                if not frames:
                    print(f"  {role}: NO RESPONSE")
        except Exception as e:
            print(f"  {role}: ERR {e}")


# ═══════════════════════════════════════════════════════════════════════════════
# H. EC8 chunk_ids (au cas où le serveur Pro utilise EC8 différemment)
# ═══════════════════════════════════════════════════════════════════════════════

def test_H_ec8_ids():
    """Teste les physical chunk_ids EC8 au cas où le Pro utilise EC8+2."""
    print("\n═══ H. EC8 physical chunk_ids (au cas où) ═══")
    ip, port = CS[0][1], CS[0][2]
    for part_idx in range(4):
        phys = ec8_physical_id(LOGICAL_CHUNK_ID, part_idx)
        probe_cs(ip, port, CLTOCS_READ,
                 struct.pack(">QIII", phys, LOGICAL_VERSION, 0, SMALL_READ),
                 label=f"DF0 EC8 part={part_idx} phys=0x{phys:016X} size=64K")
        probe_cs(ip, port, CLTOCS_READ,
                 struct.pack(">QIII", phys, LOGICAL_VERSION, 0, 0),
                 label=f"DF0 EC8 part={part_idx} phys=0x{phys:016X} size=0")


# ═══════════════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════════════

def main():
    print("=" * 70)
    print("  MooseFS EC Probe Phase 3 — Physical chunk_id derivation")
    print(f"  Logical chunk_id : 0x{LOGICAL_CHUNK_ID:016X}")
    print(f"  EC4 ecidstart    : 0x{EC4_ECID_START:016X}")
    print(f"  EC4 ecid_step    : 0x{EC4_ECID_STEP:016X}")
    print()
    print("  Physical chunk_ids attendus:")
    for role, ip, port, part_idx in CS:
        phys = ec4_physical_id(LOGICAL_CHUNK_ID, part_idx)
        print(f"    {role}(part {part_idx}): 0x{phys:016X} @ {ip}:{port}")
    print("=" * 70)

    # Test principal : physical EC chunk_ids
    shards = test_A_physical_ec_ids()

    # Tests complémentaires
    test_B_trivial()
    test_G_status_ok_raw()
    test_E_hello()
    test_D_version_zero()
    test_C_master_raw()
    test_H_ec8_ids()
    test_F_opcode_scan()

    print("\n" + "=" * 70)
    print("  Probe phase 3 terminé")
    if shards:
        print(f"  Shards lus avec succès: {list(shards.keys())}")
    else:
        print("  Aucun shard lu — hypothèse à réviser")
    print("=" * 70)


if __name__ == "__main__":
    main()
