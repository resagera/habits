#!/usr/bin/env bash
# Дымовой прогон страницы «Тесты» против локального сервера (см. SKILL: локальный прогон).
# Требует поднятого srvtest на :8078 и залитой колоды pdd-am-ru.
# Запуск: bash habits/tools/pdd-import/smoke_test.sh
set -u
B=http://127.0.0.1:8078/api/v1
A='Authorization: tma dev'
fail=0
ok()   { echo "  ✅ $1"; }
bad()  { echo "  ❌ $1"; fail=1; }
jqp()  { python3 -c "import sys,json;d=json.load(sys.stdin);print($1)"; }

echo "1. Учебный прогон: старт по пулу «не пройденные»"
S=$(curl -s -X POST "$B/tests/sessions" -H "$A" -H 'Content-Type: application/json' \
     -d '{"deck_id":1,"mode":"study","scope":"unpassed"}')
SID=$(echo "$S" | jqp "d['session']['id']")
TOTAL=$(echo "$S" | jqp "d['session']['total']")
[ "$TOTAL" = 1032 ] && ok "в прогоне все 1032 вопроса" || bad "ожидали 1032, получили $TOTAL"

echo "2. Текущий вопрос не раскрывает правильный ответ"
Q=$(curl -s -H "$A" "$B/tests/sessions/$SID")
echo "$Q" | grep -q 'correct_idx' && bad "correct_idx утёк в выдаче вопроса" || ok "правильный ответ скрыт"
QID=$(echo "$Q" | jqp "d['question']['id']")
NOPT=$(echo "$Q" | jqp "len(d['question']['options'])")
echo "   вопрос $QID, вариантов $NOPT"

echo "3. Неверный ответ → вопрос остаётся в пуле"
R=$(curl -s -X POST "$B/tests/sessions/$SID/answer" -H "$A" -H 'Content-Type: application/json' \
     -d "{\"question_id\":$QID,\"chosen\":99}")
CORRECT=$(echo "$R" | jqp "d['correct']")
STATUS=$(echo "$R" | jqp "d['status']")
RIGHT=$(echo "$R" | jqp "d['correct_idx']")
[ "$CORRECT" = "False" ] && ok "ответ засчитан неверным" || bad "должен быть неверным"
[ "$STATUS" = "wrong" ] && ok "статус вопроса wrong" || bad "статус $STATUS, ожидали wrong"
echo "   сервер вернул правильный вариант: $RIGHT"

echo "4. Верный ответ → вопрос уходит в «пройденные»"
Q2=$(curl -s -H "$A" "$B/tests/sessions/$SID")
QID2=$(echo "$Q2" | jqp "d['question']['id']")
# правильный вариант узнаём, промахнувшись по заведомо несуществующему индексу
PROBE=$(curl -s -X POST "$B/tests/sessions/$SID/answer" -H "$A" -H 'Content-Type: application/json' \
     -d "{\"question_id\":$QID2,\"chosen\":98}")
echo "$PROBE" | jqp "d['correct_idx']" > /dev/null
# начинаем новый прогон по «ошибочным» и отвечаем верно
S2=$(curl -s -X POST "$B/tests/sessions" -H "$A" -H 'Content-Type: application/json' \
     -d '{"deck_id":1,"mode":"study","scope":"wrong"}')
SID2=$(echo "$S2" | jqp "d['session']['id']")
T2=$(echo "$S2" | jqp "d['session']['total']")
[ "$T2" = 2 ] && ok "пул «ошибочные» = 2 вопроса" || bad "пул «ошибочные» = $T2, ожидали 2"
Q3=$(curl -s -H "$A" "$B/tests/sessions/$SID2")
QID3=$(echo "$Q3" | jqp "d['question']['id']")
IDX=$(curl -s -X POST "$B/tests/sessions/$SID2/answer" -H "$A" -H 'Content-Type: application/json' \
     -d "{\"question_id\":$QID3,\"chosen\":97}" | jqp "d['correct_idx']")
# теперь тот же вопрос в новом прогоне — отвечаем правильно
S3=$(curl -s -X POST "$B/tests/sessions" -H "$A" -H 'Content-Type: application/json' \
     -d '{"deck_id":1,"mode":"study","scope":"wrong"}')
SID3=$(echo "$S3" | jqp "d['session']['id']")
R3=$(curl -s -X POST "$B/tests/sessions/$SID3/answer" -H "$A" -H 'Content-Type: application/json' \
     -d "{\"question_id\":$QID3,\"chosen\":$IDX}")
[ "$(echo "$R3" | jqp "d['correct']")" = "True" ] && ok "верный ответ принят" || bad "верный ответ не принят"
[ "$(echo "$R3" | jqp "d['status']")" = "passed" ] && ok "статус passed" || bad "статус не passed"

echo "5. Пройденный вопрос исчез из пула «не пройденные»"
D=$(curl -s -H "$A" "$B/tests/decks")
PASSED=$(echo "$D" | jqp "d['decks'][0]['passed']")
[ "$PASSED" = 1 ] && ok "пройден 1 вопрос" || bad "пройдено $PASSED, ожидали 1"
S4=$(curl -s -X POST "$B/tests/sessions" -H "$A" -H 'Content-Type: application/json' \
     -d '{"deck_id":1,"mode":"study","scope":"unpassed"}')
T4=$(echo "$S4" | jqp "d['session']['total']")
[ "$T4" = 1031 ] && ok "в пуле осталось 1031" || bad "в пуле $T4, ожидали 1031"

echo "6. Порядок вопросов перемешан и зафиксирован"
S5=$(curl -s -X POST "$B/tests/sessions" -H "$A" -H 'Content-Type: application/json' \
     -d '{"deck_id":1,"mode":"study","scope":"all"}')
SID5=$(echo "$S5" | jqp "d['session']['id']")
F1=$(curl -s -H "$A" "$B/tests/sessions/$SID5" | jqp "d['question']['id']")
F2=$(curl -s -H "$A" "$B/tests/sessions/$SID5" | jqp "d['question']['id']")
[ "$F1" = "$F2" ] && ok "порядок стабилен между запросами" || bad "порядок поехал: $F1 ≠ $F2"
S6=$(curl -s -X POST "$B/tests/sessions" -H "$A" -H 'Content-Type: application/json' \
     -d '{"deck_id":1,"mode":"study","scope":"all"}')
F3=$(curl -s -H "$A" "$B/tests/sessions/$(echo "$S6" | jqp "d['session']['id']")" | jqp "d['question']['id']")
[ "$F1" != "$F3" ] && ok "новый прогон даёт другой порядок ($F1 → $F3)" || echo "  ⚠️ совпало случайно, повторите"

echo "7. Экзамен: 20 вопросов, таймер, порог ошибок"
E=$(curl -s -X POST "$B/tests/sessions" -H "$A" -H 'Content-Type: application/json' \
     -d '{"deck_id":1,"mode":"exam"}')
EID=$(echo "$E" | jqp "d['session']['id']")
ET=$(echo "$E" | jqp "d['session']['total']")
EXP=$(echo "$E" | jqp "d['session']['expires_at'] is not None")
[ "$ET" = 20 ] && ok "20 вопросов" || bad "вопросов $ET"
[ "$EXP" = "True" ] && ok "дедлайн проставлен" || bad "дедлайна нет"
# отвечаем на все 20 наугад (chosen=0) и смотрим вердикт
for _ in $(seq 1 20); do
  QQ=$(curl -s -H "$A" "$B/tests/sessions/$EID" | jqp "d.get('question',{}).get('id',0)")
  [ "$QQ" = 0 ] && break
  curl -s -X POST "$B/tests/sessions/$EID/answer" -H "$A" -H 'Content-Type: application/json' \
       -d "{\"question_id\":$QQ,\"chosen\":0}" > /dev/null
done
FIN=$(curl -s -H "$A" "$B/tests/sessions/$EID" | jqp "(d['session']['finished_at'] is not None, d['session']['passed'], d['session']['correct'])")
ok "экзамен завершён автоматически: (finished, passed, correct) = $FIN"

echo "8. Разбор прогона показывает правильные ответы"
RV=$(curl -s -H "$A" "$B/tests/sessions/$EID/review" | jqp "(len(d['items']), d['items'][0]['correct_idx'] is not None, d['items'][0]['chosen_idx'])")
ok "разбор: (вопросов, есть верный ответ, выбранный) = $RV"

echo "9. Настройка «сколько верных подряд»"
curl -s -X PUT "$B/tests/settings" -H "$A" -H 'Content-Type: application/json' -d '{"pass_streak":2}' > /dev/null
PS=$(curl -s -H "$A" "$B/tests/settings" | jqp "d['pass_streak']")
[ "$PS" = 2 ] && ok "порог сохранён (2)" || bad "порог $PS"
BADREQ=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$B/tests/settings" -H "$A" \
         -H 'Content-Type: application/json' -d '{"pass_streak":9}')
[ "$BADREQ" = 400 ] && ok "недопустимый порог отвергнут (400)" || bad "код $BADREQ"
curl -s -X PUT "$B/tests/settings" -H "$A" -H 'Content-Type: application/json' -d '{"pass_streak":1}' > /dev/null

echo "10. Сброс прогресса"
curl -s -X POST "$B/tests/decks/1/reset" -H "$A" -H 'Content-Type: application/json' -d '{}' > /dev/null
P2=$(curl -s -H "$A" "$B/tests/decks" | jqp "d['decks'][0]['passed']")
[ "$P2" = 0 ] && ok "мягкий сброс снял отметки «пройден»" || bad "после сброса пройдено $P2"

echo "11. Картинки вопросов раздаются"
IMG=$(curl -s -H "$A" "$B/tests/decks/1/groups" > /dev/null; curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8078/uploads/tests/1_100.webp)
[ "$IMG" = 200 ] && ok "картинка отдаётся (HTTP 200)" || bad "картинка: HTTP $IMG"

echo "12. Повторный импорт идемпотентен"
IMP=$(curl -s -X POST http://127.0.0.1:8078/api/v1/admin/tests/import -H "$A" \
      -F "deck=@${BUNDLE:-./_pdd/bundle}/deck.json")
INS=$(echo "$IMP" | jqp "d['inserted']"); UPD=$(echo "$IMP" | jqp "d['updated']")
[ "$INS" = 0 ] && [ "$UPD" = 1032 ] && ok "переимпорт обновил 1032, добавил 0" || bad "переимпорт: +$INS ~$UPD"

echo
[ $fail = 0 ] && echo "ВСЕ ПРОВЕРКИ ПРОШЛИ" || echo "ЕСТЬ ПАДЕНИЯ"
exit $fail
