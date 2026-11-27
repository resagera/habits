#!/bin/bash
# Снять всё, что известно про сон и парковку у диска за USB-мостом.
# НИЧЕГО НЕ МЕНЯЕТ — только читает. Запускать: sudo ~/bin/mybook-diag.sh
D=${1:-/dev/sdc}
OUT=/tmp/mybook-diag.txt
{
  echo "=== устройство: $D, $(date '+%F %T') ==="
  echo
  echo "=== текущее состояние питания (hdparm -C) ==="
  hdparm -C "$D" 2>&1
  echo
  echo "=== APM и таймер простоя (hdparm -B) ==="
  hdparm -B "$D" 2>&1
  echo
  echo "=== SMART: удостоверение ==="
  smartctl -d sat -i "$D" 2>&1 | sed -n '4,30p'
  echo
  echo "=== SMART: настройки питания ==="
  smartctl -d sat -g apm -g standby -g lookahead -g wcache "$D" 2>&1 | sed -n '4,20p'
  echo
  echo "=== SMART: атрибуты (главное — Load_Cycle_Count и Start_Stop_Count) ==="
  smartctl -d sat -A "$D" 2>&1 | sed -n '4,40p'
  echo
  echo "=== SMART: здоровье и наработка ==="
  smartctl -d sat -H "$D" 2>&1 | sed -n '4,10p'
  smartctl -d sat -a "$D" 2>&1 | grep -iE 'power_on|power-on|start_stop|load_cycle|temperature_cel' 
  echo
  echo "=== IntelliPark: таймер парковки в прошивке диска (idle3ctl -g) ==="
  idle3ctl -g "$D" 2>&1
  echo
  echo "=== страница Power Condition у моста (sdparm) ==="
  sdparm --page=po "$D" 2>&1 | head -20
  echo
  echo "=== поддерживает ли диск отключение таймера (hdparm -I) ==="
  hdparm -I "$D" 2>&1 | grep -iE 'advanced power|power management|standby|idle|apm|model|firmware' | head -15
} > "$OUT" 2>&1
chmod 644 "$OUT"
echo "готово: $OUT ($(wc -l < "$OUT") строк)"
