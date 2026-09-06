# 有界事件與亂數探針

本工具不內建 Civilization 的位址或亂數公式；遊戲配方在遊戲專案維護。
以下命令配合既有 normal input 腳本，以未修改的原版指令執行窄範圍實驗。

| 命令 | 契約 |
|---|---|
| `until selector:offset max_steps` | 在目標指令執行前停止；正整數上限、CPU 結束或未命中均失敗 |
| `keywin vk title` | 送完整鍵序到指定標題視窗；不依賴殘留焦點，不改原本 Focus |
| `poke selector:offset expected_hex replacement_hex` | 全範圍檢查後核對舊值；長度相同才一次替換；失敗不部分寫入 |
| `reg name expected16hex replacement16hex` | 比較後修改 AX/CX/DX/BX/SP/BP/SI/DI/IP；保留通用暫存器高 16 bits，IP/SP 檢查段界限，不執行指令或推進時鐘 |
| `watchmemuntil stop max_steps output.jsonl memory length` | 逐外層步驟觀察記憶體改變，保存前後完整狀態；尾筆必須 complete；single_step=false 只能視為回呼聚合變化 |
| `state output.json selector:offset length` | 保存 CPU 暫存器、步數與原始記憶體；不是可還原的整個 Process 快照 |
| `traceuntil stop max_steps watch output.jsonl memory length` | 執行至 stop，每次 watch 命中串流保存 CPU 與記憶體；尾筆明列 complete 與命中數 |

位址使用執行器的 selector:offset，**不是 IDA 線性位址**。固定 NE 段號的
selector 由 `SegSelector` 計算；遊戲配方必須核對 EXE 雜湊與段內 bytes。
RNG state 不一定在 SAV 內；在已知事件邊界明示注入，將前後值及命令保存。
比較來源 EXE、SAV、配方、工具 build 與產物雜湊，不能只看兩張圖相似。

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
