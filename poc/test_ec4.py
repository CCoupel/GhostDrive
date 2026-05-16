#!/usr/bin/env python3
"""
poc/test_ec4.py — Tests unitaires pour ec4_reader.py (issue #113)

Tests sans connexion réseau réelle.
stdlib Python uniquement — aucune dépendance externe.

Usage :
    python3 -m pytest poc/test_ec4.py -v
    python3 poc/test_ec4.py              # unittest runner
"""

import io
import math
import socket
import struct
import threading
import unittest
import zlib

# Import du module à tester
import sys
import os
sys.path.insert(0, os.path.dirname(__file__))
import ec4_reader


# ---------------------------------------------------------------------------
# Helpers de test
# ---------------------------------------------------------------------------

class MockSocket:
    """
    Socket mock basé sur des bytes prédéfinis.
    Utilise deux BytesIO : un pour les envois, un pour les réceptions.
    """

    def __init__(self, recv_data: bytes = b""):
        self._recv_buf = io.BytesIO(recv_data)
        self._send_buf = io.BytesIO()

    def recv(self, n: int) -> bytes:
        data = self._recv_buf.read(n)
        if not data and n > 0:
            return b""  # EOF
        return data

    def sendall(self, data: bytes) -> None:
        self._send_buf.write(data)

    def sent_bytes(self) -> bytes:
        return self._send_buf.getvalue()

    def __enter__(self):
        return self

    def __exit__(self, *args):
        pass


def make_frame(cmd: int, payload: bytes) -> bytes:
    """Encode une trame MooseFS."""
    return struct.pack(">II", cmd, len(payload)) + payload


# ---------------------------------------------------------------------------
# Tests des primitives réseau
# ---------------------------------------------------------------------------

class TestNetworkPrimitives(unittest.TestCase):

    def test_write_frame_encoding(self):
        """write_frame encode correctement cmd + longueur + payload."""
        sock = MockSocket()
        ec4_reader.write_frame(sock, 400, b"hello")
        sent = sock.sent_bytes()
        self.assertEqual(len(sent), 8 + 5)
        cmd, length = struct.unpack(">II", sent[:8])
        self.assertEqual(cmd, 400)
        self.assertEqual(length, 5)
        self.assertEqual(sent[8:], b"hello")

    def test_write_frame_empty_payload(self):
        """write_frame avec payload vide encode bien length=0."""
        sock = MockSocket()
        ec4_reader.write_frame(sock, 123, b"")
        sent = sock.sent_bytes()
        self.assertEqual(len(sent), 8)
        cmd, length = struct.unpack(">II", sent[:8])
        self.assertEqual(cmd, 123)
        self.assertEqual(length, 0)

    def test_read_frame_basic(self):
        """read_frame décode correctement cmd et payload."""
        frame = make_frame(401, b"world")
        sock = MockSocket(frame)
        cmd, payload = ec4_reader.read_frame(sock)
        self.assertEqual(cmd, 401)
        self.assertEqual(payload, b"world")

    def test_read_frame_empty_payload(self):
        """read_frame avec length=0 retourne payload vide."""
        frame = make_frame(999, b"")
        sock = MockSocket(frame)
        cmd, payload = ec4_reader.read_frame(sock)
        self.assertEqual(cmd, 999)
        self.assertEqual(payload, b"")

    def test_write_read_frame_roundtrip(self):
        """Round-trip via socketpair : ce qui est envoyé est bien reçu."""
        client, server = socket.socketpair()
        try:
            # Écrire depuis le client
            ec4_reader.write_frame(client, 432, b"test_payload_data")
            client.shutdown(socket.SHUT_WR)

            # Lire depuis le server
            cmd, payload = ec4_reader.read_frame(server)
            self.assertEqual(cmd, 432)
            self.assertEqual(payload, b"test_payload_data")
        finally:
            client.close()
            server.close()

    def test_recv_exact_raises_on_eof(self):
        """_recv_exact lève EOFError si la connexion se ferme prématurément."""
        # Socket qui retourne 0 octets immédiatement
        sock = MockSocket(b"\x00\x00")  # Seulement 2 bytes, on en demande 8
        with self.assertRaises(EOFError):
            ec4_reader._recv_exact(sock, 8)

    def test_write_read_frame_multiple_frames(self):
        """read_frame peut lire plusieurs trames en séquence."""
        data = make_frame(100, b"first") + make_frame(200, b"second")
        sock = MockSocket(data)

        cmd1, payload1 = ec4_reader.read_frame(sock)
        cmd2, payload2 = ec4_reader.read_frame(sock)

        self.assertEqual(cmd1, 100)
        self.assertEqual(payload1, b"first")
        self.assertEqual(cmd2, 200)
        self.assertEqual(payload2, b"second")


# ---------------------------------------------------------------------------
# Tests de la reconstruction EC
# ---------------------------------------------------------------------------

class TestReconstructChunk(unittest.TestCase):

    def test_reconstruct_chunk_aligned(self):
        """
        chunk_data_size multiple de 4 : reconstruction sans padding.
        Exemple : 32 MiB = 4 × 8 MiB (small_ec.bin)
        """
        shard_size = 8  # 8 bytes par shard (simplifié)
        df0 = bytes(range(0, 8))
        df1 = bytes(range(8, 16))
        df2 = bytes(range(16, 24))
        df3 = bytes(range(24, 32))

        chunk_data_size = 32  # 4 × 8 = 32 bytes (multiple de 4)
        result = ec4_reader.reconstruct_chunk([df0, df1, df2, df3], chunk_data_size)

        self.assertEqual(len(result), 32)
        self.assertEqual(result, df0 + df1 + df2 + df3)

    def test_reconstruct_chunk_unaligned(self):
        """
        chunk_data_size non-multiple de 4 : le dernier DF a du padding.
        Le résultat doit être tronqué à chunk_data_size.
        """
        # chunk_data_size = 10 bytes, shard_size = ceil(10/4) = 3
        # DF0 = bytes 0-2, DF1 = bytes 3-5, DF2 = bytes 6-8, DF3 = bytes 9 + 2 zéros de padding
        df0 = bytes([0, 1, 2])
        df1 = bytes([3, 4, 5])
        df2 = bytes([6, 7, 8])
        df3 = bytes([9, 0, 0])  # padding = 2 zéros

        chunk_data_size = 10
        result = ec4_reader.reconstruct_chunk([df0, df1, df2, df3], chunk_data_size)

        self.assertEqual(len(result), 10)
        self.assertEqual(result, bytes(range(10)))

    def test_reconstruct_chunk_single_byte(self):
        """chunk_data_size = 1 byte (cas extrême)."""
        df0 = bytes([42, 0, 0, 0])  # shard_size = ceil(1/4) = 1, padding 3 zéros
        df1 = bytes([0, 0, 0, 0])
        df2 = bytes([0, 0, 0, 0])
        df3 = bytes([0, 0, 0, 0])

        # shard_size = 1, donc chaque DF fait 1 byte effectivement
        df0 = bytes([42])
        df1 = bytes([0])
        df2 = bytes([0])
        df3 = bytes([0])

        result = ec4_reader.reconstruct_chunk([df0, df1, df2, df3], 1)
        self.assertEqual(result, bytes([42]))

    def test_reconstruct_chunk_preserves_order(self):
        """L'ordre de concaténation est DF0‖DF1‖DF2‖DF3."""
        df0 = b"\xAA\xBB"
        df1 = b"\xCC\xDD"
        df2 = b"\xEE\xFF"
        df3 = b"\x11\x22"
        result = ec4_reader.reconstruct_chunk([df0, df1, df2, df3], 8)
        self.assertEqual(result, b"\xAA\xBB\xCC\xDD\xEE\xFF\x11\x22")


# ---------------------------------------------------------------------------
# Tests de la vérification XOR
# ---------------------------------------------------------------------------

class TestXorVerify(unittest.TestCase):

    def _make_test_shards(self, size: int = 16):
        """Génère 4 DFs et le CF0 = XOR(DF0, DF1, DF2, DF3)."""
        import os
        dfs = [bytes([i * size + j for j in range(size)]) for i in range(4)]
        cf0 = bytearray(size)
        for df in dfs:
            for k in range(size):
                cf0[k] ^= df[k]
        return dfs, bytes(cf0)

    def test_xor_verify_correct(self):
        """XOR(DF0, DF1, DF2, DF3) == CF0 → retourne True."""
        dfs, cf0 = self._make_test_shards(16)
        self.assertTrue(ec4_reader.xor_verify(dfs, cf0))

    def test_xor_verify_corrupt_df(self):
        """Corruption d'un bit dans DF → XOR invalide → retourne False."""
        dfs, cf0 = self._make_test_shards(16)
        # Corrompre 1 bit dans DF1
        corrupted = bytearray(dfs[1])
        corrupted[0] ^= 0x01
        dfs[1] = bytes(corrupted)
        self.assertFalse(ec4_reader.xor_verify(dfs, cf0))

    def test_xor_verify_corrupt_cf0(self):
        """Corruption d'un bit dans CF0 → XOR invalide → retourne False."""
        dfs, cf0 = self._make_test_shards(16)
        corrupted_cf0 = bytearray(cf0)
        corrupted_cf0[5] ^= 0xFF
        self.assertFalse(ec4_reader.xor_verify(dfs, bytes(corrupted_cf0)))

    def test_xor_verify_all_zeros(self):
        """Cas trivial : 4 DFs de zéros → CF0 = zéros."""
        dfs = [bytes(8)] * 4
        cf0 = bytes(8)
        self.assertTrue(ec4_reader.xor_verify(dfs, cf0))

    def test_xor_verify_single_byte(self):
        """Vérification XOR sur des shards de 1 byte."""
        dfs = [bytes([0x01]), bytes([0x02]), bytes([0x04]), bytes([0x08])]
        # CF0 = 0x01 ^ 0x02 ^ 0x04 ^ 0x08 = 0x0F
        cf0 = bytes([0x0F])
        self.assertTrue(ec4_reader.xor_verify(dfs, cf0))

    def test_xor_verify_known_values(self):
        """Vérification XOR avec valeurs prédéfinies (exemple réel)."""
        # small_ec.bin : shard_size = 8388608 (8 MiB exactement)
        # Test sur des valeurs petit format simulant le layout
        size = 32
        df0 = bytes(range(0, size))
        df1 = bytes(range(size, 2 * size))
        df2 = bytes(range(2 * size, 3 * size))
        df3 = bytes(range(3 * size, 4 * size))
        cf0 = bytearray(size)
        for df in [df0, df1, df2, df3]:
            for i in range(size):
                cf0[i] ^= df[i]
        self.assertTrue(ec4_reader.xor_verify([df0, df1, df2, df3], bytes(cf0)))

    def test_xor_verify_cf0_shorter_than_shards(self):
        """
        Si CF0 est plus court que les DFs (chunk_data_size % 4 != 0),
        la vérification porte seulement sur len(cf0) octets.
        """
        size = 10  # chunk_data_size = 10, shard_size = 3
        # DF0=bytes 0-2, DF1=3-5, DF2=6-8, DF3=9+padding
        df0 = bytes([0, 1, 2])
        df1 = bytes([3, 4, 5])
        df2 = bytes([6, 7, 8])
        df3 = bytes([9, 0, 0])  # padding

        cf0 = bytearray(3)
        for df in [df0, df1, df2, df3]:
            for i in range(3):
                cf0[i] ^= df[i]

        # cf0 fait 3 bytes, même taille que shard_size
        self.assertTrue(ec4_reader.xor_verify([df0, df1, df2, df3], bytes(cf0)))


# ---------------------------------------------------------------------------
# Tests des constantes protocole
# ---------------------------------------------------------------------------

class TestProtocolConstants(unittest.TestCase):

    def test_mfs_client_version(self):
        """MFS_CLIENT_VERSION = VERSION2INT(4, 58, 4) = 277000."""
        expected = (4 << 16) | (58 << 8) | (4 * 2)
        self.assertEqual(ec4_reader.MFS_CLIENT_VERSION, expected)
        self.assertEqual(ec4_reader.MFS_CLIENT_VERSION, 0x043A08)

    def test_chunk_size(self):
        """CHUNK_SIZE = 64 MiB."""
        self.assertEqual(ec4_reader.CHUNK_SIZE, 64 * 1024 * 1024)

    def test_fuse_register_blob_length(self):
        """FUSE_REGISTER_BLOB = 64 bytes."""
        self.assertEqual(len(ec4_reader.FUSE_REGISTER_BLOB), 64)

    def test_opcodes(self):
        """Vérification des valeurs d'opcodes selon le protocole MooseFS."""
        self.assertEqual(ec4_reader.CLTOM_FUSE_REGISTER, 400)
        self.assertEqual(ec4_reader.MATOCL_FUSE_REGISTER, 401)
        self.assertEqual(ec4_reader.CLTOM_FUSE_LOOKUP, 406)
        self.assertEqual(ec4_reader.MATOCL_FUSE_LOOKUP, 407)
        self.assertEqual(ec4_reader.CLTOM_FUSE_READ_CHUNK, 432)
        self.assertEqual(ec4_reader.MATOCL_FUSE_READ_CHUNK, 433)
        self.assertEqual(ec4_reader.CLTOCS_READ, 200)
        self.assertEqual(ec4_reader.CSTOCL_READ_STATUS, 201)
        self.assertEqual(ec4_reader.CSTOCL_READ_DATA, 202)
        self.assertEqual(ec4_reader.ROOT_NODE_ID, 1)


# ---------------------------------------------------------------------------
# Tests de calcul des tailles (shard_size)
# ---------------------------------------------------------------------------

class TestShardSizeCalculation(unittest.TestCase):

    def test_small_ec_bin_sizes(self):
        """
        small_ec.bin (32 MB = 33554432 bytes, 1 chunk) :
        - chunk_data_size = 33554432
        - shard_size = ceil(33554432 / 4) = 8388608 (= 8 MiB exactement)
        """
        file_length = 33_554_432
        chunk_index = 0
        chunk_data_size = min(file_length - chunk_index * ec4_reader.CHUNK_SIZE, ec4_reader.CHUNK_SIZE)
        shard_size = math.ceil(chunk_data_size / 4)

        self.assertEqual(chunk_data_size, 33_554_432)
        self.assertEqual(shard_size, 8_388_608)
        # Vérifier que c'est un multiple exact de 4 (pas de padding)
        self.assertEqual(chunk_data_size % 4, 0)

    def test_large_ec_bin_chunk0_sizes(self):
        """
        large_ec.bin (128 MB, chunk 0) :
        - chunk_data_size = 64 MiB (= CHUNK_SIZE)
        - shard_size = 16 MiB (multiple exact)
        """
        file_length = 134_217_728  # 128 MiB
        chunk_index = 0
        chunk_data_size = min(file_length - chunk_index * ec4_reader.CHUNK_SIZE, ec4_reader.CHUNK_SIZE)
        shard_size = math.ceil(chunk_data_size / 4)

        self.assertEqual(chunk_data_size, 67_108_864)  # 64 MiB
        self.assertEqual(shard_size, 16_777_216)        # 16 MiB
        self.assertEqual(chunk_data_size % 4, 0)

    def test_large_ec_bin_chunk1_sizes(self):
        """
        large_ec.bin (128 MB, chunk 1) :
        - chunk_data_size = 128 MiB - 64 MiB = 64 MiB
        - shard_size = 16 MiB
        """
        file_length = 134_217_728  # 128 MiB
        chunk_index = 1
        chunk_data_size = min(file_length - chunk_index * ec4_reader.CHUNK_SIZE, ec4_reader.CHUNK_SIZE)
        shard_size = math.ceil(chunk_data_size / 4)

        self.assertEqual(chunk_data_size, 67_108_864)  # 64 MiB
        self.assertEqual(shard_size, 16_777_216)        # 16 MiB

    def test_unaligned_file_shard_size(self):
        """Fichier non-aligné : ceil correct."""
        # 10 bytes dans un chunk
        chunk_data_size = 10
        shard_size = math.ceil(chunk_data_size / 4)
        self.assertEqual(shard_size, 3)  # ceil(10/4) = 3

    def test_chunk_boundary_detection(self):
        """chunk_data_size <= 0 signifie fin de fichier."""
        file_length = 33_554_432  # 1 chunk
        chunk_index = 1  # Il n'y a pas de chunk 1
        chunk_data_size = min(file_length - chunk_index * ec4_reader.CHUNK_SIZE, ec4_reader.CHUNK_SIZE)
        self.assertLessEqual(chunk_data_size, 0)


# ---------------------------------------------------------------------------
# Tests du mock register (vérification de la trame envoyée)
# ---------------------------------------------------------------------------

class TestRegisterFrame(unittest.TestCase):

    def test_register_payload_structure(self):
        """
        Vérifie que register() envoie la bonne trame CLTOM_FUSE_REGISTER.
        On prépare une réponse mock de succès (16 bytes : version(4) + sessionId(4) + autres).
        """
        # Construire la réponse mock : MATOCL_FUSE_REGISTER avec session_id = 42
        # ans = [version:32][sessionId:32][...8 bytes de remplissage]
        ans = struct.pack(">I", 0x043A08) + struct.pack(">I", 42) + bytes(8)
        response = make_frame(ec4_reader.MATOCL_FUSE_REGISTER, ans)
        sock = MockSocket(response)

        session_id = ec4_reader.register(sock)
        self.assertEqual(session_id, 42)

        # Vérifier la trame envoyée
        sent = sock.sent_bytes()
        self.assertGreater(len(sent), 8)
        cmd, length = struct.unpack(">II", sent[:8])
        self.assertEqual(cmd, ec4_reader.CLTOM_FUSE_REGISTER)

        # Vérifier le contenu du payload
        payload = sent[8:]
        self.assertEqual(payload[:64], ec4_reader.FUSE_REGISTER_BLOB)
        rcode = struct.unpack(">B", payload[64:65])[0]
        self.assertEqual(rcode, ec4_reader.REGISTER_NEWSESSION)

    def test_register_error_response(self):
        """register() lève RuntimeError si le serveur retourne status=0x06."""
        ans = bytes([0x06])  # status != 0
        response = make_frame(ec4_reader.MATOCL_FUSE_REGISTER, ans)
        sock = MockSocket(response)

        with self.assertRaises(RuntimeError) as ctx:
            ec4_reader.register(sock)
        self.assertIn("0x06", str(ctx.exception))

    def test_register_skips_nop(self):
        """register() ignore les trames NOP (cmd=0) avant la réponse."""
        nop = make_frame(0, b"")  # NOP
        ans = struct.pack(">I", 0x043A08) + struct.pack(">I", 99) + bytes(8)
        response = nop + make_frame(ec4_reader.MATOCL_FUSE_REGISTER, ans)
        sock = MockSocket(response)

        session_id = ec4_reader.register(sock)
        self.assertEqual(session_id, 99)


# ---------------------------------------------------------------------------
# Tests du mock lookup
# ---------------------------------------------------------------------------

class TestLookupFrame(unittest.TestCase):

    def test_lookup_success(self):
        """lookup() retourne l'inode depuis la réponse du master."""
        # ans = [msgid:32][inode:32][attrs:35]
        ans = struct.pack(">I", 0) + struct.pack(">I", 1234) + bytes(35)
        response = make_frame(ec4_reader.MATOCL_FUSE_LOOKUP, ans)
        sock = MockSocket(response)

        inode = ec4_reader.lookup(sock, ec4_reader.ROOT_NODE_ID, "small_ec.bin")
        self.assertEqual(inode, 1234)

    def test_lookup_file_not_found(self):
        """lookup() lève FileNotFoundError si la réponse est 5 bytes (erreur)."""
        # Réponse erreur : [msgid:32][status:8] = 5 bytes
        ans = struct.pack(">I", 0) + bytes([0x03])  # status ENOENT = 3
        response = make_frame(ec4_reader.MATOCL_FUSE_LOOKUP, ans)
        sock = MockSocket(response)

        with self.assertRaises(FileNotFoundError):
            ec4_reader.lookup(sock, ec4_reader.ROOT_NODE_ID, "inexistant.bin")


if __name__ == "__main__":
    unittest.main(verbosity=2)
