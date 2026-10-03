#!/usr/bin/env python3
"""Сверка вопросов ПДД Армении с официальными PDF дорожной полиции.

Зачем: вопросы в исходном наборе извлечены из PDF полуавтоматически (ИИ по
скриншотам), поэтому перед заливкой в приложение их надо проверить по
первоисточнику. Ошибка в правильном ответе — худшее, что может случиться с
тестами: человек выучит неправильно.

Как устроен официальный PDF: страница свёрстана в ДВЕ колонки, в каждой —
текст вопроса, пронумерованные варианты «1.…2.…» и маркер правильного ответа
(«отв․՝2» в девяти группах, «Отв. 2» в восьмой). Без -layout pdftotext
разрывает связь ответа с вопросом, поэтому режем страницу по вертикали и
читаем каждую колонку отдельно.

Две проверки:

  1. ПОСТРАНИЧНАЯ (решающая) — мультимножество правильных ответов страницы
     должно совпадать с ответами тех же шести вопросов набора. Не зависит от
     порядка внутри страницы и от повторов формулировок. Любое расхождение —
     ошибка, скрипт падает.

  2. ПОВОПРОСНАЯ (информативная) — сколько вопросов удалось сопоставить с PDF
     дословно по вариантам. Часть вопросов не сопоставляется в принципе:
     у картиночных вопросов варианты повторяются слово в слово («Разрешается /
     Запрещается»), и различает их только схема. Поэтому единичные расхождения
     здесь — НЕ приговор набору, а повод посмотреть страницу глазами:

         pdftoppm -f <стр> -l <стр> -r 110 -png _pdd/pdf/ru_group_<N>.pdf стр

     Так были проверены все 6 расхождений комплекта 25.12.2025: на страницах
     маркер ответа совпал с набором, ошибался разбор колонок.

Требует pdftotext и pdfinfo (пакет poppler-utils).
"""
import argparse
import json
import re
import subprocess
import sys
from collections import Counter
from pathlib import Path

ANSWER = re.compile(r"(?:отв.?\s*՝|Отв\.)\s*(\d)")
OPTION = re.compile(r"^(\d)\.\s*(.*)$")
PER_PAGE = 6  # вопросов на странице официального PDF


def norm(s: str) -> str:
    """Нормализация текста: набор и PDF расходятся кавычками и финальной точкой."""
    s = (s.replace("«", '"').replace("»", '"').replace("“", '"').replace("”", '"')
          .replace("''", '"').replace("․", ".")
          .replace("–", "-").replace("—", "-").replace("‑", "-"))
    return re.sub(r"\s+", " ", s).strip().lower().rstrip(" .;,")


def okey(options) -> str:
    return " | ".join(norm(o) for o in options)


def pages_of(pdf: Path) -> int:
    out = subprocess.run(["pdfinfo", str(pdf)], capture_output=True, check=True)
    for line in out.stdout.decode(errors="replace").splitlines():
        if line.startswith("Pages:"):
            return int(line.split()[1])
    raise RuntimeError(f"не удалось узнать число страниц: {pdf}")


def split_columns(text: str) -> list[str]:
    """Режет страницу пополам по вертикали — каждая колонка читается отдельно."""
    lines = text.splitlines()
    width = max((len(l) for l in lines), default=0)
    if width < 40:
        return ["\n".join(lines)]
    cut = width // 2
    return ["\n".join(l[:cut].rstrip() for l in lines),
            "\n".join(l[cut:].rstrip() for l in lines)]


def parse_column(text: str) -> list[dict]:
    """Читает колонку сверху вниз: маркер ответа закрывает накопленные варианты."""
    out, cur = [], []
    for raw in text.splitlines():
        line = raw.strip()
        if not line:
            continue
        m = ANSWER.search(line)
        if m:
            if cur:
                out.append({"options": cur, "correct": int(m.group(1))})
                cur = []
            continue
        m = OPTION.match(line)
        if m:
            if int(m.group(1)) == 1:
                cur = []
            cur.append(m.group(2).strip())
            continue
        if cur:  # перенос длинного варианта на следующую строку
            cur[-1] = (cur[-1] + " " + line).strip()
    return out


def parse_pdfs(work: Path) -> dict[str, list[dict]]:
    result = {}
    for gid in range(1, 11):
        pdf = work / "pdf" / f"ru_group_{gid}.pdf"
        items = []
        for p in range(1, pages_of(pdf) + 1):
            txt = subprocess.run(
                ["pdftotext", "-enc", "UTF-8", "-layout", "-f", str(p), "-l", str(p),
                 str(pdf), "-"], capture_output=True, check=True).stdout.decode("utf-8")
            for col in split_columns(txt):
                for it in parse_column(col):
                    it["page"] = p
                    items.append(it)
        result[str(gid)] = items
    return result


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--work", default="./_pdd", help="каталог с questions.json и pdf/")
    args = ap.parse_args()
    work = Path(args.work)

    questions = json.loads((work / "questions.json").read_text("utf-8"))
    by_gid: dict[str, list] = {}
    for q in questions:
        by_gid.setdefault(q["gid"], []).append(q)
    pdf = parse_pdfs(work)

    # --- проверка 1: постранично, мультимножество ответов ---
    pages_ok = pages_partial = 0
    contradictions = []
    for gid in map(str, range(1, 11)):
        ds_pages: dict[int, list[int]] = {}
        for i, q in enumerate(by_gid[gid]):
            ds_pages.setdefault(i // PER_PAGE + 1, []).append(int(q["correct"][1:]))
        pdf_pages: dict[int, list[int]] = {}
        for it in pdf[gid]:
            pdf_pages.setdefault(it["page"], []).append(it["correct"])
        for page, answers in sorted(pdf_pages.items()):
            got, want = Counter(answers), Counter(ds_pages.get(page, []))
            if got == want:
                pages_ok += 1
            elif not (got - want):  # разобрали часть страницы, противоречий нет
                pages_partial += 1
            else:
                contradictions.append((gid, page, sorted(answers), sorted(ds_pages.get(page, []))))

    # --- проверка 2: повопросно, по текстам вариантов ---
    confirmed = ambiguous = unmatched = 0
    suspects: list[tuple] = []
    for gid in map(str, range(1, 11)):
        index: dict[str, set] = {}
        for it in pdf[gid]:
            index.setdefault(okey(it["options"]), set()).add(it["correct"])
        for q in by_gid[gid]:
            opts = [q[f"a{n}"].strip() for n in range(1, 7) if q[f"a{n}"].strip()]
            answers = index.get(okey(opts))
            mine = int(q["correct"][1:])
            if answers is None:
                unmatched += 1
            elif answers == {mine}:
                confirmed += 1
            elif mine in answers:
                ambiguous += 1
            else:
                suspects.append((gid, q["id"], sorted(answers), mine))

    total = len(questions)
    print(f"вопросов в наборе: {total}")
    print(f"страниц PDF сверено: {pages_ok + pages_partial} "
          f"(полное совпадение {pages_ok}, частичный разбор {pages_partial})")
    print(f"подтверждено дословно: {confirmed}, "
          f"неоднозначно (одинаковые варианты у картиночных вопросов): {ambiguous}, "
          f"не сопоставлено: {unmatched}")

    if suspects:
        print(f"\nтребуют проверки глазами ({len(suspects)}): у этих вопросов варианты "
              f"совпали с другим местом PDF, а ответ — нет")
        for gid, qid, pdf_ans, mine in suspects[:20]:
            print(f"  группа {gid}, вопрос {qid}: PDF {pdf_ans}, набор {mine}")

    if contradictions:
        print(f"\nПОСТРАНИЧНЫЕ ПРОТИВОРЕЧИЯ С ОФИЦИАЛЬНЫМ PDF: {len(contradictions)}",
              file=sys.stderr)
        for c in contradictions[:20]:
            print(f"  группа {c[0]}, страница {c[1]}: PDF {c[2]} против набора {c[3]}",
                  file=sys.stderr)
        return 1
    print("\nПостраничных противоречий с официальными PDF нет — набор пригоден к импорту.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
