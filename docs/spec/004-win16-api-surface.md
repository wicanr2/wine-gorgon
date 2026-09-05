# 004 — Win16 API 表面與序號表

## 1. 表面有多大

`CIV.EXE` 匯入 **157 支** API，全部以序號（不是名稱）引用：

| 模組 | 支數 |
|---|---:|
| `USER` | 76 |
| `GDI` | 43 |
| `KERNEL` | 32 |
| `COMMDLG` | 3 |
| `MMSYSTEM` | 2 |
| `WIN87EM` | 1 |

這個數字是「實作 API 而不是模擬機器」這條路線的前提（spec 001 §2）。

## 2. 序號到名稱：兩份獨立解析的交集（`已證實`）

序號式匯入的檔案裡**沒有名稱**，所以名稱一定來自外部知識。這裡的作法是
讓兩個互不相干的實作各自解析同一個檔案，再比對：

- `internal/ne` 讀模組參考表與每段的重定位表，得到 157 組 (模組, 序號)。
- IDA Pro 9.4 用它自己的 NE 載入器與 Win16 序號資料庫，得到 157 組
  (模組, 序號, 名稱)（`civ1/docs/re/generated/ida94/imports.json`）。

比對結果：

```
我的解析 157 筆，IDA 157 筆
只有我有 0：[]
只有 IDA 有 0：[]
```

**兩邊完全相同，任一邊都沒有多出來的項。** 這同時證實了兩件事：NE 解析
沒有漏讀或多讀，以及名稱可以安全地附到序號上。表在
`internal/winapi/ordinals.go`。

## 3. 參數位元組數：不同來源，要分開看（`強推論`）

`Func.ArgBytes` 的來源是 **Win16 文件上的函式簽章**，不是從 `CIV.EXE`
量出來的。Win16 是 pascal 呼叫慣例——參數由被呼叫方清掉——所以這個數字
錯了就等於堆疊指標漂掉。

**這個數字錯了會安靜地錯。** `BitBlt` 有九個參數（八個 word 加一個
dword ＝ 20 個 byte），第一版手算成 22。結果不是當機：Borland 的
`leave`（`mov sp, bp`）會把漂掉的 SP 蓋回去，於是 1,036 次 `BitBlt`
全部從錯開兩個 byte 的位置讀參數，拿到的 HDC 是 `0000`、`042F`（DGROUP
的 selector）這種值，處理器找不到就直接回傳——**畫面上只是什麼都沒畫**。

從那次之後改成機器算：`internal/winapi/ordinals_test.go` 為每一支寫下
**參數型別序列**（`w` ＝ 16 位元、`d` ＝ 32 位元），由測試加總並和表比對。
手算換成機器算，這一類錯就不會再進來。

真正救回來的是另一件事：**handle 型參數查不到就記一筆 `Note`**。
「BitBlt 的目的 DC 0000 不存在」這一行直接指出問題在參數而不在繪圖。
凡是參數裡有 handle 的 API 都值得這樣做。

`-1` 表示不是普通的 pascal 函式：`KERNEL.__AHSHIFT`（資料匯入）與
`WIN87EM.__FPMATH`（用 BX 選功能）。它們走 `RawHandler`，自己負責彈堆疊。

## 4. 派送的形狀

```go
type Handler func(p *Process, a Args) (uint32, error)
```

`Args` 的位移是「從簽章**左邊**算起的位元組數」，和 `windows.h` 的宣告
逐字對得起來。回傳值是 `DX:AX`。**處理器不自己彈堆疊**——由派送端統一
按 winapi 表上的 `ArgBytes` 做，把「彈幾個 byte」從 157 個地方收斂到一個。

`InitTask` 這種多回傳值的（AX／CX／DX／SI／DI／ES 全都要設）直接改
`p.CPU`，回傳值只負責 DX:AX 那兩個。

## 5. 已接上的（M2 進行中）

`KERNEL`：`InitTask`、`WaitEvent`、`GetWinFlags`、`GetFreeSpace`、
`GlobalCompact`、`GetDOSEnvironment`、`GetModuleFileName`、
`GlobalAlloc/ReAlloc/Free/Lock/Unlock/Size/Handle`、
`MakeProcInstance`／`FreeProcInstance`、`lstrlen`／`lstrcpy`、`hmemcpy`。

`USER`：`InitApp`、`GetFreeSystemResources`、`MessageBox`、`GetTickCount`、
`MessageBeep`、`GetSystemMetrics`（只有螢幕尺寸，其餘記進 `Notes`）。

`WIN87EM`：`__FPMATH` 一律回成功、不做浮點（見 `internal/win16/api_win87em.go`
的說明；還沒量到 CIV.EXE 哪裡真的做浮點）。

DOS／BIOS 中斷（Borland 啟動碼要的，不是 Windows 的一部分）：
`INT 1Ah AH=00`、`INT 21h AH=0Eh/19h/30h/44h/47h/4Ch`。時間走
`Process.Clock`，**預設是跟著指令數走的 StepClock**——對拍工具的每次執行
都要能重現，主機時鐘是最容易讓兩次執行分岔的東西。

## 6. 三個會影響第一幀的東西（現在是`假說`）

1. **`GetSystemMetrics` 的視窗邊框尺寸**（`SM_CYCAPTION`、`SM_CXFRAME`
   一族）。它們決定客戶區大小，也就決定地圖從哪一個像素開始畫。目前只
   回螢幕尺寸，其餘回 0 並記進 `Notes`。**接到 M4 之前必須逐項給值，
   而且要和 civ1 專案量過的版面對得起來。**
2. **`GetWinFlags` 的回傳值**。遊戲拿它決定走哪條記憶體路徑。
3. **`InitTask` 回的 `CX`（堆疊下限）**。取檔頭的 `ne_stack` 是語意上
   合理的讀法，沒有對原版量過。

## 7. 目前跑到哪裡

`nerun -stub`（未實作的一律回 0）能跑到遊戲自己的 `WinMain`，並在
第 2567 步彈出它自己的訊息框：

```
訊息框（第 2567 步）[CIV] Too many clocks or timers!
```

那是 `SetTimer` 回 0 造成的——遊戲的錯誤處理是對的，是我們還沒實作
計時器。下一步是 M3 的視窗與訊息迴圈。
