#!/bin/bash

# Скрипт для сбора всех .go файлов в проекте в один файл с указанием путей для передачи ИИ для анализа.
# Поддерживает исключение папок через параметры.

set -euo pipefail

usage() {
    cat <<EOF
Использование:
  $0 [ОПЦИИ] [ПУТЬ_К_ПРОЕКТУ] [ВЫХОДНОЙ_ФАЙЛ]

ОПЦИИ:
  -x, --exclude DIR    Исключить папку DIR (можно указать несколько раз)
  -h, --help           Показать эту справку

Примеры:
  $0 . collected.txt
  $0 -x vendor -x tests -x .git ./myproject output.txt
EOF
}

# Парсинг аргументов
EXCLUDE_DIRS=()
POSITIONAL_ARGS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        -x|--exclude)
            if [[ -z "${2:-}" || "${2:-}" == -* ]]; then
                echo "Ошибка: для --exclude требуется указать путь." >&2
                exit 1
            fi
            EXCLUDE_DIRS+=("$2")
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            POSITIONAL_ARGS+=("$@")
            break
            ;;
        -*)
            echo "Неизвестный параметр: $1" >&2
            usage >&2
            exit 1
            ;;
        *)
            POSITIONAL_ARGS+=("$1")
            shift
            ;;
    esac
done

# Восстанавливаем позиционные аргументы
set -- "${POSITIONAL_ARGS[@]}"

# Определяем PROJECT_DIR и OUTPUT_FILE
PROJECT_DIR="${1:-.}"
OUTPUT_FILE="${2:-collected_go_files.txt}"

# Преобразуем PROJECT_DIR в абсолютный путь
PROJECT_DIR="$(cd "$PROJECT_DIR" && pwd)"

if [ ! -d "$PROJECT_DIR" ]; then
    echo "Ошибка: директория '$PROJECT_DIR' не существует." >&2
    exit 1
fi

echo "Сбор .go файлов из: $PROJECT_DIR"
echo "Исключённые папки: ${EXCLUDE_DIRS[*]:-<нет>}"
echo "Результат будет сохранён в: $OUTPUT_FILE"
echo

# Очищаем выходной файл
> "$OUTPUT_FILE"

# Формируем аргументы для find: исключаем указанные папки
FIND_ARGS=()
for dir in "${EXCLUDE_DIRS[@]}"; do
    # Убираем начальный слеш, если есть, и нормализуем путь
    dir_clean="${dir#/}"
    FIND_ARGS+=(-not -path "*/$dir_clean/*")
done

# Находим все .go файлы, исключая указанные директории
# Также по умолчанию исключаем vendor и .git (если не переопределено)
DEFAULT_EXCLUDES=("vendor" ".git" "node_modules")
for dir in "${DEFAULT_EXCLUDES[@]}"; do
    # Проверяем, не исключили ли уже пользователь вручную — избегаем дублирования
    skip=false
    for user_dir in "${EXCLUDE_DIRS[@]}"; do
        if [[ "${user_dir%/}" == "$dir" ]]; then
            skip=true
            break
        fi
    done
    if [ "$skip" = false ]; then
        FIND_ARGS+=(-not -path "*/$dir/*")
    fi
done

# Счётчик файлов
file_count=0

while IFS= read -r -d '' file; do
    rel_path="${file#$PROJECT_DIR/}"
    {
        echo "// === FILE: $rel_path ==="
        cat "$file"
        echo ""
    } >> "$OUTPUT_FILE"
    echo "Добавлен: $rel_path"
    ((file_count++))
done < <(find "$PROJECT_DIR" -name "*.go" "${FIND_ARGS[@]}" -print0)

echo
echo "Готово! Обработано файлов: $file_count"
echo "Результат сохранён в: $OUTPUT_FILE"

# Примеры использования:
#Без исключений (только по умолчанию: vendor, .git, node_modules):
#./collect_go_files.sh

#Исключить tests и migrations:
#./collect_go_files.sh -x tests -x migrations
#Указать проект и выходной файл явно:
#./collect_go_files.sh -x build -x docs ./my-go-app go_code.txt
#Показать справку:
#./collect_go_files.sh --help
