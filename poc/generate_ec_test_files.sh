#!/bin/bash
# generate_ec_test_files.sh
# Génère deux fichiers de test EC pour le POC GhostDrive (issue #113)
# Usage  : ./generate_ec_test_files.sh <mfs_mountpoint> [ec_goal]
# Exemple: ./generate_ec_test_files.sh /mnt/mfs "ec(4,1)"

set -euo pipefail

MOUNT=${1:?"Usage: $0 <mfs_mountpoint> [ec_goal]"}
GOAL=${2:-"ec(4,1)"}
DIR="$MOUNT/ghostdrive_ec_test"

echo "=== GhostDrive EC Test File Generator ==="
echo "Mount : $MOUNT"
echo "Goal  : $GOAL"
echo ""

mkdir -p "$DIR"
mfssetgoal "$GOAL" "$DIR" || { echo "ERREUR: mfssetgoal a échoué — vérifie le nom du goal (ex: ec(4,1), xor4)"; exit 1; }

echo "[1/2] Génération small_ec.bin (32 MB — 1 chunk)..."
dd if=/dev/urandom of="$DIR/small_ec.bin" bs=1M count=32 status=progress 2>&1

echo ""
echo "[2/2] Génération large_ec.bin (128 MB — 2 chunks)..."
dd if=/dev/urandom of="$DIR/large_ec.bin" bs=1M count=128 status=progress 2>&1

echo ""
echo "Attente distribution EC (30s)..."
sleep 30

echo ""
echo "=== Checksums ==="
md5sum "$DIR/small_ec.bin" "$DIR/large_ec.bin"
sha256sum "$DIR/small_ec.bin" "$DIR/large_ec.bin"

echo ""
echo "=== Infos chunks MooseFS ==="
mfsfileinfo "$DIR/small_ec.bin"
echo "---"
mfsfileinfo "$DIR/large_ec.bin"

echo ""
echo "=== Résumé à transmettre à Claude ==="
echo "Master host  : <à remplir>"
echo "Master port  : 9421"
echo "Small file   : $DIR/small_ec.bin"
echo "Large file   : $DIR/large_ec.bin"
echo "Goal utilisé : $GOAL"
