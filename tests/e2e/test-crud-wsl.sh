#!/usr/bin/env bash
# =============================================================================
# test-crud-wsl.sh — Suite de tests E2E CRUD GhostDrive (WSL)
# Usage : ./tests/e2e/test-crud-wsl.sh <backend-name>
#
# Valide les opérations CRUD dans les deux sens :
#   Local (/mnt/c/GhostDrive/<backend>/) → GhD: (/mnt/g/<backend>/)
#   GhD:  (/mnt/g/<backend>/)            → Local (/mnt/c/GhostDrive/<backend>/)
#
# Prérequis :
#   - GhostDrive.exe en cours d'exécution
#   - Un backend nommé <backend-name> configuré et connecté
#   - G: monté et accessible depuis WSL en /mnt/g/
# =============================================================================

set -uo pipefail

# ---------------------------------------------------------------------------
# Argument
# ---------------------------------------------------------------------------
BACKEND_NAME="${1:-}"
if [[ -z "$BACKEND_NAME" ]]; then
    echo "Usage: $0 <backend-name>"
    echo "Exemple: $0 webdav-nas"
    exit 1
fi

# ---------------------------------------------------------------------------
# Chemins
# ---------------------------------------------------------------------------
LOCAL_DIR="/mnt/c/GhostDrive/${BACKEND_NAME}"
GHD_DIR="/mnt/g/${BACKEND_NAME}"
LOG_FILE="/mnt/c/Users/cyril/AppData/Roaming/GhostDrive/logs/ghostdrive.log"
EXE_NAME="ghostdrive.exe"

# ---------------------------------------------------------------------------
# Paramètres de sync
# ---------------------------------------------------------------------------
SYNC_TIMEOUT=10   # secondes max pour la propagation
SYNC_POLL=1       # intervalle de polling (secondes)

# Horodatage pour noms de fichiers uniques (évite collisions entre runs)
TS=$(date +%s)

# ---------------------------------------------------------------------------
# Couleurs terminal
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ---------------------------------------------------------------------------
# Compteurs globaux
# ---------------------------------------------------------------------------
PASS=0
FAIL=0

# Résultats par direction
# L = LOCAL→GhD:, G = GhD:→LOCAL
declare -A RES_L_LOCAL   # opération côté local
declare -A RES_L_REMOTE  # propagation vers GhD:
declare -A RES_G_REMOTE  # opération côté GhD:
declare -A RES_G_LOCAL   # propagation vers local

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

ts() { date '+%H:%M:%S'; }

log()  { echo -e "${CYAN}[$(ts)]${NC} $*"; }
ok()   { echo -e "${GREEN}OK${NC}"; }
fail() { echo -e "${RED}ERREUR${NC}: $*"; }

# Attendre qu'un chemin existe — retourne 0 si trouvé, 1 si timeout
wait_for_file() {
    local path="$1"
    local timeout="${2:-$SYNC_TIMEOUT}"
    local elapsed=0
    while true; do
        if [[ -e "$path" ]]; then
            return 0
        fi
        if [[ $elapsed -ge $timeout ]]; then
            return 1
        fi
        sleep "$SYNC_POLL"
        elapsed=$(( elapsed + SYNC_POLL ))
    done
}

# Attendre qu'un chemin disparaisse — retourne 0 si disparu, 1 si timeout
wait_for_no_file() {
    local path="$1"
    local timeout="${2:-$SYNC_TIMEOUT}"
    local elapsed=0
    while true; do
        if [[ ! -e "$path" ]]; then
            return 0
        fi
        if [[ $elapsed -ge $timeout ]]; then
            return 1
        fi
        sleep "$SYNC_POLL"
        elapsed=$(( elapsed + SYNC_POLL ))
    done
}

# Enregistrer un résultat (table, action, status)
record() {
    local table="$1"
    local action="$2"
    local status="$3"

    if [[ "$status" == "OK" ]]; then
        PASS=$(( PASS + 1 ))
    elif [[ "$status" == ERREUR* ]]; then
        FAIL=$(( FAIL + 1 ))
    fi
    # N/A n'est pas compté

    case "$table" in
        L_LOCAL)  RES_L_LOCAL["$action"]="$status" ;;
        L_REMOTE) RES_L_REMOTE["$action"]="$status" ;;
        G_REMOTE) RES_G_REMOTE["$action"]="$status" ;;
        G_LOCAL)  RES_G_LOCAL["$action"]="$status" ;;
    esac
}

# Lire les erreurs CF API dans les logs depuis la ligne $1
check_log_errors() {
    local from_line="$1"
    local context="$2"
    if [[ ! -f "$LOG_FILE" ]]; then
        return 0
    fi
    local total_lines
    total_lines=$(wc -l < "$LOG_FILE" 2>/dev/null || echo "0")
    if [[ "$from_line" -ge "$total_lines" ]]; then
        return 0
    fi
    local errors
    errors=$(tail -n "+${from_line}" "$LOG_FILE" 2>/dev/null \
        | grep -iE "error|CF_|cfapi|cloud.filter|cfplaceholder" \
        | head -5 || true)
    if [[ -n "$errors" ]]; then
        echo -e "  ${YELLOW}⚠ Erreurs CF API [${context}] :${NC}" >&2
        echo "$errors" | sed 's/^/    /' >&2
    fi
}

# Numéro de ligne courant dans le log
log_line_now() {
    if [[ -f "$LOG_FILE" ]]; then
        wc -l < "$LOG_FILE" 2>/dev/null || echo "1"
    else
        echo "1"
    fi
}

# ---------------------------------------------------------------------------
# Vérifications préliminaires
# ---------------------------------------------------------------------------
preflight() {
    echo -e "${BOLD}Vérifications préliminaires...${NC}"

    # 1. GhostDrive.exe actif
    log "  process ${EXE_NAME}..."
    local proc_check
    proc_check=$(cmd.exe /c "tasklist /FI \"IMAGENAME eq ${EXE_NAME}\" /NH" 2>/dev/null || true)
    if echo "$proc_check" | grep -qi "ghostdrive"; then
        echo -e "  ${GREEN}✓ ${EXE_NAME} actif${NC}"
    else
        echo -e "  ${RED}✗ ${EXE_NAME} n'est pas en cours d'exécution${NC}"
        echo -e "  Lancez : ${BOLD}cmd.exe /c start build/qualif/2.2.0/ghostdrive-v2.2.0-windows-amd64.exe${NC}"
        exit 2
    fi

    # 2. Dossier local backend
    if [[ -d "$LOCAL_DIR" ]]; then
        echo -e "  ${GREEN}✓ Local  : ${LOCAL_DIR}${NC}"
    else
        echo -e "  ${RED}✗ Dossier local absent : ${LOCAL_DIR}${NC}"
        echo -e "  Configurez le backend '${BACKEND_NAME}' dans GhostDrive."
        exit 2
    fi

    # 3. Drive GhD: monté
    if [[ -d "$GHD_DIR" ]]; then
        echo -e "  ${GREEN}✓ GhD:   : ${GHD_DIR}${NC}"
    else
        echo -e "  ${RED}✗ GhD: non accessible : ${GHD_DIR}${NC}"
        echo -e "  Vérifiez que G: est monté (le drive virtuel GhostDrive doit être actif)."
        exit 2
    fi

    # 4. Fichier log (optionnel)
    if [[ -f "$LOG_FILE" ]]; then
        echo -e "  ${GREEN}✓ Logs   : ${LOG_FILE}${NC}"
    else
        echo -e "  ${YELLOW}⚠ Log absent : ${LOG_FILE} (analyse CF API désactivée)${NC}"
    fi

    echo ""
}

# ---------------------------------------------------------------------------
# Section 1 : LOCAL → GhD:
# ---------------------------------------------------------------------------
test_local_to_ghd() {
    local SEP="${BOLD}${CYAN}──────────────────────────────────────────────────────────────────${NC}"
    echo -e "$SEP"
    echo -e "${BOLD}${CYAN}  SECTION 1 — LOCAL → GhD:${NC}"
    echo -e "${BOLD}${CYAN}  ${LOCAL_DIR}  →  ${GHD_DIR}${NC}"
    echo -e "$SEP"
    echo ""

    local log_line f f2 f3 f4 src dst src2 dst2 src3 dst3_old dst3_new content

    # ─── 1/5 : New File ───────────────────────────────────────────────────
    f="crud-new-local-${TS}.txt"
    src="${LOCAL_DIR}/${f}"
    dst="${GHD_DIR}/${f}"
    log_line=$(log_line_now)

    log "[1/5] New File — création de ${f} en local..."
    if echo "ghostdrive-e2e-${TS}" > "$src" 2>/dev/null; then
        record L_LOCAL "New File" "OK"
        log "  → créé. Attente propagation vers GhD: (timeout ${SYNC_TIMEOUT}s)..."
        if wait_for_file "$dst"; then
            record L_REMOTE "New File" "OK"
            echo -e "  ${GREEN}✓ Propagé sur GhD:${NC}"
        else
            record L_REMOTE "New File" "ERREUR (timeout ${SYNC_TIMEOUT}s)"
            echo -e "  ${RED}✗ Non propagé sur GhD: après ${SYNC_TIMEOUT}s${NC}"
        fi
    else
        record L_LOCAL "New File" "ERREUR (écriture impossible)"
        record L_REMOTE "New File" "N/A"
        echo -e "  ${RED}✗ Impossible d'écrire dans ${LOCAL_DIR}${NC}"
    fi
    check_log_errors "$log_line" "New File L→G"
    echo ""

    # ─── 2/5 : Copy File ──────────────────────────────────────────────────
    f2="crud-copy-local-${TS}.txt"
    src2="${LOCAL_DIR}/${f2}"
    dst2="${GHD_DIR}/${f2}"
    log_line=$(log_line_now)

    log "[2/5] Copy File — copie ${f} → ${f2} en local..."
    if [[ -f "$src" ]]; then
        if cp "$src" "$src2" 2>/dev/null; then
            record L_LOCAL "Copy File" "OK"
            log "  → copie créée. Attente propagation vers GhD:..."
            if wait_for_file "$dst2"; then
                record L_REMOTE "Copy File" "OK"
                echo -e "  ${GREEN}✓ Propagé sur GhD:${NC}"
            else
                record L_REMOTE "Copy File" "ERREUR (timeout ${SYNC_TIMEOUT}s)"
                echo -e "  ${RED}✗ Non propagé sur GhD: après ${SYNC_TIMEOUT}s${NC}"
            fi
        else
            record L_LOCAL "Copy File" "ERREUR (cp impossible)"
            record L_REMOTE "Copy File" "N/A"
        fi
    else
        record L_LOCAL "Copy File" "ERREUR (source absente — New File a échoué)"
        record L_REMOTE "Copy File" "N/A"
    fi
    check_log_errors "$log_line" "Copy File L→G"
    echo ""

    # ─── 3/5 : Rename File ────────────────────────────────────────────────
    f3="crud-rename-src-local-${TS}.txt"
    f4="crud-rename-dst-local-${TS}.txt"
    local src3_path="${LOCAL_DIR}/${f3}"
    local src4_path="${LOCAL_DIR}/${f4}"
    dst3_old="${GHD_DIR}/${f3}"
    dst3_new="${GHD_DIR}/${f4}"
    log_line=$(log_line_now)

    log "[3/5] Rename File — ${f3} → ${f4} (création + sync + rename)..."
    if echo "rename-test-${TS}" > "$src3_path" 2>/dev/null; then
        # Attendre sync initial avant rename (fichier doit être connu du backend)
        log "  → fichier source créé. Attente sync initial sur GhD: avant rename..."
        if wait_for_file "$dst3_old"; then
            log "  → sync initial OK. Rename en local..."
            if mv "$src3_path" "$src4_path" 2>/dev/null; then
                record L_LOCAL "Rename File" "OK"
                log "  → rename effectué. Vérification propagation sur GhD:..."
                local rename_ok=true
                # Ancien nom doit disparaître
                if ! wait_for_no_file "$dst3_old"; then
                    echo -e "  ${YELLOW}⚠ Ancien nom encore présent sur GhD: après ${SYNC_TIMEOUT}s${NC}"
                    rename_ok=false
                fi
                # Nouveau nom doit apparaître
                if wait_for_file "$dst3_new"; then
                    if [[ "$rename_ok" == true ]]; then
                        record L_REMOTE "Rename File" "OK"
                        echo -e "  ${GREEN}✓ Rename propagé (ancien absent + nouveau présent sur GhD:)${NC}"
                    else
                        record L_REMOTE "Rename File" "ERREUR (ancien nom non supprimé sur GhD:)"
                        echo -e "  ${RED}✗ Nouveau nom présent mais ancien encore visible${NC}"
                    fi
                else
                    record L_REMOTE "Rename File" "ERREUR (nouveau nom absent sur GhD: après ${SYNC_TIMEOUT}s)"
                    echo -e "  ${RED}✗ Nouveau nom non apparu sur GhD:${NC}"
                fi
            else
                record L_LOCAL "Rename File" "ERREUR (mv impossible)"
                record L_REMOTE "Rename File" "N/A"
            fi
        else
            record L_LOCAL "Rename File" "ERREUR (sync initial échoué — fichier non visible sur GhD:)"
            record L_REMOTE "Rename File" "N/A"
            echo -e "  ${RED}✗ Le fichier source n'est pas apparu sur GhD: avant le rename${NC}"
        fi
    else
        record L_LOCAL "Rename File" "ERREUR (création source impossible)"
        record L_REMOTE "Rename File" "N/A"
    fi
    check_log_errors "$log_line" "Rename File L→G"
    echo ""

    # ─── 4/5 : Open File ──────────────────────────────────────────────────
    log_line=$(log_line_now)
    log "[4/5] Open File — lecture de ${f} en local..."
    if [[ -f "$src" ]]; then
        if content=$(cat "$src" 2>/dev/null); then
            record L_LOCAL "Open File" "OK"
            record L_REMOTE "Open File" "N/A"
            echo -e "  ${GREEN}✓ Lecture OK (\"${content}\")${NC}"
        else
            record L_LOCAL "Open File" "ERREUR (lecture échouée)"
            record L_REMOTE "Open File" "N/A"
            echo -e "  ${RED}✗ Impossible de lire le fichier${NC}"
        fi
    else
        record L_LOCAL "Open File" "ERREUR (fichier absent)"
        record L_REMOTE "Open File" "N/A"
        echo -e "  ${RED}✗ Fichier absent : ${src}${NC}"
    fi
    check_log_errors "$log_line" "Open File L"
    echo ""

    # ─── 5/5 : Delete File ────────────────────────────────────────────────
    log_line=$(log_line_now)
    log "[5/5] Delete File — suppression de ${f} en local..."
    if [[ -f "$src" ]]; then
        if rm "$src" 2>/dev/null; then
            record L_LOCAL "Delete File" "OK"
            log "  → supprimé en local. Attente propagation suppression sur GhD:..."
            if wait_for_no_file "$dst"; then
                record L_REMOTE "Delete File" "OK"
                echo -e "  ${GREEN}✓ Suppression propagée sur GhD:${NC}"
            else
                record L_REMOTE "Delete File" "ERREUR (fichier toujours présent sur GhD: après ${SYNC_TIMEOUT}s)"
                echo -e "  ${RED}✗ Fichier toujours présent sur GhD: après ${SYNC_TIMEOUT}s${NC}"
            fi
        else
            record L_LOCAL "Delete File" "ERREUR (rm impossible)"
            record L_REMOTE "Delete File" "N/A"
        fi
    else
        record L_LOCAL "Delete File" "ERREUR (fichier absent)"
        record L_REMOTE "Delete File" "N/A"
        echo -e "  ${RED}✗ Fichier absent${NC}"
    fi
    check_log_errors "$log_line" "Delete File L→G"
    echo ""

    # Nettoyage fichiers résiduels section 1
    rm -f "${LOCAL_DIR}/crud-copy-local-${TS}.txt" \
          "${LOCAL_DIR}/crud-rename-src-local-${TS}.txt" \
          "${LOCAL_DIR}/crud-rename-dst-local-${TS}.txt" \
          "${GHD_DIR}/crud-copy-local-${TS}.txt" \
          "${GHD_DIR}/crud-rename-dst-local-${TS}.txt" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Section 2 : GhD: → LOCAL
# ---------------------------------------------------------------------------
test_ghd_to_local() {
    local SEP="${BOLD}${CYAN}──────────────────────────────────────────────────────────────────${NC}"
    echo -e "$SEP"
    echo -e "${BOLD}${CYAN}  SECTION 2 — GhD: → LOCAL${NC}"
    echo -e "${BOLD}${CYAN}  ${GHD_DIR}  →  ${LOCAL_DIR}${NC}"
    echo -e "$SEP"
    echo ""

    local log_line f f2 f3 f4 src dst src2 dst2 content

    # ─── 1/5 : New File ───────────────────────────────────────────────────
    f="crud-new-ghd-${TS}.txt"
    src="${GHD_DIR}/${f}"
    dst="${LOCAL_DIR}/${f}"
    log_line=$(log_line_now)

    log "[1/5] New File — création de ${f} sur GhD:..."
    if echo "ghostdrive-e2e-ghd-${TS}" > "$src" 2>/dev/null; then
        record G_REMOTE "New File" "OK"
        log "  → créé sur GhD:. Attente propagation vers local (timeout ${SYNC_TIMEOUT}s)..."
        if wait_for_file "$dst"; then
            record G_LOCAL "New File" "OK"
            echo -e "  ${GREEN}✓ Propagé en local${NC}"
        else
            record G_LOCAL "New File" "ERREUR (timeout ${SYNC_TIMEOUT}s)"
            echo -e "  ${RED}✗ Non propagé en local après ${SYNC_TIMEOUT}s${NC}"
        fi
    else
        record G_REMOTE "New File" "ERREUR (écriture impossible sur GhD:)"
        record G_LOCAL "New File" "N/A"
        echo -e "  ${RED}✗ Impossible d'écrire dans ${GHD_DIR}${NC}"
    fi
    check_log_errors "$log_line" "New File G→L"
    echo ""

    # ─── 2/5 : Copy File ──────────────────────────────────────────────────
    f2="crud-copy-ghd-${TS}.txt"
    src2="${GHD_DIR}/${f2}"
    dst2="${LOCAL_DIR}/${f2}"
    log_line=$(log_line_now)

    log "[2/5] Copy File — copie ${f} → ${f2} sur GhD:..."
    if [[ -f "$src" ]]; then
        if cp "$src" "$src2" 2>/dev/null; then
            record G_REMOTE "Copy File" "OK"
            log "  → copie créée sur GhD:. Attente propagation vers local..."
            if wait_for_file "$dst2"; then
                record G_LOCAL "Copy File" "OK"
                echo -e "  ${GREEN}✓ Propagé en local${NC}"
            else
                record G_LOCAL "Copy File" "ERREUR (timeout ${SYNC_TIMEOUT}s)"
                echo -e "  ${RED}✗ Non propagé en local après ${SYNC_TIMEOUT}s${NC}"
            fi
        else
            record G_REMOTE "Copy File" "ERREUR (cp impossible)"
            record G_LOCAL "Copy File" "N/A"
        fi
    else
        record G_REMOTE "Copy File" "ERREUR (source absente — New File a échoué)"
        record G_LOCAL "Copy File" "N/A"
    fi
    check_log_errors "$log_line" "Copy File G→L"
    echo ""

    # ─── 3/5 : Rename File ────────────────────────────────────────────────
    f3="crud-rename-src-ghd-${TS}.txt"
    f4="crud-rename-dst-ghd-${TS}.txt"
    local ghd_src="${GHD_DIR}/${f3}"
    local ghd_dst="${GHD_DIR}/${f4}"
    local loc_src="${LOCAL_DIR}/${f3}"
    local loc_dst="${LOCAL_DIR}/${f4}"
    log_line=$(log_line_now)

    log "[3/5] Rename File — ${f3} → ${f4} (création + sync + rename)..."
    if echo "rename-test-ghd-${TS}" > "$ghd_src" 2>/dev/null; then
        log "  → fichier source créé sur GhD:. Attente sync initial en local..."
        if wait_for_file "$loc_src"; then
            log "  → sync initial OK. Rename sur GhD:..."
            if mv "$ghd_src" "$ghd_dst" 2>/dev/null; then
                record G_REMOTE "Rename File" "OK"
                log "  → rename effectué. Vérification propagation en local..."
                local rename_ok=true
                if ! wait_for_no_file "$loc_src"; then
                    echo -e "  ${YELLOW}⚠ Ancien nom encore présent en local après ${SYNC_TIMEOUT}s${NC}"
                    rename_ok=false
                fi
                if wait_for_file "$loc_dst"; then
                    if [[ "$rename_ok" == true ]]; then
                        record G_LOCAL "Rename File" "OK"
                        echo -e "  ${GREEN}✓ Rename propagé (ancien absent + nouveau présent en local)${NC}"
                    else
                        record G_LOCAL "Rename File" "ERREUR (ancien nom non supprimé en local)"
                        echo -e "  ${RED}✗ Nouveau nom présent mais ancien encore visible${NC}"
                    fi
                else
                    record G_LOCAL "Rename File" "ERREUR (nouveau nom absent en local après ${SYNC_TIMEOUT}s)"
                    echo -e "  ${RED}✗ Nouveau nom non apparu en local${NC}"
                fi
            else
                record G_REMOTE "Rename File" "ERREUR (mv impossible sur GhD:)"
                record G_LOCAL "Rename File" "N/A"
            fi
        else
            record G_REMOTE "Rename File" "ERREUR (sync initial échoué — fichier non visible en local)"
            record G_LOCAL "Rename File" "N/A"
            echo -e "  ${RED}✗ Le fichier source n'est pas apparu en local avant le rename${NC}"
        fi
    else
        record G_REMOTE "Rename File" "ERREUR (création source impossible sur GhD:)"
        record G_LOCAL "Rename File" "N/A"
    fi
    check_log_errors "$log_line" "Rename File G→L"
    echo ""

    # ─── 4/5 : Open File ──────────────────────────────────────────────────
    log_line=$(log_line_now)
    log "[4/5] Open File — lecture de ${f} sur GhD:..."
    if [[ -f "$src" ]]; then
        if content=$(cat "$src" 2>/dev/null); then
            record G_REMOTE "Open File" "OK"
            record G_LOCAL "Open File" "N/A"
            echo -e "  ${GREEN}✓ Lecture OK (\"${content}\")${NC}"
        else
            record G_REMOTE "Open File" "ERREUR (lecture échouée)"
            record G_LOCAL "Open File" "N/A"
            echo -e "  ${RED}✗ Impossible de lire le fichier${NC}"
        fi
    else
        record G_REMOTE "Open File" "ERREUR (fichier absent)"
        record G_LOCAL "Open File" "N/A"
        echo -e "  ${RED}✗ Fichier absent : ${src}${NC}"
    fi
    check_log_errors "$log_line" "Open File G"
    echo ""

    # ─── 5/5 : Delete File ────────────────────────────────────────────────
    log_line=$(log_line_now)
    log "[5/5] Delete File — suppression de ${f} sur GhD:..."
    if [[ -f "$src" ]]; then
        if rm "$src" 2>/dev/null; then
            record G_REMOTE "Delete File" "OK"
            log "  → supprimé sur GhD:. Attente propagation suppression en local..."
            if wait_for_no_file "$dst"; then
                record G_LOCAL "Delete File" "OK"
                echo -e "  ${GREEN}✓ Suppression propagée en local${NC}"
            else
                record G_LOCAL "Delete File" "ERREUR (fichier toujours présent en local après ${SYNC_TIMEOUT}s)"
                echo -e "  ${RED}✗ Fichier toujours présent en local après ${SYNC_TIMEOUT}s${NC}"
            fi
        else
            record G_REMOTE "Delete File" "ERREUR (rm impossible)"
            record G_LOCAL "Delete File" "N/A"
        fi
    else
        record G_REMOTE "Delete File" "ERREUR (fichier absent)"
        record G_LOCAL "Delete File" "N/A"
        echo -e "  ${RED}✗ Fichier absent${NC}"
    fi
    check_log_errors "$log_line" "Delete File G→L"
    echo ""

    # Nettoyage fichiers résiduels section 2
    rm -f "${GHD_DIR}/crud-copy-ghd-${TS}.txt" \
          "${GHD_DIR}/crud-rename-src-ghd-${TS}.txt" \
          "${GHD_DIR}/crud-rename-dst-ghd-${TS}.txt" \
          "${LOCAL_DIR}/crud-copy-ghd-${TS}.txt" \
          "${LOCAL_DIR}/crud-rename-src-ghd-${TS}.txt" \
          "${LOCAL_DIR}/crud-rename-dst-ghd-${TS}.txt" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Affichage du tableau de résultats final
# ---------------------------------------------------------------------------
print_results() {
    local ACTIONS=("New File" "Copy File" "Rename File" "Open File" "Delete File")

    local W1=20  # largeur colonne Action
    local W2=36  # largeur colonnes valeurs

    # Séparateur
    local SEP_LINE
    printf -v SEP_LINE "%-${W1}s-+-%-${W2}s-+-%-${W2}s" \
        "$(printf '%0.s─' $(seq 1 $W1))" \
        "$(printf '%0.s─' $(seq 1 $W2))" \
        "$(printf '%0.s─' $(seq 1 $W2))"

    # Colorer un statut
    colored() {
        local s="$1"
        if [[ "$s" == "OK" ]]; then
            printf "${GREEN}${BOLD}%-${W2}s${NC}" "$s"
        elif [[ "$s" == ERREUR* ]]; then
            printf "${RED}%-${W2}s${NC}" "$s"
        else
            printf "%-${W2}s" "$s"
        fi
    }

    echo ""
    echo -e "${BOLD}══════════════════════════════════════════════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}  RÉSULTATS CRUD — backend : ${BACKEND_NAME}${NC}"
    echo -e "${BOLD}══════════════════════════════════════════════════════════════════════════════════════════════════${NC}"
    echo ""

    # ── Tableau 1 : LOCAL → GhD: ─────────────────────────────────────────
    echo -e "${BOLD}Direction : LOCAL → GhD:${NC}  (opération en local, propagation vérifiée sur GhD:)"
    echo ""
    printf "${BOLD}%-${W1}s | %-${W2}s | %-${W2}s${NC}\n" \
        "Action" \
        "Local (/mnt/c/GhostDrive/)" \
        "GhD: (/mnt/g/)"
    echo "$SEP_LINE"
    for action in "${ACTIONS[@]}"; do
        local l="${RES_L_LOCAL[$action]:-N/A}"
        local r="${RES_L_REMOTE[$action]:-N/A}"
        printf "%-${W1}s | " "$action"
        colored "$l"
        printf " | "
        colored "$r"
        echo ""
    done
    echo ""

    # ── Tableau 2 : GhD: → LOCAL ─────────────────────────────────────────
    echo -e "${BOLD}Direction : GhD: → LOCAL${NC}  (opération sur GhD:, propagation vérifiée en local)"
    echo ""
    printf "${BOLD}%-${W1}s | %-${W2}s | %-${W2}s${NC}\n" \
        "Action" \
        "GhD: (/mnt/g/)" \
        "Local (/mnt/c/GhostDrive/)"
    echo "$SEP_LINE"
    for action in "${ACTIONS[@]}"; do
        local g="${RES_G_REMOTE[$action]:-N/A}"
        local l="${RES_G_LOCAL[$action]:-N/A}"
        printf "%-${W1}s | " "$action"
        colored "$g"
        printf " | "
        colored "$l"
        echo ""
    done
    echo ""

    # ── Bilan global ──────────────────────────────────────────────────────
    local total=$(( PASS + FAIL ))
    echo -e "${BOLD}══════════════════════════════════════════════════════════════════════════════════════════════════${NC}"
    if [[ $FAIL -eq 0 && $total -gt 0 ]]; then
        echo -e "  ${GREEN}${BOLD}✓ PASS — ${PASS}/${total} vérifications OK${NC}  (N/A exclus du compte)"
    elif [[ $FAIL -gt 0 ]]; then
        echo -e "  ${RED}${BOLD}✗ FAIL — ${FAIL} erreur(s) / ${total} vérifications  (${PASS} OK)${NC}"
    else
        echo -e "  ${YELLOW}⚠ Aucun résultat enregistré — vérifiez les prérequis${NC}"
    fi
    echo -e "${BOLD}══════════════════════════════════════════════════════════════════════════════════════════════════${NC}"
    echo ""

    [[ $FAIL -eq 0 && $total -gt 0 ]]
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    echo ""
    echo -e "${BOLD}${CYAN}╔══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BOLD}${CYAN}║  GhostDrive — Tests E2E CRUD (WSL)                          ║${NC}"
    echo -e "${BOLD}${CYAN}║  Backend : ${BACKEND_NAME}$(printf '%*s' $(( 51 - ${#BACKEND_NAME} )) '')║${NC}"
    echo -e "${BOLD}${CYAN}║  Timeout sync : ${SYNC_TIMEOUT}s                                          ║${NC}"
    echo -e "${BOLD}${CYAN}╚══════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    preflight
    test_local_to_ghd
    test_ghd_to_local
    print_results
}

main
