#!/usr/bin/env python3
"""Сборка пакета импорта колоды «ПДД Армении» для страницы «Тесты».

Что делает:
  1. качает вопросы (JSON) и картинки из открытого репозитория-источника;
  2. качает официальные PDF дорожной полиции Армении;
  3. СВЕРЯЕТ вопросы с официальными PDF (см. verify.py) — без этого не собирает;
  4. складывает deck.json (наш формат импорта) и images.zip.

Дальше пакет заливается админским эндпоинтом:

    curl -s -X POST "$BASE/api/v1/admin/tests/import" \
         -H "Authorization: tma <initData>" \
         -F "deck=@deck.json" -F "images=@images.zip"

Источники:
  вопросы  — github.com/alexmasyukov/pdd-armenia (извлечены из PDF полиции)
  эталон   — roadpolice.am/exam/ru_group_{1..10}.pdf (официальный комплект)
"""
import argparse
import json
import shutil
import subprocess
import sys
import urllib.request
import zipfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

RAW = "https://raw.githubusercontent.com/alexmasyukov/pdd-armenia/main"
QUESTIONS_URL = f"{RAW}/5__source_json_from_all_csv_files_ai_25_12_2025/ru_2026.json"
GROUPS_URL = f"{RAW}/src/data/groups.json"
IMAGES_API = ("https://api.github.com/repos/alexmasyukov/pdd-armenia/"
              "git/trees/HEAD?recursive=1")
IMAGES_PREFIX = "public/images/questions/"
PDF_URL = "https://roadpolice.am/exam/ru_group_{}.pdf"

DECK = {
    "slug": "pdd-am-ru",
    "title": "ПДД Армении",
    "description": ("Официальные экзаменационные вопросы дорожной полиции "
                    "Армении, русская версия. Комплект от 25.12.2025."),
    "lang": "ru",
    "source_url": "https://roadpolice.am/ru/viv-exam",
    "revision": "2025-12-25",
    # как на настоящем экзамене
    "exam_size": 20,
    "exam_minutes": 30,
    "exam_allowed_mistakes": 2,
}


# Заплатки к дефектам первоисточника. Каждая — с обоснованием: правим только
# то, что подтверждается официальным же документом на другом языке.
PATCHES: dict[int, dict] = {
    # В русском PDF (группа 8, стр. 13) у вопроса напечатаны три варианта,
    # а правильный ответ — «Отв. 4»: при вёрстке потеряли четвёртую строку.
    # В английской версии (en_group_8.pdf, та же страница) она на месте:
    # «4.In all the cases listed.» с «Ans․՝4». Возвращаем её по-русски.
    854: {
        "append_option": "Во всех перечисленных случаях.",
        "why": "en_group_8.pdf: 4.In all the cases listed. (Ans 4)",
    },
}


def fetch(url: str, dest: Path) -> Path:
    if dest.exists() and dest.stat().st_size > 0:
        return dest
    dest.parent.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(url, timeout=120) as r, open(dest, "wb") as f:
        shutil.copyfileobj(r, f)
    return dest


def fetch_sources(work: Path) -> tuple[list, list, Path]:
    questions = json.loads(fetch(QUESTIONS_URL, work / "questions.json").read_text("utf-8"))
    groups = json.loads(fetch(GROUPS_URL, work / "groups.json").read_text("utf-8"))["groups"]

    with ThreadPoolExecutor(max_workers=10) as pool:
        list(pool.map(lambda i: fetch(PDF_URL.format(i), work / "pdf" / f"ru_group_{i}.pdf"),
                      range(1, 11)))

    img_dir = work / "img"
    with urllib.request.urlopen(IMAGES_API, timeout=120) as r:
        tree = json.load(r)["tree"]
    paths = [t["path"] for t in tree
             if t["type"] == "blob" and t["path"].startswith(IMAGES_PREFIX)
             and " " not in t["path"]]  # в источнике есть мусорный «820 copy.webp»

    def grab(p: str):
        name = p[len(IMAGES_PREFIX):].replace("/", "_")
        fetch(f"{RAW}/{p}", img_dir / name)

    with ThreadPoolExecutor(max_workers=16) as pool:
        list(pool.map(grab, paths))
    print(f"источники: {len(questions)} вопросов, {len(groups)} групп, "
          f"{len(list(img_dir.iterdir()))} картинок, 10 PDF")
    return questions, groups, img_dir


def build_deck(questions: list, groups: list, img_dir: Path) -> dict:
    images = {p.name for p in img_dir.iterdir() if p.is_file()}
    out, missing = [], 0
    patched = 0
    for q in questions:
        opts = [q[f"a{n}"].strip() for n in range(1, 7) if q[f"a{n}"].strip()]
        correct = int(q["correct"][1:]) - 1  # a3 → 2 (0-based)
        if patch := PATCHES.get(int(q["id"])):
            opts = opts + [patch["append_option"]]
            patched += 1
            print(f"  заплатка к вопросу {q['id']}: + «{patch['append_option']}» "
                  f"({patch['why']})")
        if not (0 <= correct < len(opts)):
            raise SystemExit(f"вопрос {q['id']}: correct={q['correct']} вне вариантов {opts}")
        name = f"{q['gid']}_{q['id']}.webp"
        if name not in images:
            name, missing = "", missing + 1
        out.append({
            "num": int(q["id"]),
            "group": int(q["gid"]),
            "text": q["q"].strip(),
            "options": opts,
            "correct_idx": correct,
            "image": name,
            "explanation": "",
        })
    print(f"собрано вопросов: {len(out)} (без картинки: {missing}, заплаток: {patched})")
    return {**DECK,
            "groups": [{"num": int(g["id"]), "title": g["name"]} for g in groups],
            "questions": out}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", default="./_pdd", help="каталог для скачанных источников")
    ap.add_argument("--out", default="./_pdd/bundle", help="куда положить deck.json и images.zip")
    ap.add_argument("--skip-verify", action="store_true",
                    help="не сверять с официальными PDF (не рекомендуется)")
    args = ap.parse_args()

    work, out = Path(args.work), Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    questions, groups, img_dir = fetch_sources(work)

    if not args.skip_verify:
        verify = Path(__file__).with_name("verify.py")
        code = subprocess.call([sys.executable, str(verify), "--work", str(work)])
        if code != 0:
            print("сверка с официальными PDF не прошла — пакет не собран", file=sys.stderr)
            return code

    deck = build_deck(questions, groups, img_dir)
    (out / "deck.json").write_text(json.dumps(deck, ensure_ascii=False), encoding="utf-8")

    zip_path = out / "images.zip"
    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_STORED) as z:  # webp уже сжат
        for p in sorted(img_dir.iterdir()):
            if p.is_file():
                z.write(p, p.name)
    print(f"\nготово: {out/'deck.json'} ({(out/'deck.json').stat().st_size//1024} КБ), "
          f"{zip_path} ({zip_path.stat().st_size//1024//1024} МБ)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
