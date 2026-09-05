# 003 — 16 位元 CPU 核心

## 1. 範圍

`internal/cpu` 實作 8086／80186／80286 的**應用層**指令集，加上 386 的
運算元大小前綴與 32 位元暫存器。

**位寬是參數，不是分支**：每條指令帶一個 `Size`（1／2／4 個 byte），
`66` 前綴只是把預設的 2 換成 4。所以 32 位元運算不需要第二套指令表，
之後接別的老遊戲要往上補也只是多幾個 case。CIV.EXE 確實用得到——
`01F7:0013` 的記憶體檢查就是 `pop eax` ＋ `cmp dword [B390], 002625A0h`。

**做**：整數運算與邏輯、位移與旋轉（含 186 的立即數形式）、ModRM 全部
定址模式、字串指令（含 REP／REPE／REPNE）、近程與遠程的呼叫與跳躍、
條件跳躍、`ENTER`／`LEAVE`、`LES`／`LDS`、旗標操作、`PUSHA`／`POPA`、
186 的三運算元 `IMUL`；386 的 `66` 前綴、`0F 8x`（近程條件跳躍）、
`0F 9x`（`SETcc`）、`0F AF`（雙運算元 `IMUL`）、`0F B6/B7/BE/BF`
（`MOVZX`／`MOVSX`）。

**不做**（明記，不是忘了）：

| 不做的東西 | 理由 |
|---|---|
| 保護模式與 LDT | selector 當 handle 用就夠（spec 001 §3）|
| 8087／x87 浮點 | CIV.EXE 匯入 `WIN87EM`，浮點走那支模擬器的 API，不走 ESC 指令 |
| `INT` 的實際派送 | Win16 應用透過 API 而非中斷；碰到就停下來報號碼 |
| 32 位元定址（`67` 前綴）、`FS`／`GS` | CIV.EXE 沒用到；碰到會帶位址停下 |
| 保護模式的系統指令（`LGDT` 一族）| 不做保護模式 |
| BCD 調整（`AAA`／`DAA` 一族）| 沒量到；碰到會以「未實作的 opcode」停下 |

上表最後兩列的處理方式相同，而那正是這一層的設計重點：**未實作的東西
要以帶位址的錯誤停下，不能靜靜跳過**。接一支新程式時最貴的不是實作指令，
是找出它停在哪裡。

## 2. 記憶體介面

CPU 只認一個介面：

```go
type Bus interface {
	ReadU8(sel, off uint16) (uint8, error)
	ReadU16(sel, off uint16) (uint16, error)
	WriteU8(sel, off uint16, v uint8) error
	WriteU16(sel, off uint16, v uint16) error
}
```

`*win16.Memory` 直接滿足它。抽成介面的目的是讓指令測試不必先鋪一個 NE：
`internal/cpu` 的測試用的是一個四段各 64 KiB 的假匯流排，**指令的正確性
不依賴載入器**。

位移不繞回：越界回錯誤而不是 wrap。真硬體在 16 位元段裡是會繞的，但在
這個工具上，「繞回去讀到別的東西」等於把一個 bug 換成一份看起來合理的
垃圾資料——寧可停下來。

## 3. 兩個容易寫錯而且不會當掉的地方

**預設段。** 只要 16 位元定址的基底用到 `BP`（`[BP+SI]`、`[BP+DI]`、
`[BP+disp]`），預設段就是 `SS`，其餘是 `DS`。寫錯的話局部變數會去讀資料段，
不當機，只是拿到看起來合理的值。測試 `TestBPAddressingDefaultsToStack`
正反兩面都驗：`SS` 那格要有值，`DS` 的同一個位移要是空的。

**字串指令的目的地。** 來源段可以被前綴覆寫，**目的地永遠是 `ES`**，
覆寫不了。這是 x86 少數不對稱的地方。

## 4. API 攔截的接法

CPU 只提供一個回呼：

```go
OnFarCall func(c *CPU, sel, off uint16) (handled bool, err error)
```

`farTransfer` 先把 `CS:IP` 推入堆疊（far call）、再設好新的 `CS:IP`、
**然後**才呼叫回呼。所以處理器看到的狀態和真正的被呼叫方一模一樣：
回傳位址已經在堆疊上。做完事就呼叫 `c.RetFar(參數位元組數)` 回去——
Win16 是 pascal 呼叫慣例，參數由被呼叫方清掉。

`internal/win16.Process` 在這個回呼上收口：只有落到 `ThunkSel` 的才算
API 呼叫，查 `Module.ImportAt` 翻成 `KERNEL.#91` 這種鍵，再找登記的
處理器。沒登記就回 `UnhandledAPIError`，訊息裡帶「哪一支、誰呼叫的、
第幾步」。

## 5. 驗收（M1b，已達成）

`nerun` 對原版 `CIV.EXE`：

```
進入點 000F:0000，DS=042F SS:SP=042F:E4BE
執行 1 條指令，攔到 1 次 API 呼叫
  #1      KERNEL.#91               ← 000F:0005
停在未實作的 API：KERNEL.#91
```

`KERNEL.#91` 是 `InitTask`——Windows 啟動碼的第一件事，位置正確。

`-stub`（每支 API 都回 0，不彈參數）是一次**量測**而不是模擬：

```
執行 107 條指令，攔到 2 次 API 呼叫
  #1      KERNEL.#91               ← 000F:0005
  #61     WIN87EM.#1               ← 000F:41AF
停在 CPU：cpu: 000F:00CD（第 107 步）未實作的軟體中斷 INT 21h
```

`INT 21h` 的 `AH=4C` 是結束行程：`InitTask` 回 0 被啟動碼判成失敗而中止。
這正是預期——它說明的是「要往下走必須真的實作 `InitTask`」，不是 CPU 有問題。

## 6. 下一步

M2：`KERNEL` 的最小集合。第一批照上面的量測結果排——`InitTask`、
`WIN87EM.#1`（浮點模擬器初始化）、`InitApp`、`GetModuleHandle` 一族。
處理器鍵一律用 **(模組, 序號)**，不用名稱：CIV.EXE 的匯入 100% 是序號式
（spec 002 §4.2）。
