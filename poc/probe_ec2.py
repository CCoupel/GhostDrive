#!/usr/bin/env python3
"""
poc/probe_ec2.py — Diagnostic phase 2 : protocole EC MooseFS Pro.

Insights de probe_ec.py :
- size=0 → STATUS=OK  : le chunk EXISTE sur le CS à chunk_id=0x012EB689
- size>0 → NOCHUNK    : opcode 200 refuse de lire les données EC
- 22-byte payload     : CS ferme la connexion (validation taille stricte)
- Aucun opcode 201-260 ne répond avec payload vide

Hypothèses à tester ici :
1. Raw hex de la réponse SIZE=0 (données cachées dans la réponse OK ?)
2. Protocole stateful : SIZE=0 d'abord, puis lire avec un autre opcode
3. Scan d'opcodes avec payload de 20 octets (pas vide)
4. Scan d'opcodes 260-500
5. Scan sur port CS alternatif (9424, 9425)
"""

import socket, struct, sys, time

CLTOCS_READ = 200
CSTOCL_READ_STATUS = 201
CSTOCL_READ_DATA = 202

LOGICAL_CHUNK_ID = 0x00000000012EB689
LOGICAL_VERSION = 1
SMALL_READ = 65536
SHARD_SIZE = 8 * 1024 * 1024

CS_DF0 = ("192.168.2.218", 9423)


def _recv_exact(sock, n):
    buf = bytearray()
    while len(buf) < n:
        c = sock.recv(n - len(buf))
        if not c:
            raise EOFError(f"closed (got {len(buf)}/{n})")
        buf.extend(c)
    return bytes(buf)


def write_frame(sock, cmd, payload):
    sock.sendall(struct.pack(">II", cmd, len(payload)) + payload)


def read_frame(sock):
    hdr = _recv_exact(sock, 8)
    cmd, length = struct.unpack(">II", hdr)
    data = _recv_exact(sock, length) if length > 0 else b""
    return cmd, data


# ---------------------------------------------------------------------------
# Test 1 : Raw hex de la réponse STATUS=OK pour size=0
# ---------------------------------------------------------------------------
def test_size0_raw():
    print("\n--- Test 1: Raw hex de SIZE=0 STATUS=OK ---")
    ip, port = CS_DF0
    try:
        with socket.create_connection((ip, port), timeout=5) as cs:
            cs.settimeout(5)
            payload = struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, 0)
            write_frame(cs, CLTOCS_READ, payload)
            cmd, data = read_frame(cs)
            print(f"  cmd={cmd}  len={len(data)}")
            print(f"  hex: {data.hex()}")
            if len(data) > 9:
                print(f"  → réponse plus longue que 9 bytes — données supplémentaires !")
    except Exception as e:
        print(f"  ERR: {e}")


# ---------------------------------------------------------------------------
# Test 2 : Stateful — SIZE=0 sur connexion X, puis opcode Y sur même connexion
# ---------------------------------------------------------------------------
def test_stateful(opcodes_to_try):
    print(f"\n--- Test 2: Stateful — SIZE=0 puis opcodes {opcodes_to_try[0]}-{opcodes_to_try[-1]} sur même connexion ---")
    ip, port = CS_DF0
    payload_read = struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, SMALL_READ)
    payload_check = struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, 0)

    for opcode in opcodes_to_try:
        try:
            with socket.create_connection((ip, port), timeout=3) as cs:
                cs.settimeout(3)
                # Phase 1 : existence check (size=0)
                write_frame(cs, CLTOCS_READ, payload_check)
                cmd0, data0 = read_frame(cs)
                if cmd0 != CSTOCL_READ_STATUS or (data0[8] if len(data0) >= 9 else 0xFF) != 0:
                    print(f"  opcode={opcode}: SIZE=0 check failed (cmd={cmd0})")
                    continue
                # Phase 2 : essayer un autre opcode sur la même connexion
                write_frame(cs, opcode, payload_read)
                try:
                    cmd2, data2 = read_frame(cs)
                    if cmd2 == CSTOCL_READ_STATUS:
                        status = data2[8] if len(data2) >= 9 else 0xFF
                        if status == 0:
                            print(f"  opcode={opcode}: *** STATUS=OK après SIZE=0 *** (stateful !) data={data2.hex()}")
                        else:
                            pass  # NOCHUNK/etc. - pas intéressant
                    elif cmd2 == CSTOCL_READ_DATA:
                        print(f"  opcode={opcode}: *** DATA après SIZE=0 *** len={len(data2)} ← TROUVÉ !")
                    else:
                        print(f"  opcode={opcode}: cmd={cmd2} len={len(data2)}  ← INATTENDU")
                except (EOFError, socket.timeout):
                    pass
        except Exception:
            pass


# ---------------------------------------------------------------------------
# Test 3 : Scan d'opcodes avec payload 20 bytes (lecture standard, size=0)
# ---------------------------------------------------------------------------
def test_opcode_scan_with_payload(start, end):
    print(f"\n--- Test 3: Scan opcodes {start}-{end} avec payload 20b (size=0) ---")
    ip, port = CS_DF0
    payload = struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, 0)
    for opcode in range(start, end + 1):
        if opcode == 200:
            continue
        try:
            with socket.create_connection((ip, port), timeout=1) as cs:
                cs.settimeout(1)
                write_frame(cs, opcode, payload)
                try:
                    cmd, data = read_frame(cs)
                    status_str = ""
                    if cmd == CSTOCL_READ_STATUS and len(data) >= 9:
                        status_str = f"status=0x{data[8]:02x}"
                    print(f"  opcode={opcode:<5d} cmd={cmd:<6d} len={len(data):<6d} {status_str}  ← RÉPONSE")
                except socket.timeout:
                    pass  # pas de réponse = opcode non reconnu
        except (ConnectionResetError, EOFError):
            pass  # CS a fermé = opcode invalide ou erreur
        except Exception:
            pass


# ---------------------------------------------------------------------------
# Test 4 : Port alternatifs (9424, 9425, 9426)
# ---------------------------------------------------------------------------
def test_alt_ports():
    print("\n--- Test 4: Ports alternatifs sur CS 218 ---")
    ip = CS_DF0[0]
    for port in (9424, 9425, 9426, 9430, 9450):
        payload = struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, 0)
        try:
            with socket.create_connection((ip, port), timeout=2) as cs:
                cs.settimeout(2)
                write_frame(cs, CLTOCS_READ, payload)
                cmd, data = read_frame(cs)
                status = data[8] if cmd == CSTOCL_READ_STATUS and len(data) >= 9 else 0xFF
                print(f"  port={port}: cmd={cmd} status=0x{status:02x}")
        except (ConnectionRefusedError, TimeoutError, OSError):
            pass  # port fermé
        except Exception as e:
            print(f"  port={port}: ERR {e}")


# ---------------------------------------------------------------------------
# Test 5 : Tenter de lire avec version=labelmask sur port alternatif DF1 (9424)
# ---------------------------------------------------------------------------
def test_df1_port_9424():
    print("\n--- Test 5: DF1 sur port 9424 — lecture avec diverses variantes ---")
    ip, port = "192.168.2.216", 9424
    cid = LOGICAL_CHUNK_ID
    for ver in (1, LOGICAL_VERSION, 0x00000004, 0x00010001):
        try:
            with socket.create_connection((ip, port), timeout=3) as cs:
                cs.settimeout(3)
                # size=0
                write_frame(cs, CLTOCS_READ, struct.pack(">QIII", cid, ver, 0, 0))
                cmd, data = read_frame(cs)
                s = data[8] if len(data) >= 9 else 0xFF
                print(f"  DF1 ver=0x{ver:08X} size=0 → status=0x{s:02x}")
                # size=SMALL si OK
                if s == 0:
                    with socket.create_connection((ip, port), timeout=3) as cs2:
                        cs2.settimeout(3)
                        write_frame(cs2, CLTOCS_READ, struct.pack(">QIII", cid, ver, 0, SMALL_READ))
                        cmd2, data2 = read_frame(cs2)
                        s2 = data2[8] if cmd2 == CSTOCL_READ_STATUS and len(data2) >= 9 else 0xFF
                        print(f"  DF1 ver=0x{ver:08X} size=64K → status=0x{s2:02x}  ← {'DATA !' if cmd2 == CSTOCL_READ_DATA else ''}")
        except Exception as e:
            print(f"  DF1 ver=0x{ver:08X} ERR: {e}")


# ---------------------------------------------------------------------------
# Test 6 : Connexion brute — envoyer le payload "standard" puis attendre tout
# ---------------------------------------------------------------------------
def test_raw_full_read():
    print("\n--- Test 6: Lecture brute size=SHARD avec réponse complète ---")
    ip, port = CS_DF0
    try:
        with socket.create_connection((ip, port), timeout=30) as cs:
            cs.settimeout(30)
            # Envoyer une requête taille réelle du shard
            write_frame(cs, CLTOCS_READ, struct.pack(">QIII", LOGICAL_CHUNK_ID, LOGICAL_VERSION, 0, SHARD_SIZE))
            for i in range(5):
                try:
                    cmd, data = read_frame(cs)
                    if cmd == CSTOCL_READ_STATUS:
                        print(f"  frame {i}: STATUS cmd={cmd} len={len(data)} hex={data.hex()}")
                        break
                    elif cmd == CSTOCL_READ_DATA:
                        print(f"  frame {i}: DATA cmd={cmd} len={len(data)}")
                    else:
                        print(f"  frame {i}: OTHER cmd={cmd} len={len(data)} hex={data[:32].hex()}")
                except socket.timeout:
                    print(f"  frame {i}: TIMEOUT")
                    break
    except Exception as e:
        print(f"  ERR: {e}")


# ---------------------------------------------------------------------------
# Test 7 : Identifier si la réponse NOCHUNK contient des infos sur les physical IDs
# ---------------------------------------------------------------------------
def test_nochunk_response_hex():
    print("\n--- Test 7: Hex complet des réponses NOCHUNK ---")
    ip, port = CS_DF0
    for desc, ver, size in [
        ("std v=1 size=64K", LOGICAL_VERSION, SMALL_READ),
        ("std v=1 size=8MB", LOGICAL_VERSION, SHARD_SIZE),
    ]:
        try:
            with socket.create_connection((ip, port), timeout=5) as cs:
                cs.settimeout(5)
                write_frame(cs, CLTOCS_READ, struct.pack(">QIII", LOGICAL_CHUNK_ID, ver, 0, size))
                cmd, data = read_frame(cs)
                print(f"  {desc}: cmd={cmd} len={len(data)} hex={data.hex()}")
        except Exception as e:
            print(f"  {desc}: ERR {e}")


def main():
    print("=== MooseFS EC Probe Phase 2 ===\n")

    test_size0_raw()
    test_nochunk_response_hex()
    test_raw_full_read()
    test_df1_port_9424()
    test_alt_ports()
    test_opcode_scan_with_payload(201, 260)
    test_opcode_scan_with_payload(261, 350)
    test_stateful(list(range(201, 260)))


if __name__ == "__main__":
    main()
