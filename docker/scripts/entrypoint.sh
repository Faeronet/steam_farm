#!/bin/bash
set -e

echo "=== Steam Farm Sandbox — $GAME_TYPE ==="

# Machine ID for HWID spoofing
if [ -n "$MACHINE_ID" ]; then
    echo "$MACHINE_ID" | sudo tee /etc/machine-id > /dev/null 2>&1 || true
fi

# XDG runtime dir
export XDG_RUNTIME_DIR=/tmp/runtime-steam
mkdir -p "$XDG_RUNTIME_DIR"

# Steam SDK symlinks
if [ -f /home/steam/.steam/sdk64/steamclient.so ]; then
    echo "[OK] Steam SDK mounted"
elif [ -f /opt/steam/linux64/steamclient.so ]; then
    ln -sf /opt/steam/linux64/steamclient.so /home/steam/.steam/sdk64/steamclient.so 2>/dev/null || true
    echo "[OK] Steam SDK linked from steamcmd"
fi

# Ensure Steam directories exist
mkdir -p /home/steam/.steam/steam 2>/dev/null || true
ln -sf /home/steam/.steam/sdk64 /home/steam/.steam/sdk64 2>/dev/null || true

# Start virtual display
echo "[1/5] Starting Xvfb on ${DISPLAY:-:99}..."
Xvfb ${DISPLAY:-:99} -screen 0 1024x768x24 -ac +extension GLX +render -noreset &
XVFB_PID=$!
sleep 2

if ! kill -0 $XVFB_PID 2>/dev/null; then
    echo "ERROR: Xvfb failed to start"
    exit 1
fi
export DISPLAY=${DISPLAY:-:99}

# Start VNC server
echo "[2/5] Starting x11vnc on port 5900..."
x11vnc -display $DISPLAY -nopw -forever -shared -rfbport 5900 -bg -o /tmp/x11vnc.log 2>&1 || true
sleep 1

# Determine game app ID and install dir
if [ "$GAME_TYPE" = "cs2" ]; then
    APPID=730
    INSTALL_DIR="/home/steam/Steam/steamapps/common/Counter-Strike Global Offensive"
    GAME_BIN="game/bin/linuxsteamrt64/cs2"
    LAUNCH_OPTS="-novid -nojoy -low -w 640 -h 480 +fps_max 15 +cl_disablehtmlmotd 1 -nosound -insecure"
elif [ "$GAME_TYPE" = "dota2" ]; then
    APPID=570
    INSTALL_DIR="/home/steam/Steam/steamapps/common/dota 2 beta"
    GAME_BIN="game/bin/linuxsteamrt64/dota2"
    LAUNCH_OPTS="-novid -nojoy -low -w 640 -h 480 +fps_max 15 -nosound"
else
    echo "ERROR: Unknown GAME_TYPE=$GAME_TYPE"
    exit 1
fi

# Download game if needed
if [ "$SKIP_DOWNLOAD" = "1" ] && [ -d "$INSTALL_DIR" ]; then
    echo "[3/5] Game files mounted from host — skipping download"
else
    echo "[3/5] Downloading $GAME_TYPE (AppID $APPID) via SteamCMD..."
    if [ -z "$STEAM_USER" ] || [ -z "$STEAM_PASS" ]; then
        echo "ERROR: STEAM_USER and STEAM_PASS must be set"
        exit 1
    fi
    /opt/steam/steamcmd.sh \
        +@sSteamCmdForcePlatformType linux \
        +force_install_dir "$INSTALL_DIR" \
        +login "$STEAM_USER" "$STEAM_PASS" \
        +app_update $APPID validate \
        +quit || echo "WARNING: SteamCMD download had issues, continuing..."
fi

# Copy configs (non-fatal)
if [ "$GAME_TYPE" = "cs2" ] && [ -f /home/steam/.autoexec.cfg ]; then
    mkdir -p "$INSTALL_DIR/game/csgo/cfg" 2>/dev/null || true
    cp /home/steam/.autoexec.cfg "$INSTALL_DIR/game/csgo/cfg/autoexec.cfg" 2>/dev/null || true
    [ -f /home/steam/.cs2_video.txt ] && cp /home/steam/.cs2_video.txt "$INSTALL_DIR/game/csgo/cfg/video.txt" 2>/dev/null || true
elif [ "$GAME_TYPE" = "dota2" ] && [ -f /home/steam/.autoexec.cfg ]; then
    mkdir -p "$INSTALL_DIR/game/dota/cfg" 2>/dev/null || true
    cp /home/steam/.autoexec.cfg "$INSTALL_DIR/game/dota/cfg/autoexec.cfg" 2>/dev/null || true
fi

# Start SteamCMD as background Steam session
echo "[4/5] Starting Steam session via SteamCMD..."
if [ -n "$STEAM_USER" ] && [ -n "$STEAM_PASS" ]; then
    # Create a steam_appid.txt for the game
    echo "$APPID" > "$INSTALL_DIR/steam_appid.txt" 2>/dev/null || true
    echo "$APPID" > /home/steam/steam_appid.txt 2>/dev/null || true

    # Run steamcmd login in background to maintain session
    /opt/steam/steamcmd.sh +login "$STEAM_USER" "$STEAM_PASS" +app_info_update 1 +sleep 999999 &
    STEAM_PID=$!
    echo "SteamCMD session started (PID=$STEAM_PID), waiting 10s for initialization..."
    sleep 10
fi

# Launch the game
echo "[5/5] Launching $GAME_TYPE (AppID $APPID)..."
export SteamAppId=$APPID
export SteamGameId=$APPID
export LD_LIBRARY_PATH="$INSTALL_DIR/game/bin/linuxsteamrt64:${LD_LIBRARY_PATH:-}"

if [ -f "$INSTALL_DIR/$GAME_BIN" ]; then
    cd "$INSTALL_DIR"
    echo "Running: ./$GAME_BIN $LAUNCH_OPTS"
    exec "./$GAME_BIN" $LAUNCH_OPTS
else
    echo "Game binary not found: $INSTALL_DIR/$GAME_BIN"
    echo "Contents of $INSTALL_DIR:"
    ls -la "$INSTALL_DIR/" 2>/dev/null || echo "(directory not found)"
    sleep infinity
fi
