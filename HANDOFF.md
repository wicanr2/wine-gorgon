# 固定種子對拍擴充（2026-09-06，Civ1 I539）

新增 until／keywin／poke／state／traceuntil，契約與回呼觀察限制見
`docs/probe-control.md`。合成測試涵蓋準確停止、逾限、原子寫入、重播、指定視窗。
原版實驗由 civ1 的 `tools/oracle/wine_gorgon_barbarians.py` 驗證：三組種子，
同種子 trace／單位表逐 byte 相同，另一種子改變登陸位置；完整 RNG state
與 civ1 正式 RNG／隊伍 helper 都相符。這不是整體 Win16 或野蠻人完整 parity。

原版資料與收據留 civ1 本機 i539-oracle/replay-final，不提交本倉庫。
原版函式與科技條件的定位留 civ1 DS335；執行器不硬編 Civilization 位址。
原始 HEAD 7913171，擴充隨本輪提交；Docker --rm，未建立映像。

---

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

## `wins`：問執行檔「這個視窗多大」（2026-09-06）

新增腳本命令 `wins`，列出目前所有視窗的 `win=` / `client=` / `style`。
起因是 civ1 小地圖白框的追蹤分支，第二道閘是「client 高 / scale == 50」
——參考幀量到的黑色區域 162×103 算出來是 1（分支永遠不跑），實際的
`GetClientRect` 是 160×100 算出來是 0（分支恆成立）。差的那一圈是視窗框
最內側的黑內線。

`client=` 可信、`win=` 會受本執行器的邊框度量影響（168 vs 真 Win3.1 的
166），因為遊戲是先決定客戶區再用 `AdjustWindowRect` 反推視窗大小。

腳本：`scripts/worldmap-geometry.txt`。說明在 `docs/spec/006` §5.3。

## Status 面板還沒對上（2026-09-06）

三個客戶區只剩 Status 對不上（82.7% 不同）。這一輪把鏈追到一半：

- 底色由 `sub_5FDC8(rect, 色號)` 畫，色號是 `byte_6C344`（0x42）與
  `byte_6C34B`（0x0F）——用新加的 `peek` 讀出來的執行時值。
- `sub_28175(色號)` 在 256 色模式下**不是**建實心筆刷，而是建一張 8×8、
  每個像素都是那個 index 的點陣圖再 `CreatePatternBrush`。這是 Win16 拿到
  「就是這一格 index」的標準做法。
- 這一邊原本把圖樣筆刷當成 `Brush.Index` 用 ⇒ 那些區域全成了索引 0（黑）。
  已補上圖樣填色（`DC.FillPattern` ＋ `TestFillPatternTilesBitmap`）。
- **補上之後 Status 還是黑的**，鏈上還有一環。停在這裡而不是繼續補特例。

順帶新增 `peek <sel:off> [len]`：看執行時的全域。它一次讀出 civ1 spec 22
§7 第 1 項列為未知的那組色（`00 04 02 06 01 05 03 07 F8 FC FA FE F9 FD FB FF`），
與參考幀上量到的系統色 0／4／3 和閃爍白 0xFF 完全一致。

## 落點修好了：視窗 DC 的幾何不能凍住（2026-09-05）

追了好幾輪的「橫向差 169 px」與「小地圖整片是黑的」是**同一個 bug**：
`newWindowDC` 把 `GetDC` 當下的客戶區抄進 DC 就不再更新，而 CIV.EXE 先用
小尺寸 `CreateWindow`（`CIV` 600×400、`WdwSmMap` 40×40）拿到 DC，**之後
才 `MoveWindow`**，然後用同一個 DC 畫。

- 主地圖：原點停在 `(3,41)` 而不是 `(171,41)` → 內容往左 168 px。
- 小地圖：裁剪停在 `(23,42)-(57,57)`（40×40 的客戶區 35×16）→ 160×100 的
  blit 幾乎整片被裁掉 → client 全黑。

修法：`p.dc()` 取出視窗 DC 時重算幾何；`BeginPaint` 的更新矩形改存相對
客戶區的值。細節與驗收數字見 `docs/spec/006` §4。

第二個病因是**邊框用錯了度量**：`frameFor` 對 `WS_THICKFRAME` 用
`SM_CXDLGFRAME`（3）而不是 `SM_CXFRAME`（4）。當初選 3 的理由是「量到的
客戶區是 162×102」，而那是量錯了——框最內側那一道線是黑的，未揭露的地圖格
也是黑的。改用 4 之後小地圖客戶區正好是 **160**×100（＝遊戲配置的緩衝區
大小）、主視窗客戶區原點正好是 **(172,42)**（＝參考幀上 tile 內容的起點）。

**結果（掃過海浪與閃爍兩個相位，不需要任何位移）**：

| 區域 | 結果 |
|---|---|
| 主地圖 client `(172,42)` 608×538 ＝ 327,104 px | **只差 54 px** ＝ 原版截圖上的滑鼠游標 |
| World Map client `(4,23)` 160×100 | **逐點相同** |
| Status client `(4,267)` 160×329 | 82.7% 不同——還沒畫，下一個缺口 |

## 當第二個 oracle 用：活動單位閃爍（2026-09-05）

`scripts/blink-phases.txt` 直接跑原版 CIV.EXE，抓出活動單位閃爍的兩個相位。
**這是不經過 DOSBox 的第二個 oracle**，而且是決定性的（同樣的輸入給同樣
的畫面）。結果：

- 整個 800×600 畫面上，一秒內**只有一塊 30×30 在變**——位置 `(291,297)`，
  與 DOSBox 參考幀上的 `(460,298)` 差 `(169,1)`，正是 §4 那個已知的落點偏移。
- 兩個相位的 30×30 拿去和 civ1 重製端的兩個相位比：**各 0/900**，
  交叉配對 897/900。
- 小地圖那一格（`(83,73)` 2×2）跟著切：畫出來的相位是**索引 3**
  ＝ `RGB(128,128,0)` ＝ 系統色 3 ＝ 陸色；藏起來的相位是**索引 255**
  ＝ 純白。civ1 的 DS329 §3 從指令推出「塗 `byte_6C31D` ＝ `0xFF`」，
  這裡直接在畫面上量到實體 palette index 255。

過程中修掉兩件事：

1. **`clickwin` 改成由上而下找**（`WindowOrder` 反向掃）。原本由前往後
   找，一連串對話框都有「OK」時會點到最舊的那一個——腳本於是卡在後來
   才開的那個對話框的模態迴圈裡（`IsDialogMessage` 被呼叫 110 萬次而
   `GetTickCount` 只有 734 次，一眼就看得出沒進到遊戲主迴圈）。
2. **新增 `-clock-us`**：見 `docs/spec/006` §5.1。量佔空比一定要調小。

## civ1 那一側的對拍現在到哪裡（2026-09-05）

對拍的回歸護欄已經進到 civ1 專案本體，不再只是這裡的一次性比對：
`civ1go/internal/ui/mainmap_oracle_test.go` 的 `TestMainMapOracleFullClient`
拿 **13 張獨立產生的原版幀**比**整個主地圖客戶區** 610×540，例外區以外
合計 **4,221,356 個像素零差異**。

那一輪同時發現 **13 張參考幀有 7 張帶著原版自己沒畫好的區域**（詳見
civ1 的 `docs/re/328-main-map-oracle-full-client.md`）。這件事直接影響
這個專案的定位，寫在 `docs/spec/006` §4.1：擷取真實 Windows 畫面要跟
繪製賽跑，而 `nerun` 照指令數推進、畫面完成的時機是決定的——**橫向
落點修好之後，這裡產出的參考幀會比 DOSBox 擷取更適合當逐像素 oracle**。

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
