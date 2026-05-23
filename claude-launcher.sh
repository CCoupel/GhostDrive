#!/usr/bin/env bash
# Nécessite : fzf tmux jq
# Installation : apt install fzf tmux jq  /  brew install fzf tmux jq
#
# Usage : ./claude.sh
#   - Session unique "claude-hub"
#   - Window "[menu]" : sélecteur fzf permanent
#   - Un window par projet avec claude
#   - Layout agent teams :
#       Ligne haute : agentType == "cdp"  (inclut le team-lead)
#       Ligne basse : tous les autres agents (côte-à-côte)
#   - Nettoyage automatique des teams orphelines au démarrage du menu
#
# Raccourci tmux pour forcer le relayout :
#   Ajouter dans ~/.tmux.conf :
#   bind R run-shell "bash /chemin/vers/claude.sh --relayout '#{session_name}' '#{window_id}'"

# ════════════════════════════════════════════════════════════════════════════
# VÉRIFICATION DES PRÉREQUIS
# ════════════════════════════════════════════════════════════════════════════
check_prerequisites() {
  local missing=()
  local warnings=()

  printf "\033[0;90m  Vérification des prérequis...\033[0m\n"
  for cmd in tmux fzf jq curl gh claude; do
    if command -v "$cmd" &>/dev/null; then
      printf "\033[1;32m  ✓\033[0m %-10s %s\n" "$cmd" "$(command -v "$cmd")"
    else
      printf "\033[1;31m  ✗\033[0m %-10s manquant\n" "$cmd"
      missing+=("$cmd")
    fi
  done
  printf "\n"

  if [[ ${#missing[@]} -gt 0 ]]; then
    printf "\033[1;31m  Installation requise :\033[0m\n"
    local apt_pkgs=() brew_pkgs=()
    for cmd in "${missing[@]}"; do
      case "$cmd" in
        tmux|fzf|jq|curl) apt_pkgs+=("$cmd"); brew_pkgs+=("$cmd") ;;
        gh)    apt_pkgs+=("gh");     brew_pkgs+=("gh") ;;
        claude) printf "    claude  → npm install -g @anthropic-ai/claude-code\n" ;;
      esac
    done
    [[ ${#apt_pkgs[@]} -gt 0 ]] && \
      printf "    apt     → sudo apt install %s\n" "${apt_pkgs[*]}"
    [[ ${#brew_pkgs[@]} -gt 0 ]] && \
      printf "    brew    → brew install %s\n" "${brew_pkgs[*]}"
    printf "\n"
    exit 1
  fi

  # Avertissements non bloquants
  if ! gh auth status &>/dev/null; then
    warnings+=("gh non authentifié — définir GITHUB_TOKEN dans la config ou lancer : gh auth login")
  fi

  for w in "${warnings[@]}"; do
    printf "\033[1;33m  ⚠  %s\033[0m\n" "$w"
  done
  [[ ${#warnings[@]} -gt 0 ]] && printf "\n"
}

# Sauter la vérification pour les modes internes (appelés par tmux en arrière-plan)
case "${1:-}" in
  --do-layout|--debug-layout|--relayout|--layout-watch) ;;
  *) check_prerequisites ;;
esac

# --no-update : désactive l'auto-update GitHub pour cette exécution
NO_AUTO_UPDATE=0
if [[ "${1:-}" == "--no-update" ]]; then
  NO_AUTO_UPDATE=1
  shift
fi

SESSION="claude-hub"
TEMPLATE_REPO="CCoupel/claude_project_template"
TEMPLATE_BRANCH="main"
CONFIG_FILE="${HOME}/.config/claude-launcher.conf"

# ── Valeurs par défaut (écrasées par le fichier de config) ───────────────────
GITHUB_DIR="$HOME/GITHUB"
GITHUB_TOKEN=""
CLAUDE_DISABLE_MOUSE=1
CLAUDE_EXPERIMENTAL_TEAMS=1
CLAUDE_OPTIONS=""

load_config() {
  [[ -f "$CONFIG_FILE" ]] || return
  # shellcheck source=/dev/null
  source "$CONFIG_FILE"
}

create_default_config() {
  mkdir -p "$(dirname "$CONFIG_FILE")"
  cat > "$CONFIG_FILE" <<'EOF'
# claude-launcher — configuration utilisateur
# Éditer puis relancer le launcher.

# Répertoire contenant vos projets
GITHUB_DIR="$HOME/GITHUB"

# Token GitHub (gh CLI + Claude Code MCP)
GITHUB_TOKEN=""

# Options Claude Code
CLAUDE_DISABLE_MOUSE=1        # 1 = désactive la souris dans le terminal
CLAUDE_EXPERIMENTAL_TEAMS=1  # 1 = active les agent teams
CLAUDE_OPTIONS="--allow-dangerously-skip-permissions"

# Variables d'environnement supplémentaires passées à Claude Code
# Format : "NOM=valeur"  (une par ligne, sans export)
# Exemple :
#   EXTRA_ENVS=(
#     "ANTHROPIC_API_KEY=sk-ant-..."
#     "MY_API_URL=https://api.example.com"
#   )
EXTRA_ENVS=()
EOF
}

build_claude_exports() {
  local e=""
  [[ -n "$GITHUB_TOKEN" ]] && \
    e+=$'\n'"export GITHUB_PERSONAL_ACCESS_TOKEN=\"${GITHUB_TOKEN}\""$'\n'"export GH_TOKEN=\"${GITHUB_TOKEN}\""
  [[ "${CLAUDE_DISABLE_MOUSE:-0}" == "1" ]] && \
    e+=$'\n'"export CLAUDE_CODE_DISABLE_MOUSE=1"
  [[ "${CLAUDE_EXPERIMENTAL_TEAMS:-0}" == "1" ]] && \
    e+=$'\n'"export CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1"
  for env_var in "${EXTRA_ENVS[@]+"${EXTRA_ENVS[@]}"}"; do
    [[ -n "$env_var" ]] && e+=$'\n'"export ${env_var}"
  done
  printf '%s' "$e"
}

auto_update() {
  local tmp
  tmp=$(mktemp)
  if curl -fsSL --ipv4 --max-time 5 \
      "https://raw.githubusercontent.com/${TEMPLATE_REPO}/${TEMPLATE_BRANCH}/claude-launcher.sh" \
      -o "$tmp" 2>/dev/null; then
    if ! cmp -s "$tmp" "$SCRIPT_PATH"; then
      printf "\033[1;32m  ↑ Nouvelle version disponible — mise à jour...\033[0m\n"
      chmod +x "$tmp"
      mv "$tmp" "$SCRIPT_PATH"
      printf "  ✓ Launcher mis à jour. Relancement...\n\n"
      exec "$SCRIPT_PATH"
    fi
  fi
  rm -f "$tmp"
}

load_config

# ════════════════════════════════════════════════════════════════════════════
# FONCTION : find_team_config PROJECT_DIR
# Cherche le config.json de la team correspondant au projet
# Retourne le chemin ou "" si non trouvé
# ════════════════════════════════════════════════════════════════════════════
find_team_config() {
  local project_dir="$1"

  # Normalise le chemin : supprime slash final, résout si possible
  local real_project
  real_project=$(realpath "$project_dir" 2>/dev/null) || real_project="${project_dir%/}"

  for cfg in "$HOME/.claude/teams/"*/config.json; do
    [[ -f "$cfg" ]] || continue
    local cwd
    cwd=$(jq -r '.members[] | select(.name=="team-lead") | .cwd' "$cfg" 2>/dev/null)
    [[ -z "$cwd" || "$cwd" == "null" ]] && continue

    # Normalise le cwd du json de la même façon
    local real_cwd
    real_cwd=$(realpath "$cwd" 2>/dev/null) || real_cwd="${cwd%/}"

    # Comparaison directe et insensible à la casse (chemins WSL /mnt/c/...)
    if [[ "$real_cwd" == "$real_project" ]]     || [[ "${real_cwd,,}" == "${real_project,,}" ]]; then
      echo "$cfg"
      return
    fi
  done
}

# ════════════════════════════════════════════════════════════════════════════
# FONCTION : do_layout SESSION WIN_ID LEADER_PANE TEAM_CONFIG
# Classe les panes selon agentType dans config.json :
#   agentType == "cdp"  → ligne haute
#   tout le reste       → ligne basse
# ════════════════════════════════════════════════════════════════════════════
do_layout() {
  local TARGET_SESSION="$1"
  local WIN_ID="$2"
  local LEADER_PANE="$3"
  local TEAM_CONFIG="$4"
  local win="${TARGET_SESSION}:${WIN_ID}"

  # Lock anti race-condition
  local lock="/tmp/claude_layout_${WIN_ID}.lock"
  if [[ -f "$lock" ]]; then
    touch "/tmp/claude_layout_${WIN_ID}.rerun"
    return
  fi
  touch "$lock"
  trap 'rm -f "$lock"' RETURN

  # ── Récupère tous les panes ───────────────────────────────────────────────
  mapfile -t all_panes < <(tmux list-panes -t "$win" -F '#{pane_id}' 2>/dev/null)
  [[ ${#all_panes[@]} -le 1 ]] && return

  # ── Classification ────────────────────────────────────────────────────────
  local top_panes=("$LEADER_PANE")
  local bot_panes=()

  if [[ -f "$TEAM_CONFIG" ]]; then
    # Collecte nom + pane_id + agentType, triés par nom pour les bots
    local bot_named=()
    while IFS=$'\t' read -r pane_id agent_type agent_name; do
      [[ -z "$pane_id" || "$pane_id" == "null" ]] && continue
      [[ "$pane_id" == "$LEADER_PANE" ]] && continue
      tmux list-panes -t "$win" -F '#{pane_id}' 2>/dev/null \
        | grep -qxF "$pane_id" || continue
      if [[ "$agent_type" == "cdp" ]]; then
        top_panes+=("$pane_id")
      else
        # Stocke "nom|pane_id" pour tri alphabétique
        bot_named+=("${agent_name}|${pane_id}")
      fi
    done < <(jq -r '
      .members[]
      | select(.tmuxPaneId != null and .tmuxPaneId != "")
      | [.tmuxPaneId, .agentType, .name]
      | @tsv
    ' "$TEAM_CONFIG" 2>/dev/null)

    # Trie les bots par nom alphabétique
    local sorted_bots
    while IFS= read -r entry; do
      bot_panes+=("${entry#*|}")
    done < <(printf '%s\n' "${bot_named[@]}" | sort)
  fi

  # Panes non référencés (panes manuels) → ligne basse non triée
  for p in "${all_panes[@]}"; do
    [[ "$p" == "$LEADER_PANE" ]] && continue
    local found=0
    for tp in "${top_panes[@]}"; do [[ "$p" == "$tp" ]] && found=1 && break; done
    for bp in "${bot_panes[@]}"; do [[ "$p" == "$bp" ]] && found=1 && break; done
    [[ $found -eq 0 ]] && bot_panes+=("$p")
  done

  local n_top=${#top_panes[@]}
  local n_bot=${#bot_panes[@]}

  # ── Dimensions ────────────────────────────────────────────────────────────
  local W H
  W=$(tmux display-message -t "$win" -p '#{window_width}'  2>/dev/null)
  H=$(tmux display-message -t "$win" -p '#{window_height}' 2>/dev/null)
  local top_h=$(( H * 55 / 100 ))
  local bot_h=$(( H - top_h - 1 ))

  # ── Construction de la layout string tmux ────────────────────────────────
  # Format exact (vérifié sur layout réelle) :
  #   CHECKSUM,WxH,0,0[TOP_ROW,BOT_ROW]
  #   TOP_ROW = WxH,0,0{W1xH,0,0,IDX1,W2xH,X2,0,IDX2,...}
  #   BOT_ROW = WxH,0,Y{W1xH,0,Y,IDX3,...}
  #   IDX     = pane_index (entier, pas %N)
  #
  # Le checksum est un CRC16 sur la string sans le préfixe "XXXX,"

  # Récupère le pane_index depuis un pane_id (%N)
  get_idx() {
    tmux list-panes -t "$win" -F '#{pane_id} #{pane_index}' 2>/dev/null \
      | awk -v id="$1" '$1==id{print $2}'
  }

  # Construit une rangée : N panes côte-à-côte
  # build_row ROW_W ROW_H ROW_X ROW_Y idx1 idx2 ...
  build_row() {
    local rw=$1 rh=$2 rx=$3 ry=$4
    shift 4
    local idxs=("$@")
    local n=${#idxs[@]}

    if [[ $n -eq 1 ]]; then
      echo "${rw}x${rh},${rx},${ry},${idxs[0]}"
      return
    fi

    # Largeur par colonne (les séparateurs comptent 1 char chacun)
    local col_w=$(( (rw - n + 1) / n ))
    local rem=$(( rw - col_w * n - (n - 1) ))
    local cells="" cx=$rx
    for (( i=0; i<n; i++ )); do
      local pw=$col_w
      [[ $i -eq $((n-1)) ]] && pw=$(( col_w + rem ))
      [[ -n "$cells" ]] && cells+=","
      cells+="${pw}x${rh},${cx},${ry},${idxs[$i]}"
      cx=$(( cx + pw + 1 ))
    done
    echo "${rw}x${rh},${rx},${ry}{${cells}}"
  }

  # Calcul checksum CRC16 tmux
  tmux_checksum() {
    local str="$1" csum=0 c
    for (( i=0; i<${#str}; i++ )); do
      c=$(printf '%d' "'${str:$i:1}")
      csum=$(( ((csum >> 1) + ((csum & 1) << 15) + c) & 0xFFFF ))
    done
    printf '%04x' "$csum"
  }

  # Récupère les indices des panes (dans l'ordre de classification)
  local top_idxs=() bot_idxs=()
  for p in "${top_panes[@]}"; do
    top_idxs+=("$(get_idx "$p")")
  done
  for p in "${bot_panes[@]}"; do
    bot_idxs+=("$(get_idx "$p")")
  done

  # Construit la layout string
  local layout_body top_row bot_row

  if [[ $n_bot -eq 0 ]]; then
    layout_body=$(build_row "$W" "$H" 0 0 "${top_idxs[@]}")
  else
    top_row=$(build_row "$W" "$top_h" 0 0 "${top_idxs[@]}")
    bot_row=$(build_row "$W" "$bot_h" 0 $(( top_h + 1 )) "${bot_idxs[@]}")
    layout_body="${W}x${H},0,0[${top_row},${bot_row}]"
  fi

  local checksum
  checksum=$(tmux_checksum "$layout_body")
  local final_layout="${checksum},${layout_body}"

  # ── Application atomique ──────────────────────────────────────────────────
  tmux select-layout -t "$win" "$final_layout" 2>/dev/null
}

# ════════════════════════════════════════════════════════════════════════════
# MODE --do-layout  (appelé en interne par --layout-watch)
# Usage : bash claude.sh --do-layout SESSION WIN_ID LEADER_PANE TEAM_CONFIG
# ════════════════════════════════════════════════════════════════════════════
if [[ "$1" == "--do-layout" ]]; then
  do_layout "$2" "$3" "$4" "$5"
  exit 0
fi

# ════════════════════════════════════════════════════════════════════════════
# MODE --debug-layout  : affiche la classification sans appliquer le layout
# Usage : bash claude.sh --debug-layout SESSION WIN_ID
# ════════════════════════════════════════════════════════════════════════════
if [[ "$1" == "--debug-layout" ]]; then
  TARGET_SESSION="$2"
  WIN_ID="$3"
  win="${TARGET_SESSION}:${WIN_ID}"

  echo "=== PANES TMUX dans $win ==="
  tmux list-panes -t "$win" -F '#{pane_id} #{pane_index} #{pane_current_command}' 2>/dev/null

  echo ""
  echo "=== LEADER PANE (index 0) ==="
  leader=$(tmux list-panes -t "$win" -F '#{pane_index} #{pane_id}' | awk '$1==0{print $2}')
  echo "leader_pane = $leader"

  echo ""
  echo "=== CONFIG.JSON trouvé ==="
  project_dir=$(tmux display-message -t "${win}.0" -p '#{pane_current_path}' 2>/dev/null)
  echo "project_dir = $project_dir"
  cfg=""
  for c in "$HOME/.claude/teams/"*/config.json; do
    [[ -f "$c" ]] || continue
    cwd=$(jq -r '.members[] | select(.name=="team-lead") | .cwd' "$c" 2>/dev/null)
    [[ -z "$cwd" || "$cwd" == "null" ]] && continue
    real_cwd=$(realpath "$cwd" 2>/dev/null) || real_cwd="${cwd%/}"
    real_proj=$(realpath "$project_dir" 2>/dev/null) || real_proj="${project_dir%/}"
    if [[ "$real_cwd" == "$real_proj" ]] || [[ "${real_cwd,,}" == "${real_proj,,}" ]]; then
      cfg="$c"
      break
    fi
  done
  echo "config = $cfg"

  if [[ -f "$cfg" ]]; then
    echo ""
    echo "=== MEMBRES avec tmuxPaneId ==="
    jq -r '.members[] | "\(.name)	\(.agentType)	\(.tmuxPaneId // "VIDE")"' "$cfg" 2>/dev/null

    echo ""
    echo "=== CLASSIFICATION ==="
    while IFS=$'	' read -r pane_id agent_type; do
      [[ -z "$pane_id" || "$pane_id" == "null" ]] && continue
      if tmux list-panes -t "$win" -F '#{pane_id}' | grep -qxF "$pane_id"; then
        if [[ "$agent_type" == "cdp" ]]; then
          echo "  TOP : $pane_id ($agent_type)"
        else
          echo "  BOT : $pane_id ($agent_type)"
        fi
      else
        echo "  ABSENT du window : $pane_id ($agent_type)"
      fi
    done < <(jq -r '.members[] | select(.tmuxPaneId != null and .tmuxPaneId != "") | [.tmuxPaneId, .agentType] | @tsv' "$cfg" 2>/dev/null)
  fi
  exit 0
fi

# ════════════════════════════════════════════════════════════════════════════
# MODE --relayout  (déclenché manuellement via bind tmux Ctrl+b R)
# Retrouve automatiquement le leader et le config.json du window courant
# Usage : bash claude.sh --relayout SESSION WIN_ID
#
# Dans ~/.tmux.conf :
#   bind R run-shell "bash /chemin/vers/claude.sh --relayout '#{session_name}' '#{window_id}'"
# ════════════════════════════════════════════════════════════════════════════
if [[ "$1" == "--relayout" ]]; then
  TARGET_SESSION="$2"
  WIN_ID="$3"
  win="${TARGET_SESSION}:${WIN_ID}"

  # Vérifie qu'il y a plusieurs panes
  pane_count=$(tmux list-panes -t "$win" 2>/dev/null | wc -l | tr -d ' ')
  if [[ "$pane_count" -le 1 ]]; then
    tmux display-message -t "$win" "Relayout : pas de team active (1 seul pane)"
    exit 0
  fi

  # Le leader = premier pane du window (pane_index == 0)
  leader_pane=$(tmux list-panes -t "$win" -F '#{pane_index} #{pane_id}' 2>/dev/null \
    | awk '$1==0{print $2}')

  # Retrouve le cwd du leader pour trouver le bon config.json
  project_dir=$(tmux display-message -t "${win}.0" -p '#{pane_current_path}' 2>/dev/null)
  team_config=$(find_team_config "$project_dir")

  if [[ -z "$team_config" ]]; then
    tmux display-message -t "$win" "Relayout : aucun config.json trouvé pour $project_dir"
    exit 0
  fi

  do_layout "$TARGET_SESSION" "$WIN_ID" "$leader_pane" "$team_config"
  tmux display-message -t "$win" "Relayout appliqué  ($(basename "$(dirname "$team_config")"))"
  exit 0
fi

# ════════════════════════════════════════════════════════════════════════════
# MODE --layout-watch  (tourne en arrière-plan pour chaque window projet)
# Surveille les changements de panes, attend le config.json, déclenche do_layout
# Usage : bash claude.sh --layout-watch SESSION WIN_ID LEADER_PANE PROJECT_DIR SCRIPT_PATH
# ════════════════════════════════════════════════════════════════════════════
if [[ "$1" == "--layout-watch" ]]; then
  TARGET_SESSION="$2"
  WIN_ID="$3"
  LEADER_PANE="$4"
  PROJECT_DIR="$5"
  SCRIPT_PATH="$6"

  prev_count=1

  while true; do
    sleep 1

    # Quitte si le window projet n'existe plus
    if ! tmux list-windows -t "$TARGET_SESSION" -F '#{window_id}' 2>/dev/null         | grep -qxF "$WIN_ID"; then
      exit 0
    fi

    pane_count=$(tmux list-panes -t "${TARGET_SESSION}:${WIN_ID}" 2>/dev/null       | wc -l | tr -d ' ')

    # Vérifie si un re-run a été demandé pendant un layout précédent
    rerun_flag="/tmp/claude_layout_${WIN_ID}.rerun"
    if [[ -f "$rerun_flag" && ! -f "/tmp/claude_layout_${WIN_ID}.lock" ]]; then
      rm -f "$rerun_flag"
      prev_count=0  # Force un relayout
    fi

    if [[ "$pane_count" -ne "$prev_count" ]]; then
      prev_count="$pane_count"
      [[ "$pane_count" -le 1 ]] && continue

      # Agents attendus = pane_count - 1 (leader sans tmuxPaneId)
      expected=$(( pane_count - 1 ))

      # Attend que TOUS les panes agents soient dans le config.json
      team_config=""
      for attempt in 1 2 3 4 5 6 7 8 9 10; do
        sleep 0.8
        cfg=$(find_team_config "$PROJECT_DIR")
        if [[ -n "$cfg" ]]; then
          has_pane=$(jq -r '
            [.members[] | select(.tmuxPaneId != null and .tmuxPaneId != "")]
            | length
          ' "$cfg" 2>/dev/null)
          if [[ "$has_pane" -ge "$expected" ]]; then
            team_config="$cfg"
            break
          fi
        fi
      done

      do_layout "$TARGET_SESSION" "$WIN_ID" "$LEADER_PANE" "$team_config"
    fi
  done
  exit 0
fi

# ════════════════════════════════════════════════════════════════════════════
# FONCTION : cleanup_orphan_teams
#   - Pas de config.json          → suppression immédiate
#   - Window tmux fermé           → suppression de toutes les teams du projet
#   - Window tmux ouvert          → on ne touche à rien
# ════════════════════════════════════════════════════════════════════════════
cleanup_orphan_teams() {
  [[ ! -d "$HOME/.claude/teams" ]] && return

  local team_dir team_name cwd project

  for team_dir in "$HOME/.claude/teams/"/; do
    [[ -d "$team_dir" ]] || continue
    team_name=$(basename "$team_dir")

    # Pas de config.json → suppression directe
    if [[ ! -f "$team_dir/config.json" ]]; then
      echo "  🧹 $team_name — pas de config.json"
      rm -rf "$team_dir"
      rm -rf "$HOME/.claude/tasks/$team_name"
      continue
    fi

    cwd=$(jq -r '.members[] | select(.name=="team-lead") | .cwd' \
      "$team_dir/config.json" 2>/dev/null)
    [[ -z "$cwd" || "$cwd" == "null" ]] && continue

    project=$(basename "$cwd")

    # Window ouvert → on ne touche à rien
    tmux list-windows -t "$SESSION" -F '#{window_name}' 2>/dev/null \
      | grep -qxF "$project" && continue

    # Window fermé → suppression
    echo "  🧹 $team_name ($project) — window fermé"
    rm -rf "$team_dir"
    rm -rf "$HOME/.claude/tasks/$team_name"
  done
}

# ════════════════════════════════════════════════════════════════════════════
# MODE --update  : met à jour le launcher depuis GitHub (préserve la config)
# ════════════════════════════════════════════════════════════════════════════
if [[ "$1" == "--update" ]]; then
  SCRIPT_PATH="$(realpath "$0")"
  echo "Mise à jour du launcher depuis GitHub (${TEMPLATE_REPO}@${TEMPLATE_BRANCH})..."
  tmp=$(mktemp)
  if curl -fsSL --ipv4 \
      "https://raw.githubusercontent.com/${TEMPLATE_REPO}/${TEMPLATE_BRANCH}/claude-launcher.sh" \
      -o "$tmp"; then
    chmod +x "$tmp"
    mv "$tmp" "$SCRIPT_PATH"
    echo "✓ Launcher mis à jour : $SCRIPT_PATH"
    echo "  Config préservée   : $CONFIG_FILE"
  else
    echo "✗ Échec du téléchargement"
    rm -f "$tmp"
    exit 1
  fi
  exit 0
fi

# ════════════════════════════════════════════════════════════════════════════
# MODE --configure  : crée ou édite le fichier de config
# ════════════════════════════════════════════════════════════════════════════
if [[ "$1" == "--configure" ]]; then
  if [[ ! -f "$CONFIG_FILE" ]]; then
    create_default_config
    echo "✓ Config créée : $CONFIG_FILE"
  else
    echo "  Config existante : $CONFIG_FILE"
  fi
  ${EDITOR:-nano} "$CONFIG_FILE"
  exit 0
fi

# ════════════════════════════════════════════════════════════════════════════
# MODE --menu  (boucle fzf dans le window [menu])
# ════════════════════════════════════════════════════════════════════════════
if [[ "$1" == "--menu" ]]; then
  SCRIPT_PATH="${2:-$(realpath "$0")}"

  if [[ -n "$GITHUB_TOKEN" ]] && command -v gh &>/dev/null; then
    export GH_TOKEN="$GITHUB_TOKEN"
    printf '%s' "$GITHUB_TOKEN" | gh auth login --with-token 2>/dev/null \
      && printf "\033[0;90m  ✓ gh auth configuré\033[0m\n"
  fi

  cleanup_orphan_teams

  while true; do
    clear
    printf "\033[1;36m  Claude Code Launcher\033[0m  —  session : %s\n" "$SESSION"
    printf "\033[0;90m  [Entrée] ouvrir  ·  [Esc] annuler  ·  Ctrl+b R relayout\033[0m\n\n"

    existing_windows=$(tmux list-windows -t "$SESSION" -F '#{window_name}' 2>/dev/null)

    entries="__quit__"$'\t'"  \033[0;90m✕ Quitter le launcher\033[0m\n"
    entries+="__new__"$'\t'"  \033[1;36m✦ Créer nouveau projet\033[0m\n"
    while IFS= read -r entry; do
      [[ -d "$GITHUB_DIR/$entry" ]] || continue
      if echo "$existing_windows" | grep -qxF "$entry"; then
        entries+="$entry"$'\t'$'\033[1;32m'"● $entry"$'\033[0;32m'" [ouvert]"$'\033[0m\n'
      else
        entries+="$entry"$'\t'"  $entry\n"
      fi
    done < <(ls -1A "$GITHUB_DIR" 2>/dev/null)

    preview_script='
      entry=$(printf "%s" "$1" | cut -f1)
      full="'"$GITHUB_DIR"'/$entry"
      wins=$(tmux list-windows -t "'"$SESSION"'" -F "#{window_name}" 2>/dev/null)
      if echo "$wins" | grep -qxF "$entry"; then
        printf "\033[1;32m  ● window ouvert : %s\033[0m\n\n" "$entry"
      fi
      if [ -d "$full" ]; then
        ls -lhp --color=always "$full" 2>/dev/null | head -20
      fi
    '

    selected=$(printf "%b" "$entries" | fzf \
      --ansi \
      --delimiter=$'\t' \
      --with-nth=2 \
      --nth=1 \
      --prompt "  $GITHUB_DIR/ > " \
      --height=70% --border \
      --preview-window=right:45%:wrap \
      --preview "bash -c '$preview_script' -- {}" \
      --color 'hl:#5DCAA5,hl+:#1D9E75' \
      --bind 'esc:abort' \
      --bind 'left-click:accept')

    [[ -z "$selected" ]] && { sleep 0.2; continue; }

    project="${selected%%$'\t'*}"

    if [[ "$project" == "__quit__" ]]; then
      other_wins=$(tmux list-windows -t "$SESSION" -F '#{window_name}' 2>/dev/null \
        | grep -v '^\[menu\]$' | wc -l | tr -d ' ')
      if [[ "$other_wins" -gt 0 ]]; then
        tmux detach-client
      else
        tmux kill-session -t "$SESSION"
      fi
      exit 0
    fi

    if [[ "$project" == "__new__" ]]; then
      clear
      printf "\033[1;36m  Créer nouveau projet\033[0m\n\n"
      read -rp "  Nom du projet : " project
      [[ -z "$project" ]] && continue
      read -rp "  URL git remote (optionnel, Entrée pour ignorer) : " git_remote
      project_dir="$GITHUB_DIR/$project"
      if [[ -d "$project_dir" ]]; then
        printf "\033[1;31m  ✗ Le répertoire existe déjà : %s\033[0m\n" "$project_dir"
        sleep 2
        continue
      fi
      mkdir -p "$project_dir"
      git -C "$project_dir" init -q
      [[ -n "$git_remote" ]] && git -C "$project_dir" remote add origin "$git_remote"
      printf "\n  \033[1;32m✓ Projet créé : %s\033[0m\n\n" "$project_dir"
      sleep 1
    else
      project_dir="$GITHUB_DIR/$project"
    fi

    if tmux list-windows -t "$SESSION" -F '#{window_name}' 2>/dev/null \
        | grep -qxF "$project"; then
      tmux select-window -t "$SESSION:$project"
    else
      tmux new-window -t "$SESSION" -n "$project"
      style_project_window "$SESSION" "$project"

      win_id=$(tmux list-windows -t "$SESSION" -F '#{window_name} #{window_id}' 2>/dev/null \
        | awk -v p="$project" '$1==p{print $2}')
      leader_pane=$(tmux list-panes -t "$SESSION:$project" -F '#{pane_id}' 2>/dev/null \
        | head -1)

      # Toujours récupérer la dernière version de init-project.md
      mkdir -p "$project_dir/.claude/commands"
      local init_cmd="$project_dir/.claude/commands/init-project.md"
      local _b64
      if _b64=$(gh api "repos/${TEMPLATE_REPO}/contents/init-project.md?ref=${TEMPLATE_BRANCH}" \
          --jq '.content' 2>/dev/null); then
        printf '%s' "$_b64" | base64 -d > "$init_cmd"
      elif [[ ! -f "$init_cmd" ]]; then
        tmux send-keys -t "$SESSION:$project" \
          "echo '⚠  init-project.md introuvable (GitHub inaccessible) — /init-project indisponible'" \
          Enter
      fi

      CLAUDE_EXPORTS=$(build_claude_exports)
      tmux send-keys -t "$SESSION:$project" \
        "cd '$project_dir'${CLAUDE_EXPORTS}
claude ${CLAUDE_OPTIONS}" \
        Enter

      tmux run-shell -b "bash '$SCRIPT_PATH' --layout-watch '$SESSION' '$win_id' '$leader_pane' '$project_dir' '$SCRIPT_PATH'"

      tmux select-window -t "$SESSION:$project"
    fi

    sleep 0.2
  done
  exit 0
fi

# ════════════════════════════════════════════════════════════════════════════
# FONCTION : setup_tmux_style SESSION
# Configure la status bar et les styles de fenêtres :
#   [menu]           → orange, italique
#   projet inactif   → gris discret
#   fenêtre active   → blanc gras sur fond bleu, bien visible
# ════════════════════════════════════════════════════════════════════════════
setup_tmux_style() {
  local s="$1"

  # ── Status bar ──────────────────────────────────────────────────────────
  tmux set-option -t "$s" status on
  tmux set-option -t "$s" status-position bottom
  tmux set-option -t "$s" status-style        "bg=colour236,fg=colour250"
  tmux set-option -t "$s" status-left         "#[fg=colour81,bold] ❯ Claude Hub #[fg=colour240,nobold] │ "
  tmux set-option -t "$s" status-left-length  25
  tmux set-option -t "$s" status-right        "#[fg=colour240] %H:%M "
  tmux set-option -t "$s" status-right-length 10
  tmux set-option -t "$s" window-status-separator "  "
  tmux set-option -t "$s" window-status-format         " #W "
  tmux set-option -t "$s" window-status-current-format " ◆ #W "

  # ── Style [menu] ────────────────────────────────────────────────────────
  # inactif → orange italique  |  actif → noir sur orange vif
  tmux set-window-option -t "$s:[menu]" window-status-style         "fg=colour214,italics,bg=colour236"
  tmux set-window-option -t "$s:[menu]" window-status-current-style "bold,fg=colour232,bg=colour208,italics"

  # ── Style fenêtres projet existantes ────────────────────────────────────
  # Appliqué explicitement sur chaque fenêtre (l'héritage session est instable)
  tmux list-windows -t "$s" -F '#{window_name}' | grep -v '^\[menu\]$' | while read -r win; do
    style_project_window "$s" "$win"
  done
}

# Applique le style projet sur une fenêtre (inactif gris / actif blanc-bleu)
style_project_window() {
  local s="$1" win="$2"
  tmux set-window-option -t "$s:$win" window-status-style         "fg=colour242,bg=colour236"
  tmux set-window-option -t "$s:$win" window-status-current-style "bold,fg=colour255,bg=colour27"
}

# ════════════════════════════════════════════════════════════════════════════
# POINT D'ENTRÉE PRINCIPAL
# ════════════════════════════════════════════════════════════════════════════
SCRIPT_PATH="$(realpath "$0")"

# Auto-update silencieux au lancement (sauf si --no-update)
[[ "$NO_AUTO_UPDATE" == "0" ]] && auto_update

# Premier lancement : créer la config par défaut si absente
if [[ ! -f "$CONFIG_FILE" ]]; then
  create_default_config
  printf "\033[1;33m  Premier lancement — config créée : %s\033[0m\n" "$CONFIG_FILE"
  printf "  Renseigner GITHUB_TOKEN et GITHUB_DIR avant de continuer.\n\n"
  printf "  Lancer  \033[1m%s --configure\033[0m  pour éditer.\n" "$SCRIPT_PATH"
  printf "  Lancer  \033[1m%s\033[0m           pour démarrer avec les valeurs par défaut.\n\n" "$SCRIPT_PATH"
  read -rp "  Continuer maintenant avec les valeurs par défaut ? [o/N] " ans
  [[ "${ans,,}" != "o" && "${ans,,}" != "oui" && "${ans,,}" != "y" && "${ans,,}" != "yes" ]] && exit 0
  load_config
fi

# Si on arrive ici sans argument reconnu et que la session existe → attach
if tmux has-session -t "$SESSION" 2>/dev/null; then
  setup_tmux_style "$SESSION"
  exec tmux attach-session -t "$SESSION"
fi

tmux new-session -d -s "$SESSION" -n "[menu]"

setup_tmux_style "$SESSION"

tmux send-keys -t "$SESSION:[menu]" \
  "bash '$SCRIPT_PATH' --menu '$SCRIPT_PATH'" Enter

exec tmux attach-session -t "$SESSION"
