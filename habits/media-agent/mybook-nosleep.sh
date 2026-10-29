#!/bin/bash
# Выключить таймер сна в мосте коробки WD My Book. НУЖЕН ROOT.
# Меняет ровно два поля страницы Power Condition и показывает «было/стало».
# Вернуть как было: sdparm --set=STANDBY_Z=1 --set=SZCT=18000 --save /dev/sdc
D=${1:-/dev/sdc}
OUT=/tmp/mybook-nosleep.txt
# пишем и в файл: root есть только у запускающего, а разбирать вывод потом
# удобнее без root — /tmp читается всеми
exec > >(tee "$OUT") 2>&1
show() { sdparm --page=po "$D" 2>&1 | grep -E 'STANDBY_Z|SZCT' | sed 's/^/  /'; }

echo "=== было ==="; show
echo
echo "--- выключаю таймер (с сохранением в мосте) ---"
if sdparm --clear=STANDBY_Z --set=SZCT=0 --save "$D" 2>&1; then
    echo "сохранено"
else
    echo "с --save не вышло, пробую только на текущий сеанс"
    sdparm --clear=STANDBY_Z --set=SZCT=0 "$D" 2>&1
    echo "ВНИМАНИЕ: без сохранения настройка сбросится при отключении питания"
fi
echo
echo "=== стало ==="; show
echo
echo "=== счётчики для сверки (парковка и пуски шпинделя) ==="
smartctl -d sat -A "$D" 2>/dev/null | grep -E 'Start_Stop_Count|Power_On_Hours|Load_Cycle_Count' | sed 's/^/  /'

sync
chmod 644 "$OUT" 2>/dev/null
echo
echo "вывод продублирован в $OUT"
