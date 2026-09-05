# 接手點

## 現在跑得到哪裡

```sh
docker run --rm --network none --memory 2g --cpus 2 \
  -e GOPATH=/tmp/gp -e GOCACHE=/tmp/gc -e GOPROXY=off -e GOFLAGS=-buildvcs=false \
  --user "$(id -u):$(id -g)" \
  -v "$PWD:/src" -v /path/to/CIV:/game:ro -v "$PWD/workplace/out:/out" \
  -w /src dsds-go:1.25 \
  sh -c 'export PATH=/usr/local/go/bin:$PATH; go run ./cmd/nerun -data /game -script /out/play.txt /game/CIV.EXE'
```

腳本 `play.txt`：

```
run 60000000
click 250,280      # Start a New Game
run 3000000
click 405,369      # OK
run 60000000
shot /out/p2.png
```

跑到主選單，六個項目以原版 `CIVFONTS.FON` 的哥德體畫出來，可以點。
點完 `Start a New Game` ＋ `OK` 之後遊戲把那六個選項的視窗銷毀了
（`WM_DESTROY` ×6，所以那一步有被處理），但**沒有出現下一個對話框**，
之後就停在自己的訊息迴圈裡空轉。

## 下一步（照這個順序）

1. **查清楚「按下 OK 之後為什麼沒有下一個畫面」。**
   已知：訊息迴圈還在跑（`PeekMessage` 每幾十條指令一次），熱點在
   `00A7:02F3`（`PeekMessage` 的呼叫點）與 `01F7:030E`。已排除的：
   不是當機、不是未實作的 API（`nerun` 會報）、不是步數不夠（多跑 6,000 萬
   條沒有變化）。**下一步該做的是把 OK 之後的訊息序列印出來**，
   看遊戲在等哪一則。`Process.MsgCount` 已經有計數，缺的是逐則的紀錄。
2. **視窗銷毀後要重畫下面的東西。** 目前銷毀不會讓任何人重畫，
   螢幕上會留著已經不存在的視窗的像素。這在對拍時是致命的。
3. **`GetSystemMetrics` 的邊框尺寸逐項核對**（`docs/spec/004` §6）。
   目前是 Windows 3.1 VGA 的常見值，**沒有對原版量過**，而它們決定
   客戶區大小，也就決定地圖從哪一個像素開始畫。
4. **`Polygon` 的填色**（目前只畫外框）。
5. 走到主地圖之後才談 M4 的逐點比對。

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
