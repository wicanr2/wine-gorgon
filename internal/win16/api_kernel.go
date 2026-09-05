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
		flags, size := a.Word(0), a.Long(2)
		if size == 0 {
			size = 1
		}
		if size > 0x10000 {
			// 大於 64 KiB 要多個 selector，這一層還沒做；先明白地報出來。
			return 0, errUnsupported("GlobalAlloc 要求 %d bytes，超過單一 selector 的 64 KiB", size)
		}
		b := p.Mod.Mem.Alloc("GlobalAlloc", int(size))
		if flags&gmemZeroInit == 0 {
			// GMEM_ZEROINIT 沒設時內容是未定義的。這裡照樣給零：
			// 「未定義」在對拍工具上要可重現，不能真的是垃圾。
			_ = b
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
		if p.Mod.Mem.Free(a.Word(0)) {
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
		return uint32(len(b.Data)), nil
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
		n := a.Long(8)
		for i := uint32(0); i < n; i++ {
			b, err := p.Mod.Mem.ReadU8(sSel, sOff+uint16(i))
			if err != nil {
				return 0, err
			}
			if err := p.Mod.Mem.WriteU8(dSel, dOff+uint16(i), b); err != nil {
				return 0, err
			}
		}
		return 0, nil
	}
}

const gmemZeroInit = 0x0040
