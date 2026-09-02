# Brand — tree logo concepts

Tree logos in the visual language of the [Better Place Forests mark by &Walsh](https://logosystem.co/logo/better-place-forests)
(geometric, bold, radiating rays, horizon band), colored from `frontend/app/globals.css`.

Open `preview.html` in a browser for the full side-by-side comparison, or see `preview.png`.

## Concepts

| File        | Concept       | Description                                                                        |
| ----------- | ------------- | ---------------------------------------------------------------------------------- |
| `htt-tiers` | HTT Tiers     | The tree built from the initials: H crown → T → T + trunk. Reads HTT top to bottom |
| `htt-roots` | HTT Roots     | Chevron pine with HTT block-letter roots below the band (tall 96×128 lockup)       |
| `pine`      | Chevron Pine  | Three stacked chevron tiers at one consistent angle + trunk + band                 |
| `rays`      | Radiant Tree  | Closest to the reference — five rays fan up from a horizon band, trunk below       |
| `blocks`    | Shift Blocks  | Canopy of stacked squares, like calendar shifts arranged as a tree                 |
| `disc`      | Radiant Crown | Round crown of five rays joined by an arc — deciduous counterpart to `rays`        |

The current pick in the app is **HTT Tiers, duet colorway** (`frontend/app/icon.svg`,
`frontend/components/Logo.tsx`).

## Files

- `symbols/<concept>.svg` — transparent symbol masters, painted with `currentColor`
  (recolor via CSS `color`). Canopy elements carry class `c`, trunk/ground class `t`
  for two-tone use.
- `tiles/<concept>-<colorway>.svg` — ready-to-use 96×96 app-icon tiles (rounded square,
  baked colors).
- `preview.html` / `preview.png` — the full option sheet.

## Colorways (from `globals.css`)

| Colorway | Background               | Symbol                             |
| -------- | ------------------------ | ---------------------------------- |
| `orange` | `--orange-500 #BF5700`   | white                              |
| `navy`   | `--navy-900 #001020`     | white                              |
| `paper`  | `#FAFAF7` (`--bg-color`) | `--navy-900 #001020`               |
| `duet`   | white                    | canopy `#BF5700` / trunk `#001020` |

All concepts verified legible down to 16 px (favicon size).
