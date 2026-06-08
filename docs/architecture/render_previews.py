"""Render .excalidraw JSON files to simplified PNG previews using Pillow."""
import json, sys, os
from PIL import Image, ImageDraw, ImageFont

def hex_to_rgb(h):
    h = h.lstrip('#')
    return tuple(int(h[i:i+2], 16) for i in (0, 2, 4))

def render_excalidraw(input_path, output_path, scale=1.5):
    with open(input_path, 'r', encoding='utf-8') as f:
        data = json.load(f)

    elements = [e for e in data['elements'] if not e.get('isDeleted', False)]
    bg_color = data.get('appState', {}).get('viewBackgroundColor', '#ffffff')

    # Calculate bounds
    min_x = min_y = float('inf')
    max_x = max_y = float('-inf')
    for e in elements:
        x, y = e.get('x', 0), e.get('y', 0)
        w, h = e.get('width', 0), e.get('height', 0)
        min_x = min(min_x, x)
        min_y = min(min_y, y)
        max_x = max(max_x, x + w)
        max_y = max(max_y, y + h)

    padding = 40
    img_w = int((max_x - min_x + padding * 2) * scale)
    img_h = int((max_y - min_y + padding * 2) * scale)

    img = Image.new('RGB', (img_w, img_h), hex_to_rgb(bg_color))
    draw = ImageDraw.Draw(img)

    off_x = -min_x + padding
    off_y = -min_y + padding

    try:
        font = ImageFont.truetype("arial.ttf", int(11 * scale))
        font_small = ImageFont.truetype("arial.ttf", int(9 * scale))
    except:
        font = ImageFont.load_default()
        font_small = font

    # Draw rectangles and ellipses first
    for e in elements:
        t = e.get('type')
        x = int((e.get('x', 0) + off_x) * scale)
        y = int((e.get('y', 0) + off_y) * scale)
        w = int(e.get('width', 0) * scale)
        h = int(e.get('height', 0) * scale)
        opacity = e.get('opacity', 100)

        stroke = e.get('strokeColor', '#000000')
        bg = e.get('backgroundColor', 'transparent')
        stroke_w = max(1, int(e.get('strokeWidth', 1) * scale * 0.7))

        if opacity < 50 and t == 'rectangle':
            # Grouping boxes - draw with reduced opacity
            if bg != 'transparent':
                fill_rgb = hex_to_rgb(bg)
                fill_rgba = fill_rgb + (40,)
                overlay = Image.new('RGBA', (w, h), fill_rgba)
                img.paste(Image.blend(
                    img.crop((x, y, x+w, y+h)).convert('RGBA'),
                    overlay, 0.3
                ).convert('RGB'), (x, y))
            style = e.get('strokeStyle', 'solid')
            draw.rectangle([x, y, x+w, y+h], outline=hex_to_rgb(stroke), width=stroke_w)
        elif t == 'rectangle' and opacity >= 50:
            fill = hex_to_rgb(bg) if bg != 'transparent' else hex_to_rgb(bg_color)
            draw.rounded_rectangle([x, y, x+w, y+h], radius=int(6*scale),
                                   fill=fill, outline=hex_to_rgb(stroke), width=stroke_w)
        elif t == 'ellipse':
            fill = hex_to_rgb(bg) if bg != 'transparent' else hex_to_rgb(bg_color)
            draw.ellipse([x, y, x+w, y+h], fill=fill, outline=hex_to_rgb(stroke), width=stroke_w)

    # Draw arrows
    for e in elements:
        if e.get('type') != 'arrow':
            continue
        x = (e.get('x', 0) + off_x) * scale
        y = (e.get('y', 0) + off_y) * scale
        points = e.get('points', [])
        stroke = hex_to_rgb(e.get('strokeColor', '#000000'))
        stroke_w = max(1, int(e.get('strokeWidth', 1) * scale * 0.7))

        if len(points) >= 2:
            coords = [(int(x + p[0]*scale), int(y + p[1]*scale)) for p in points]
            for i in range(len(coords)-1):
                draw.line([coords[i], coords[i+1]], fill=stroke, width=stroke_w)
            # arrowhead
            end = coords[-1]
            prev = coords[-2]
            draw.polygon([end,
                         (end[0]-int(6*scale), end[1]-int(4*scale)),
                         (end[0]-int(6*scale), end[1]+int(4*scale))],
                        fill=stroke)

    # Draw text
    for e in elements:
        if e.get('type') != 'text':
            continue
        x = int((e.get('x', 0) + off_x) * scale)
        y = int((e.get('y', 0) + off_y) * scale)
        text = e.get('text', '')
        color = hex_to_rgb(e.get('strokeColor', '#000000'))
        fsize = e.get('fontSize', 12)
        opacity = e.get('opacity', 100)

        if opacity < 40:
            continue

        try:
            f = ImageFont.truetype("arial.ttf", max(8, int(fsize * scale * 0.75)))
        except:
            f = font_small

        # Multi-line text centering
        lines = text.split('\n')
        line_h = int(fsize * scale * 0.85)
        total_h = line_h * len(lines)
        w = e.get('width', 200)

        for i, line in enumerate(lines):
            bbox = draw.textbbox((0, 0), line, font=f)
            tw = bbox[2] - bbox[0]
            tx = x + int(w * scale / 2) - tw // 2 if e.get('textAlign') == 'center' else x
            ty = y + i * line_h
            draw.text((tx, ty), line, fill=color, font=f)

    img.save(output_path, 'PNG', quality=95)
    print(f'OK: {output_path} ({img_w}x{img_h})')

if __name__ == '__main__':
    base = r'D:\SmartNPC\docs\architecture'
    files = [
        'system-architecture',
        'request-lifecycle',
        'mcp-tools-map',
        'hermes-profile-system',
        'smapi-mod-internals',
    ]
    for name in files:
        inp = os.path.join(base, f'{name}.excalidraw')
        out = os.path.join(base, f'{name}.png')
        if os.path.exists(inp):
            render_excalidraw(inp, out)
        else:
            print(f'SKIP: {inp} not found')
