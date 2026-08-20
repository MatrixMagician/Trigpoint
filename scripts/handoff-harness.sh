#!/usr/bin/env bash
# Drives docs/handoff-test-matrix.md steps 1-10 against a real terminal emulator
# on a real Wayland session, so the per-emulator runs the matrix asks for do not
# all have to be typed by hand. It does not replace looking: the visual steps
# (colour, reflow, the alternate screen) are captured as screenshots for a human
# — or an agent that can read images — to judge.
#
#   usage: scripts/handoff-harness.sh <kitty|alacritty|foot|konsole|ghostty> <direct|nested> [outdir]
#
# Needs: ydotoold running with YDOTOOL_SOCKET set, spectacle (KDE) or grim, and a
# real logged-in Wayland session. Keystrokes go to whatever window has focus, so
# leave the machine alone while it runs.
#
# ponytail: keystrokes are timed with sleeps rather than synchronised against the
# emulator; sync on tmux state where there is tmux state to sync on. If a run
# goes flaky, raise STEP_PAUSE before reaching for anything cleverer.
set -uo pipefail

TERMINAL=${1:?terminal}
MODE=${2:?direct|nested}
OUT=${3:-/tmp/handoff-harness/$TERMINAL-$MODE}
TRIG=${TRIG:-$(command -v trig || echo ./trig)}
STEP_PAUSE=${STEP_PAUSE:-1.2}
COLS=100 ROWS=32

export YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/tmp/.ydotool_socket}
rm -rf "$OUT"; mkdir -p "$OUT"
REPORT=$OUT/report.txt
XDGDIR=$OUT/xdg; mkdir -p "$XDGDIR"

say()  { printf '%s\n' "$*" | tee -a "$REPORT"; }
step() { say ""; say "== step $*"; }
ok()   { say "PASS  $*"; }
bad()  { say "FAIL  $*"; }
info() { say "INFO  $*"; }

shot() { # shot <name>
  sleep 0.4
  if command -v spectacle >/dev/null; then
    spectacle -b -n -a -o "$OUT/$1.png" >/dev/null 2>&1
  else
    grim "$OUT/$1.png" >/dev/null 2>&1
  fi
  [ -s "$OUT/$1.png" ] || bad "screenshot $1 not captured"
}
type_()  { ydotool type "$1"; sleep "$STEP_PAUSE"; }
key()    { ydotool key "$@"; sleep "$STEP_PAUSE"; }
enter()  { key 28:1 28:0; }
line()   { type_ "$1"; enter; }

K_ESC=1 K_ALT=56 K_CTRL=29 K_META=125
K_LBRACK=26 K_Q=16 K_H=35 K_J=36 K_K=37 K_L=38 K_Z=44 K_N=49
K_PGUP=104 K_LEFT=105 K_DOWN=108

sessions() { tmux list-sessions -F '#{session_name} attached=#{session_attached}' 2>/dev/null; }
# Trigpoint holds a tmux control-mode client open for the whole run, and that
# counts towards #{session_attached}. Only a client that is not in control mode
# is the handoff having really taken the terminal.
clients() { tmux list-clients -t "$1" -F '#{client_control_mode}' 2>/dev/null | grep -c '^0$'; }
trig_session() { tmux list-sessions -F '#{session_name}' 2>/dev/null | grep '^trig_' | head -1; }
# Trigpoint's nodes live on the default tmux socket, which is also where the
# user's real work is, so the harness kills only what it made: it refuses to
# start if a trig_* or outer session is already there, and everything matching
# afterwards is therefore its own.
harness_sessions() { tmux list-sessions -F '#{session_name}' 2>/dev/null | grep '^trig_\|^outer$'; }
refuse_if_sessions_exist() {
  existing=$(harness_sessions)
  [ -z "$existing" ] && return 0
  echo "refusing to run: these tmux sessions are already on the default socket" >&2
  echo "$existing" | sed 's/^/  /' >&2
  echo "the harness cannot tell them from its own and would kill them. Quit them first." >&2
  exit 3
}
cleanup_sessions() { harness_sessions | xargs -r -n1 tmux kill-session -t 2>/dev/null; }

# ---------------------------------------------------------------- inner script
# Runs inside the emulator. Records the terminal's line discipline either side of
# the whole run, which is the release-blocking property (SPEC 14).
cat > "$OUT/inner.sh" <<INNER
#!/bin/sh
export XDG_CONFIG_HOME=$XDGDIR XDG_STATE_HOME=$XDGDIR
stty -a > $OUT/stty.before 2>&1
echo "SCROLLBACK-MARKER-BEFORE-TRIG"
echo "cols=\$(tput cols) lines=\$(tput lines)"
INNER
if [ "$MODE" = nested ]; then
  # tmux saves and restores the outer terminal's termios itself, so stty either
  # side of the tmux client proves nothing about Trigpoint. The pty that has to
  # come back canonical is the outer tmux pane, measured from inside it.
  cat >> "$OUT/inner.sh" <<INNER
tmux new-session -s outer "sh -c 'stty -a > $OUT/stty.inner.before 2>&1; $TRIG; stty -a > $OUT/stty.inner.after 2>&1'"
INNER
else
  cat >> "$OUT/inner.sh" <<INNER
$TRIG
INNER
fi
cat >> "$OUT/inner.sh" <<INNER
echo "TRIG-EXITED rc=\$?"
stty -a > $OUT/stty.after 2>&1
echo HARNESS-DONE > $OUT/done.marker
sleep 600
INNER
chmod +x "$OUT/inner.sh"

launch() {
  case $TERMINAL in
    kitty)     kitty -T trigharness -o initial_window_width=${COLS}c \
                     -o initial_window_height=${ROWS}c -o confirm_os_window_close=0 \
                     "$OUT/inner.sh" & ;;
    alacritty) alacritty -T trigharness -o "window.dimensions.columns=$COLS" \
                     -o "window.dimensions.lines=$ROWS" -e "$OUT/inner.sh" & ;;
    foot)      foot -T trigharness -W "${COLS}x${ROWS}" "$OUT/inner.sh" & ;;
    konsole)   konsole -p tabtitle=trigharness -e "$OUT/inner.sh" & ;;
    ghostty)   ghostty --title=trigharness --window-width=$COLS --window-height=$ROWS \
                     -e "$OUT/inner.sh" & ;;
    *) echo "unknown terminal: $TERMINAL" >&2; exit 2 ;;
  esac
  TERM_PID=$!
}

finish() { kill "${TERM_PID:-0}" 2>/dev/null; pkill -f "$OUT/inner.sh" 2>/dev/null; cleanup_sessions; }

say "terminal: $TERMINAL   mode: $MODE   trig: $TRIG"
say "version:  $(case $TERMINAL in ghostty) ghostty +version 2>&1|head -2|tr '\n' ' ';; *) "$TERMINAL" --version 2>&1|head -1;; esac)"
say "tmux:     $(tmux -V)"
refuse_if_sessions_exist
# Armed only once the refusal above has passed, so that a run which declines to
# start cannot clean up the sessions it declined to touch.
trap finish EXIT
launch
sleep 4

# ------------------------------------------------------------------ 1. the map
step "1 start and create a node"
shot 01-map-empty
key $K_N:1 $K_N:0
type_ "probe"; enter
shot 02-map-card
if [ "$MODE" = nested ]; then
  sessions | grep -q '^outer attached=1' && ok "outer tmux session attached" || bad "no attached outer session"
fi

# --------------------------------------------------------------- 2. the attach
step "2 enter the node"
enter
S=""
for _ in $(seq 1 20); do S=$(trig_session); [ -n "$S" ] && [ "$(clients "$S")" -ge 1 ] && break; sleep 0.5; done
say "$(sessions | sed 's/^/      /')"
if [ "$(clients "$S")" -ge 1 ]; then ok "node session $S has a real (non control-mode) client"
else bad "node session not attached"; fi
BIND=$(tmux list-keys -T root 2>/dev/null | grep -i escape)
say "      ${BIND:-<no root escape binding>}"
if [ -z "$S" ]; then
  bad "no node session to check the detach binding against"
else
  case $BIND in *"detach-client -s =$S"*) ok "detach binding targets the session (ADR 0002)";;
                *) bad "detach binding missing or not session-scoped";; esac
fi
shot 03-attached
line 'echo "inside: TERM=$TERM cols=$(tput cols) lines=$(tput lines)"'
shot 04-inside-node

# --------------------------------------------------- 3. a full-screen program
step "3 full-screen application (vim)"
line "vim"
sleep 1
shot 05-vim
line ":q"
sleep 1
shot 06-after-vim
tmux capture-pane -p -t "$S" 2>/dev/null | tail -3 | sed 's/^/      /' | tee -a "$REPORT" >/dev/null

# ------------------------------------------------------------------ 4. colour
step "4 colour"
line 'tput setaf 208; echo 256-colour-orange; tput sgr0'
line 'tput bold; tput smul; tput setaf 1; echo bold-underlined-red; tput sgr0' 
shot 07-colour
tmux capture-pane -p -e -t "$S" 2>/dev/null | grep -a 'orange\|underlined' | cat -v \
  | sed 's/^/      /' | tee -a "$REPORT" >/dev/null

# ------------------------------------------------------------------ 5. resize
step "5 resize"
# The resize is driven by KWin's "tile left" / "restore" shortcuts, because no
# emulator here offers a way to resize its own window from outside. On another
# compositor Meta+Left is usually "focus left" instead — which would move focus
# off the terminal, and every later step would then be typed into someone else's
# window. So a resize that does not land ends the run rather than carrying on.
BEFORE=$(tmux display-message -p -t "$S" '#{client_width}x#{client_height}' 2>/dev/null)
[ -n "$BEFORE" ] || { bad "no attached client to measure — stopping before the resize"; exit 1; }
key $K_META:1 $K_LEFT:1 $K_LEFT:0 $K_META:0     # KWin: tile to left half
sleep 1
AFTER=$(tmux display-message -p -t "$S" '#{client_width}x#{client_height}' 2>/dev/null)
shot 08-resized
line 'echo resized-to-$(tput cols)x$(tput lines)'
key $K_META:1 $K_DOWN:1 $K_DOWN:0 $K_META:0     # restore
sleep 1
BACK=$(tmux display-message -p -t "$S" '#{client_width}x#{client_height}' 2>/dev/null)
shot 09-resized-back
say "      client size: $BEFORE -> $AFTER -> $BACK"
if [ -n "$AFTER" ] && [ "$BEFORE" != "$AFTER" ]; then
  ok "session reflowed with the window"
else
  bad "client size did not change: the compositor did not tile the window."
  bad "stopping — the keystrokes may no longer be going to the terminal."
  exit 1
fi
# KWin's restore does not always give back the original geometry, so this is
# recorded rather than asserted; what step 9 needs is only that it is legible.
[ "$BACK" = "$BEFORE" ] && info "window restored to its original size" \
                        || info "window restored to $BACK, not the original $BEFORE"

# --------------------------------------------------------------- 6. copy-mode
step "6 copy-mode"
line "seq 1 200"
# ~/.tmux.conf is the user's own and is not isolated, so read the prefix rather
# than assuming C-b: sending the wrong one would report a Trigpoint failure for
# a keystroke that never asked tmux for anything.
LETTERS="a30 b48 c46 d32 e18 f33 g34 h35 i23 j36 k37 l38 m50 n49 o24 p25 q16 r19 s31 t20 u22 v47 w17 x45 y21 z44"
TMUX_PREFIX=$(tmux show-options -gv prefix 2>/dev/null)
PREFIX_CODE=$(case $TMUX_PREFIX in
  C-?) for e in $LETTERS; do [ "${e%%[0-9]*}" = "${TMUX_PREFIX#C-}" ] && echo "${e#?}"; done ;;
esac)
if [ -z "$PREFIX_CODE" ]; then
  info "step 6 skipped: tmux prefix is '$TMUX_PREFIX', which this harness cannot send"
else
  # Nested, the outer tmux takes the first prefix; the inner one needs a second.
  prefix() { key $K_CTRL:1 $PREFIX_CODE:1 $PREFIX_CODE:0 $K_CTRL:0; }
  prefix
  [ "$MODE" = nested ] && prefix
  key $K_LBRACK:1 $K_LBRACK:0
  key $K_PGUP:1 $K_PGUP:0
  MODEKEYS=$(tmux display-message -p -t "$S" '#{pane_in_mode}' 2>/dev/null)
  shot 10-copy-mode
  [ "$MODEKEYS" = 1 ] && ok "prefix ($TMUX_PREFIX) reached tmux, copy-mode entered" \
                      || bad "copy-mode not entered (pane_in_mode=$MODEKEYS)"
  key $K_Q:1 $K_Q:0
  [ "$(tmux display-message -p -t "$S" '#{pane_in_mode}' 2>/dev/null)" = 0 ] \
    && ok "copy-mode exited" || bad "copy-mode did not exit"
fi

# -------------------------------------------------------------- 7. detach key
step "7 the detach key"
key $K_ALT:1 $K_ESC:1 $K_ESC:0 $K_ALT:0
sleep 1
shot 11-back-on-map
say "$(sessions | sed 's/^/      /')"
[ "$(clients "$S")" = 0 ] && ok "node session detached" || bad "node session still attached"
if [ "$MODE" = nested ]; then
  sessions | grep -q '^outer attached=1' \
    && ok "outer session still attached — ADR 0002 failure mode did not happen" \
    || bad "OUTER SESSION DETACHED — the ADR 0002 failure mode"
fi
tmux list-keys -T root 2>/dev/null | grep -qi escape \
  && bad "detach binding outlived the attach" || ok "detach binding removed on return"

# --------------------------------------------------- 8. the map has the keys
step "8 the map has the keyboard back"
for k in $K_H $K_J $K_K $K_L; do key $k:1 $k:0; done
key $K_Z:1 $K_Z:0; key $K_Z:1 $K_Z:0
shot 12-map-after-keys
say "      second trip through the handoff"
enter
for _ in $(seq 1 20); do [ "$(clients "$S")" -ge 1 ] && break; sleep 0.5; done
[ "$(clients "$S")" -ge 1 ] && ok "second attach" || bad "second attach failed"
shot 13-attached-again
key $K_ALT:1 $K_ESC:1 $K_ESC:0 $K_ALT:0
sleep 1
[ "$(clients "$S")" = 0 ] && ok "second detach" || bad "second detach failed"
shot 14-map-again

# ------------------------------------------------------ 9. quit and the termios
step "9 quit, and the state of the terminal"
key $K_Q:1 $K_Q:0
for _ in $(seq 1 20); do [ -f "$OUT/done.marker" ] && break; sleep 0.5; done
sleep 1
shot 15-after-quit
nogeom() { sed -E 's/rows [0-9]+; columns [0-9]+; //' "$1"; }
check_termios() { # check_termios <label> <before> <after>
  if [ ! -f "$3" ]; then bad "$1: never came back to the shell (no $(basename "$3"))"; return; fi
  BADFLAGS=$(tr ' ;' '\n\n' < "$3" | grep -Ex '\-(icanon|echo)')
  if [ -z "$BADFLAGS" ]; then ok "$1: left in canonical mode with echo"
  else bad "$1: RAW MODE LEFT BEHIND: $BADFLAGS"; fi
  if diff <(nogeom "$2") <(nogeom "$3") >/dev/null; then
    ok "$1: termios identical either side of the run (window geometry aside)"
  else
    bad "$1: termios changed: $(diff <(nogeom "$2") <(nogeom "$3") | tr '\n' ' ' | cut -c1-200)"
  fi
}
check_termios "terminal" "$OUT/stty.before" "$OUT/stty.after"
# Nested, the check above is nearly free: tmux saves and restores the outer
# terminal's termios on its way out whatever Trigpoint did. The pty that carries
# the evidence is the outer tmux pane the handoff actually ran in.
[ "$MODE" = nested ] && check_termios "outer tmux pane" "$OUT/stty.inner.before" "$OUT/stty.inner.after"

# ------------------------------------------- 10. sessions outlive Trigpoint
step "10 sessions outlive Trigpoint"
say "$(sessions | sed 's/^/      /')"
sessions | grep -q "^$S " && ok "$S still listed after quit" || bad "$S gone after quit"

say ""
say "screenshots: $OUT/*.png   (01 map, 03 attached, 07 colour, 08 resized, 10 copy-mode, 11 back on map, 15 after quit)"
say "summary: $(grep -c '^PASS' "$REPORT") pass, $(grep -c '^FAIL' "$REPORT") fail"
exit 0
