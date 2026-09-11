#!/usr/bin/env python3
"""產生「劇本 0 戰略畫面逐選單取證」的 nerun 腳本。

用法：python3 scripts/pto2-s0-menus.py <選單名>… > scripts/.gen/<名>.txt
     python3 scripts/pto2-s0-menus.py --remake <選單名>…   # remake 重播格式
     選單名：order assign personnel info armament option execute、
             CONTROLS 裡的控制項名，或 controls（全部控制項）、deep（第二層子選單）、
             all（選單＋控制項＋第二層）

每一支腳本都從冷開機走 pto2-s0-strategy.txt 到戰略畫面，再：
  1. 點右欄圖示展開子選單，存一幀（<選單>.png）
  2. 逐項點子選單，存一幀（<選單>_<項>.png）
  3. 右鍵取消，存一幀（<選單>_<項>_back.png）——用來確認真的退回來了；
     退不回來的話後面每一幀都不能用，所以這一幀一定要看
  4. 每一項開始前先回到已知狀態：右鍵三下退到主選單、再點圖示展開

量到的行為（INFO 那一輪）：右鍵從表格退回子選單，**子選單保持開著**；
子選單開著時再點同一個圖示是**切換**，會把它收掉。所以不能用「再點一次
圖示」當作叫回子選單——那會讓每隔一項落空。右鍵在主選單層沒有作用。

座標（螢幕座標；WinG DIB 是 (x+1, y-39)）：
  右欄圖示中線 x=504，y=195+24k（DIB 156+24k，R193 量的）
  子選單按鈕中線 x=584，同一組 y
輸出幀是 WinG DIB 的上面 640×400（遊戲自己的畫面），存在 /out/s0menu/。
"""
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
PREFIX = (HERE / "pto2-s0-strategy.txt").read_text(encoding="utf-8")

# 子選單項目（原版 Win16 英文，R193 抓的畫面）
MENUS = {
    "order":     (0, ["fleet", "submarine", "base_af", "marines"]),
    "assign":    (1, ["fleet", "base_af", "marines", "arms", "base_op"]),
    "personnel": (2, ["navy", "army", "admiral", "general", "spy"]),
    "info":      (3, ["fleet", "unit", "base", "post", "nation", "foreign", "goals"]),
    "armament":  (4, ["in_service", "building", "sunk", "warship", "aircraft", "tank", "submarine"]),
    "option":    (5, ["save", "quit", "setup", "difficulty"]),
    "execute":   (6, []),
}
# 選單以外、點了會開東西的控制項。座標是 **DIB 座標**（map.png 上量的），
# 輸出 nerun 腳本時才換成螢幕座標。每一個都是：退回主選單 → 點一下 →
# 存幀 → 右鍵 → 存幀。
CONTROLS = {
    "tool_route":   (388, 387),  # 底列第 1 個：箭頭＋紅點
    "tool_base":    (411, 387),  # 第 2 個：基地建築
    "tool_world":   (436, 387),  # 第 3 個：世界地圖
    "tool_weather": (459, 387),  # 第 4 個：太陽與雲
    "tool_scroll":  (483, 387),  # 第 5 個：十字箭頭
    "fleet_title":  (140, 301),  # 艦隊面板標題列「Fleet」
    "fleet_cell01": (52, 327),   # 艦隊面板第 01 格
    "fleet_up":     (28, 300),   # 面板左側 ▲
    "fleet_down":   (28, 347),   # 面板左側 ▼
    "compass_nw":   (415, 296),  # 方位盤 NW
    "compass_clock": (443, 322), # 方位盤中央的時鐘
    "weather_grid": (443, 45),   # 天候九宮格中央
    "minimap":      (567, 360),  # 右下小地圖
    "officer":      (567, 90),   # 階段條下的軍官插畫
    "date":         (567, 12),   # 日期框
    "map_kure":     (281, 219),  # 地圖上吳港的艦隊圖示
    "map_tokyo":    (424, 188),  # 地圖上東京基地
}
# 第二層子選單：點了子項之後右欄又換成一組子選單的那幾個。
# 鍵是 (選單, 子項)，值是第二層各項名。量到的（INFO 那一輪）：
# INFO→UNIT、INFO→NATION 都會把右欄換成第二層。
DEEP = {
    ("info", "unit"):   ["navy_af", "army_af", "marines", "army_div", "submarine"],
    ("info", "nation"): ["data", "navy", "army", "new_arms"],
}
ICON_X, ITEM_X, Y0, DY = 504, 584, 195, 24
OUT = "/out/s0menu"


def press(x, y, right=False, hold=400000, after=(8000000, 8000000)):
    d, u = ("rmousedown", "rmouseup") if right else ("mousedown", "mouseup")
    s = f"{d} {x},{y}\nrun {hold}\n{u} {x},{y}\n"
    return s + "".join(f"run {n}\n" for n in after)


def reset():
    """右鍵三下：從任何一層退回主選單（主選單層的右鍵沒有作用）。
    點在日本海的空白海面（DIB 300,60），避開按鈕、基地與艦隊。"""
    return "".join(press(299, 99, right=True, after=(6000000,)) for _ in range(3))


def wing(name):
    return f"wing {OUT}/{name}.png 0,0,640,400\n"


def menu_script(name):
    k, items = MENUS[name]
    y = Y0 + DY * k
    s = f"\n# ── {name}（右欄第 {k + 1} 項）──\n"
    s += reset() + press(ICON_X, y) + wing(name)
    for j, item in enumerate(items):
        iy = Y0 + DY * j
        s += f"# {name} → {item}\n"
        if j > 0:
            s += reset() + press(ICON_X, y)
        s += press(ITEM_X, iy) + wing(f"{name}_{item}")
        s += press(ITEM_X, iy, right=True) + wing(f"{name}_{item}_back")
    if not items:
        # EXECUTE 是確認框；右鍵取消，不按 YES（按了會推進一回合）
        s += press(ICON_X, y, right=True) + wing(f"{name}_back")
    return s


def remake_script(names):
    """同一組點擊，輸出成 remake 的重播格式（internal/app/input_replay.go）。

    座標換成邏輯座標（= WinG DIB）：螢幕 (x, y) → (x+1, y-39)。
    remake 不走開場流程，`scenario 0` 直接開新局到戰略地圖——開場流程
    另外對拍，這裡只管戰略畫面上的選單。"""
    def dib(x, y):
        return x + 1, y - 39

    def click(x, y, right=False):
        dx, dy = dib(x, y)
        return f"{'rclick' if right else 'click'} {dx},{dy}\n"

    out = ["# 由 wine-gorgon/scripts/pto2-s0-menus.py --remake 產生，不要手改。\n",
           "# 與原版 oracle 的 nerun 腳本是同一串點擊；幀名也相同，可以一對一比對。\n",
           "scenario 0\n", "shot map\n"]
    for name in names:
        if name == "deep":
            for menu, item in DEEP:
                k, items = MENUS[menu]
                j = items.index(item)
                out.append(f"\n# ── {menu} → {item} 的第二層 ──\n")
                for m, sub in enumerate(DEEP[(menu, item)]):
                    out += [click(299, 99, True)] * 3
                    out += [click(ICON_X, Y0 + DY * k), click(ITEM_X, Y0 + DY * j),
                            click(ITEM_X, Y0 + DY * m), f"shot {menu}_{item}_{sub}\n",
                            click(ITEM_X, Y0 + DY * m, True), f"shot {menu}_{item}_{sub}_back\n"]
            continue
        if name in CONTROLS:
            dx, dy = CONTROLS[name]
            out.append(f"\n# ── 控制項 {name} ──\n")
            out += [click(299, 99, True)] * 3
            out += [f"click {dx},{dy}\n", f"shot {name}\n",
                    f"rclick {dx},{dy}\n", f"shot {name}_back\n"]
            continue
        k, items = MENUS[name]
        y = Y0 + DY * k
        out.append(f"\n# ── {name}（右欄第 {k + 1} 項）──\n")
        out += [click(299, 99, True)] * 3
        out += [click(ICON_X, y), f"shot {name}\n"]
        for j, item in enumerate(items):
            iy = Y0 + DY * j
            out.append(f"# {name} → {item}\n")
            if j > 0:
                out += [click(299, 99, True)] * 3 + [click(ICON_X, y)]
            out += [click(ITEM_X, iy), f"shot {name}_{item}\n",
                    click(ITEM_X, iy, True), f"shot {name}_{item}_back\n"]
        if not items:
            out += [click(ICON_X, y, True), f"shot {name}_back\n"]
    return "".join(out)


def deep_script(menu, item):
    """第三層：圖示 → 子項 → 第二層每一項，各存一幀並右鍵退回。"""
    k, items = MENUS[menu]
    j = items.index(item)
    y, iy = Y0 + DY * k, Y0 + DY * j
    s = f"\n# ── {menu} → {item} 的第二層 ──\n"
    for m, sub in enumerate(DEEP[(menu, item)]):
        sy = Y0 + DY * m
        s += reset() + press(ICON_X, y) + press(ITEM_X, iy)
        s += press(ITEM_X, sy) + wing(f"{menu}_{item}_{sub}")
        s += press(ITEM_X, sy, right=True) + wing(f"{menu}_{item}_{sub}_back")
    return s


# ASSIGN→ARMS 是整頁畫面（紅色錨紋底圖），**右鍵退不出來**，要點右上的 END。
# 上排四鈕的 DIB 中線（assign_arms.png 量的）：DESIGN 385、BUILD 465、
# SCUTTLE 545、END 613，y=11。
ARMS_BUTTONS = [("design", 385), ("build", 465), ("scuttle", 545)]
ARMS_END = (613, 11)


def arms_script():
    k, items = MENUS["assign"]
    y, iy = Y0 + DY * k, Y0 + DY * items.index("arms")
    ex, ey = ARMS_END[0] - 1, ARMS_END[1] + 39
    s = "\n# ── assign → arms 的四鈕 ──\n"
    for name, bx in ARMS_BUTTONS:
        s += reset() + press(ICON_X, y) + press(ITEM_X, iy)
        s += press(bx - 1, 11 + 39) + wing(f"assign_arms_{name}")
        s += press(bx - 1, 11 + 39, right=True) + wing(f"assign_arms_{name}_back")
        s += press(ex, ey) + wing(f"assign_arms_{name}_end")
        s += press(ex, ey)  # 子畫面裡點一次 END 可能只退一層，再點一次
    # 重抓 BASE OP.（第一輪那一幀還停在造艦畫面）
    by = Y0 + DY * items.index("base_op")
    s += reset() + press(ICON_X, y) + press(ITEM_X, by) + wing("assign_base_op")
    s += press(ITEM_X, by, right=True) + wing("assign_base_op_back")
    return s


def control_script(name):
    dx, dy = CONTROLS[name]
    x, y = dx - 1, dy + 39
    s = f"\n# ── 控制項 {name}（DIB {dx},{dy}）──\n"
    s += reset() + press(x, y) + wing(name)
    s += press(x, y, right=True) + wing(f"{name}_back")
    return s


def main():
    args = sys.argv[1:]
    remake = "--remake" in args
    names = [a for a in args if a != "--remake"] or ["all"]
    if names == ["all"]:
        names = list(MENUS) + list(CONTROLS) + ["deep"]
    elif names == ["controls"]:
        names = list(CONTROLS)
    if remake:
        sys.stdout.write(remake_script(names))
        return
    out = PREFIX + wing("map")
    for n in names:
        if n == "deep":
            for menu, item in DEEP:
                out += deep_script(menu, item)
            out += arms_script()
        elif n in CONTROLS:
            out += control_script(n)
        else:
            out += menu_script(n)
    sys.stdout.write(out)


if __name__ == "__main__":
    main()
