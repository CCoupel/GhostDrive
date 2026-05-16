#!/usr/bin/env python3
"""
poc/validate_fixtures.py — Validation XOR des fixtures JSON EC4+1 (issue #113)

Lit les fichiers *_shards.json générés par ec4_reader.py et vérifie que
XOR(DF0, DF1, DF2, DF3) == CF0 sur les échantillons.

Usage :
    python3 poc/validate_fixtures.py                    # cherche dans tests/fixtures/ec/ec4/
    python3 poc/validate_fixtures.py --dir /chemin/     # répertoire spécifique
    python3 poc/validate_fixtures.py --verbose          # affichage détaillé
"""

import argparse
import base64
import glob
import json
import os
import sys


# ---------------------------------------------------------------------------
# Validation XOR
# ---------------------------------------------------------------------------

def xor_bytes(buffers: list) -> bytes:
    """Calcule le XOR de plusieurs buffers de même longueur."""
    if not buffers:
        raise ValueError("Aucun buffer fourni")
    n = len(buffers[0])
    for b in buffers[1:]:
        if len(b) != n:
            raise ValueError(f"Longueurs incohérentes : {n} vs {len(b)}")
    result = bytearray(n)
    for buf in buffers:
        for i in range(n):
            result[i] ^= buf[i]
    return bytes(result)


def validate_shards_file(path: str, verbose: bool = False) -> bool:
    """
    Valide un fichier *_shards.json.

    Retourne True si le XOR est correct, False sinon.
    """
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)

    chunk_id   = data.get("chunk_id", "?")
    version    = data.get("version", "?")
    shard_size = data.get("shard_size", "?")
    sample_size = data.get("sample_size", "?")
    xor_claim  = data.get("xor_verified", None)

    if verbose:
        print(f"  chunk_id   : {chunk_id}")
        print(f"  version    : {version}")
        print(f"  shard_size : {shard_size}")
        print(f"  sample_size: {sample_size}")

    # Décoder les échantillons base64
    try:
        df0 = base64.b64decode(data["df0_sample_b64"])
        df1 = base64.b64decode(data["df1_sample_b64"])
        df2 = base64.b64decode(data["df2_sample_b64"])
        df3 = base64.b64decode(data["df3_sample_b64"])
        cf0 = base64.b64decode(data["cf0_sample_b64"])
    except KeyError as e:
        print(f"  ERREUR : champ manquant dans le JSON : {e}")
        return False

    if verbose:
        print(f"  DF0 ({len(df0)}B) : {df0[:8].hex()}...")
        print(f"  DF1 ({len(df1)}B) : {df1[:8].hex()}...")
        print(f"  DF2 ({len(df2)}B) : {df2[:8].hex()}...")
        print(f"  DF3 ({len(df3)}B) : {df3[:8].hex()}...")
        print(f"  CF0 ({len(cf0)}B) : {cf0[:8].hex()}...")

    # Vérifier les longueurs cohérentes
    lengths = {len(df0), len(df1), len(df2), len(df3), len(cf0)}
    if len(lengths) > 1:
        print(f"  ERREUR : longueurs incohérentes entre shards : {lengths}")
        return False

    # Calculer XOR(DF0, DF1, DF2, DF3)
    computed_xor = xor_bytes([df0, df1, df2, df3])

    # Comparer avec CF0
    if computed_xor == cf0:
        print(f"  XOR vérifié ✓")
        if xor_claim is not None and not xor_claim:
            print(f"  AVERTISSEMENT : xor_verified=false dans le fichier mais XOR calculé est correct")
        return True
    else:
        print(f"  XOR INVALIDE ✗")
        print(f"  XOR calculé : {computed_xor[:8].hex()}...")
        print(f"  CF0 attendu : {cf0[:8].hex()}...")
        # Localiser le premier octet différent
        for i in range(min(len(computed_xor), len(cf0))):
            if computed_xor[i] != cf0[i]:
                print(f"  Premier diff à offset {i} : XOR=0x{computed_xor[i]:02x}, CF0=0x{cf0[i]:02x}")
                break
        return False


def validate_meta_file(path: str, verbose: bool = False) -> bool:
    """
    Valide un fichier *_meta.json (vérification de cohérence des champs).

    Retourne True si le fichier est valide, False sinon.
    """
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)

    required_fields = [
        "filename", "file_size", "chunk_index", "chunk_id", "version",
        "proto", "chunk_data_size", "shard_size", "servers", "md5_reconstructed"
    ]

    missing = [f for f in required_fields if f not in data]
    if missing:
        print(f"  ERREUR : champs manquants : {missing}")
        return False

    # Vérifications de cohérence
    errors = []

    if data["proto"] != 3:
        errors.append(f"proto={data['proto']} attendu 3 (EC)")

    if len(data["servers"]) != 5:
        errors.append(f"servers: {len(data['servers'])} serveurs, attendu 5")

    expected_roles = ["DF0", "DF1", "DF2", "DF3", "CF0"]
    actual_roles = [s.get("role") for s in data["servers"]]
    if actual_roles != expected_roles:
        errors.append(f"rôles serveurs incorrects : {actual_roles}")

    import math
    expected_shard_size = math.ceil(data["chunk_data_size"] / 4)
    if data["shard_size"] != expected_shard_size:
        errors.append(
            f"shard_size={data['shard_size']} mais ceil({data['chunk_data_size']}/4)={expected_shard_size}"
        )

    if data["file_size"] <= 0:
        errors.append(f"file_size={data['file_size']} invalide")

    if not data["chunk_id"].startswith("0x"):
        errors.append(f"chunk_id={data['chunk_id']!r} devrait commencer par '0x'")

    if errors:
        for err in errors:
            print(f"  ERREUR : {err}")
        return False

    if verbose:
        print(f"  filename       : {data['filename']}")
        print(f"  file_size      : {data['file_size']}")
        print(f"  chunk_index    : {data['chunk_index']}")
        print(f"  chunk_id       : {data['chunk_id']}")
        print(f"  chunk_data_size: {data['chunk_data_size']}")
        print(f"  shard_size     : {data['shard_size']}")
        print(f"  md5            : {data['md5_reconstructed']}")
        for srv in data["servers"]:
            print(f"  {srv['role']} : {srv['ip']}:{srv['port']}")

    print(f"  Métadonnées valides ✓")
    return True


# ---------------------------------------------------------------------------
# Scan du répertoire et validation globale
# ---------------------------------------------------------------------------

def validate_directory(fixtures_dir: str, verbose: bool = False) -> tuple:
    """
    Valide tous les fichiers JSON dans le répertoire fixtures.

    Retourne (nb_ok, nb_total).
    """
    if not os.path.isdir(fixtures_dir):
        print(f"ERREUR : répertoire introuvable : {fixtures_dir}")
        return 0, 0

    shards_files = sorted(glob.glob(os.path.join(fixtures_dir, "*_shards.json")))
    meta_files   = sorted(glob.glob(os.path.join(fixtures_dir, "*_meta.json")))
    all_files    = shards_files + meta_files

    if not all_files:
        print(f"Aucun fichier fixture trouvé dans : {fixtures_dir}")
        return 0, 0

    print(f"\nValidation de {len(all_files)} fichier(s) dans {fixtures_dir}")
    print("=" * 60)

    nb_ok = 0
    nb_total = len(all_files)

    for path in shards_files:
        name = os.path.basename(path)
        print(f"\n[SHARDS] {name}")
        ok = validate_shards_file(path, verbose=verbose)
        if ok:
            nb_ok += 1

    for path in meta_files:
        name = os.path.basename(path)
        print(f"\n[META] {name}")
        ok = validate_meta_file(path, verbose=verbose)
        if ok:
            nb_ok += 1

    return nb_ok, nb_total


# ---------------------------------------------------------------------------
# Point d'entrée CLI
# ---------------------------------------------------------------------------

def main() -> None:
    parser = argparse.ArgumentParser(
        description="Validation XOR des fixtures JSON EC4+1 MooseFS"
    )
    parser.add_argument(
        "--dir",
        default="tests/fixtures/ec/ec4/",
        help="Répertoire des fixtures JSON (défaut : tests/fixtures/ec/ec4/)",
    )
    parser.add_argument(
        "--verbose", "-v",
        action="store_true",
        help="Affichage détaillé des valeurs",
    )
    args = parser.parse_args()

    nb_ok, nb_total = validate_directory(args.dir, verbose=args.verbose)

    print("\n" + "=" * 60)
    if nb_total == 0:
        print("Aucune fixture à valider.")
        sys.exit(0)
    elif nb_ok == nb_total:
        print(f"Résultat : {nb_ok}/{nb_total} fichiers valides ✓")
        sys.exit(0)
    else:
        print(f"Résultat : {nb_ok}/{nb_total} fichiers valides — {nb_total - nb_ok} ÉCHEC(S) ✗")
        sys.exit(1)


if __name__ == "__main__":
    main()
