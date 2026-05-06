"""
Test script: crop all frames from xiami.png based on xiami.json config.
Outputs individual frame PNGs + a contact sheet for visual inspection.

Usage: python crop_test.py
Output: test_crops/ directory with all cropped frames and _contact_sheet.png
"""
import json
import os
from PIL import Image, ImageDraw

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
CONFIG_PATH = os.path.join(SCRIPT_DIR, "xiami.json")
SPRITE_PATH = os.path.join(SCRIPT_DIR, "xiami.png")
OUTPUT_DIR = os.path.join(SCRIPT_DIR, "test_crops")


def main():
    with open(CONFIG_PATH, "r", encoding="utf-8") as f:
        config = json.load(f)

    img = Image.open(SPRITE_PATH).convert("RGBA")
    w, h = img.size
    print(f"Sprite sheet: {w}x{h} (mode: {img.mode})")
    print(f"Layout: {config['layout']['cols']} cols x {config['layout']['rows']} rows, cell={config['layout']['cell_width']}x{config['layout']['cell_height']}")

    frames = config["frames"]
    print(f"Frames to crop: {len(frames)}")

    # Clean and recreate output dir
    os.makedirs(OUTPUT_DIR, exist_ok=True)

    # Crop each frame
    for frame_name, frame_data in frames.items():
        bbox = frame_data["bbox"]
        x, y, fw, fh = bbox["x"], bbox["y"], bbox["w"], bbox["h"]
        cropped = img.crop((x, y, x + fw, y + fh))
        out_path = os.path.join(OUTPUT_DIR, f"{frame_name}.png")
        cropped.save(out_path)

    # Generate flipped right-side frames from left frames
    right_count = 0
    for frame_name in frames:
        if frame_name.startswith("side_left_"):
            bbox = frames[frame_name]["bbox"]
            x, y, fw, fh = bbox["x"], bbox["y"], bbox["w"], bbox["h"]
            cropped = img.crop((x, y, x + fw, y + fh))
            flipped = cropped.transpose(Image.FLIP_LEFT_RIGHT)
            right_name = frame_name.replace("side_left_", "side_right_")
            out_path = os.path.join(OUTPUT_DIR, f"{right_name}.png")
            flipped.save(out_path)
            right_count += 1

    # --- Contact sheet ---
    frames_list = list(frames.items())
    cols = 8
    rows_needed = (len(frames_list) + cols - 1) // cols
    cell_w, cell_h = 180, 200
    contact = Image.new("RGBA", (cols * cell_w, rows_needed * cell_h), (240, 240, 240, 255))
    draw = ImageDraw.Draw(contact)

    for idx, (frame_name, frame_data) in enumerate(frames_list):
        bbox = frame_data["bbox"]
        x, y, fw, fh = bbox["x"], bbox["y"], bbox["w"], bbox["h"]
        cropped = img.crop((x, y, x + fw, y + fh))

        # Scale to fit
        max_w, max_h = 160, 165
        scale = min(max_w / fw, max_h / fh, 1.0)
        if scale < 1.0:
            new_w, new_h = int(fw * scale), int(fh * scale)
            cropped = cropped.resize((new_w, new_h), Image.LANCZOS)
        else:
            new_w, new_h = fw, fh

        col = idx % cols
        row = idx // cols
        paste_x = col * cell_w + (cell_w - new_w) // 2
        paste_y = row * cell_h + (cell_h - new_h - 22) // 2
        contact.paste(cropped, (paste_x, paste_y), cropped)

        # Label
        text_x = col * cell_w + cell_w // 2
        text_y = row * cell_h + cell_h - 10
        draw.text((text_x, text_y), frame_name, fill=(0, 0, 0, 255), anchor="mb")

    contact_path = os.path.join(OUTPUT_DIR, "_contact_sheet.png")
    contact.save(contact_path)

    print(f"\nDone!")
    print(f"  Individual frames: {OUTPUT_DIR}\\")
    print(f"  Contact sheet:     {contact_path}")
    print(f"  Cropped: {len(frames)} frames + {right_count} right-flipped = {len(frames) + right_count} total")


if __name__ == "__main__":
    main()
