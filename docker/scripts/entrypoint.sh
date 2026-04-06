#!/bin/bash
set -e

echo "=== Steam Farm Sandbox — $GAME_TYPE ==="

# Machine ID for HWID spoofing
if [ -n "$MACHINE_ID" ]; then
    echo "$MACHINE_ID" | sudo tee /etc/machine-id > /dev/null 2>&1 || true
fi

# Start virtual display
echo "[1/4] Starting Xvfb on ${DISPLAY:-:99}..."
Xvfb ${DISPLAY:-:99} -screen 0 800x600x24 -ac +extension GLX +render -noreset &
XVFB_PID=$!
sleep 2

if ! kill -0 $XVFB_PID 2>/dev/null; then
    echo "ERROR: Xvfb failed to start"
    exit 1
fi

export DISPLAY=${DISPLAY:-:99}

# Start VNC server
echo "[2/4] Starting x11vnc on port 5900..."
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

# Install/update game via SteamCMD
echo "[3/4] Installing $GAME_TYPE (AppID $APPID) via SteamCMD..."
if [ -z "$STEAM_USER" ] || [ -z "$STEAM_PASS" ]; then
    echo "ERROR: STEAM_USER and STEAM_PASS must be set"
    exit 1
fi

GUARD_CODE=""
if [ -n "$STEAM_GUARD_CODE" ]; then
    GUARD_CODE="+set_steam_guard_code $STEAM_GUARD_CODE"
fi

/opt/steam/steamcmd.sh \
    +@sSteamCmdForcePlatformType linux \
    +force_install_dir "$INSTALL_DIR" \
    +login "$STEAM_USER" "$STEAM_PASS" $GUARD_CODE \
    +app_update $APPID validate \
    +quit || {
    echo "WARNING: SteamCMD install failed, checking if game exists..."
}

# Copy configs
if [ "$GAME_TYPE" = "cs2" ] && [ -f /home/steam/.autoexec.cfg ]; then
    mkdir -p "$INSTALL_DIR/game/csgo/cfg"
    cp /home/steam/.autoexec.cfg "$INSTALL_DIR/game/csgo/cfg/autoexec.cfg"
    if [ -f /home/steam/.cs2_video.txt ]; then
        mkdir -p "$INSTALL_DIR/game/csgo/cfg"
        cp /home/steam/.cs2_video.txt "$INSTALL_DIR/game/csgo/cfg/video.txt"
    fi
elif [ "$GAME_TYPE" = "dota2" ] && [ -f /home/steam/.autoexec.cfg ]; then
    mkdir -p "$INSTALL_DIR/game/dota/cfg"
    cp /home/steam/.autoexec.cfg "$INSTALL_DIR/game/dota/cfg/autoexec.cfg"
fi

# Launch the game
echo "[4/4] Launching $GAME_TYPE..."
if [ -f "$INSTALL_DIR/$GAME_BIN" ]; then
    cd "$INSTALL_DIR"
    exec "./$GAME_BIN" $LAUNCH_OPTS
else
    echo "Game binary not found at $INSTALL_DIR/$GAME_BIN"
    echo "Attempting launch via Steam runtime..."
    /opt/steam/steamcmd.sh \
        +login "$STEAM_USER" "$STEAM_PASS" \
        +app_run $APPID $LAUNCH_OPTS \
        +quit &
    
    # Keep container alive
    exec tail -f /dev/null
fi
