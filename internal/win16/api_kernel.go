package win16

import (
	"github.com/wicanr2/wine-gorgon/internal/cpu"
)

// KERNEL 的最小集合。
//
// 全域堆積直接用位址空間的動態 selector：`spec 001` §3 的「selector 就是
// handle」在這裡兌現——`GlobalAlloc` 回的 handle 就是 selector，
// `GlobalLock` 回的 far 指標就是 `handle:0000`。真 Windows 的可移動區塊
// handle 和 selector 是兩個數字，這裡合成一個；代價是 `GlobalReAlloc`
// 不能真的搬家（selector 不變），而那正好是我們要的。

// RegisterKernel 把 KERNEL 的處理器登記上去。
func RegisterKernel(p *Process) {
	h := p.Handlers

	// FatalExit（KERNEL.#1）在 Wine 9.0 的 Win16 spec 仍是 stub，沒有可用的
	// pascal 參數形狀。保留目前 AX 的低位元組作診斷離開碼；真正撞到 caller
	// 時再依該 callsite 訂正，不能在這裡猜一個 stack 參數。
	h["KERNEL.#1"] = func(p *Process, _ Args) (uint32, error) {
		code := uint8(p.CPU.R16(cpu.AX))
		p.CPU.Halt = true
		return 0, &ExitError{Code: code}
	}

	// InitTask：Windows 啟動碼的第一件事，回傳值全部在暫存器裡。
	//   AX = 1 成功
	//   CX = 堆疊下限（bytes）
	//   DX = nCmdShow
	//   SI = 前一個實體的 hInstance（0 表示沒有）
	//   DI = 本實體的 hInstance
	//   ES:BX = PSP 裡的命令列
	h["KERNEL.#91"] = func(p *Process, _ Args) (uint32, error) {
		c := p.CPU
		ds := c.Seg[cpu.DS]
		c.SetR16(cpu.CX, p.StackLimit)
		c.SetR16(cpu.SI, 0)
		c.SetR16(cpu.DI, ds) // Win16 的 hInstance 就是 DGROUP 的 selector
		c.Seg[cpu.ES] = p.PSP
		c.SetR16(cpu.BX, 0x80)
		return 1<<16 | 1, nil // DX = SW_SHOWNORMAL、AX = 成功
	}

	h["KERNEL.#30"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // WaitEvent

	// DOS3Call（KERNEL.#102）是 register 入口：以目前暫存器執行 INT 21h。
	// wine-gorgon 的 handler 派送仍負責 far return；把 INT 21h 改過的 DX:AX
	// 原樣回傳，避免一般 handler 的回傳寫回覆蓋 DOS 結果。
	h["KERNEL.#102"] = func(p *Process, _ Args) (uint32, error) {
		handled, err := p.onInt(p.CPU, 0x21)
		if err != nil {
			return 0, err
		}
		if !handled {
			return 0, errUnsupported("DOS3Call 不支援 INT 21h AH=%02X", uint8(p.CPU.R16(cpu.AX)>>8))
		}
		return uint32(p.CPU.R16(cpu.DX))<<16 | uint32(p.CPU.R16(cpu.AX)), nil
	}

	// LockSegment（KERNEL.#23）：把一個段釘住，不讓它被移動或丟棄。
	// wine-gorgon 的段一載入就固定在位址空間裡，兩件事都不會發生，所以這是 no-op；
	// 要緊的是回傳值不能是 0，否則呼叫端會當成「鎖不住」。
	//
	// 參數 0xFFFF 是「目前的資料段」的慣例。PTO2 第 17 條指令就這樣呼叫
	// （AX=FFFF），與 Borland 啟動碼在 InitTask 之後鎖 DGROUP 的行為吻合。
	h["KERNEL.#23"] = func(p *Process, a Args) (uint32, error) {
		sel := a.Word(0)
		if sel == 0xFFFF {
			sel = p.CPU.Seg[cpu.DS]
		}
		return uint32(sel), nil
	}

	// UnlockSegment（KERNEL.#24）成對出現，同樣是 no-op；PTO2 目前沒匯入它，
	// 撞到再補，不先寫沒有呼叫端的程式碼。

	// GetVersion（KERNEL.#3）：LOWORD 是 Windows 版本，低位元組 major、高位元組 minor
	// ——Wine 的 GetVersion16 用 MAKEWORD(major, minor)，所以 3.10 是 0x0A03。
	// HIWORD 是 DOS 版本，反過來排：Wine 的 int21.c 取 DOS major 用
	// HIBYTE(HIWORD(...))，所以 6.22 是 0x0616。
	//
	// 這兩個版本號沒有從原版量過。PTO2 若有版本分支，要回頭量它實際跑在哪個
	// Windows／DOS 版本，不要讓這裡的預設值決定遊戲走哪條路。
	h["KERNEL.#3"] = func(p *Process, _ Args) (uint32, error) {
		const win310, dos622 = 0x0A03, 0x0616
		return dos622<<16 | win310, nil
	}

	// GetWinFlags：保護模式 ＋ 386 ＋ 加強模式。這組值決定遊戲走哪條
	// 記憶體路徑，之後如果發現它靠這個分支，要回頭量原版跑在哪個模式。
	h["KERNEL.#132"] = func(p *Process, _ Args) (uint32, error) {
		const wfPMode, wfCPU386, wfEnhanced = 0x0001, 0x0004, 0x0020
		return wfPMode | wfCPU386 | wfEnhanced, nil
	}

	h["KERNEL.#49"] = func(p *Process, a Args) (uint32, error) { // GetModuleFileName
		sel, off := a.Ptr(2)
		max := int(a.Word(6))
		n := 0
		for ; n < len(p.ModulePath) && n < max-1; n++ {
			if err := p.Mod.Mem.WriteU8(sel, off+uint16(n), p.ModulePath[n]); err != nil {
				return 0, err
			}
		}
		if err := p.Mod.Mem.WriteU8(sel, off+uint16(n), 0); err != nil {
			return 0, err
		}
		return uint32(n), nil
	}

	h["KERNEL.#169"] = func(p *Process, _ Args) (uint32, error) { return 4 << 20, nil } // GetFreeSpace
	h["KERNEL.#25"] = func(p *Process, _ Args) (uint32, error) { return 4 << 20, nil }  // GlobalCompact
	// GetDOSEnvironment：回一個 far 指標指向 DOS 環境區塊。回 0 會讓
	// 呼叫端拿 ES=0000 去讀，第一個位元組就炸。
	h["KERNEL.#131"] = func(p *Process, _ Args) (uint32, error) {
		return uint32(p.Env) << 16, nil
	}

	// GlobalAlloc(UINT flags, DWORD bytes)
	h["KERNEL.#15"] = func(p *Process, a Args) (uint32, error) {
		size := a.Long(2)
		if size == 0 {
			size = 1
		}
		// GMEM_ZEROINIT 沒設時內容是「未定義」。這裡一律給零：
		// 「未定義」在對拍工具上必須可重現，不能真的是垃圾。
		var b *Block
		if size > 0x10000 {
			b = p.Mod.Mem.AllocHuge("GlobalAlloc", int(size))
		} else {
			b = p.Mod.Mem.Alloc("GlobalAlloc", int(size))
		}
		if b == nil {
			// selector 用完了。回 0 是 Win16 的「配不到記憶體」，
			// 遊戲會自己處理；但這是我們的上限不是它的，所以記一筆。
			p.note("GlobalAlloc(%d) 配不到 selector（動態 selector 用完）", size)
			return 0, nil
		}
		return uint32(b.Sel), nil
	}

	// GlobalReAlloc(HGLOBAL, DWORD bytes, UINT flags)
	h["KERNEL.#16"] = func(p *Process, a Args) (uint32, error) {
		sel, size := a.Word(0), a.Long(2)
		if size == 0 || size > 0x10000 {
			return 0, nil
		}
		if !p.Mod.Mem.Resize(sel, int(size)) {
			return 0, nil
		}
		return uint32(sel), nil
	}

	h["KERNEL.#17"] = func(p *Process, a Args) (uint32, error) { // GlobalFree
		if p.Mod.Mem.FreeHuge(a.Word(0)) {
			return 0, nil
		}
		return uint32(a.Word(0)), nil // 失敗時回傳原 handle，這是 Win16 的約定
	}

	h["KERNEL.#18"] = func(p *Process, a Args) (uint32, error) { // GlobalLock
		sel := a.Word(0)
		if _, ok := p.Mod.Mem.Block(sel); !ok {
			return 0, nil
		}
		return uint32(sel) << 16, nil // sel:0000
	}

	h["KERNEL.#19"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // GlobalUnlock

	h["KERNEL.#20"] = func(p *Process, a Args) (uint32, error) { // GlobalSize
		b, ok := p.Mod.Mem.Block(a.Word(0))
		if !ok {
			return 0, nil
		}
		n := uint32(0)
		for b != nil {
			n += uint32(len(b.Data))
			if b.HugeNext == 0 {
				break
			}
			b, _ = p.Mod.Mem.Block(b.HugeNext)
		}
		return n, nil
	}

	h["KERNEL.#21"] = func(p *Process, a Args) (uint32, error) { // GlobalHandle
		sel := a.Word(0)
		return uint32(sel)<<16 | uint32(sel), nil // handle 與 selector 同一個數字
	}

	// MakeProcInstance 在真 Windows 上是造一段設好 DS 的 thunk。這裡的
	// DS 從頭到尾就是 DGROUP，所以原樣回傳即可——這是「實作 API 而不是
	// 模擬機器」的一個具體例子。
	h["KERNEL.#51"] = func(p *Process, a Args) (uint32, error) { return a.Long(0), nil }
	h["KERNEL.#52"] = func(p *Process, _ Args) (uint32, error) { return 0, nil }

	// LoadLibrary(LPCSTR)：回大於 32 的值算成功。這一層不會真的載入
	// 另一個模組——記下名字就好，需要它的功能再回頭處理。
	h["KERNEL.#95"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(0)
		p.Libraries = append(p.Libraries, p.CString(sel, off))
		return 0x0100, nil
	}

	h["KERNEL.#90"] = func(p *Process, a Args) (uint32, error) { // lstrlen
		sel, off := a.Ptr(0)
		n := uint32(0)
		for {
			b, err := p.Mod.Mem.ReadU8(sel, off+uint16(n))
			if err != nil || b == 0 {
				return n, nil
			}
			n++
		}
	}

	h["KERNEL.#88"] = func(p *Process, a Args) (uint32, error) { // lstrcpy(dst, src)
		dSel, dOff := a.Ptr(0)
		sSel, sOff := a.Ptr(4)
		for i := uint16(0); ; i++ {
			b, err := p.Mod.Mem.ReadU8(sSel, sOff+i)
			if err != nil {
				return 0, err
			}
			if err := p.Mod.Mem.WriteU8(dSel, dOff+i, b); err != nil {
				return 0, err
			}
			if b == 0 {
				break
			}
		}
		return uint32(dSel)<<16 | uint32(dOff), nil
	}

	// hmemcpy(dst, src, DWORD n)：來源與目的都是 huge 指標。這一層沒有
	// huge 定址（跨 selector 自動進位），所以超過區塊尾端會回錯誤而不是
	// 靜靜繞過去。
	h["KERNEL.#348"] = func(p *Process, a Args) (uint32, error) {
		dSel, dOff := a.Ptr(0)
		sSel, sOff := a.Ptr(4)
		n := int(a.Long(8))
		// 兩邊都可能是 huge 配置，所以兩邊都要跟著 selector 鏈走。
		var buf []byte
		p.Mod.Mem.Walk(sSel, sOff, n, func(part []byte) bool {
			buf = append(buf, part...)
			return true
		})
		if len(buf) < n {
			p.note("hmemcpy 只讀到 %d／%d bytes（來源 %04X:%04X 到頭了）", len(buf), n, sSel, sOff)
		}
		wrote := 0
		p.Mod.Mem.Walk(dSel, dOff, len(buf), func(part []byte) bool {
			copy(part, buf[wrote:])
			wrote += len(part)
			return true
		})
		return 0, nil
	}

	// GetDriveType（KERNEL.#136）：0 是 A、1 是 B、依此類推。
	//
	// Win16 的傳回值是 0＝判斷不出來、1＝沒有這台、2＝可移除、3＝固定、
	// 4＝網路。**CD-ROM 在 Windows 3.1 底下是 4**——MSCDEX 是掛成網路
	// 重導向的。PTO2 就靠這一點找光碟機（`RE:cseg11:0x2022`：掃 A..Z 找
	// 型別 4 而且根目錄開得起 `TEKE2WIN.HLP` 的那一台）。
	h["KERNEL.#136"] = func(p *Process, a Args) (uint32, error) {
		drive := byte('A' + a.Word(0))
		if p.FS != nil {
			if _, ok := p.FS.Mounts[drive]; ok {
				return driveRemote, nil
			}
			if drive == p.FS.Drive {
				return driveFixed, nil
			}
		}
		return driveNone, nil
	}

	// FatalAppExit（KERNEL.#137）顯示系統 modal 訊息後以 0xFF 結束目前 task。
	// 無頭執行器不畫系統訊息框，但把文案完整留在 MessageBoxes 供測試與診斷。
	h["KERNEL.#137"] = func(p *Process, a Args) (uint32, error) {
		action := a.Word(0)
		sel, off := a.Ptr(2)
		p.MessageBoxes = append(p.MessageBoxes, MessageBoxCall{
			Text: p.CString(sel, off), Style: 0x1000, Steps: p.CPU.Steps,
		})
		if action != 0 {
			p.note("FatalAppExit action=%04X（Wine 9.0 忽略此值）", action)
		}
		p.CPU.Halt = true
		return 0, &ExitError{Code: 0xFF}
	}
}

// Win16 的 GetDriveType 傳回值。
const (
	driveUnknown   = 0
	driveNone      = 1
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
)

const gmemZeroInit = 0x0040
