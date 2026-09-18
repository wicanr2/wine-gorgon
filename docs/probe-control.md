# 有界事件與亂數探針

本工具不內建 Civilization 的位址或亂數公式；遊戲配方在遊戲專案維護。
以下命令配合既有 normal input 腳本，以未修改的原版指令執行窄範圍實驗。

| 命令 | 契約 |
|---|---|
| `until selector:offset max_steps` | 在目標指令執行前停止；正整數上限、CPU 結束或未命中均失敗 |
| `keywin vk title` | 送完整鍵序到指定標題視窗；不依賴殘留焦點，不改原本 Focus |
| `poke selector:offset expected_hex replacement_hex` | 全範圍檢查後核對舊值；長度相同才一次替換；失敗不部分寫入 |
| `reg name expected16hex replacement16hex` | 比較後修改 AX/CX/DX/BX/SP/BP/SI/DI/IP；保留通用暫存器高 16 bits，IP/SP 檢查段界限，不執行指令或推進時鐘 |
| `watchmemuntil stop max_steps output.jsonl memory length [memory length …] [keepgoing]` | 逐外層步驟觀察記憶體改變，保存前後完整狀態；尾筆必須 complete；single_step=false 只能視為回呼聚合變化。多組範圍在**同一條 trace** 上觀測，每筆變更標 `range_index`；收據 schema 為 `wine-gorgon.memory-writes.v2` |
| `state output.json selector:offset length` | 保存 CPU 暫存器、步數與原始記憶體；不是可還原的整個 Process 快照 |
| `traceuntil stop max_steps watch output.jsonl memory length` | 執行至 stop，每次 watch 命中串流保存 CPU 與記憶體；尾筆明列 complete 與命中數 |
| `menu 文字\|#id` | 選一個選單項：送 `WM_COMMAND` 給擁有選單的視窗；子選單與停用中的項目一律拒絕，不送訊息 |
| `menus` | 印出選單樹與**目前的勾選／停用狀態**（程式自己用 `CheckMenuItem` 寫的那一份）|

位址可以寫成 `selector:offset`（十六進位）或 `seg<十進位段號>:offset`，後者由
`(段號 << 3) | 7` 換算（`docs/spec/002-address-space.md` §1），在總入口統一處理，
所有吃位址的命令都吃得到。**都不是 IDA 線性位址**：那個位址空間是反組譯器攤平出來的，
執行器沒有對照表，要用的呼叫端自己在遊戲專案裡換算。遊戲配方必須核對 EXE 雜湊與段內 bytes。

**手算 selector 算錯不會報錯**——那個位址多半仍然可讀，只是讀到別的東西，
於是整份收據看起來完整而內容是錯的。這就是 `seg<段號>:` 存在的理由。
RNG state 不一定在 SAV 內；在已知事件邊界明示注入，將前後值及命令保存。
比較來源 EXE、SAV、配方、工具 build 與產物雜湊，不能只看兩張圖相似。

## 步數用盡：`keepgoing`

`until`／`traceuntil`／`watchmemuntil` 跑滿步數上限時，**預設中止整個腳本**——
後面的指令一條都不跑。加上結尾的 `keepgoing` 只放寬這一種結果：步數用盡變成
**該命令的結果**，腳本繼續往下跑，執行器印一行說明，`watchmemuntil` 的收據尾筆
另外標 `steps_exhausted: true`。

放寬的只有步數用盡。**CPU 結束（HLT）、參數錯誤、寫檔失敗照樣中止腳本**——
那些是真的壞掉了，尾筆的 `cpu_halted` 分得出來。

為什麼要這個：沒有它，一份腳本只能有一段有界觀測（而且得放在最後），多段量測只好
退回「段間讀 state 再反推次數」——拿得到次數，拿不到位置。

## 收據會說自己看不到什麼

`watchmemuntil` 的尾筆有 `not_observed`：同值寫入看不見、回呼裡的寫入只看得到合併結果、
被步數截斷時再加一條。**「沒看到」與「沒發生」在收據裡長得一樣**，所以由工具自己列出來，
不要留給讀的人猜。

限制：探針在外層 CPU.Step 間觀察；API 內同步執行的 Win16 回呼不是可中斷的
觀察點。步數統計包含回呼耗用，但單次 Step 可越過門檻，仍須外層容器逾時。
本次 Civilization 隨機常式與事件的 trace 必須另外驗證，不由合成測試冒稱涵蓋。
`poke` 是實驗狀態注入，不能用這種收據宣稱正常玩家完整局；失敗 trace 不能當成功。

I542 已由 Civ1 原版執行證實：`watchmemuntil` 定位到先前函式的 `push ds`，
`reg` 與 `poke` 直接送入五種 ZOC 參數，兩種呼叫脈絡共 90 次呼叫，
原版亂數狀態未變。遊戲配方及來源雜湊保存在 civ1 專案的
`tools/oracle/wine_gorgon_zoc.py` 與 DS337；不把邊界注入算作正常完整局。
IP 僅檢查記憶體界限，呼叫者仍須以原始 bytes／IDA 證明指令邊界與堆疊契約。

`watchmemuntil` 記錄的是位元組改變；寫回相同值不產生紀錄。最後一筆變更
只能稱最後可觀察改值，所有 writer 的完整性仍需指令／資料流證據。

取樣窗是固定位址而 `SS:SP` 會移動：拿固定窗去讀堆疊時，窗開錯位置會讓**整批**取樣
落在窗外，而解讀程式看到的只是「沒資料」。讀 `state`／`traceuntil` 的收據時先用每筆的
`registers` 裡的 SP 檢查窗位置——**窗外不是零，是沒看到**。


## 選單

選單由視窗類別的 `lpszMenuName` 隱式載入（CIV.EXE 不呼叫 `LoadMenu`／
`SetMenu`／`TrackPopupMenu`；實跑一趟只有 `GetMenu` 1 次、`CheckMenuItem`
24 次、`EnableMenuItem` 81 次、`ModifyMenu` 16 次——**它只讀寫項目狀態，
不自己組選單**）。因此本工具做的是：解析 `RT_MENU` 範本成一棵樹、把勾選與
啟用狀態記住、把「點一個項目」變成送 `WM_COMMAND`。

**不做的**：滑鼠追蹤、彈出視窗的繪製、鍵盤導覽（Alt＋助憶字元）。腳本要的是
「那個命令有沒有進到程式的 WndProc」，不是選單長什麼樣——`menu` 送的訊息與
真 Windows 選了那一項時送的是同一則。

`menus` 的勾選狀態值得單獨講：它**不是本工具推出來的**，是程式自己呼叫
`CheckMenuItem` 寫進去的。所以它可以當成「這個選項現在是開的嗎」的獨立訊號
——與程式內部的選項字是兩條不同的路徑，兩邊對得上才算證實。
