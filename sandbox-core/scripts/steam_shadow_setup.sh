#!/bin/bash
# Builds $HOME/.local/share/Steam shadow (same layout as snap inner script).
# Required env: HOME, STEAM_REAL
# Optional: COMPAT_APP_ID, COMMON_FOLDER, TRY_FUSE_CS2_OVERLAY (0|1), SNAP_COMMON

: "${HOME:?}"
: "${STEAM_REAL:?}"

COMPAT_APP_ID="${COMPAT_APP_ID:-}"
COMMON_FOLDER="${COMMON_FOLDER:-}"
TRY_FUSE_CS2_OVERLAY="${TRY_FUSE_CS2_OVERLAY:-0}"
SNAP_COMMON="${SNAP_COMMON:-}"

STEAM_LOCAL="$HOME/.local/share/Steam"

mkdir -p "$HOME/.sfarm-tmp"
export TMPDIR="$HOME/.sfarm-tmp"
export TMP="$TMPDIR"
export TEMP="$TMPDIR"
export TEMPDIR="$TMPDIR"

mkdir -p "$STEAM_LOCAL"

for item in "$STEAM_REAL"/*; do
    [ -e "$item" ] || continue
    name=$(basename "$item")
    [ "$name" = "ubuntu12_32" ] && continue
    [ "$name" = "ubuntu12_64" ] && continue
    [ "$name" = "config" ] && continue
    [ "$name" = "steamapps" ] && continue
    [ "$name" = "userdata" ] && continue
    [ "$name" = "registry.vdf" ] && continue
    [ "$name" = "registry.vdf.bak" ] && continue
    [ "$name" = "steam.pid" ] && continue
    if [ "$name" = "steam.sh" ]; then
        cp "$item" "$STEAM_LOCAL/$name"
        chmod +x "$STEAM_LOCAL/$name"
    else
        ln -sfn "$item" "$STEAM_LOCAL/$name" 2>/dev/null || true
    fi
done

if [ -d "$STEAM_REAL/userdata" ]; then
    cp -a "$STEAM_REAL/userdata" "$STEAM_LOCAL/userdata"
fi
for f in registry.vdf registry.vdf.bak; do
    if [ -f "$STEAM_REAL/$f" ]; then cp -a "$STEAM_REAL/$f" "$STEAM_LOCAL/$f"; fi
done
rm -f "$STEAM_LOCAL/steam.pid" 2>/dev/null || true

mkdir -p "$STEAM_LOCAL/steamapps"
for item in "$STEAM_REAL/steamapps"/*; do
    [ -e "$item" ] || continue
    name=$(basename "$item")
    case "$name" in
        appmanifest_*.acf|libraryfolders.vdf)
            cp "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null || true
            ;;
        compatdata)
            if [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ]; then
                :
            else
                ln -sfn "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null || true
            fi
            ;;
        common)
            if [ -n "$COMMON_FOLDER" ] && [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ]; then
                mkdir -p "$STEAM_LOCAL/steamapps/common"
                for sub in "$item"/*; do
                    [ -e "$sub" ] || continue
                    sname=$(basename "$sub")
                    M="$STEAM_LOCAL/steamapps/common/$sname"
                    if [ "$sname" = "$COMMON_FOLDER" ] && [ -d "$sub" ]; then
                        if [ "$TRY_FUSE_CS2_OVERLAY" = "1" ] && command -v fuse-overlayfs >/dev/null 2>&1; then
                            U="$HOME/.sfarm-cs2-overlay-upper"
                            W="$HOME/.sfarm-cs2-overlay-work"
                            mkdir -p "$U" "$W"
                            rm -rf "$M" 2>/dev/null || true
                            mkdir -p "$M"
                            if fuse-overlayfs -o "lowerdir=$sub,upperdir=$U,workdir=$W" "$M" 2>/dev/null; then
                                :
                            else
                                ln -sfn "$sub" "$M"
                            fi
                        else
                            ln -sfn "$sub" "$M"
                        fi
                    else
                        ln -sfn "$sub" "$M" 2>/dev/null || true
                    fi
                done
            else
                ln -sfn "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null || true
            fi
            ;;
        *)
            ln -sfn "$item" "$STEAM_LOCAL/steamapps/$name" 2>/dev/null || true
            ;;
    esac
done

if [ -n "$COMPAT_APP_ID" ] && [ "$COMPAT_APP_ID" != "0" ] && [ -d "$STEAM_REAL/steamapps/compatdata" ]; then
    rm -rf "$STEAM_LOCAL/steamapps/compatdata" 2>/dev/null || true
    mkdir -p "$STEAM_LOCAL/steamapps/compatdata"
    for citem in "$STEAM_REAL/steamapps/compatdata"/*; do
        [ -e "$citem" ] || continue
        cname=$(basename "$citem")
        if [ "$cname" = "$COMPAT_APP_ID" ]; then
            cp -a "$citem" "$STEAM_LOCAL/steamapps/compatdata/$cname"
        else
            ln -sfn "$citem" "$STEAM_LOCAL/steamapps/compatdata/$cname" 2>/dev/null || true
        fi
    done
    if [ ! -e "$STEAM_LOCAL/steamapps/compatdata/$COMPAT_APP_ID" ]; then
        mkdir -p "$STEAM_LOCAL/steamapps/compatdata/$COMPAT_APP_ID"
    fi
fi

shopt -s nullglob
for m in "$STEAM_LOCAL/steamapps"/appmanifest_*.acf; do
    [ -f "$m" ] && sed -i 's/"StateFlags"[[:space:]]*"[0-9]*"/"StateFlags"\t\t"4"/' "$m"
done
shopt -u nullglob

mkdir -p "$STEAM_LOCAL/config"
for item in "$STEAM_REAL/config"/*; do
    [ -e "$item" ] || continue
    name=$(basename "$item")
    [ "$name" = "htmlcache" ] && continue
    cp -a "$item" "$STEAM_LOCAL/config/$name" 2>/dev/null || true
done
mkdir -p "$STEAM_LOCAL/config/htmlcache"

mkdir -p "$STEAM_LOCAL/ubuntu12_32"
for item in "$STEAM_REAL/ubuntu12_32"/*; do
    [ -e "$item" ] || continue
    name=$(basename "$item")
    if [ "$name" = "steam" ]; then
        cp "$item" "$STEAM_LOCAL/ubuntu12_32/$name"
        chmod +x "$STEAM_LOCAL/ubuntu12_32/$name"
    else
        ln -sfn "$item" "$STEAM_LOCAL/ubuntu12_32/$name" 2>/dev/null || true
    fi
done

mkdir -p "$STEAM_LOCAL/ubuntu12_64"
for item in "$STEAM_REAL/ubuntu12_64"/*; do
    [ -e "$item" ] || continue
    name=$(basename "$item")
    [ "$name" = "steamwebhelper_sniper_wrap.sh" ] && continue
    ln -sfn "$item" "$STEAM_LOCAL/ubuntu12_64/$name" 2>/dev/null || true
done

cat > "$STEAM_LOCAL/ubuntu12_64/steamwebhelper_sniper_wrap.sh" << 'WRAPEOF'
#!/bin/bash
export LD_LIBRARY_PATH=.${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}
export LIBGL_ALWAYS_SOFTWARE=1
export GALLIUM_DRIVER=llvmpipe
export MESA_GL_VERSION_OVERRIDE=4.5
export MESA_GLSL_VERSION_OVERRIDE=450
export MESA_LOADER_DRIVER_OVERRIDE=swrast
echo "<6>exec ./steamwebhelper (sandbox-wrapped) $*"
echo "<remaining-lines-assume-level=7>"
exec ./steamwebhelper \
    --no-sandbox \
    --disable-dev-shm-usage \
    "$@"
WRAPEOF
chmod +x "$STEAM_LOCAL/ubuntu12_64/steamwebhelper_sniper_wrap.sh"

mkdir -p "$HOME/.steam"
ln -sfn "$STEAM_LOCAL" "$HOME/.steam/steam"
ln -sfn "$STEAM_LOCAL" "$HOME/.steam/root"

if [ -n "$SNAP_COMMON" ] && [ -d "$SNAP_COMMON" ]; then
    mkdir -p "$HOME/.cache"
    for dir in mesa_shader_cache nvidia fontconfig; do
        [ -d "$SNAP_COMMON/.cache/$dir" ] && ln -sfn "$SNAP_COMMON/.cache/$dir" "$HOME/.cache/$dir"
    done
    [ -d "$SNAP_COMMON/.nv" ] && ln -sfn "$SNAP_COMMON/.nv" "$HOME/.nv"
fi

mkdir -p "$HOME/bin"
printf '#!/bin/sh\nexit 0\n' > "$HOME/bin/zenity"
chmod +x "$HOME/bin/zenity"

exit 0
