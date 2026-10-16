#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Confeziona ciò che scripts/build.sh ha prodotto in ../build nel pacchetto
# che riceve l'utente, per il sistema su cui gira. Lo usano le prove locali
# (scripts/ci-locale.sh) e GitHub (.github/workflows/build.yml): stessi
# comandi, stesso risultato.
#
#   macOS    ../build/NetworkToolkit-AppleSilicon.zip, NetworkToolkit-Intel.zip
#   Linux    ../build/NetworkToolkit-Linux-x86_64.AppImage
#   Windows  ../build/NetworkToolkit-Windows.zip (cartella con exe e LICENZE)
set -euo pipefail
cd "$(dirname "$0")/.."
B=../build

pacchetto_macos() {
  for nome in NetworkToolkit-AppleSilicon NetworkToolkit-Intel; do
    rm -f "$B/$nome.zip"
    # --keepParent obbligatorio: senza, lo zip contiene Contents/ e non l'app
    ditto -c -k --sequesterRsrc --keepParent "$B/$nome.app" "$B/$nome.zip"
    echo "→ $B/$nome.zip"
  done
}

pacchetto_linux() {
  # appimagetool: quello indicato da APPIMAGETOOL, altrimenti si tiene in
  # ~/.local/bin, scaricato la prima volta (sui runner ogni volta).
  local tool="${APPIMAGETOOL:-$HOME/.local/bin/appimagetool-x86_64.AppImage}"
  local url="https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage"
  if [ ! -x "$tool" ]; then
    mkdir -p "$(dirname "$tool")"
    curl -fsSL -o "$tool.part" "$url"
    chmod +x "$tool.part"
    mv "$tool.part" "$tool"
  fi
  local lavoro A
  lavoro="$(mktemp -d "${TMPDIR:-/tmp}/ntk-appimage-XXXXXX")"
  A="$lavoro/NetworkToolkit.AppDir"
  mkdir -p "$A/usr/bin" "$A/usr/share/icons/hicolor/256x256/apps"
  cp "$B/NetworkToolkit-Linux-amd64/NetworkToolkit" "$A/usr/bin/"
  mkdir -p "$A/usr/share/doc"
  cp -R "$B/NetworkToolkit-Linux-amd64/LICENZE" "$A/usr/share/doc/networktoolkit"
  cp assets/appicon.png "$A/networktoolkit.png"
  cp assets/appicon.png "$A/usr/share/icons/hicolor/256x256/apps/networktoolkit.png"
  cat > "$A/NetworkToolkit.desktop" <<'DESKTOP'
[Desktop Entry]
Name=NetworkToolkit
Exec=NetworkToolkit
Icon=networktoolkit
Type=Application
Categories=Utility;Network;
StartupWMClass=NetworkToolkit
DESKTOP
  ln -s usr/bin/NetworkToolkit "$A/AppRun"
  # --appimage-extract-and-run: i runner non hanno FUSE.
  rm -f "$B/NetworkToolkit-Linux-x86_64.AppImage"
  ARCH=x86_64 "$tool" --appimage-extract-and-run "$A" "$B/NetworkToolkit-Linux-x86_64.AppImage"
  rm -rf "$lavoro"
  echo "→ $B/NetworkToolkit-Linux-x86_64.AppImage"
}

pacchetto_windows() {
  # Git Bash non ha zip: il tar di Windows (bsdtar) sceglie il formato
  # dall'estensione (PROBLEMI-NOTI.md §28).
  rm -f "$B/NetworkToolkit-Windows.zip"
  /c/Windows/System32/tar.exe -a -c -f "$(cygpath -w "$PWD/$B/NetworkToolkit-Windows.zip")" \
    -C "$(cygpath -w "$PWD/$B")" NetworkToolkit-Windows
  echo "→ $B/NetworkToolkit-Windows.zip"
}

case "$(uname -s)" in
  Darwin) pacchetto_macos ;;
  Linux) pacchetto_linux ;;
  MINGW*|MSYS*|CYGWIN*) pacchetto_windows ;;
  *) echo "Piattaforma non riconosciuta"; exit 1 ;;
esac
