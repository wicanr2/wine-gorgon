package winapi

import "testing"

// 參數位元組數是**手算**出來的，而手算會錯：`BitBlt` 有九個參數，
// 八個 word 加一個 dword ＝ 20，第一版寫成 22。症狀不是當機——Borland
// 的 `leave` 會把漂掉的 SP 蓋回去，所以 1,036 次呼叫全部安靜地讀錯位置，
// 畫面上只是「什麼都沒畫」。
//
// 這支測試把手算換成機器算：每一支寫下**參數型別序列**，由測試加總。
// 型別碼：`w` ＝ 16 位元、`d` ＝ 32 位元（含 far 指標與 DWORD）。
var signatures = map[string]string{
	// KERNEL
	"KERNEL.#15":  "wd",  // GlobalAlloc(UINT, DWORD)
	"KERNEL.#16":  "wdw", // GlobalReAlloc(HGLOBAL, DWORD, UINT)
	"KERNEL.#17":  "w",   // GlobalFree
	"KERNEL.#18":  "w",   // GlobalLock
	"KERNEL.#19":  "w",   // GlobalUnlock
	"KERNEL.#20":  "w",   // GlobalSize
	"KERNEL.#21":  "w",   // GlobalHandle
	"KERNEL.#25":  "d",   // GlobalCompact(DWORD)
	"KERNEL.#30":  "w",   // WaitEvent(HTASK)
	"KERNEL.#49":  "wdw", // GetModuleFileName(HINSTANCE, LPSTR, int)
	"KERNEL.#51":  "dw",  // MakeProcInstance(FARPROC, HINSTANCE)
	"KERNEL.#52":  "d",   // FreeProcInstance(FARPROC)
	"KERNEL.#60":  "wdd", // FindResource(HINSTANCE, LPCSTR, LPCSTR)
	"KERNEL.#61":  "ww",  // LoadResource(HINSTANCE, HRSRC)
	"KERNEL.#63":  "w",   // FreeResource(HGLOBAL)
	"KERNEL.#81":  "w",   // _lclose(HFILE)
	"KERNEL.#82":  "wdw", // _lread(HFILE, LPSTR, UINT)
	"KERNEL.#83":  "dw",  // _lcreat(LPCSTR, int)
	"KERNEL.#84":  "wdw", // _llseek(HFILE, LONG, int)
	"KERNEL.#85":  "dw",  // _lopen(LPCSTR, int)
	"KERNEL.#86":  "wdw", // _lwrite(HFILE, LPCSTR, UINT)
	"KERNEL.#88":  "dd",  // lstrcpy(LPSTR, LPCSTR)
	"KERNEL.#90":  "d",   // lstrlen(LPCSTR)
	"KERNEL.#91":  "",    // InitTask()
	"KERNEL.#95":  "d",   // LoadLibrary(LPCSTR)
	"KERNEL.#131": "",    // GetDOSEnvironment()
	"KERNEL.#132": "",    // GetWinFlags()
	"KERNEL.#169": "w",   // GetFreeSpace(UINT)
	"KERNEL.#348": "ddd", // hmemcpy(void huge*, void huge*, DWORD)
	"KERNEL.#349": "wdd", // _hread(HFILE, void huge*, long)
	"KERNEL.#350": "wdd", // _hwrite(HFILE, const void huge*, long)

	// GDI
	"GDI.#1":   "wd",             // SetBkColor(HDC, COLORREF)
	"GDI.#2":   "ww",             // SetBkMode(HDC, int)
	"GDI.#6":   "ww",             // SetPolyFillMode(HDC, int)
	"GDI.#9":   "wd",             // SetTextColor(HDC, COLORREF)
	"GDI.#19":  "www",            // LineTo(HDC, int, int)
	"GDI.#20":  "www",            // MoveTo(HDC, int, int)
	"GDI.#31":  "wwwd",           // SetPixel(HDC, int, int, COLORREF)
	"GDI.#33":  "wwwdw",          // TextOut(HDC, int, int, LPCSTR, int)
	"GDI.#34":  "wwwwwwwwd",      // BitBlt：八個 word ＋ 一個 dword
	"GDI.#36":  "wdw",            // Polygon(HDC, const POINT far*, int)
	"GDI.#37":  "wdw",            // Polyline
	"GDI.#45":  "ww",             // SelectObject(HDC, HGDIOBJ)
	"GDI.#48":  "wwwwd",          // CreateBitmap(int,int,BYTE,BYTE,const void far*)
	"GDI.#52":  "w",              // CreateCompatibleDC(HDC)
	"GDI.#56":  "wwwwwwwwwwwwwd", // CreateFont：13 個 word ＋ 字體名
	"GDI.#57":  "d",              // CreateFontIndirect(const LOGFONT far*)
	"GDI.#60":  "w",              // CreatePatternBrush(HBITMAP)
	"GDI.#61":  "wwd",            // CreatePen(int, int, COLORREF)
	"GDI.#66":  "d",              // CreateSolidBrush(COLORREF)
	"GDI.#68":  "w",              // DeleteDC(HDC)
	"GDI.#69":  "w",              // DeleteObject(HGDIOBJ)
	"GDI.#74":  "wdd",            // GetBitmapBits(HBITMAP, LONG, LPSTR)
	"GDI.#75":  "w",              // GetBkColor(HDC)
	"GDI.#76":  "w",              // GetBkMode(HDC)
	"GDI.#80":  "ww",             // GetDeviceCaps(HDC, int)
	"GDI.#83":  "www",            // GetPixel(HDC, int, int)
	"GDI.#87":  "w",              // GetStockObject(int)
	"GDI.#90":  "w",              // GetTextColor(HDC)
	"GDI.#91":  "wdw",            // GetTextExtent(HDC, LPCSTR, int)
	"GDI.#93":  "wd",             // GetTextMetrics(HDC, TEXTMETRIC far*)
	"GDI.#106": "wdd",            // SetBitmapBits(HBITMAP, DWORD, const void far*)
	"GDI.#119": "d",              // AddFontResource(LPCSTR)
	"GDI.#136": "d",              // RemoveFontResource(LPCSTR)
	"GDI.#150": "w",              // UnrealizeObject(HGDIOBJ)
	"GDI.#162": "w",              // GetBitmapDimension(HBITMAP)
	"GDI.#163": "www",            // SetBitmapDimension(HBITMAP, int, int)
	"GDI.#330": "wddd",           // EnumFontFamilies(HDC, LPCSTR, FONTENUMPROC, LPARAM)
	"GDI.#346": "ww",             // SetTextAlign(HDC, UINT)
	"GDI.#360": "d",              // CreatePalette(const LOGPALETTE far*)
	"GDI.#364": "wwwd",           // SetPaletteEntries(HPALETTE, UINT, UINT, const PALETTEENTRY far*)
	"GDI.#367": "wwwd",           // AnimatePalette
	"GDI.#375": "wwwd",           // GetSystemPaletteEntries(HDC, UINT, UINT, PALETTEENTRY far*)
	"GDI.#442": "wddddw",         // CreateDIBitmap(HDC, BITMAPINFOHEADER far*, DWORD, void far*, BITMAPINFO far*, UINT)

	// USER
	"USER.#1":   "wddw",        // MessageBox(HWND, LPCSTR, LPCSTR, UINT)
	"USER.#5":   "w",           // InitApp(HINSTANCE)
	"USER.#6":   "w",           // PostQuitMessage(int)
	"USER.#10":  "wwwd",        // SetTimer(HWND, int, UINT, TIMERPROC)
	"USER.#12":  "ww",          // KillTimer(HWND, int)
	"USER.#13":  "",            // GetTickCount()
	"USER.#17":  "d",           // GetCursorPos(POINT far*)
	"USER.#18":  "w",           // SetCapture(HWND)
	"USER.#19":  "",            // ReleaseCapture()
	"USER.#22":  "w",           // SetFocus(HWND)
	"USER.#29":  "wd",          // ScreenToClient(HWND, POINT far*)
	"USER.#32":  "wd",          // GetWindowRect(HWND, RECT far*)
	"USER.#33":  "wd",          // GetClientRect(HWND, RECT far*)
	"USER.#34":  "ww",          // EnableWindow(HWND, BOOL)
	"USER.#35":  "w",           // IsWindowEnabled(HWND)
	"USER.#36":  "wdw",         // GetWindowText(HWND, LPSTR, int)
	"USER.#37":  "wd",          // SetWindowText(HWND, LPCSTR)
	"USER.#39":  "wd",          // BeginPaint(HWND, PAINTSTRUCT far*)
	"USER.#40":  "wd",          // EndPaint(HWND, const PAINTSTRUCT far*)
	"USER.#41":  "dddwwwwwwwd", // CreateWindow
	"USER.#42":  "ww",          // ShowWindow(HWND, int)
	"USER.#46":  "w",           // GetParent(HWND)
	"USER.#48":  "ww",          // IsChild(HWND, HWND)
	"USER.#53":  "w",           // DestroyWindow(HWND)
	"USER.#56":  "wwwwww",      // MoveWindow(HWND, int, int, int, int, BOOL)
	"USER.#57":  "d",           // RegisterClass(const WNDCLASS far*)
	"USER.#62":  "wwww",        // SetScrollPos(HWND, int, int, BOOL)
	"USER.#64":  "wwwww",       // SetScrollRange(HWND, int, int, int, BOOL)
	"USER.#65":  "wwdd",        // GetScrollRange(HWND, int, int far*, int far*)
	"USER.#66":  "w",           // GetDC(HWND)
	"USER.#68":  "ww",          // ReleaseDC(HWND, HDC)
	"USER.#69":  "w",           // SetCursor(HCURSOR)
	"USER.#72":  "dwwww",       // SetRect(RECT far*, int, int, int, int)
	"USER.#76":  "dd",          // PtInRect(const RECT far*, POINT)
	"USER.#77":  "dww",         // OffsetRect(RECT far*, int, int)
	"USER.#78":  "dww",         // InflateRect(RECT far*, int, int)
	"USER.#81":  "wdw",         // FillRect(HDC, const RECT far*, HBRUSH)
	"USER.#83":  "wdw",         // FrameRect(HDC, const RECT far*, HBRUSH)
	"USER.#85":  "wdwdw",       // DrawText(HDC, LPCSTR, int, RECT far*, UINT)
	"USER.#89":  "wdwd",        // CreateDialog(HINSTANCE, LPCSTR, HWND, DLGPROC)
	"USER.#90":  "wd",          // IsDialogMessage(HWND, MSG far*)
	"USER.#91":  "ww",          // GetDlgItem(HWND, int)
	"USER.#101": "wwwwd",       // SendDlgItemMessage(HWND, int, UINT, WPARAM, LPARAM)
	"USER.#104": "w",           // MessageBeep(UINT)
	"USER.#107": "wwwd",        // DefWindowProc(HWND, UINT, WPARAM, LPARAM)
	"USER.#109": "dwwww",       // PeekMessage(MSG far*, HWND, UINT, UINT, UINT)
	"USER.#111": "wwwd",        // SendMessage(HWND, UINT, WPARAM, LPARAM)
	"USER.#113": "d",           // TranslateMessage(const MSG far*)
	"USER.#114": "d",           // DispatchMessage(const MSG far*)
	"USER.#124": "w",           // UpdateWindow(HWND)
	"USER.#125": "wdw",         // InvalidateRect(HWND, const RECT far*, BOOL)
	"USER.#127": "wd",          // ValidateRect(HWND, const RECT far*)
	"USER.#133": "ww",          // GetWindowWord(HWND, int)
	"USER.#134": "www",         // SetWindowWord(HWND, int, WORD)
	"USER.#136": "wwd",         // SetWindowLong(HWND, int, LONG)
	"USER.#154": "www",         // CheckMenuItem(HMENU, UINT, UINT)
	"USER.#155": "www",         // EnableMenuItem(HMENU, UINT, UINT)
	"USER.#157": "w",           // GetMenu(HWND)
	"USER.#160": "w",           // DrawMenuBar(HWND)
	"USER.#171": "wdwd",        // WinHelp(HWND, LPCSTR, UINT, DWORD)
	"USER.#173": "wd",          // LoadCursor(HINSTANCE, LPCSTR)
	"USER.#174": "wd",          // LoadIcon(HINSTANCE, LPCSTR)
	"USER.#177": "wd",          // LoadAccelerators(HINSTANCE, LPCSTR)
	"USER.#178": "wwd",         // TranslateAccelerator(HWND, HACCEL, MSG far*)
	"USER.#179": "w",           // GetSystemMetrics(int)
	"USER.#219": "wdwd",        // CreateDialogIndirect(HINSTANCE, const void far*, HWND, DLGPROC)
	"USER.#222": "d",           // GetKeyboardState(BYTE far*)
	"USER.#223": "d",           // SetKeyboardState(BYTE far*)
	"USER.#243": "",            // GetDialogBaseUnits()
	"USER.#277": "w",           // GetDlgCtrlID(HWND)
	"USER.#282": "www",         // SelectPalette(HDC, HPALETTE, BOOL)
	"USER.#283": "w",           // RealizePalette(HDC)
	"USER.#284": "w",           // GetFreeSystemResources(UINT)
	"USER.#286": "",            // GetDesktopWindow()
	"USER.#308": "wwwd",        // DefDlgProc(HWND, UINT, WPARAM, LPARAM)
	"USER.#414": "wwwwd",       // ModifyMenu(HMENU, UINT, UINT, UINT, LPCSTR)

	// 其他
	"MMSYSTEM.#2":   "dw",   // sndPlaySound(LPCSTR, UINT)
	"MMSYSTEM.#701": "wwdd", // mciSendCommand(UINT, UINT, DWORD, DWORD)
	"COMMDLG.#1":    "d",    // GetOpenFileName(OPENFILENAME far*)
	"COMMDLG.#2":    "d",    // GetSaveFileName(OPENFILENAME far*)
	"COMMDLG.#26":   "",     // CommDlgExtendedError()
}

func sigBytes(sig string) int {
	n := 0
	for _, c := range sig {
		switch c {
		case 'w':
			n += 2
		case 'd':
			n += 4
		}
	}
	return n
}

func TestArgBytesMatchSignatures(t *testing.T) {
	for key, sig := range signatures {
		f, ok := Table[key]
		if !ok {
			t.Errorf("%s 不在表上", key)
			continue
		}
		if want := sigBytes(sig); f.ArgBytes != want {
			t.Errorf("%s（%s）ArgBytes = %d，依簽章應該是 %d", key, f.Name, f.ArgBytes, want)
		}
	}
}

func TestTableHasExactlyTheCIVSurface(t *testing.T) {
	if len(Table) != 157 {
		t.Errorf("表上有 %d 支，預期 157（CIV.EXE 的匯入表面）", len(Table))
	}
	for key, f := range Table {
		if f.Name == "" {
			t.Errorf("%s 沒有名字", key)
		}
		if f.ArgBytes < -1 || f.ArgBytes%2 != 0 && f.ArgBytes != -1 {
			t.Errorf("%s（%s）的 ArgBytes = %d：pascal 參數一定是偶數個 byte",
				key, f.Name, f.ArgBytes)
		}
	}
}
