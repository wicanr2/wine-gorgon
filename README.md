# wine-gorgon

**無頭、決定性、可以當 Go 套件 import 的 Win16 執行器**——為**程式化對拍**而生，
不是為了給人看畫面。你問「原版跑到這一步時這個像素／這個變數是多少」，它當場回答。

第一個案例是《文明帝國》（MicroProse，1993，Windows 3.1）的 remake
[`civ1-remake-cht`](https://github.com/wicanr2/civ1-remake-cht)。

姊妹專案 [`dosgolem`](https://github.com/wicanr2/dosgolem) 做的是同一件事的
DOS 版本；wine-gorgon 換掉最底下兩層（機器 → Win16 API），觀測層的形狀照抄。

## 起源：Wine 不好跟 AI agent 配合

Wine 是為「人坐在前面用 Windows 程式」設計的——畫面出在 X 上、輸入靠 X 事件、
要觀察就得截圖。人用的時候這樣剛好。

AI agent 在 remake 專案裡做的是另一件事：**對拍**，把 remake 的畫面與狀態逐項
比對原版。對拍需要的是**問得到、量得到、可重播**，不是看得到。

- 「這一格的單位圖畫在哪個像素」隔著截圖只能從畫面反推，而截圖本身還帶著
  視窗框、滑鼠游標、調色盤動畫相位。
- 「跑到收租常式再停下來」隔著 X 只能睡兩秒再猜。
- 「這一幀的調色盤 entry 64..71 輪到第幾格」——截圖給不了答案，只給你結果。

civ1 remake 目前的對拍是 docker ＋ Xvfb ＋ Wine／DOSBox ＋ xdotool ＋ 截圖。
一次探針約 1–2 分鐘，而且每一次都要處理「探針自己的滑鼠游標算不算差異」
這種問題——最近一輪的 chrome 對拍殘差 54 px，全部是探針的箭頭游標。

## 名字

**wine-gorgon**：`wine` 是它取代的東西，`go` 寫在中間（Go 語言），
gorgon 是那隻怪獸。

雅典娜把戈爾貢的頭裝在神盾上——**看一眼就把東西定格**。
抓幀對拍要的正是這個：讓畫面在指定的一刻停住，然後逐點數。

## 走哪條路：實作 API，不模擬 Windows

有兩條路可以跑 Win16 程式：

1. **模擬整台機器**，再在上面跑真正的 Windows 3.1。要 286／386 保護模式、
   VMM、虛擬機管理員——而且跑起來之後你面對的還是同一個「只能從外面看畫面」
   的問題。
2. **直接實作 Win16 API**：載入 NE 執行檔，用 16 位元 x86 核心跑它的碼，
   `KERNEL`／`USER`／`GDI` 用 Go 實作，GDI 畫進記憶體裡的 DIB。

wine-gorgon 走第二條，理由是**表面小到量得出來**。用 `cmd/neinfo` 量 CIV.EXE：

```
NE 檔頭 0x60；段 133；模組參考 6；進入點 段 1:0000
程式段 69、資料段 64、可移動 133；重定位 20124 筆

=== 匯入表面（每個模組的相異項數）===
  COMMDLG       3
  GDI          43
  KERNEL       32
  MMSYSTEM      2
  USER         76
  WIN87EM       1

相異匯入 157 項，被 2546 筆重定位引用
```

**157 項**就是要實作的全部。而且引用次數極度集中：前 20 項就佔掉一半以上的
呼叫點。這個數字是先量出來的，不是估的——「先量再寫」是這個專案的第一條規矩。

civ1 這個案例還有一個結構上的便宜：它自己帶一套繪圖函式庫（`GR_*`，
`gr_pic.c`／`PortTileBlt`），大部分繪圖發生在自己的離屏 port 上，GDI 只負責
最後的 `BitBlt`、文字與調色盤。要逐像素對拍的那一層，反而不在 GDI 裡。

## 分層

照 dosgolem 的四層拆，換掉下面兩層：

| 層 | 是什麼 | 換一個程式要不要改 |
|---|---|---|
| **機器** `internal/cpu` | 16 位元 x86（8086／186／286 應用層指令）＋ selector 定址 | **不用** |
| **系統** `internal/ne`、`internal/kernel`、`internal/user`、`internal/gdi` | NE 載入、Win16 三大模組 | **不用**。缺的 API 就補，補完誰都受惠 |
| **runtime** `runtime/borlandc` | 編譯器慣例：Borland C++ Win16 的 far prolog、helper、`__FPMATH` | **同一個編譯器就複用**；換 MSC／VB 就在 `runtime/` 下新增一包 |
| **程式** `apps/civ1` | 那一支程式自己的位址、流程、攔截點 | **一定要自己寫** |

`selector` 不做 LDT。Win16 應用程式看到的 selector 只是「一塊記憶體的 handle」，
所以查表就夠——這是這條路能在合理篇幅內做完的關鍵決定。

## 現況

| | 里程碑 | 狀態 |
|---|---|---|
| M0 | NE 載入器：段、重定位、匯入、進入點 | **完成**。對 CIV.EXE 的數字與獨立的 Python 解析器逐項吻合 |
| M1a | 位址空間：段配置、重定位、API thunk | **完成**。CIV.EXE 133 段 577 KiB、20,124 筆重定位、157 個 thunk |
| M1b | 16 位元 CPU 核心 ＋ selector 定址 | **完成**。8086／186／286 應用層指令，加 386 的 `66` 前綴與 32 位元暫存器 |
| M2 | KERNEL：`Global*`、檔案、資源 | **完成**。全域堆積（含 >64 KiB 的 huge 配置）、檔案系統、NE 資源表 |
| M3 | USER 訊息迴圈骨架 ＋ GDI BitBlt | 進行中。已跑到遊戲主選單並能用腳本點選；缺文字繪製 |
| M4 | **主地圖第一幀與原版 oracle PNG 逐點相同** | 未開始 |

目前 `nerun` 帶著原版安裝目錄，能跑 6,000 萬條指令、讀進五個
`CIVDATA*.RSC`、建出遊戲自己的視窗類別（`CIV`、`CIVDIALOG`、`WDWSMMAP`、
`WDWSTATUS`…），並停在可以操作的主選單：

```
  0806 V RanDoMRadio (215,272 156x18) "Start a New Game"
  0807 V RanDoMRadio (215,290 156x18) "Load a Saved Game"
  0808 V RanDoMRadio (215,308 156x18) "Play on EARTH"
  0809 V RanDoMRadio (215,326 156x18) "Customize World"
  080A V RanDoMRadio (215,344 156x18) "View Hall of Fame"
  080B V RanDoMRadio (215,362 156x18) "Quit"
```

畫面上還是空的：那些選項的字是 1,662 次 `TextOut` 畫的，而字型還沒接
（遊戲用它自己的 `CIVFONTS.FON`）。**下一步是點陣字型與圖片的 blit 路徑。**

M4 是 MVP 的驗收線。選它是因為 civ1 那邊已經有原版的參考幀，判準現成——
不必新做一組 oracle 來驗這個 oracle。

## 授權

RRSAL-1.0（見 [`LICENSE`](LICENSE)）。非商業使用免費，含修改與再散布；
實況與影片明示允許；商業使用請洽談。
