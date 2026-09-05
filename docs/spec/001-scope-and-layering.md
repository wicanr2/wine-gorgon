# 001：範圍與分層

- 狀態：`READY`（M0 已交付：`internal/ne` ＋ `cmd/neinfo`）
- 日期：2026-09-05

## 1. 這個工具回答什麼問題

一句話：**「原版跑到這一步時，畫面／記憶體是什麼樣子」，用程式問，不用眼睛看。**

remake 專案的對拍需要三件事，而截圖只給得起第三件：

| 需要 | 截圖給得起嗎 |
|---|---|
| **問得到**：任意時刻讀任意位址、任意 DC 的像素 | 否——只能從最終畫面反推 |
| **量得到**：停在指定的指令、指定的 API 呼叫 | 否——只能睡幾秒再猜 |
| **可重播**：同樣輸入必得同樣輸出，含亂數與計時 | 否——時鐘與 X 事件都不受控 |

## 2. 走「實作 API」而不是「模擬機器」

見 README「走哪條路」。決定的依據是**量出來的表面大小**：CIV.EXE 只用 157 項
Win16 匯入。這個數字由 `cmd/neinfo` 產出，並與一支獨立寫的 Python 解析器
交叉驗過（段 133、模組 6、相異匯入 157、重定位引用 2,546，四項全部吻合）。

**兩份獨立實作得到同一個數字**才算數——這是這個專案對「已證實」的定義。

## 3. selector 當 handle，不做 LDT

Win16 應用程式碼裡的 `mov es, ax` 之後 `es:[bx]`，語意是「拿 selector 指向的
那塊記憶體」。應用層看不到 descriptor 的內容，也幾乎不會自己造 descriptor。

所以 `internal/cpu` 的段暫存器是一個 `selector → []byte` 的查表，不是
GDT／LDT 模擬。這決定讓 M1 的篇幅落在可完成的範圍內。

**已知的邊界**：程式若自己呼叫 `AllocSelector`／`ChangeSelector`／
`__AHSHIFT` 做 huge pointer 算術，要另外處理。CIV.EXE 的匯入表裡沒有這些，
所以 MVP 不做——**這是量過才決定不做的，不是忘了**。

## 4. API 攔截點在重定位表

NE 的 `IMPORTORDINAL`／`IMPORTNAME` 重定位就是 far call 的目標。載入時把
每一個匯入目標填成一個**魔術 far 位址**（段號取自保留區），CPU 執行到那裡
就轉呼叫 Go 實作。

好處是攔截點與「這支程式用了哪些 API」是同一份資料，不會漂：
`neinfo` 印得出來的東西，就是 `internal/kernel` 等必須提供的東西。

## 5. 分層與「演算法通用，位址不通用」

分層見 README。從 dosgolem 學到的一條：**同一份 runtime 連結進不同 binary
會落在不同位址**。所以 `runtime/borlandc` 放的是「Borland Win16 的 far prolog
長什麼樣」這種形狀知識，具體位址由 `apps/*` 的 Config 給。

拿別的程式的位址套過來不會報錯，只會一次都攔不到——**這種錯不會自己現形**，
所以要靠分層在型別上擋掉。

## 6. MVP 的驗收線

**M4：主地圖第一幀與原版 oracle PNG 逐點相同。**

選它的理由是判準現成：`civ1-remake-cht` 已經有 Win3.1／DOSBox 探針拍下的
參考幀，而且那邊已經把「探針自己的滑鼠游標」「調色盤動畫相位」這兩個干擾
項辨識出來了。**不必為了驗這個 oracle 再做另一個 oracle。**

反過來說，wine-gorgon 做完之後那兩個干擾項會消失——它不畫游標，
而調色盤相位是它自己的狀態，問得到。

## 7. 明記的不做

- **386 enhanced mode、VxD、多工**：只跑一個 task。
- **GDI 的完整 region／clip 語意**：先做矩形裁切；CIV.EXE 沒有用 region 的匯入。
- **列印、DDE、OLE、剪貼簿**：匯入表裡沒有。
- **週期精確的時序**：計時由虛擬時鐘推進，`GetTickCount` 由腳本決定。
  這是**刻意的**——決定性比擬真重要。

## 8. 里程碑

| | 內容 | 驗收 |
|---|---|---|
| M0 | NE 載入器 | 對 CIV.EXE 的四個數字與獨立 Python 解析器吻合 **✓** |
| M1 | CPU ＋ selector ＋ thunk | 跑到 `WinMain` 進入點不崩，攔得到第一個 API 呼叫 |
| M2 | KERNEL：`Global*`、檔案、資源 | 載完五個 `.RSC`，資源位元組與 `tools/re/ne.py` 抽出的相同 |
| M3 | USER 骨架 ＋ GDI DIB／BitBlt | 主視窗建立、`WM_PAINT` 走完、吐得出一張 DIB |
| M4 | 主地圖第一幀逐點相同 | 與 `root-main-map.png` 的內容區 0 px 差異 |
