package win16

import "github.com/wicanr2/wine-gorgon/internal/ne"

// RegisterResource 登記資源相關的 API。
//
// Win16 的資源名稱有兩種形態：真正的字串，或 `MAKEINTRESOURCE(n)`
// ——後者是一個 selector 為 0、位移就是編號的假指標。兩種都要認。
func RegisterResource(p *Process) {
	h := p.Handlers

	// FindResource(HINSTANCE, LPCSTR name, LPCSTR type) → HRSRC
	h["KERNEL.#60"] = func(p *Process, a Args) (uint32, error) {
		nameSel, nameOff := a.Ptr(2)
		typeSel, typeOff := a.Ptr(6)
		typeID, typeName := p.resName(typeSel, typeOff)
		id, name := p.resName(nameSel, nameOff)
		r, ok := p.Mod.Image.FindResource(typeID, typeName, name, id)
		if !ok {
			p.note("FindResource 找不到 型別=%s/%d 名稱=%s/%d", typeName, typeID, name, id)
			return 0, nil
		}
		p.resources = append(p.resources, r)
		return uint32(len(p.resources)), nil // HRSRC ＝ 1-based 索引
	}

	// LoadResource(HINSTANCE, HRSRC) → HGLOBAL
	h["KERNEL.#61"] = func(p *Process, a Args) (uint32, error) {
		i := int(a.Word(2))
		if i < 1 || i > len(p.resources) {
			return 0, nil
		}
		r := p.resources[i-1]
		data, err := p.Mod.Image.ResourceData(r)
		if err != nil {
			return 0, err
		}
		blk := p.Mod.Mem.Alloc("資源 "+r.String(), len(data))
		copy(blk.Data, data)
		return uint32(blk.Sel), nil
	}

	h["KERNEL.#63"] = func(p *Process, a Args) (uint32, error) { // FreeResource
		return boolTo(!p.Mod.Mem.Free(a.Word(0))), nil // 回 0 表示釋放成功
	}

	// LoadAccelerators：加速鍵表不影響畫面，先只確認資源在、回一個 handle。
	h["USER.#177"] = func(p *Process, a Args) (uint32, error) {
		sel, off := a.Ptr(2)
		id, name := p.resName(sel, off)
		if _, ok := p.Mod.Image.FindResource(ne.RTAccelerator, "", name, id); !ok {
			p.note("LoadAccelerators 找不到加速鍵表 %s/%d", name, id)
			return 0, nil
		}
		return 0x0200, nil
	}

	h["USER.#178"] = func(p *Process, _ Args) (uint32, error) { return 0, nil } // TranslateAccelerator
}

// resName 把 Win16 的「字串或 MAKEINTRESOURCE」拆開。
func (p *Process) resName(sel, off uint16) (id uint16, name string) {
	if sel == 0 {
		return off, ""
	}
	s := p.CString(sel, off)
	if len(s) > 1 && s[0] == '#' {
		n := uint16(0)
		for _, c := range s[1:] {
			if c < '0' || c > '9' {
				return 0, s
			}
			n = n*10 + uint16(c-'0')
		}
		return n, ""
	}
	return 0, s
}
