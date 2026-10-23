#!/usr/bin/env bash
# Сборка APK «Habits TV» для приставки и выкладка на машину с медиаагентом.
#
#   ./build.sh            — собрать dist/habits-tv.apk
#   ./build.sh --deploy   — собрать и выложить: медиаагент раздаёт APK по
#                           /tv/app/habits-tv.apk, ссылка — значок Android в
#                           шапке медиатеки
#
# Ключ подписи создаётся при первой сборке в ~/.config/habits-tv/ и в
# репозиторий не попадает. Его нельзя терять: обновление, подписанное другим
# ключом, Android поверх старой версии не поставит.
set -euo pipefail
cd "$(dirname "$0")"

KEYDIR="$HOME/.config/habits-tv"
PROPS="$KEYDIR/keystore.properties"
if [ ! -f "$PROPS" ]; then
  mkdir -p "$KEYDIR"
  chmod 700 "$KEYDIR"
  PASS=$(openssl rand -hex 16)
  keytool -genkeypair -keystore "$KEYDIR/release.jks" -alias habits-tv -keyalg RSA \
    -keysize 2048 -validity 10000 -storepass "$PASS" -keypass "$PASS" \
    -dname "CN=Habits TV, O=resager" >/dev/null 2>&1
  printf 'storeFile=%s\nstorePassword=%s\nkeyAlias=habits-tv\nkeyPassword=%s\n' \
    "$KEYDIR/release.jks" "$PASS" "$PASS" > "$PROPS"
  chmod 600 "$PROPS" "$KEYDIR/release.jks"
  echo "==> создан ключ подписи $KEYDIR/release.jks — сохраните его"
fi

export ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
./gradlew --no-daemon -q assembleRelease
mkdir -p dist
cp app/build/outputs/apk/release/app-release.apk dist/habits-tv.apk
echo "==> dist/habits-tv.apk ($(stat -c %s dist/habits-tv.apk) байт)"

if [ "${1:-}" = "--deploy" ]; then
  python3 - <<'PY'
import importlib.util, os, shlex
here = os.path.abspath('.')
spec = importlib.util.spec_from_file_location(
    'rr', os.path.join(here, '..', 'media-agent', 'recode-remote.py'))
rr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rr)
remote = rr.Remote(*rr.load_access())
dst = '/home/res/.cache/habits-media/app/habits-tv.apk'
remote.run('mkdir -p ' + shlex.quote(os.path.dirname(dst)))
if remote.push('dist/habits-tv.apk', dst + '.part') != 0 or \
        remote.size(dst + '.part') != os.path.getsize('dist/habits-tv.apk'):
    raise SystemExit('не выложилось')
remote.run(f'mv -f {shlex.quote(dst)}.part {shlex.quote(dst)}')
remote.close()
print('==> выложено: http://192.168.0.79/tv/app/habits-tv.apk')
PY
fi
