"""
Build SDV-standard NPC spritesheet from xiami.png using xiami.json bbox.

SDV's NPC.AnimatedSprite indexes walk frames by `facingDirection * 4`:
  facing 0 (up)    -> frame 8
  facing 1 (right) -> frame 4
  facing 2 (down)  -> frame 0
  facing 3 (left)  -> frame 12

So the spritesheet MUST be laid out as 4 frames per direction in this order:
  Row 0 (idx 0-3):  Down walk
  Row 1 (idx 4-7):  Right walk
  Row 2 (idx 8-11): Up walk
  Row 3 (idx 12-15): Left walk

We sample 4 frames from each original 8-frame walk cycle (indices 0, 2, 4, 6
= _01, _03, _05, _07). Right walk is produced by horizontally mirroring the
left-walk frames (XiaMi only has left-facing art).

Action frames follow after the walk block (no strict layout required, referenced
by FrameXxx constants in XiaMiData.cs):
  Row 4  (16-19): hoe_01, 03, 05, 07
  Row 5  (20-23): hoe_09, watering_01, 03, 05
  Row 6  (24-27): watering_07, hold_pumpkin, hold_crop, hold_flower
  Row 7  (28-31): idle_stand, hold_chicken, emote_cheer, emote_heart
  Row 8  (32-33): emote_surprised, emote_sleepy [+ 2 empty]

Total: 34 used frames across 9 rows. Frame size: 16x32. Output: 64x288 RGBA.
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

# Sentinel for blank cells in the layout.
EMPTY = None
# Sentinel marker: tuple ("mirror", name) horizontally flips the source frame.
def mirror(name):
    return ("mirror", name)


# Frame layout — order matches linear sprite index (idx = row*COLS + col).
# Indices 0..15 are strictly walk (SDV engine requirement).
frame_order = [
    # Row 0 — Down walk (idx 0-3)
    "front_01", "front_03", "front_05", "front_07",
    # Row 1 — Right walk (idx 4-7): mirror of left
    mirror("side_left_01"), mirror("side_left_03"), mirror("side_left_05"), mirror("side_left_07"),
    # Row 2 — Up walk (idx 8-11)
    "back_01", "back_03", "back_05", "back_07",
    # Row 3 — Left walk (idx 12-15)
    "side_left_01", "side_left_03", "side_left_05", "side_left_07",
    # Row 4 — Hoe A (idx 16-19)
    "hoe_01", "hoe_03", "hoe_05", "hoe_07",
    # Row 5 — Hoe_09 + watering A (idx 20-23)
    "hoe_09", "watering_01", "watering_03", "watering_05",
    # Row 6 — Watering tail + holds (idx 24-27)
    "watering_07", "hold_pumpkin_01", "hold_crop_01", "hold_flower_01",
    # Row 7 — Holds + emotes (idx 28-31)
    "idle_stand_01", "hold_chicken_01", "emote_cheer_01", "emote_heart_01",
    # Row 8 — Emotes tail (idx 32-33) + 2 empty
    "emote_surprised_01", "emote_sleepy_01", EMPTY, EMPTY,
]

ROWS = (len(frame_order) + COLS - 1) // COLS  # ceil division


# Load source image, remove near-white checkerboard background.
img = Image.open(src_img_path).convert("RGBA")
arr = np.array(img)
mask = (arr[:, :, 0] > 230) & (arr[:, :, 1] > 230) & (arr[:, :, 2] > 230)
arr[mask, 3] = 0
img = Image.fromarray(arr)

# Load JSON metadata.
with open(json_path, "r", encoding="utf-8") as f:
    meta = json.load(f)
frames_data = meta["frames"]


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


# Build output sheet.
out = Image.new("RGBA", (FRAME_W * COLS, FRAME_H * ROWS), (0, 0, 0, 0))

for idx, entry in enumerate(frame_order):
    if entry is EMPTY:
        continue
    row = idx // COLS
    col = idx % COLS

    if isinstance(entry, tuple) and entry[0] == "mirror":
        name = entry[1]
        frame = crop_frame(name).transpose(Image.FLIP_LEFT_RIGHT)
    else:
        name = entry
        frame = crop_frame(name)

    cell = fit_to_cell(frame)
    out.paste(cell, (col * FRAME_W, row * FRAME_H), cell)

out.save(dst_path)
print(f"Saved: {dst_path}")
print(f"Size: {out.size[0]}x{out.size[1]} (RGBA), {COLS}x{ROWS} grid, frame: {FRAME_W}x{FRAME_H}")
print(f"Total frames: {len(frame_order)} (indices 0..{len(frame_order)-1}; {sum(1 for e in frame_order if e is EMPTY)} empty)")
print(f"C#: new AnimatedSprite(\"Characters\\\\XiaMi\", 0, {FRAME_W}, {FRAME_H})")

# Print frame index reference.
print("\n--- Frame Index Reference (for C# code) ---")
for idx, entry in enumerate(frame_order):
    if entry is EMPTY:
        label = "(empty)"
    elif isinstance(entry, tuple):
        label = f"{entry[1]} [mirrored]"
    else:
        label = entry
    print(f"  {idx:2d}: {label}")
