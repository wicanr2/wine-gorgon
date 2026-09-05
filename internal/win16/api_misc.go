package win16

// fileDialog 是 GetOpenFileName／GetSaveFileName 的共同實作。
func (p *Process) fileDialog(a Args) (uint32, error) {
	if p.FileDialogPath == "" {
		return 0, nil // 使用者按了取消
	}
	sel, off := a.Ptr(0)
	fileOff, _ := p.Mod.Mem.ReadU16(sel, off+24)
	fileSel, _ := p.Mod.Mem.ReadU16(sel, off+26)
	maxLo, _ := p.Mod.Mem.ReadU16(sel, off+28)
	if fileSel == 0 {
		p.note("GetOpenFileName 的 lpstrFile 是空指標")
		return 0, nil
	}
	path := p.FileDialogPath
	if int(maxLo) > 0 && len(path)+1 > int(maxLo) {
		p.note("GetOpenFileName 的緩衝區只有 %d bytes，放不下 %q", maxLo, path)
		return 0, nil
	}
	for i := 0; i < len(path); i++ {
		if err := p.Mod.Mem.WriteU8(fileSel, fileOff+uint16(i), path[i]); err != nil {
			return 0, err
		}
	}
	if err := p.Mod.Mem.WriteU8(fileSel, fileOff+uint16(len(path)), 0); err != nil {
		return 0, err
	}
	// nFileOffset／nFileExtension：檔名與副檔名在字串裡的位置。
	nameOff, extOff := 0, len(path)
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '\\' || path[i] == ':' {
			nameOff = i + 1
			break
		}
	}
	for i := len(path) - 1; i > nameOff; i-- {
		if path[i] == '.' {
			extOff = i + 1
			break
		}
	}
	_ = p.Mod.Mem.WriteU16(sel, off+52, uint16(nameOff))
	_ = p.Mod.Mem.WriteU16(sel, off+54, uint16(extOff))
	p.FileDialogCalls = append(p.FileDialogCalls, path)
	return 1, nil
}

// RegisterMisc 登記 MMSYSTEM 與 COMMDLG。
//
// 這兩個模組都不影響畫面，但**不能靜靜回 0**：遊戲會用回傳值決定要不要
// 走替代路徑。聲音一律回「播成功」，檔案對話框一律回「使用者按了取消」，
// 兩者都是明確而且不會讓遊戲卡住的答案。
func RegisterMisc(p *Process) {
	h := p.Handlers

	h["MMSYSTEM.#2"] = func(p *Process, a Args) (uint32, error) { // sndPlaySound
		sel, off := a.Ptr(0)
		name := ""
		if sel != 0 {
			name = p.CString(sel, off)
		}
		p.Sounds = append(p.Sounds, name)
		return 1, nil
	}

	h["MMSYSTEM.#701"] = func(p *Process, a Args) (uint32, error) { // mciSendCommand
		p.note("mciSendCommand(%04X) 回 0（沒有 MCI 裝置）", a.Word(2))
		return 0, nil
	}

	// GetOpenFileName／GetSaveFileName(OPENFILENAME far*)
	//
	// 沒有畫面可以讓人挑檔案，所以改成「由外面決定它回什麼」：
	// `Process.FileDialogPath` 有值就當成使用者選了那個檔，空的就是按了取消。
	// 這是把**互動**換成**參數**——對拍腳本要能重現，不能靠人點。
	//
	// Win16 的 OPENFILENAME：+24 lpstrFile（far 指標）、+28 nMaxFile。
	h["COMMDLG.#1"] = func(p *Process, a Args) (uint32, error) { return p.fileDialog(a) }
	h["COMMDLG.#2"] = func(p *Process, a Args) (uint32, error) { return p.fileDialog(a) }
	h["COMMDLG.#26"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // CommDlgExtendedError

	h["USER.#171"] = func(p *Process, _ Args) (uint32, error) { return 1, nil } // WinHelp

	// 選單：Civilization 的選單列會吃掉客戶區的高度（已在版面算進去），
	// 但選單項目的勾選與啟用只影響選單本身的外觀。
	h["USER.#157"] = func(p *Process, a Args) (uint32, error) { // GetMenu
		w, ok := p.Window(a.Word(0))
		if !ok || !w.HasMenu {
			return 0, nil
		}
		return 0x0300, nil
	}
	h["USER.#154"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // CheckMenuItem
	h["USER.#155"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // EnableMenuItem
	h["USER.#160"] = func(p *Process, _ Args) (uint32, error) { return 1, nil } // DrawMenuBar
	h["USER.#414"] = func(p *Process, _ Args) (uint32, error) { return 1, nil } // ModifyMenu

	// 捲軸：位置與範圍會被遊戲讀回去算地圖捲動，所以要真的記住。
	h["USER.#62"] = func(p *Process, a Args) (uint32, error) { // SetScrollPos
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		bar := a.Word(2)
		old := w.ScrollPos[bar&1]
		w.ScrollPos[bar&1] = int(int16(a.Word(4)))
		return uint32(uint16(old)), nil
	}
	h["USER.#64"] = func(p *Process, a Args) (uint32, error) { // SetScrollRange
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		bar := a.Word(2) & 1
		w.ScrollMin[bar] = int(int16(a.Word(4)))
		w.ScrollMax[bar] = int(int16(a.Word(6)))
		return 1, nil
	}
	h["USER.#65"] = func(p *Process, a Args) (uint32, error) { // GetScrollRange
		w, ok := p.Window(a.Word(0))
		if !ok {
			return 0, nil
		}
		bar := a.Word(2) & 1
		minSel, minOff := a.Ptr(4)
		maxSel, maxOff := a.Ptr(8)
		_ = p.Mod.Mem.WriteU16(minSel, minOff, uint16(int16(w.ScrollMin[bar])))
		_ = p.Mod.Mem.WriteU16(maxSel, maxOff, uint16(int16(w.ScrollMax[bar])))
		return 1, nil
	}
}
