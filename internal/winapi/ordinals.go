package winapi

// 這張表把 (模組, 序號) 翻成名稱與參數位元組數。
//
// **名稱與序號的來源是兩份獨立解析的交集**：`internal/ne` 讀 CIV.EXE 的
// 模組參考表與重定位表得到 (模組, 序號)；IDA Pro 9.4 獨立載入同一個檔案，
// 用它自己的 Win16 序號資料庫得到 (模組, 序號, 名稱)。兩邊的 157 組
// (模組, 序號) **完全相同，任一邊都沒有多出來的項**（docs/spec/004 §2）。
//
// `ArgBytes` 的來源不同，要分開看：那是 Win16 **文件上的函式簽章**
// （pascal 呼叫慣例，參數由被呼叫方清掉），不是從 CIV.EXE 量出來的。
// 寫錯不會安靜——被呼叫方多彈或少彈幾個 byte，呼叫端的 `leave`／`retf`
// 就會拿到錯的返回位址，`nerun` 會在幾條指令內報出位址。`-1` 表示
// 「不是普通的 pascal 函式」（資料匯入、浮點模擬器入口）。
//
// 這一份只涵蓋 CIV.EXE 用到的 157 支。加新遊戲時往這裡補，不要在
// 處理器裡硬寫數字。

// Func 是一支 Win16 匯入函式。
type Func struct {
	Module   string
	Ordinal  int
	Name     string
	ArgBytes int // -1 表示不是普通的 pascal 函式
}

// Table 的鍵和 ne.Import.Key() 同形：`GDI.#45`。
var Table = map[string]Func{
	// --- KERNEL（32 支）---
	"KERNEL.#15":  {"KERNEL", 15, "GLOBALALLOC", 6},
	"KERNEL.#16":  {"KERNEL", 16, "GLOBALREALLOC", 8},
	"KERNEL.#17":  {"KERNEL", 17, "GLOBALFREE", 2},
	"KERNEL.#18":  {"KERNEL", 18, "GLOBALLOCK", 2},
	"KERNEL.#19":  {"KERNEL", 19, "GLOBALUNLOCK", 2},
	"KERNEL.#20":  {"KERNEL", 20, "GLOBALSIZE", 2},
	"KERNEL.#21":  {"KERNEL", 21, "GLOBALHANDLE", 2},
	"KERNEL.#25":  {"KERNEL", 25, "GLOBALCOMPACT", 4},
	"KERNEL.#30":  {"KERNEL", 30, "WAITEVENT", 2},
	"KERNEL.#49":  {"KERNEL", 49, "GETMODULEFILENAME", 8},
	"KERNEL.#51":  {"KERNEL", 51, "MAKEPROCINSTANCE", 6},
	"KERNEL.#52":  {"KERNEL", 52, "FREEPROCINSTANCE", 4},
	"KERNEL.#60":  {"KERNEL", 60, "FINDRESOURCE", 10},
	"KERNEL.#61":  {"KERNEL", 61, "LOADRESOURCE", 4},
	"KERNEL.#63":  {"KERNEL", 63, "FREERESOURCE", 2},
	"KERNEL.#81":  {"KERNEL", 81, "_LCLOSE", 2},
	"KERNEL.#82":  {"KERNEL", 82, "_LREAD", 8},
	"KERNEL.#83":  {"KERNEL", 83, "_LCREAT", 6},
	"KERNEL.#84":  {"KERNEL", 84, "_LLSEEK", 8},
	"KERNEL.#85":  {"KERNEL", 85, "_LOPEN", 6},
	"KERNEL.#86":  {"KERNEL", 86, "_LWRITE", 8},
	"KERNEL.#88":  {"KERNEL", 88, "LSTRCPY", 8},
	"KERNEL.#90":  {"KERNEL", 90, "LSTRLEN", 4},
	"KERNEL.#91":  {"KERNEL", 91, "INITTASK", 0},
	"KERNEL.#95":  {"KERNEL", 95, "LOADLIBRARY", 4},
	"KERNEL.#113": {"KERNEL", 113, "__AHSHIFT", -1},
	"KERNEL.#131": {"KERNEL", 131, "GETDOSENVIRONMENT", 0},
	"KERNEL.#132": {"KERNEL", 132, "GETWINFLAGS", 0},
	"KERNEL.#169": {"KERNEL", 169, "GETFREESPACE", 2},
	"KERNEL.#348": {"KERNEL", 348, "HMEMCPY", 12},
	"KERNEL.#349": {"KERNEL", 349, "_HREAD", 10},
	"KERNEL.#350": {"KERNEL", 350, "_HWRITE", 10},
	// --- USER（76 支）---
	"USER.#1":   {"USER", 1, "MESSAGEBOX", 12},
	"USER.#5":   {"USER", 5, "INITAPP", 2},
	"USER.#6":   {"USER", 6, "POSTQUITMESSAGE", 2},
	"USER.#10":  {"USER", 10, "SETTIMER", 10},
	"USER.#12":  {"USER", 12, "KILLTIMER", 4},
	"USER.#13":  {"USER", 13, "GETTICKCOUNT", 0},
	"USER.#17":  {"USER", 17, "GETCURSORPOS", 4},
	"USER.#18":  {"USER", 18, "SETCAPTURE", 2},
	"USER.#19":  {"USER", 19, "RELEASECAPTURE", 0},
	"USER.#22":  {"USER", 22, "SETFOCUS", 2},
	"USER.#29":  {"USER", 29, "SCREENTOCLIENT", 6},
	"USER.#32":  {"USER", 32, "GETWINDOWRECT", 6},
	"USER.#33":  {"USER", 33, "GETCLIENTRECT", 6},
	"USER.#34":  {"USER", 34, "ENABLEWINDOW", 4},
	"USER.#35":  {"USER", 35, "ISWINDOWENABLED", 2},
	"USER.#36":  {"USER", 36, "GETWINDOWTEXT", 8},
	"USER.#37":  {"USER", 37, "SETWINDOWTEXT", 6},
	"USER.#39":  {"USER", 39, "BEGINPAINT", 6},
	"USER.#40":  {"USER", 40, "ENDPAINT", 6},
	"USER.#41":  {"USER", 41, "CREATEWINDOW", 30},
	"USER.#42":  {"USER", 42, "SHOWWINDOW", 4},
	"USER.#46":  {"USER", 46, "GETPARENT", 2},
	"USER.#48":  {"USER", 48, "ISCHILD", 4},
	"USER.#53":  {"USER", 53, "DESTROYWINDOW", 2},
	"USER.#56":  {"USER", 56, "MOVEWINDOW", 12},
	"USER.#57":  {"USER", 57, "REGISTERCLASS", 4},
	"USER.#62":  {"USER", 62, "SETSCROLLPOS", 8},
	"USER.#64":  {"USER", 64, "SETSCROLLRANGE", 10},
	"USER.#65":  {"USER", 65, "GETSCROLLRANGE", 12},
	"USER.#66":  {"USER", 66, "GETDC", 2},
	"USER.#68":  {"USER", 68, "RELEASEDC", 4},
	"USER.#69":  {"USER", 69, "SETCURSOR", 2},
	"USER.#72":  {"USER", 72, "SETRECT", 12},
	"USER.#76":  {"USER", 76, "PTINRECT", 8},
	"USER.#77":  {"USER", 77, "OFFSETRECT", 8},
	"USER.#78":  {"USER", 78, "INFLATERECT", 8},
	"USER.#81":  {"USER", 81, "FILLRECT", 8},
	"USER.#83":  {"USER", 83, "FRAMERECT", 8},
	"USER.#85":  {"USER", 85, "DRAWTEXT", 14},
	"USER.#89":  {"USER", 89, "CREATEDIALOG", 12},
	"USER.#90":  {"USER", 90, "ISDIALOGMESSAGE", 6},
	"USER.#91":  {"USER", 91, "GETDLGITEM", 4},
	"USER.#101": {"USER", 101, "SENDDLGITEMMESSAGE", 12},
	"USER.#104": {"USER", 104, "MESSAGEBEEP", 2},
	"USER.#107": {"USER", 107, "DEFWINDOWPROC", 10},
	"USER.#109": {"USER", 109, "PEEKMESSAGE", 12},
	"USER.#111": {"USER", 111, "SENDMESSAGE", 10},
	"USER.#113": {"USER", 113, "TRANSLATEMESSAGE", 4},
	"USER.#114": {"USER", 114, "DISPATCHMESSAGE", 4},
	"USER.#124": {"USER", 124, "UPDATEWINDOW", 2},
	"USER.#125": {"USER", 125, "INVALIDATERECT", 8},
	"USER.#127": {"USER", 127, "VALIDATERECT", 6},
	"USER.#133": {"USER", 133, "GETWINDOWWORD", 4},
	"USER.#134": {"USER", 134, "SETWINDOWWORD", 6},
	"USER.#136": {"USER", 136, "SETWINDOWLONG", 8},
	"USER.#154": {"USER", 154, "CHECKMENUITEM", 6},
	"USER.#155": {"USER", 155, "ENABLEMENUITEM", 6},
	"USER.#157": {"USER", 157, "GETMENU", 2},
	"USER.#160": {"USER", 160, "DRAWMENUBAR", 2},
	"USER.#171": {"USER", 171, "WINHELP", 12},
	"USER.#173": {"USER", 173, "LOADCURSOR", 6},
	"USER.#174": {"USER", 174, "LOADICON", 6},
	"USER.#177": {"USER", 177, "LOADACCELERATORS", 6},
	"USER.#178": {"USER", 178, "TRANSLATEACCELERATOR", 8},
	"USER.#179": {"USER", 179, "GETSYSTEMMETRICS", 2},
	"USER.#219": {"USER", 219, "CREATEDIALOGINDIRECT", 12},
	"USER.#222": {"USER", 222, "GETKEYBOARDSTATE", 4},
	"USER.#223": {"USER", 223, "SETKEYBOARDSTATE", 4},
	"USER.#243": {"USER", 243, "GETDIALOGBASEUNITS", 0},
	"USER.#277": {"USER", 277, "GETDLGCTRLID", 2},
	"USER.#282": {"USER", 282, "SELECTPALETTE", 6},
	"USER.#283": {"USER", 283, "REALIZEPALETTE", 2},
	"USER.#284": {"USER", 284, "GETFREESYSTEMRESOURCES", 2},
	"USER.#286": {"USER", 286, "GETDESKTOPWINDOW", 0},
	"USER.#308": {"USER", 308, "DEFDLGPROC", 10},
	"USER.#414": {"USER", 414, "MODIFYMENU", 12},
	// --- GDI（43 支）---
	"GDI.#1":   {"GDI", 1, "SETBKCOLOR", 6},
	"GDI.#2":   {"GDI", 2, "SETBKMODE", 4},
	"GDI.#6":   {"GDI", 6, "SETPOLYFILLMODE", 4},
	"GDI.#9":   {"GDI", 9, "SETTEXTCOLOR", 6},
	"GDI.#19":  {"GDI", 19, "LINETO", 6},
	"GDI.#20":  {"GDI", 20, "MOVETO", 6},
	"GDI.#31":  {"GDI", 31, "SETPIXEL", 10},
	"GDI.#33":  {"GDI", 33, "TEXTOUT", 12},
	"GDI.#34":  {"GDI", 34, "BITBLT", 22},
	"GDI.#36":  {"GDI", 36, "POLYGON", 8},
	"GDI.#37":  {"GDI", 37, "POLYLINE", 8},
	"GDI.#45":  {"GDI", 45, "SELECTOBJECT", 4},
	"GDI.#48":  {"GDI", 48, "CREATEBITMAP", 12},
	"GDI.#52":  {"GDI", 52, "CREATECOMPATIBLEDC", 2},
	"GDI.#56":  {"GDI", 56, "CREATEFONT", 30},
	"GDI.#57":  {"GDI", 57, "CREATEFONTINDIRECT", 4},
	"GDI.#60":  {"GDI", 60, "CREATEPATTERNBRUSH", 2},
	"GDI.#61":  {"GDI", 61, "CREATEPEN", 8},
	"GDI.#66":  {"GDI", 66, "CREATESOLIDBRUSH", 4},
	"GDI.#68":  {"GDI", 68, "DELETEDC", 2},
	"GDI.#69":  {"GDI", 69, "DELETEOBJECT", 2},
	"GDI.#74":  {"GDI", 74, "GETBITMAPBITS", 10},
	"GDI.#75":  {"GDI", 75, "GETBKCOLOR", 2},
	"GDI.#76":  {"GDI", 76, "GETBKMODE", 2},
	"GDI.#80":  {"GDI", 80, "GETDEVICECAPS", 4},
	"GDI.#83":  {"GDI", 83, "GETPIXEL", 6},
	"GDI.#87":  {"GDI", 87, "GETSTOCKOBJECT", 2},
	"GDI.#90":  {"GDI", 90, "GETTEXTCOLOR", 2},
	"GDI.#91":  {"GDI", 91, "GETTEXTEXTENT", 8},
	"GDI.#93":  {"GDI", 93, "GETTEXTMETRICS", 6},
	"GDI.#106": {"GDI", 106, "SETBITMAPBITS", 10},
	"GDI.#119": {"GDI", 119, "ADDFONTRESOURCE", 4},
	"GDI.#136": {"GDI", 136, "REMOVEFONTRESOURCE", 4},
	"GDI.#150": {"GDI", 150, "UNREALIZEOBJECT", 2},
	"GDI.#162": {"GDI", 162, "GETBITMAPDIMENSION", 2},
	"GDI.#163": {"GDI", 163, "SETBITMAPDIMENSION", 6},
	"GDI.#330": {"GDI", 330, "ENUMFONTFAMILIES", 14},
	"GDI.#346": {"GDI", 346, "SETTEXTALIGN", 4},
	"GDI.#360": {"GDI", 360, "CREATEPALETTE", 4},
	"GDI.#364": {"GDI", 364, "SETPALETTEENTRIES", 10},
	"GDI.#367": {"GDI", 367, "ANIMATEPALETTE", 10},
	"GDI.#375": {"GDI", 375, "GETSYSTEMPALETTEENTRIES", 10},
	"GDI.#442": {"GDI", 442, "CREATEDIBITMAP", 20},
	// --- WIN87EM（1 支）---
	"WIN87EM.#1": {"WIN87EM", 1, "__FPMATH", -1},
	// --- MMSYSTEM（2 支）---
	"MMSYSTEM.#2":   {"MMSYSTEM", 2, "SNDPLAYSOUND", 6},
	"MMSYSTEM.#701": {"MMSYSTEM", 701, "MCISENDCOMMAND", 12},
	// --- COMMDLG（3 支）---
	"COMMDLG.#1":  {"COMMDLG", 1, "GETOPENFILENAME", 4},
	"COMMDLG.#2":  {"COMMDLG", 2, "GETSAVEFILENAME", 4},
	"COMMDLG.#26": {"COMMDLG", 26, "COMMDLGEXTENDEDERROR", 0},
}

// Lookup 用 ne.Import.Key() 的鍵查表。
func Lookup(key string) (Func, bool) {
	f, ok := Table[key]
	return f, ok
}

// Describe 回傳給人看的名字：查得到就是 `GDI.BITBLT`，查不到退回原鍵。
func Describe(key string) string {
	if f, ok := Table[key]; ok {
		return f.Module + "." + f.Name
	}
	return key
}

// ValueImports 是「匯入的不是函式，是一個常數」。
//
// `KERNEL.__AHSHIFT` 是最典型的：它被重定位進 `mov cx, ????` 的立即數，
// 後面接 `shl bx, cl`——那是 huge 指標換算「跨過幾個 selector」的算式。
// 把它當成函式位址填進去，位移運算就會整個歪掉，而且不會當掉。
//
// 值取自 286 保護模式的 selector 配置：selector 每 8 遞增一格，
// 所以 `__AHINCR` ＝ 8、`__AHSHIFT` ＝ 3。CIV.EXE 只用到後者（151 筆重定位）。
var ValueImports = map[string]uint16{
	"KERNEL.#113": 3, // __AHSHIFT
	"KERNEL.#114": 8, // __AHINCR
}
