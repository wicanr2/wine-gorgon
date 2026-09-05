# 接手點

## 現在跑得到哪裡

**主地圖畫得出來，而且和原版逐點相同。**

```sh
docker run --rm --network none --memory 2g --cpus 2 --user "$(id -u):$(id -g)" \
  -v /path/to/CIV:/game:ro -v "$PWD/workplace/write:/gw" -v "$PWD/workplace/out:/out" \
  -w /out dsds-go:1.25 \
  ./nerun -data /game -write /gw -screen 800x600 \
          -open 'C:\CIV\NEWUNIT.SAV' -script oracle-main-map.txt /game/CIV.EXE
```

（`NEWUNIT.SAV` 要先放進可寫目錄；它來自 civ1 專案的
`workplace/test-output/win31-new-game-unit-v1/`。腳本在 `scripts/`。）

比對：

```
$ nediff -a-rect 172,42,440,538 -b-rect 340,42,440,538 ours.png oracle.png
比對 440x538 ＝ 236720 個像素：不同 54（0.0228%）
```

那 54 個是參考幀上的滑鼠游標（Windows 畫的，不是遊戲畫的）。

## 下一步（照這個順序）

1. **橫向落點還差 168 個像素**（`docs/spec/006` §4）。內容完全相同，
   只是整塊往左偏。已排除：`WM_SIZE` 沒送、客戶區算錯、選單高度。
   遊戲拿到的 `GetClientRect(CIV)` 是 608×538（和參考幀一致），但地圖
   內容左緣落在客戶區內第 88 個像素，像是它以為客戶區只有 272 寬。
   **下一步是把地圖第一次繪製之前所有「量尺寸」類 API 的回傳值連同
   呼叫端位址印出來**，對照 civ1 `docs/re/322` 的邏輯→螢幕換算。
2. **非客戶區沒有畫**：標題列、選單列、捲軸都是空的。要和參考幀比整個
   畫面就得補上，而那要先有 Windows 3.1 的框線規則與系統字型。
3. `Polygon` 的填色（目前只畫外框）。
4. 走「開新遊戲」那條路時世界是亂數生成的，**不能拿來對拍**；對拍一律
   從存檔進去。

## 診斷工具（先用這些，不要先猜）

| 想知道 | 用什麼 |
|---|---|
| 停在哪一支 API | `nerun` 的結束訊息（帶呼叫端位址） |
| 指令一直跑但沒有 API 呼叫 | `run` 之後印的 CS:IP 抽樣 |
| 某個位址寫了什麼、那裡的重定位打到誰 | `nedump -at <sel:off>` |
| 畫面上有什麼字 | 腳本的 `text` 指令（`TextOut` 紀錄帶螢幕座標） |
| 有哪些視窗、在哪裡 | 腳本的 `print` 指令 |
| 哪一支 API 被叫了幾次 | `nerun` 結束時的直方圖（**沒有上限**，`Trace` 才有） |
| 「做了但不完整」的清單 | `nerun` 結束時的「備註」 |

## 這個專案踩過、不要再踩的

- **手算參數位元組數會錯。** `BitBlt` 是八個 word ＋ 一個 dword ＝ 20，
  曾經寫成 22，1,036 次呼叫全部安靜地讀錯位置（Borland 的 `leave` 會把
  漂掉的 SP 蓋回去，所以不會當機）。新增 API 一律同時在
  `internal/winapi/ordinals_test.go` 補一行**參數型別序列**，讓測試算。
- **「回 0」不等於安全。** `GetDesktopWindow` 回 0 讓對話框被擺到螢幕外；
  `hMenu` 對子視窗是控制項編號而不是選單，混在一起讓 `WM_COMMAND` 分不出
  是誰。這一類錯的共同症狀是「畫面少了東西，沒有任何錯誤訊息」。
  凡是參數或回傳值裡有 handle 的，查不到就記一筆 `Note`。
- **時間不要讀主機時鐘。** 預設的 `StepClock` 跟著指令數走；改用真實時間
  會讓兩次執行不同，而這是一個對拍工具。
