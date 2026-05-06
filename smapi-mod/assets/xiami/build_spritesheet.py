"""
Build SDV-compatible full NPC spritesheet from xiami.png using xiami.json bbox.

Layout: 4 columns, 13 rows (52 cells, 49 used + 3 empty).
Frame size: 32x64 each. Total: 128x832.

Row mapping (frame indices):
  Row 0:  front_01..04       (walk front A)     idx 0-3
  Row 1:  front_05..08       (walk front B)     idx 4-7
  Row 2:  back_01..04        (walk back A)      idx 8-11
  Row 3:  back_05..08        (walk back B)      idx 12-15
  Row 4:  side_left_01..04   (walk left A)      idx 16-19
  Row 5:  side_left_05..08   (walk left B)      idx 20-23
  Row 6:  hoe_01..04         (hoe A)            idx 24-27
  Row 7:  hoe_05..08         (hoe B)            idx 28-31
  Row 8:  hoe_09, water_01..03                  idx 32-35
  Row 9:  water_04..07                          idx 36-39
  Row 10: hold_pumpkin, hold_crop, hold_flower, idle_stand  idx 40-43
  Row 11: hold_chicken, emote_cheer, emote_heart, emote_surprised  idx 44-47
  Row 12: emote_sleepy, (empty x3)             idx 48

Right-facing walk = left frames flipped (handled at runtime by game code).
"""
import json
from PIL import Image
import numpy as np

FRAME_W = 16
FRAME_H = 32
COLS = 4

src_img_path = r"d:\SmartNPC\smapi-mod\assets\xiami\xiami.png"
json_path = r"d:\SmartNPC\smapi-mod\assets\xiami\xiami.json"
dst_path = r"d:\SmartNPC\smapi-mod\assets\xiami\XiaMi_spritesheet.png"

# Load source image, make transparent
img = Image.open(src_img_path).convert("RGBA")
arr = np.array(img)
mask = (arr[:, :, 0] > 230) & (arr[:, :, 1] > 230) & (arr[:, :, 2] > 230)
arr[mask, 3] = 0
img = Image.fromarray(arr)

# Load JSON metadata
with open(json_path, "r", encoding="utf-8") as f:
    meta = json.load(f)
frames_data = meta["frames"]

# Ordered frame layout (all 49 frames)
frame_order = [
    # Row 0-1: front walk (8 frames)
    "front_01", "front_02", "front_03", "front_04",
    "front_05", "front_06", "front_07", "front_08",
    # Row 2-3: back walk (8 frames)
    "back_01", "back_02", "back_03", "back_04",
    "back_05", "back_06", "back_07", "back_08",
    # Row 4-5: left walk (8 frames)
    "side_left_01", "side_left_02", "side_left_03", "side_left_04",
    "side_left_05", "side_left_06", "side_left_07", "side_left_08",
    # Row 6-7: hoe (8 frames)
    "hoe_01", "hoe_02", "hoe_03", "hoe_04",
    "hoe_05", "hoe_06", "hoe_07", "hoe_08",
    # Row 8: hoe_09 + watering 01-03
    "hoe_09", "watering_01", "watering_02", "watering_03",
    # Row 9: watering 04-07
    "watering_04", "watering_05", "watering_06", "watering_07",
    # Row 10: hold items
    "hold_pumpkin_01", "hold_crop_01", "hold_flower_01", "idle_stand_01",
    # Row 11: hold + emotes
    "hold_chicken_01", "emote_cheer_01", "emote_heart_01", "emote_surprised_01",
    # Row 12: last emote
    "emote_sleepy_01",
]

ROWS = (len(frame_order) + COLS - 1) // COLS  # ceil division


def crop_frame(name):
    info = frames_data[name]
    bbox = info["bbox"]
    x, y, w, h = bbox["x"], bbox["y"], bbox["w"], bbox["h"]
    return img.crop((x, y, x + w, y + h))


def fit_to_cell(frame_img):
    cw, ch = frame_img.size
    scale = min(FRAME_W / cw, FRAME_H / ch)
    new_w = max(1, int(cw * scale))
    new_h = max(1, int(ch * scale))
    resized = frame_img.resize((new_w, new_h), Image.LANCZOS)

    cell = Image.new("RGBA", (FRAME_W, FRAME_H), (0, 0, 0, 0))
    paste_x = (FRAME_W - new_w) // 2
    paste_y = FRAME_H - new_h  # bottom-align
    cell.paste(resized, (paste_x, paste_y), resized)
    return cell


# Build output
out = Image.new("RGBA", (FRAME_W * COLS, FRAME_H * ROWS), (0, 0, 0, 0))

for idx, name in enumerate(frame_order):
    row = idx // COLS
    col = idx % COLS
    frame = crop_frame(name)
    cell = fit_to_cell(frame)
    out.paste(cell, (col * FRAME_W, row * FRAME_H), cell)

out.save(dst_path)
print(f"Saved: {dst_path}")
print(f"Size: {out.size[0]}x{out.size[1]} (RGBA), {COLS}x{ROWS} grid, frame: {FRAME_W}x{FRAME_H}")
print(f"Total frames: {len(frame_order)} (indices 0..{len(frame_order)-1})")
print(f"C#: new AnimatedSprite(\"Characters\\\\XiaMi\", 0, {FRAME_W}, {FRAME_H})")

# Print frame index reference
print("\n--- Frame Index Reference (for C# code) ---")
for idx, name in enumerate(frame_order):
    print(f"  {idx:2d}: {name}")
