"""
Generate updated sprite_actions_positions JSON for the new xiami.png (1536x1024).
Uses auto-detected frame bboxes from checkerboard-bg removal analysis.
"""
import json
import pathlib

OUT_PATH = pathlib.Path(r"D:\SmartNPC\smapi-mod\assets\xiami\sprite_actions_positions_1448x1086.json")

rows_data = [
    {"y_start": 33, "y_end": 150, "frames": [
        (98, 76), (265, 77), (439, 76), (607, 76), (773, 78), (944, 77), (1111, 78), (1278, 77)
    ]},
    {"y_start": 167, "y_end": 286, "frames": [
        (96, 76), (266, 76), (441, 76), (606, 76), (775, 76), (945, 77), (1111, 77), (1278, 76)
    ]},
    {"y_start": 305, "y_end": 418, "frames": [
        (100, 72), (269, 73), (447, 72), (612, 72), (781, 72), (950, 72), (1116, 72), (1284, 71)
    ]},
    {"y_start": 444, "y_end": 574, "frames": [
        (92, 84), (242, 111), (423, 92), (589, 95), (747, 122), (925, 118), (1104, 103), (1263, 110)
    ]},
    {"y_start": 604, "y_end": 755, "frames": [
        (87, 89), (219, 132), (396, 124), (564, 125), (759, 97), (926, 119), (1092, 118), (1248, 118)
    ]},
    {"y_start": 804, "y_end": 949, "frames": [
        (76, 87), (249, 83), (416, 81), (579, 84), (746, 84), (895, 127), (1063, 89), (1203, 89), (1354, 95)
    ]},
]

frame_names = [
    ["front_01","front_02","front_03","front_04","front_05","front_06","front_07","front_08"],
    ["back_01","back_02","back_03","back_04","back_05","back_06","back_07","back_08"],
    ["side_left_01","side_left_02","side_left_03","side_left_04","side_left_05","side_left_06","side_left_07","side_left_08"],
    ["hoe_01","hoe_02","hoe_03","hoe_04","hoe_05","hoe_06","hoe_07","hoe_08"],
    ["watering_01","watering_02","watering_03","watering_04","watering_05","watering_06","watering_07","watering_08"],
    ["hold_pumpkin_01","hold_crop_01","hold_flower_01","hold_fish_01","hold_chicken_01","emote_cheer_01","emote_heart_01","emote_surprised_01","emote_sleepy_01"],
]

directions = ["front", "back", "left", "mixed", "mixed", "front"]
action_zh_list = ["正面待机/行走", "背面待机/行走", "左侧待机/行走", "锄地/工具动作", "浇水动作", "特殊动作"]
special_action_zh = {
    "hold_pumpkin_01": "手持南瓜", "hold_crop_01": "手持作物",
    "hold_flower_01": "手持花朵", "hold_fish_01": "手持鱼",
    "hold_chicken_01": "手持小鸡", "emote_cheer_01": "欢呼",
    "emote_heart_01": "爱心", "emote_surprised_01": "惊讶",
    "emote_sleepy_01": "困倦",
}

frames_dict = {}
idx = 1
for row_idx, row_info in enumerate(rows_data):
    y = row_info["y_start"]
    h = row_info["y_end"] - row_info["y_start"] + 1
    names = frame_names[row_idx]
    for col_idx, (x, fw) in enumerate(row_info["frames"]):
        name = names[col_idx]
        action_zh = special_action_zh.get(name, action_zh_list[row_idx])
        frames_dict[name] = {
            "index": idx,
            "action_zh": action_zh,
            "row": row_idx + 1,
            "col": col_idx + 1,
            "direction": directions[row_idx],
            "bbox": {"x": x, "y": y, "w": fw, "h": h},
        }
        idx += 1

config = {
    "source_image": {
        "file_name": "xiami.png",
        "width": 1536,
        "height": 1024,
    },
    "coordinate_system": {
        "origin": "top_left",
        "unit": "pixel",
        "bbox_format": "x, y, w, h",
    },
    "notes": [
        "此 JSON 坐标基于 1536x1024 尺寸的 xiami.png。",
        "背景为棋盘格（非透明），裁切时需去除近白/浅灰像素。",
        "右侧待机/行走没有独立帧，使用 left 帧加 flip_x=true。",
    ],
    "animations": {
        "idle_front": {"name_zh": "正面待机", "direction": "front", "fps": 8, "loop": False, "frames": ["front_01"]},
        "walk_front": {"name_zh": "正面行走", "direction": "front", "fps": 8, "loop": True, "frames": [f"front_{i:02d}" for i in range(1, 9)]},
        "idle_back": {"name_zh": "背面待机", "direction": "back", "fps": 8, "loop": False, "frames": ["back_01"]},
        "walk_back": {"name_zh": "背面行走", "direction": "back", "fps": 8, "loop": True, "frames": [f"back_{i:02d}" for i in range(1, 9)]},
        "idle_left": {"name_zh": "左侧待机", "direction": "left", "fps": 8, "loop": False, "frames": ["side_left_01"]},
        "walk_left": {"name_zh": "左侧行走", "direction": "left", "fps": 8, "loop": True, "frames": [f"side_left_{i:02d}" for i in range(1, 9)]},
        "idle_right": {"name_zh": "右侧待机", "direction": "right", "fps": 8, "loop": False, "frames": ["side_left_01"], "transform": {"flip_x": True}, "note": "水平翻转 left 帧。"},
        "walk_right": {"name_zh": "右侧行走", "direction": "right", "fps": 8, "loop": True, "frames": [f"side_left_{i:02d}" for i in range(1, 9)], "transform": {"flip_x": True}, "note": "水平翻转 left 帧。"},
        "hoe_action": {"name_zh": "锄地/工具动作", "direction": "mixed", "fps": 10, "loop": False, "frames": [f"hoe_{i:02d}" for i in range(1, 9)]},
        "watering_can": {"name_zh": "浇水动作", "direction": "mixed", "fps": 10, "loop": False, "frames": [f"watering_{i:02d}" for i in range(1, 9)]},
        "hold_pumpkin": {"name_zh": "手持南瓜", "direction": "front", "fps": 8, "loop": False, "frames": ["hold_pumpkin_01"]},
        "hold_crop": {"name_zh": "手持作物", "direction": "front", "fps": 8, "loop": False, "frames": ["hold_crop_01"]},
        "hold_flower": {"name_zh": "手持花朵", "direction": "front", "fps": 8, "loop": False, "frames": ["hold_flower_01"]},
        "hold_fish": {"name_zh": "手持鱼", "direction": "front", "fps": 8, "loop": False, "frames": ["hold_fish_01"]},
        "hold_chicken": {"name_zh": "手持小鸡", "direction": "front", "fps": 8, "loop": False, "frames": ["hold_chicken_01"]},
        "emote_cheer": {"name_zh": "欢呼", "direction": "front", "fps": 8, "loop": False, "frames": ["emote_cheer_01"]},
        "emote_heart": {"name_zh": "爱心", "direction": "front", "fps": 8, "loop": False, "frames": ["emote_heart_01"]},
        "emote_surprised": {"name_zh": "惊讶", "direction": "front", "fps": 8, "loop": False, "frames": ["emote_surprised_01"]},
        "emote_sleepy": {"name_zh": "困倦", "direction": "front", "fps": 8, "loop": False, "frames": ["emote_sleepy_01"]},
    },
    "frames": frames_dict,
}

with open(OUT_PATH, "w", encoding="utf-8") as f:
    json.dump(config, f, ensure_ascii=False, indent=2)

print(f"Done: {len(frames_dict)} frames written to {OUT_PATH}")
