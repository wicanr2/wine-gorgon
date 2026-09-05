#!/bin/sh
# 對拍：原版執行檔的畫面 對 重製版的畫面。
#
# 三段：
#   1. nerun 跑原版 CIV.EXE，載入指定存檔，輸出一張 PNG
#   2. 重製版跑同一份存檔，輸出一張 PNG
#   3. nediff 逐點比對指定的兩個矩形
#
# 用**同一份存檔**而不是各自開新局：世界是亂數生成的。
# 用兩個矩形而不是一個：兩邊的版面不必相同，要比的是「畫出來的東西」。
#
# 環境變數：
#   GAME     原版安裝目錄（唯讀）
#   SAV      存檔檔名（要放在 WRITE 目錄裡）
#   WRITE    可寫目錄（存檔放這裡；原版目錄不會被寫到）
#   REMAKE   重製版的執行命令；會在後面接 -png <輸出>
#   OUT      輸出目錄
#   A_RECT   原版這一側的比對矩形 x,y,w,h
#   B_RECT   重製版這一側的比對矩形 x,y,w,h
#   IGNORE   忽略的矩形（比對座標系），例如參考幀上的滑鼠游標
set -eu

: "${GAME:?請設定 GAME＝原版安裝目錄}"
: "${SAV:?請設定 SAV＝存檔檔名}"
: "${WRITE:?請設定 WRITE＝可寫目錄}"
: "${OUT:=out}"
: "${A_RECT:=}"
: "${B_RECT:=}"
: "${IGNORE:=}"

mkdir -p "$OUT"

echo "== 1／3 原版 =="
./nerun -data "$GAME" -write "$WRITE" -screen 800x600 \
        -open "C:\\CIV\\$SAV" -trace 0 \
        -script scripts/oracle-main-map.txt "$GAME/CIV.EXE" \
  | grep -E "waitfor|clickwin|視窗 [0-9]|停在"

if [ -n "${REMAKE:-}" ]; then
  echo "== 2／3 重製版 =="
  # shellcheck disable=SC2086
  $REMAKE -png "$OUT/remake.png"
fi

echo "== 3／3 比對 =="
./nediff ${A_RECT:+-a-rect "$A_RECT"} ${B_RECT:+-b-rect "$B_RECT"} \
         ${IGNORE:+-ignore "$IGNORE"} \
         -out "$OUT/diff.png" "$OUT/remake.png" "$OUT/ours.png"
