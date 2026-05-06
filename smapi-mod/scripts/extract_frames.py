"""
Extract individual frames from xiami.png based on JSON bbox data.
Removes checkerboard background (makes it transparent).

Output dir: assets/xiami/frames/

Usage:
  python smapi-mod/scripts/extract_frames.py
"""

import json
import pathlib
import numpy as np
from PIL import Image

ASSETS_DIR = pathlib.Path(__file__).resolve().parent.parent / "assets" / "xiami"
SOURCE_PNG = ASSETS_DIR / "xiami.png"
SOURCE_JSON = ASSETS_DIR / "sprite_actions_positions_1448x1086.json"
OUT_DIR = ASSETS_DIR / "frames"


def main():
    OUT_DIR.mkdir(exist_ok=True)

    with open(SOURCE_JSON, "r", encoding="utf-8") as f:
        meta = json.load(f)

    frames = meta["frames"]
    src_info = meta["source_image"]
    expected_w = src_info["width"]
    expected_h = src_info["height"]

    src = Image.open(SOURCE_PNG).convert("RGBA")
    actual_w, actual_h = src.size
    print(f"Source image: {actual_w}x{actual_h}")
    print(f"JSON expects: {expected_w}x{expected_h}")

    # Remove checkerboard background -> transparent
    pixels = np.array(src)
    r, g, b = pixels[:, :, 0].astype(int), pixels[:, :, 1].astype(int), pixels[:, :, 2].astype(int)
    is_bg = ((r > 240) & (g > 240) & (b > 240)) | \
            ((np.abs(r - 244) < 8) & (np.abs(g - 244) < 8) & (np.abs(b - 244) < 8))
    pixels[is_bg, 3] = 0
    src = Image.fromarray(pixels)

    # Scale factor (in case image size differs from JSON expectation)
    sx = actual_w / expected_w
    sy = actual_h / expected_h
    print(f"Scale factor: sx={sx:.4f}, sy={sy:.4f}")
    print(f"Extracting {len(frames)} frames to {OUT_DIR}\n")

    for name, info in frames.items():
        bbox = info["bbox"]
        bx, by, bw, bh = bbox["x"], bbox["y"], bbox["w"], bbox["h"]
        x = int(bx * sx)
        y = int(by * sy)
        w = int(bw * sx)
        h = int(bh * sy)
        crop = src.crop((x, y, x + w, y + h))
        out_path = OUT_DIR / f"{name}.png"
        crop.save(out_path)
        print(f"  {name:20s}  bbox=({x:4d},{y:4d},{w:3d},{h:3d})  -> {out_path.name}")

    print(f"\nDone. Check {OUT_DIR}")


if __name__ == "__main__":
    main()
