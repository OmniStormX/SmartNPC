"""
Extract frames from xiami.png and produce SDV-standard sprite sheet + portrait.
Removes checkerboard background before processing.

Output:
  - assets/xiami/XiaMi.png          : 64x128 character sprite (4 cols x 4 rows, 16x32/frame)
  - assets/xiami/XiaMi_Portrait.png : 64x64 portrait

SDV sprite layout (each cell 16x32):
  Row 0 (Down/Front):  front_01, front_02, front_03, front_04
  Row 1 (Right):       side_left_01..04 flipped horizontally
  Row 2 (Up/Back):     back_01, back_02, back_03, back_04
  Row 3 (Left):        side_left_01..04

Usage:
  pip install Pillow numpy
  python smapi-mod/scripts/process_sprite.py
"""

import json
import pathlib
import numpy as np
from PIL import Image

FRAME_W, FRAME_H = 16, 32
COLS, ROWS = 4, 4
PORTRAIT_SIZE = 64

ASSETS_DIR = pathlib.Path(__file__).resolve().parent.parent / "assets" / "xiami"
SOURCE_PNG = ASSETS_DIR / "xiami.png"
SOURCE_JSON = ASSETS_DIR / "sprite_actions_positions_1448x1086.json"
OUT_SPRITE = ASSETS_DIR / "XiaMi.png"
OUT_PORTRAIT = ASSETS_DIR / "XiaMi_Portrait.png"

LAYOUT = [
    # Row 0: Down/Front
    ["front_01", "front_02", "front_03", "front_04"],
    # Row 1: Right (flip left frames)
    ["side_left_01", "side_left_02", "side_left_03", "side_left_04"],
    # Row 2: Up/Back
    ["back_01", "back_02", "back_03", "back_04"],
    # Row 3: Left
    ["side_left_01", "side_left_02", "side_left_03", "side_left_04"],
]
FLIP_ROWS = {1}


def remove_checker_bg(img):
    """Remove checkerboard background (near-white/light-gray) -> transparent."""
    pixels = np.array(img)
    r, g, b = pixels[:, :, 0].astype(int), pixels[:, :, 1].astype(int), pixels[:, :, 2].astype(int)
    is_bg = ((r > 240) & (g > 240) & (b > 240)) | \
            ((np.abs(r - 244) < 8) & (np.abs(g - 244) < 8) & (np.abs(b - 244) < 8))
    pixels[is_bg, 3] = 0
    return Image.fromarray(pixels)


def main():
    with open(SOURCE_JSON, "r", encoding="utf-8") as f:
        meta = json.load(f)

    frames = meta["frames"]
    src_info = meta["source_image"]
    expected_w = src_info["width"]
    expected_h = src_info["height"]

    src = Image.open(SOURCE_PNG).convert("RGBA")
    actual_w, actual_h = src.size

    # Remove checkerboard background
    src = remove_checker_bg(src)

    # Scale factor
    sx = actual_w / expected_w
    sy = actual_h / expected_h

    sheet = Image.new("RGBA", (COLS * FRAME_W, ROWS * FRAME_H), (0, 0, 0, 0))

    for row_idx, row_frames in enumerate(LAYOUT):
        for col_idx, frame_name in enumerate(row_frames):
            bbox = frames[frame_name]["bbox"]
            bx, by, bw, bh = bbox["x"], bbox["y"], bbox["w"], bbox["h"]
            x, y, w, h = int(bx * sx), int(by * sy), int(bw * sx), int(bh * sy)
            crop = src.crop((x, y, x + w, y + h))
            resized = crop.resize((FRAME_W, FRAME_H), Image.LANCZOS)
            if row_idx in FLIP_ROWS:
                resized = resized.transpose(Image.FLIP_LEFT_RIGHT)
            sheet.paste(resized, (col_idx * FRAME_W, row_idx * FRAME_H))

    sheet.save(OUT_SPRITE)
    print(f"Sprite sheet saved: {OUT_SPRITE} ({sheet.size[0]}x{sheet.size[1]})")

    # Portrait: use front_01 full frame resized to 64x64
    bbox = frames["front_01"]["bbox"]
    bx, by, bw, bh = bbox["x"], bbox["y"], bbox["w"], bbox["h"]
    x, y, w, h = int(bx * sx), int(by * sy), int(bw * sx), int(bh * sy)
    full_frame = src.crop((x, y, x + w, y + h))
    portrait = full_frame.resize((PORTRAIT_SIZE, PORTRAIT_SIZE), Image.LANCZOS)
    portrait.save(OUT_PORTRAIT)
    print(f"Portrait saved: {OUT_PORTRAIT} ({PORTRAIT_SIZE}x{PORTRAIT_SIZE})")


if __name__ == "__main__":
    main()
