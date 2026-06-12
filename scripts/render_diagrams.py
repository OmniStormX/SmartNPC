"""
render_diagrams.py — render code blocks in docs/useful-skills.md to PNG images.

Supports: infographic (Node SSR + Playwright), plantuml (public server),
          dot (local Graphviz), inline HTML (Playwright).
"""
import re, os, base64, zlib, subprocess, urllib.request, urllib.error
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent
MD = BASE / "docs" / "useful-skills.md"
IMG = BASE / "docs" / "images"
IMG.mkdir(parents=True, exist_ok=True)

def slug(t, n=50):
    s = re.sub(r'[^a-zA-Z0-9_一-鿿一-鿿-]', '-', str(t).strip())
    return re.sub(r'-+', '-', s).strip('-')[:n] or "diagram"

def read(p):
    with open(p, encoding="utf-8") as f: return f.read()
def write(p, c):
    with open(p, "w", encoding="utf-8", newline="\n") as f: f.write(c)
def writeb(p, d):
    p.parent.mkdir(parents=True, exist_ok=True)
    with open(p, "wb") as f: f.write(d)

# ── Playwright helpers ──

def svg2png(svg_str, out_png):
    """Render SVG string to PNG via Playwright screenshot."""
    html = f"""<!DOCTYPE html><html><head><meta charset="utf-8"><style>
body{{margin:0;padding:10px;background:white;display:flex;justify-content:center;align-items:flex-start}}</style></head>
<body>{svg_str}</body></html>"""
    tmp = out_png.with_suffix(".html")
    write(tmp, html)
    from playwright.sync_api import sync_playwright
    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        ctx = browser.new_context(viewport={"width": 1400, "height": 1200}, device_scale_factor=2)
        page = ctx.new_page()
        page.goto(f"file:///{tmp.as_posix()}")
        page.wait_for_timeout(800)
        el = page.query_selector("svg")
        if el:
            box = el.bounding_box()
            if box:
                page.set_viewport_size({"width": int(box["width"]) + 40, "height": int(box["height"]) + 40})
                page.wait_for_timeout(200)
                el.screenshot(path=str(out_png))
            else:
                el.screenshot(path=str(out_png))
        else:
            page.screenshot(path=str(out_png), full_page=True)
        ctx.close()
        browser.close()
    os.unlink(tmp)
    return out_png.exists()

def html2png(html, out_png):
    """Render full HTML to PNG."""
    tmp = out_png.with_suffix(".html")
    write(tmp, html)
    from playwright.sync_api import sync_playwright
    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        ctx = browser.new_context(viewport={"width": 1280, "height": 1024}, device_scale_factor=2)
        page = ctx.new_page()
        page.goto(f"file:///{tmp.as_posix()}")
        page.wait_for_timeout(1200)
        container = page.query_selector('[style*="1200px"]') or page.query_selector("body")
        if container:
            container.screenshot(path=str(out_png))
        else:
            page.screenshot(path=str(out_png), full_page=True)
        ctx.close()
        browser.close()
    os.unlink(tmp)
    return out_png.exists()

# ── renderers ──

def render_infographic(syntax, name):
    """infographic → SVG via Node SSR → PNG via Playwright."""
    esc = syntax.replace("\\", "\\\\").replace("`", "\\`").replace("$", "\\$")
    code = f"""(async()=>{{
  const {{renderToString}}=require('@antv/infographic/ssr');
  const s=await renderToString(`{esc}`,{{format:'svg',theme:'light'}});
  process.stdout.write(s);
}})();"""
    r = subprocess.run(["node", "-e", code], capture_output=True, timeout=30, cwd=str(BASE))
    if r.returncode != 0:
        print(f"  SSR fail: {r.stderr.decode('utf-8','replace')[:200]}")
        return None
    svg = r.stdout
    if not svg or b"<svg" not in svg[:500]:
        print(f"  Not SVG")
        return None
    out = IMG / f"infographic-{name}.png"
    svg2png(svg.decode("utf-8","replace"), out)
    return out

def render_plantuml(code, name):
    """PlantUML → PNG via public server."""
    z = zlib.compress(code.encode("utf-8"), 9)
    enc = base64.b64encode(z).decode().replace("+","-").replace("/","_")
    url = f"http://www.plantuml.com/plantuml/png/{enc}"
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=20) as resp:
            data = resp.read()
        out = IMG / f"plantuml-{name}.png"
        writeb(out, data)
        return out
    except Exception as e:
        print(f"  PlantUML fail: {e}")
        return None

def render_dot(code, name):
    """DOT → PNG via local Graphviz."""
    dot = r"C:\Program Files\Graphviz\bin\dot.exe"
    if not os.path.exists(dot):
        print(f"  No Graphviz")
        return None
    r = subprocess.run([dot, "-Tpng", "-Gdpi=150"], input=code.encode(),
                       capture_output=True, timeout=30)
    if r.returncode != 0:
        print(f"  dot fail: {r.stderr.decode('utf-8','replace')[:200]}")
        return None
    out = IMG / f"dot-{name}.png"
    writeb(out, r.stdout)
    return out

def render_html(html, name):
    """HTML block → PNG."""
    full = f"""<!DOCTYPE html><html><head><meta charset="utf-8"><style>
body{{margin:0;padding:20px;background:#fafbfc;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif}}</style></head>
<body>{html}</body></html>"""
    out = IMG / f"html-{name}.png"
    html2png(full, out)
    return out

# ── extract blocks ──

def extract(md):
    fence = re.compile(r'^```(\w*)\s*\n(.*?)^```\s*$', re.MULTILINE | re.DOTALL)
    for m in fence.finditer(md):
        lang = m.group(1).strip()
        if lang in ("infographic","plantuml","dot"):
            content = m.group(2).strip()
            # title from preceding heading
            before = md[max(0,m.start()-600):m.start()]
            titles = re.findall(r'#{2,4}\s+(.+?)$', before, re.MULTILINE)
            hint = titles[-1].strip() if titles else content.split("\n")[0][:50]
            yield (m.group(0), lang, content, hint, m.start(), m.end())

    # Large inline HTML architecture block
    html_p = re.compile(
        r'(<div\s+style="width:\s*1200px.*?</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*)',
        re.DOTALL)
    for m in html_p.finditer(md):
        yield (m.group(0), "html", m.group(0).strip(), "SmartNPC System Architecture",
               m.start(), m.end())

# ── main ──

def main():
    md = read(MD)

    # Backup
    write(Path(str(MD) + ".bak"), md)

    blocks = list(extract(md))
    print(f"Found {len(blocks)} blocks\n")

    renders = {"infographic": render_infographic, "plantuml": render_plantuml,
               "dot": render_dot, "html": render_html}

    # Render first, then replace
    batch = []
    for i, (orig, lang, content, hint, s, e) in enumerate(blocks):
        name = slug(hint)
        png = IMG / f"{lang}-{name}.png"
        print(f"[{i+1}/{len(blocks)}] {lang}: {hint[:65]}")

        if png.exists() and png.stat().st_size > 100:
            print(f"  skip (exists)")
            batch.append((s, e, png.name, hint, lang))
            continue

        try:
            result = renders[lang](content, name)
            if result and result.exists() and result.stat().st_size > 100:
                kb = result.stat().st_size/1024
                print(f"  OK {result.name} ({kb:.1f}KB)")
                batch.append((s, e, result.name, hint, lang))
            else:
                print(f"  no output, keeping source")
        except Exception as ex:
            print(f"  FAIL: {ex}")

    if not batch:
        print("\nNo new renders (all existing or all failed)")
        return

    # Apply replacements right→left
    batch.sort(key=lambda x: x[0], reverse=True)
    for s, e, png_name, hint, lang in batch:
        before = md[:s].rstrip()
        # Inside <details>?
        dm = list(re.finditer(r'<details>\s*<summary>(.*?)</summary>', before, re.DOTALL))
        img = f"\n![{hint}](images/{png_name})\n"

        if dm:
            # Replace whole <details>...block...</details>
            ds = dm[-1].start()
            after = md[e:]
            em = re.search(r'\s*</details>', after)
            if em:
                fe = e + em.end()
                md = md[:ds] + img + md[fe:]
            else:
                md = md[:ds] + re.sub(r'^.*?\n', '', md[ds:s], count=1) + img + md[e:]
        else:
            md = md[:s] + img + md[e:]

    md = re.sub(r'\n{3,}', '\n\n', md)
    md = re.sub(r'<details>\s*</details>\n*', '', md)
    # Fix empty <details> left by nested replacements
    md = re.sub(r'\n+\s*<details>\s*\n', '\n', md)
    md = re.sub(r'\n\s*</details>\s*\n', '\n', md)

    write(MD, md)
    print(f"\nDone → {MD}")
    print(f"Images → {IMG}")

if __name__ == "__main__":
    main()