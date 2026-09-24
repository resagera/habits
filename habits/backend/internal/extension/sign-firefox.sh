#!/usr/bin/env bash
# Подпись расширения для Firefox на addons.mozilla.org (канал unlisted).
#
# Зачем: Firefox ставит неподписанные дополнения только временно — до
# перезапуска браузера. Подпись Mozilla снимает это ограничение, при этом
# unlisted означает, что дополнение НЕ публикуется в каталоге: проверка
# автоматическая, а раздаём мы его сами со своего домена.
#
# Ключи берутся из ~/.config/habits-amo.env (AMO_JWT_ISSUER/AMO_JWT_SECRET) и
# в репозиторий не попадают. Получить их: addons.mozilla.org → Developer Hub →
# Manage API Keys.
#
# Результат кладётся в files/signed/habits-firefox.xpi — он вшивается в
# бинарник (go:embed) и раздаётся с /ext/habits-firefox.xpi. Поэтому подпись
# делается ОДИН раз на версию, а не при каждой сборке.
#
# Использование:
#   ./sign-firefox.sh [URL приложения]
# По умолчанию URL — прод. Он зашивается в подписанный файл: изменить его
# потом нельзя, подпись покрывает содержимое.
set -euo pipefail

cd "$(dirname "$0")"
APP_URL="${1:-https://telegram.resager.ru/app/habits/}"
[[ "$APP_URL" == */ ]] || APP_URL="$APP_URL/"
ENV_FILE="${AMO_ENV_FILE:-$HOME/.config/habits-amo.env}"
OUT_DIR="files/signed"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "нет файла с ключами: $ENV_FILE" >&2
  echo "создайте его с AMO_JWT_ISSUER=... и AMO_JWT_SECRET=... (chmod 600)" >&2
  exit 1
fi
# shellcheck disable=SC1090
source "$ENV_FILE"
: "${AMO_JWT_ISSUER:?нет AMO_JWT_ISSUER в $ENV_FILE}"
: "${AMO_JWT_SECRET:?нет AMO_JWT_SECRET в $ENV_FILE}"

# nvm-окружение: под systemd и в чистом shell node может не найтись в PATH
if [[ -d "$HOME/.nvm/versions/node" ]]; then
  NEWEST_NODE="$(ls -1d "$HOME"/.nvm/versions/node/*/bin 2>/dev/null | sort -V | tail -1 || true)"
  [[ -n "$NEWEST_NODE" ]] && PATH="$NEWEST_NODE:$PATH"
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

# собираем ровно то, что попадёт в архив: манифест firefox под именем
# manifest.json + попап + иконки, с подстановкой адреса приложения
sed "s|{{APP_URL}}|$APP_URL|g" files/manifest.firefox.json > "$STAGE/manifest.json"
sed "s|{{APP_URL}}|$APP_URL|g" files/popup.html > "$STAGE/popup.html"
cp -r files/icons "$STAGE/icons"

VERSION="$(grep -oE '"version"[^,]*' "$STAGE/manifest.json" | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"
echo "==> подписываем Habits $VERSION для $APP_URL"

mkdir -p "$OUT_DIR"
npx --yes web-ext@latest sign \
  --source-dir "$STAGE" \
  --artifacts-dir "$STAGE/out" \
  --channel unlisted \
  --api-key "$AMO_JWT_ISSUER" \
  --api-secret "$AMO_JWT_SECRET"

SIGNED="$(find "$STAGE/out" -name '*.xpi' | head -1)"
[[ -n "$SIGNED" ]] || { echo "подписанный файл не найден" >&2; exit 1; }
cp "$SIGNED" "$OUT_DIR/habits-firefox.xpi"

echo
echo "готово: $OUT_DIR/habits-firefox.xpi ($(du -h "$OUT_DIR/habits-firefox.xpi" | cut -f1))"
echo "не забудьте: при следующей подписи поднять version в files/manifest.firefox.json —"
echo "AMO не примет повторно уже загруженную версию."
