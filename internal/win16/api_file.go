package win16

import "io"

// RegisterFile 登記 KERNEL 的檔案 API。
//
// Win16 的 `_lopen` 一族其實就是 DOS handle 呼叫包一層，錯誤一律回
// `HFILE_ERROR`（-1）。這裡照這個約定：**找不到檔案不是我們的錯誤，
// 是遊戲要處理的情況**——但要記進 FileSystem.Opened，不然「遊戲說找不到
// 檔案」會查不出到底找的是哪一個。
func RegisterFile(p *Process) {
	h := p.Handlers
	const hfileError = 0xFFFF

	h["KERNEL.#85"] = func(p *Process, a Args) (uint32, error) { // _lopen(name, mode)
		sel, off := a.Ptr(0)
		name := p.CString(sel, off)
		fh, err := p.FS.Open(name, int(int16(a.Word(4))))
		if err != nil {
			return hfileError, nil
		}
		return uint32(fh), nil
	}

	h["KERNEL.#83"] = func(p *Process, a Args) (uint32, error) { // _lcreat(name, attr)
		sel, off := a.Ptr(0)
		fh, err := p.FS.Create(p.CString(sel, off))
		if err != nil {
			p.note("_lcreat 失敗：%v", err)
			return hfileError, nil
		}
		return uint32(fh), nil
	}

	h["KERNEL.#81"] = func(p *Process, a Args) (uint32, error) { // _lclose
		if p.FS.Close(a.Word(0)) {
			return 0, nil
		}
		return hfileError, nil
	}

	h["KERNEL.#82"] = func(p *Process, a Args) (uint32, error) { // _lread(h, buf, n)
		return p.readInto(a.Word(0), a, 2, uint32(a.Word(6)))
	}
	h["KERNEL.#349"] = func(p *Process, a Args) (uint32, error) { // _hread(h, buf, long n)
		return p.readInto(a.Word(0), a, 2, a.Long(6))
	}

	h["KERNEL.#86"] = func(p *Process, a Args) (uint32, error) { // _lwrite(h, buf, n)
		return p.writeFrom(a.Word(0), a, 2, uint32(a.Word(6)))
	}
	h["KERNEL.#350"] = func(p *Process, a Args) (uint32, error) { // _hwrite
		return p.writeFrom(a.Word(0), a, 2, a.Long(6))
	}

	h["KERNEL.#84"] = func(p *Process, a Args) (uint32, error) { // _llseek(h, long off, origin)
		f, ok := p.FS.File(a.Word(0))
		if !ok {
			return hfileError, nil
		}
		pos, err := f.Seek(int64(int32(a.Long(2))), int(int16(a.Word(6))))
		if err != nil {
			return hfileError, nil
		}
		return uint32(pos), nil
	}
}

// readInto 把檔案內容讀進 16 位元位址空間。
//
// 目的地是一個 selector，超過它的大小就停下並回實際讀了幾個 byte——
// 這一層沒有 huge 指標的自動進位（spec 001 §3），所以不能假裝讀得下。
func (p *Process) readInto(fh uint16, a Args, ptrOff int, n uint32) (uint32, error) {
	f, ok := p.FS.File(fh)
	if !ok {
		return 0xFFFF, nil
	}
	sel, off := a.Ptr(ptrOff)
	total := 0
	var readErr error
	got := p.Mod.Mem.Walk(sel, off, int(n), func(part []byte) bool {
		k, err := io.ReadFull(f, part)
		total += k
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return false
		}
		if err != nil {
			readErr = err
			return false
		}
		return true
	})
	if readErr != nil {
		return 0xFFFF, nil
	}
	if got < int(n) && total == got {
		// 走不完通常是目的地不夠大，不是檔案讀完了。
		p.note("_lread 要 %d bytes，目的地 %04X:%04X 只放得下 %d", n, sel, off, got)
	}
	return uint32(total), nil
}

func (p *Process) writeFrom(fh uint16, a Args, ptrOff int, n uint32) (uint32, error) {
	f, ok := p.FS.File(fh)
	if !ok {
		return 0xFFFF, nil
	}
	sel, off := a.Ptr(ptrOff)
	total := 0
	var writeErr error
	p.Mod.Mem.Walk(sel, off, int(n), func(part []byte) bool {
		k, err := f.Write(part)
		total += k
		if err != nil {
			writeErr = err
			return false
		}
		return true
	})
	if writeErr != nil {
		return 0xFFFF, nil
	}
	return uint32(total), nil
}
