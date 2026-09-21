# AphelionDMM artwork

The dog astronaut icon was created by **Vinylspiders** for Meridian's [wiki](https://meridian-wiki.a13.info/wiki/Main_Page) and [website](https://meridian.a13.info).

The project owner supplied this copy and explicitly approved its use as the AphelionDMM icon for the `v.a.1` release on September 21, 2026. Use in this project is recorded under that authorization. No separate license for independent reuse of the artwork was supplied; this record does not assert that Vinylspiders released the artwork under the source code's GPL license.

## Source and transformations

- Original: `docs/branding/apheliondmm-source.webp`, 330 × 330 pixels.
- Original SHA-256: `027b6cb4faa2cf81bef9fee2e503383138e5853e09d10551ef2ede43faf9c936`.
- PNG: `internal/rsc/png/editor_icon.png`, a lossless conversion of the decoded source pixels, with no redraw, crop, or background removal.
- ICO: `internal/rsc/icon.ico`, containing 16, 24, 32, 48, 64, 128, and 256 pixel variants.
- Generator: Python with Pillow 12.3.0. Run the following from the repository root to reproduce the generated files:

```python
from PIL import Image
with Image.open('docs/branding/apheliondmm-source.webp') as source:
    icon = source.convert('RGBA')
    icon.save('internal/rsc/png/editor_icon.png')
    icon.save('internal/rsc/icon.ico', sizes=[(16,16),(24,24),(32,32),(48,48),(64,64),(128,128),(256,256)])
```

Verification: decode the PNG and source as RGBA and compare their pixel bytes; inspect the ICO sizes with `Image.open('internal/rsc/icon.ico').ico.sizes()`. The PNG dimensions must match `rsc.EditorIcon`'s texture bounds.

The inherited StrongDMM icon by Clément “Topy” remains in Git history. Existing historical StrongDMM documentation images and the editor's independent toolbar icon font are retained with their attribution.
