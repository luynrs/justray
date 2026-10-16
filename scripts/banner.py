import subprocess
import sys
from pathlib import Path

import pyfiglet
from PIL import Image, ImageDraw, ImageFont

STOPS = [
    (0.00, (137, 180, 250)),
    (0.28, (180, 190, 254)),
    (0.55, (203, 166, 247)),
    (0.80, (245, 194, 231)),
    (1.00, (242, 205, 205)),
]

BANNERS = [
    ("J", ".github/assets/icon.png"),
    ("JustRay", ".github/assets/logotype.png"),
    ("> Join us", ".github/assets/contributing.png"),
    ("> Privacy", ".github/assets/privacy.png"),
]


def color(t):
    for (p1, c1), (p2, c2) in zip(STOPS, STOPS[1:]):
        if p1 <= t <= p2:
            f = (t - p1) / (p2 - p1)
            return tuple(int(a + (b - a) * f) for a, b in zip(c1, c2))
    return STOPS[-1][1]


def render(text, path):
    lines = [x.rstrip() for x in pyfiglet.figlet_format(text, font="small_slant").splitlines() if x.strip()]
    if not lines:
        return

    try:
        font = subprocess.check_output(
            ["fc-match", "-f", "%{file}", "JetBrains Mono"], text=True
        ).strip()
    except Exception:
        font = "JetBrains Mono"

    font = ImageFont.truetype(font, 25)
    tall = len(lines) >= 5
    width, height = int(max(map(len, lines)) * 15) + 30, 145

    mask = Image.new("L", (width, height))
    draw = ImageDraw.Draw(mask)

    for i, line in enumerate(lines):
        draw.text(
            (15, (-25 if tall else 6) + i * (35.5 if tall else 34)),
            line,
            font=font,
            fill=255,
        )

    image = Image.new("RGBA", (width, height))
    for y in range(height):
        image.paste((*color(y / (height - 1)), 255), (0, y, width, y + 1))

    image.putalpha(mask)

    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    image.save(path)
    print(f"Generated {path.name}: {width}x{height} ({text})")


if __name__ == "__main__":
    if len(sys.argv) > 1:
        render(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else ".github/assets/banner.png")
    else:
        for text, path in BANNERS:
            render(text, path)
