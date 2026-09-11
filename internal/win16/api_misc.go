package win16

import "strings"

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

	// mciSendCommand(UINT devID, UINT msg, DWORD flags, DWORD param)
	//
	// 回 0 是「成功」，非 0 是 MCI 錯誤碼。這裡有三條路可走，前兩條都錯：
	//
	//   一律回 0     等於說「裝置開起來了」，遊戲接著等播放完成的通知，
	//                而我們不送 → 卡在開場。
	//   一律回錯誤   遊戲走「這台機器沒有 CD／影片」的路徑 → 播完開場就結束，
	//                因為 PTO2 是 CD 遊戲。
	//   有狀態       OPEN 發代號、PLAY 立刻完成並送 MM_MCINOTIFY、
	//                STATUS 回報已停止。這一條才讓遊戲往下走。
	//
	// 「立刻完成」是刻意的：對拍要的是決定性，不是真的播放。影片與 CD 音軌
	// 的內容不影響遊戲狀態，只影響經過的時間——而時間在這裡是可控的。
	h["MMSYSTEM.#701"] = func(p *Process, a Args) (uint32, error) {
		const (
			mciOpen   = 0x0803
			mciClose  = 0x0804
			mciPlay   = 0x0806
			mciSeek   = 0x0807
			mciStop   = 0x0808
			mciInfo   = 0x080A
			mciSet    = 0x080D
			mciStatus = 0x0814

			mciNotify           = 0x0001
			mmMCINotify         = 0x03B9
			mciNotifySuccessful = 0x0001
			mciModeStop         = 525

			// MCI_OPEN 的旗標與欄位
			mciOpenType = 0x2000

			// MCI_SET_TIME_FORMAT 與格式編號
			mciSetTimeFormat = 0x0400
			formatMS         = 0
			formatMSF        = 2
			formatFrames     = 3
			formatTMSF       = 10

			// MCI_STATUS 的旗標與 dwItem
			mciStatusItem      = 0x0100
			mciTrack           = 0x0010
			statusLength       = 1
			statusPosition     = 2
			statusNumTracks    = 3
			statusMode         = 4
			statusMediaPresent = 5
			statusReady        = 7
			statusCDATypeTrack = 0x4001
			cdaTrackAudio      = 0x4E03

			mcierrInvalidDeviceID   = 258
			mcierrInvalidDeviceName = 261
			mcierrUnsupportedFunc   = 268
			mcierrOutOfRange        = 269
		)
		devID := a.Word(0)
		msg := a.Word(2)
		flags := a.Long(4)
		psel, poff := a.Ptr(8)

		put16 := func(d, v uint16) {
			if psel != 0 {
				_ = p.Mod.Mem.WriteU16(psel, poff+d, v)
			}
		}
		put32 := func(d uint16, v uint32) { put16(d, uint16(v)); put16(d+2, uint16(v>>16)) }
		// dwCallback 在每種 MCI_*_PARMS 的第一個欄位，低 16 位是視窗。
		callbackHwnd := func() uint16 {
			if psel == 0 {
				return 0
			}
			v, _ := p.Mod.Mem.ReadU16(psel, poff)
			return v
		}
		notify := func() {
			if flags&mciNotify != 0 {
				p.PostMessage(callbackHwnd(), mmMCINotify, mciNotifySuccessful, uint32(devID))
			}
		}

		get32 := func(d uint16) uint32 {
			if psel == 0 {
				return 0
			}
			lo, _ := p.Mod.Mem.ReadU16(psel, poff+d)
			hi, _ := p.Mod.Mem.ReadU16(psel, poff+d+2)
			return uint32(hi)<<16 | uint32(lo)
		}
		dev := p.MCIOpen[devID]

		switch msg {
		case mciOpen:
			devType := ""
			if flags&mciOpenType != 0 && psel != 0 {
				// MCI_OPEN_PARMS.lpstrDeviceType 在 +8。
				sel, _ := p.Mod.Mem.ReadU16(psel, poff+10)
				off, _ := p.Mod.Mem.ReadU16(psel, poff+8)
				if sel != 0 {
					devType = strings.ToLower(p.CString(sel, off))
				}
			}
			// 沒有光碟就開不起來。這不是保守，是誠實：遊戲會據此顯示
			// 「Can't open CD-ROM DEVICE.」，那正是真實機器上的行為。
			if devType == "cdaudio" && p.Disc == nil {
				return mcierrInvalidDeviceName, nil
			}
			if p.MCIOpen == nil {
				p.MCIOpen = map[uint16]*MCIDevice{}
			}
			p.MCINextID++
			id := p.MCINextID
			p.MCIOpen[id] = &MCIDevice{Type: devType, TimeFormat: formatMSF}
			put16(4, id) // MCI_OPEN_PARMS.wDeviceID
			notify()
			return 0, nil

		case mciClose:
			delete(p.MCIOpen, devID)
			notify()
			return 0, nil

		case mciPlay, mciSeek, mciStop:
			if devID != 0 && dev == nil {
				return mcierrInvalidDeviceID, nil
			}
			// 立刻完成。真的播放要等的是牆鐘時間，而對拍不能等牆鐘。
			notify()
			return 0, nil

		case mciSet:
			if devID != 0 && dev == nil {
				return mcierrInvalidDeviceID, nil
			}
			if flags&mciSetTimeFormat != 0 && dev != nil {
				dev.TimeFormat = get32(4) // MCI_SET_PARMS.dwTimeFormat
			}
			notify()
			return 0, nil

		case mciStatus:
			if devID != 0 && dev == nil {
				return mcierrInvalidDeviceID, nil
			}
			ret, err := p.mciStatus(dev, flags, get32(8), get32(12))
			if err != 0 {
				return err, nil
			}
			put32(4, ret) // MCI_STATUS_PARMS.dwReturn
			notify()
			return 0, nil

		case mciInfo:
			notify()
			return 0, nil
		}
		p.note("mciSendCommand：沒處理的命令 %04X（回成功）", msg)
		notify()
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

// mciStatus 回答 MCI_STATUS 的查詢。第二個回傳值不為 0 時是 MCI 錯誤碼。
//
// 為什麼要分出來：`cdaudio` 的查詢答案取決於真實 TOC，而 TOC 是外面給的
// （`Process.Disc`）。把它和「送通知、寫回傳值」的殼分開，測試就能直接
// 問「第 2 軌多長」，不必鋪一份 MCI_STATUS_PARMS。
func (p *Process) mciStatus(dev *MCIDevice, flags, item, track uint32) (uint32, uint32) {
	const (
		mciStatusItem      = 0x0100
		mciTrack           = 0x0010
		statusLength       = 1
		statusPosition     = 2
		statusNumTracks    = 3
		statusMode         = 4
		statusMediaPresent = 5
		statusReady        = 7
		statusCDATypeTrack = 0x4001
		cdaTrackAudio      = 0x4E03
		mciModeStop        = 525

		mcierrOutOfRange      = 269
		mcierrUnsupportedFunc = 268
	)
	if flags&mciStatusItem == 0 {
		return mciModeStop, 0
	}
	cd := dev != nil && dev.Type == "cdaudio"
	if !cd || p.Disc == nil {
		// 非 cdaudio 的裝置（開場影片走 MCI_OPEN 不帶型別）維持舊行為：
		// 一律回「已停止」。它們的內容不影響遊戲狀態。
		switch item {
		case statusMode:
			return mciModeStop, 0
		case statusReady, statusMediaPresent:
			return 1, 0
		}
		return mciModeStop, 0
	}

	switch item {
	case statusNumTracks:
		return uint32(p.Disc.Count()), 0
	case statusMode:
		return mciModeStop, 0
	case statusReady, statusMediaPresent:
		return 1, 0
	case statusCDATypeTrack:
		return cdaTrackAudio, 0
	case statusLength, statusPosition:
		frames := 0
		if flags&mciTrack != 0 {
			n := int(track)
			if p.Disc.TrackFrames(n) == 0 && p.Disc.TrackStart(n) == 0 {
				return 0, mcierrOutOfRange
			}
			if item == statusLength {
				frames = p.Disc.TrackFrames(n)
			} else {
				frames = p.Disc.TrackStart(n)
			}
		} else if item == statusLength {
			frames = p.Disc.End
		}
		return p.mciTime(dev, frames), 0
	}
	return 0, mcierrUnsupportedFunc
}

// mciTime 把幀數換成裝置目前時間格式下的值。
func (p *Process) mciTime(dev *MCIDevice, frames int) uint32 {
	const (
		formatMS     = 0
		formatFrames = 3
		formatTMSF   = 10
	)
	switch dev.TimeFormat {
	case formatFrames:
		return uint32(frames)
	case formatMS:
		return uint32(frames * 1000 / FramesPerSecond)
	case formatTMSF:
		// TMSF 的第一個 byte 是音軌；查詢單軌長度時原版填 0。
		return FramesToMSF(frames) << 8
	}
	return FramesToMSF(frames)
}
