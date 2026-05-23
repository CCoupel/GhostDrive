#!/usr/bin/env python3
"""
poc/probe_ec.py — Diagnostic: probe MooseFS CS protocol for EC chunk reads.

Hypotheses to test:
1. Standard 20-byte CLTOCS_READ with size=0 → does the chunk exist at all?
2. EC type encoded in version field upper bits
3. 21-byte payload with ec_type appended
4. 21-byte payload with proto byte prepended (like WRITE proto=1)
5. 22-byte [proto:8][chunkId:64][ver:32][ec_type:8][off:32][size:32]
6. version=labelmask (labelmask might encode CS-side chunk type)
7. Opcode scan 200-260 for EC-specific opcodes
8. Physical chunk IDs from previous scan — full shard read + XOR check

READ ONLY — accès MooseFS lecture seule uniquement.
"""

import socket
import struct
import sys

# ---------------------------------------------------------------------------
# Constantes
# ---------------------------------------------------------------------------

CLTOCS_READ = 200
CSTOCL_READ_STATUS = 201
CSTOCL_READ_DATA = 202

# Proto=3 master response pour small_ec.bin chunk 0
LOGICAL_CHUNK_ID = 0x00000000012EB689
LOGICAL_VERSION  = 1
# labelmask par serveur (issu de la réponse master)
LABELMASK_DF0 = 0x00040004  # CS 218
LABELMASK_DF1 = 0x00000004  # CS 216
LABELMASK_DF2 = 0x00040004  # CS 217
LABELMASK_DF3 = 0x00040004  # CS 211

SHARD_SIZE  = 8 * 1024 * 1024   # 8 MiB (= 32 MiB / 4)
SMALL_READ  = 65536              # 64 KiB pour les sondes

# EC servers (from master proto=3 response)
CS = {
    "DF0": ("192.168.2.218", 9423, LABELMASK_DF0),
    "DF1": ("192.168.2.216", 9424, LABELMASK_DF1),
    "DF2": ("192.168.2.217", 9423, LABELMASK_DF2),
    "DF3": ("192.168.2.211", 9423, LABELMASK_DF3),
    "CF0": ("192.168.2.100", 9423, 0),
}

# Physical chunk IDs found in previous ±25 scan
PHYSICAL_IDS = {
    "DF0": ("192.168.2.218", 9423, 0x012EB67F),
    "DF1": ("192.168.2.216", 9424, 0x012EB692),
    "DF2": ("192.168.2.217", 9423, 0x012EB691),
    "DF3": ("192.168.2.211", 9423, 0x012EB691),
    "CF0": ("192.168.2.100", 9423, 0x012EB67F),
}


# ---------------------------------------------------------------------------
# Primitives réseau
# ---------------------------------------------------------------------------

def _recv_exact(sock, n):
    buf = bytearray()
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise EOFError(f"Connection closed (expected {n}, got {len(buf)})")
        buf.extend(chunk)
    return bytes(buf)


def write_frame(sock, cmd, payload):
    sock.sendall(struct.pack(">II", cmd, len(payload)) + payload)


def read_frame(sock):
    hdr = _recv_exact(sock, 8)
    cmd, length = struct.unpack(">II", hdr)
    data = _recv_exact(sock, length) if length > 0 else b""
    return cmd, data


# ---------------------------------------------------------------------------
# Sonde CLTOCS_READ générique
# ---------------------------------------------------------------------------

def probe(ip, port, opcode, payload, timeout=3, label="", print_result=True):
    """
    Envoie opcode+payload au CS, retourne une description de la réponse.
    Ne lève aucune exception — capture et retourne les erreurs.
    """
    try:
        with socket.create_connection((ip, port), timeout=timeout) as cs:
            cs.settimeout(timeout)
            write_frame(cs, opcode, payload)
            frames = []
            for _ in range(4):
                try:
                    cmd, data = read_frame(cs)
                    if cmd == CSTOCL_READ_STATUS:
                        status = data[8] if len(data) >= 9 else 0xFF
                        frames.append(f"STATUS=0x{status:02x}({_status_name(status)})")
                        break
                    elif cmd == CSTOCL_READ_DATA:
                        block_size = struct.unpack_from(">I", data, 12)[0] if len(data) >= 16 else 0
                        frames.append(f"DATA({block_size}b)")
                    elif cmd == 0:
                        frames.append("NOP")
                    else:
                        frames.append(f"CMD={cmd}({len(data)}b)")
                except socket.timeout:
                    frames.append("TIMEOUT")
                    break
            result = " + ".join(frames) if frames else "NO_RESPONSE"
    except ConnectionResetError:
        result = "RESET"
    except ConnectionRefusedError:
        result = "REFUSED"
    except Exception as e:
        result = f"ERR:{type(e).__name__}:{e}"

    if print_result:
        print(f"  {label:<65s} → {result}")
    return result


def _status_name(s):
    return {0: "OK", 0x0d: "NOCHUNK", 0x03: "ENOENT", 0x01: "EPERM"}.get(s, "?")


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

def test_standard(ip, port, cid, ver):
    print("\n--- Test 1: Standard 20-byte CLTOCS_READ ---")
    probe(ip, port, CLTOCS_READ, struct.pack(">QIII", cid, ver, 0, 0),
          label=f"size=0 (chunk existence check)")
    probe(ip, port, CLTOCS_READ, struct.pack(">QIII", cid, ver, 0, SMALL_READ),
          label=f"size=64K (standard)")
    probe(ip, port, CLTOCS_READ, struct.pack(">QIII", cid, ver, 0, SHARD_SIZE),
          label=f"size=8MB (full shard)")


def test_version_encoding(ip, port, cid):
    print("\n--- Test 2: Version field encodings (type<<16)|ver ---")
    for vtype in range(8):
        v = (vtype << 16) | LOGICAL_VERSION
        probe(ip, port, CLTOCS_READ, struct.pack(">QIII", cid, v, 0, SMALL_READ),
              label=f"ver=(type={vtype}<<16)|1 = 0x{v:08X}")
    # labelmask comme version
    for name, (_, _, lm) in [("DF0", ("192.168.2.218", 9423, LABELMASK_DF0))]:
        probe(ip, port, CLTOCS_READ, struct.pack(">QIII", cid, lm, 0, SMALL_READ),
              label=f"ver=labelmask=0x{lm:08X}")


def test_21byte_appended(ip, port, cid, ver):
    """[chunkId:64][ver:32][off:32][size:32][ec_type:8] = 21 bytes"""
    print("\n--- Test 3: 21-byte, ec_type APPENDED [cid][ver][off][size][ec_type] ---")
    for ec_type in range(8):
        pl = struct.pack(">QIII", cid, ver, 0, SMALL_READ) + struct.pack(">B", ec_type)
        probe(ip, port, CLTOCS_READ, pl, label=f"ec_type={ec_type} appended")


def test_21byte_prepended(ip, port, cid, ver):
    """[proto:8][chunkId:64][ver:32][off:32][size:32] = 21 bytes"""
    print("\n--- Test 4: 21-byte, proto PREPENDED [proto][cid][ver][off][size] ---")
    for proto in range(1, 8):
        pl = struct.pack(">B", proto) + struct.pack(">QIII", cid, ver, 0, SMALL_READ)
        probe(ip, port, CLTOCS_READ, pl, label=f"proto={proto} prepended")


def test_22byte(ip, port, cid, ver):
    """[proto:8][chunkId:64][ver:32][ec_type:8][off:32][size:32] = 22 bytes"""
    print("\n--- Test 5: 22-byte [proto][cid][ver][ec_type][off][size] ---")
    for proto in range(1, 4):
        for ec_type in range(8):
            pl = (struct.pack(">B", proto) + struct.pack(">Q", cid) +
                  struct.pack(">I", ver) + struct.pack(">B", ec_type) +
                  struct.pack(">II", 0, SMALL_READ))
            probe(ip, port, CLTOCS_READ, pl, label=f"proto={proto} ec_type={ec_type}")


def test_21byte_type_after_ver(ip, port, cid, ver):
    """[chunkId:64][ver:32][ec_type:8][off:32][size:32] = 21 bytes"""
    print("\n--- Test 6: 21-byte [cid][ver][ec_type][off][size] ---")
    for ec_type in range(8):
        pl = (struct.pack(">Q", cid) + struct.pack(">I", ver) +
              struct.pack(">B", ec_type) + struct.pack(">II", 0, SMALL_READ))
        probe(ip, port, CLTOCS_READ, pl, label=f"ec_type={ec_type} after ver")


def test_opcode_scan(ip, port):
    print("\n--- Test 7: Opcode scan 200-260 (empty payload) ---")
    for opcode in range(200, 261):
        if opcode in (200, 201, 202):
            continue  # known opcodes
        r = probe(ip, port, opcode, b"", timeout=1, label=f"opcode={opcode}", print_result=False)
        if not r.startswith("RESET") and r != "NO_RESPONSE" and not r.startswith("ERR"):
            print(f"  opcode={opcode:<6d} → {r}  *** RESPONSE ***")
        # Only print unexpected responses


def test_physical_ids():
    print("\n--- Test 8: Physical chunk IDs (from previous scan) — full shard read ---")
    shards = {}
    ok_count = 0
    for role, (phys_ip, phys_port, phys_cid) in PHYSICAL_IDS.items():
        pl = struct.pack(">QIII", phys_cid, 1, 0, SHARD_SIZE)
        label = f"{role} phys_cid=0x{phys_cid:08X} size=8MB"
        r = probe(phys_ip, phys_port, CLTOCS_READ, pl, timeout=15, label=label, print_result=True)

        if "DATA" in r:
            # Read actual data
            try:
                with socket.create_connection((phys_ip, phys_port), timeout=15) as cs:
                    cs.settimeout(15)
                    write_frame(cs, CLTOCS_READ, pl)
                    shard_data = bytearray()
                    for _ in range(200):
                        cmd, data = read_frame(cs)
                        if cmd == CSTOCL_READ_DATA:
                            block_size = struct.unpack_from(">I", data, 12)[0]
                            shard_data.extend(data[20:20 + block_size])
                        elif cmd == CSTOCL_READ_STATUS:
                            status = data[8]
                            if status == 0:
                                shards[role] = bytes(shard_data)
                                print(f"    → Read {len(shards[role])} bytes OK")
                                ok_count += 1
                            else:
                                print(f"    → STATUS=0x{status:02x}")
                            break
            except Exception as e:
                print(f"    → Read error: {e}")

    if ok_count >= 4 and all(r in shards for r in ("DF0", "DF1", "DF2", "DF3")):
        print("\n--- Test 8b: XOR verification of physical shards ---")
        sizes = {r: len(shards[r]) for r in ("DF0", "DF1", "DF2", "DF3")}
        print(f"  Shard sizes: {sizes}")
        if len(set(sizes.values())) == 1:
            n = sizes["DF0"]
            xor_result = bytearray(n)
            for r in ("DF0", "DF1", "DF2", "DF3"):
                for i in range(n):
                    xor_result[i] ^= shards[r][i]
            nonzero = sum(1 for b in xor_result if b != 0)
            print(f"  XOR(DF0,DF1,DF2,DF3): {nonzero} non-zero bytes")
            if nonzero == 0:
                print("  XOR = 0x00...00 (all zeros) — these may NOT be EC parts (or trivial data)")
            else:
                print(f"  XOR ≠ 0 → parity bytes; first 32: {xor_result[:32].hex()}")
            if "CF0" in shards:
                match = xor_result == bytearray(shards["CF0"])
                print(f"  XOR == CF0: {match}")
        else:
            print("  Shard sizes differ — cannot XOR")


def test_all_servers_with_logical_id():
    """Teste le logical chunk_id sur TOUS les servers EC (pas seulement DF0)."""
    print("\n--- Test 9: Logical chunk_id sur tous les servers EC ---")
    for role, (ip, port, lm) in CS.items():
        probe(ip, port, CLTOCS_READ,
              struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, SMALL_READ),
              label=f"{role} ({ip}:{port}) std read")

    print("\n  Avec labelmask comme version:")
    for role, (ip, port, lm) in CS.items():
        probe(ip, port, CLTOCS_READ,
              struct.pack(">QIII", LOGICAL_CHUNK_ID, lm, 0, SMALL_READ),
              label=f"{role} ver=labelmask=0x{lm:08X}")


# ---------------------------------------------------------------------------
# Point d'entrée
# ---------------------------------------------------------------------------

def main():
    # CS principal pour les tests ciblés (DF0)
    ip, port = CS["DF0"][:2]
    cid = LOGICAL_CHUNK_ID
    ver = LOGICAL_VERSION

    print(f"=== MooseFS EC Probe — chunk_id=0x{cid:016X} v={ver} ===")
    print(f"=== Primary CS: DF0 @ {ip}:{port} ===\n")

    test_standard(ip, port, cid, ver)
    test_version_encoding(ip, port, cid)
    test_21byte_appended(ip, port, cid, ver)
    test_21byte_prepended(ip, port, cid, ver)
    test_22byte(ip, port, cid, ver)
    test_21byte_type_after_ver(ip, port, cid, ver)
    test_opcode_scan(ip, port)
    test_all_servers_with_logical_id()
    test_physical_ids()

    print("\n=== Probe terminé ===")


if __name__ == "__main__":
    main()
