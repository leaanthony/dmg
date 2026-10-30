# dmg

A Go library for creating styled macOS DMG disk images with custom Finder window
appearance — background image, icon positions, window geometry, and volume/file
icons.

It ships **two backends**:

- a **pure-Go backend** (`hfsplus` + `udif`) that needs no macOS tools and works
  when cross-compiled to **any platform** — so you can build a macOS `.dmg` from
  a Linux or Windows CI runner; and
- a **macOS `hdiutil` backend** for the widest format/filesystem support on macOS.

By default it picks `hdiutil` on macOS and the pure-Go backend everywhere else.

## Features

- Styled Finder window: background image, solid-color background, window
  geometry, and per-icon positions
- `/Applications` symlink for drag-to-install
- Volume icons (shown when the DMG is mounted) and DMG file icons (shown on the
  `.dmg` in Finder)
- Accepts `.icns`, `.png`, `.jpg`, and macOS Tahoe `.icon` packages as icon input
- Pure-Go `.DS_Store` generation (no AppleScript / Finder scripting)
- Pure-Go HFS+ volume writer (`hfsplus`) and UDIF/`.dmg` container writer
  (`udif`) — no `hdiutil` dependency, cross-compiles to any OS
- Pure-Go ICNS generation from images (`icns` sub-package)
- Optional macOS `hdiutil` backend for LZFSE/LZMA/bzip2 formats and APFS

## Installation

```bash
go get github.com/leaanthony/dmg
```

Requires Go 1.24+.

## Usage

```go
package main

import "github.com/leaanthony/dmg/dmg"

func main() {
    opts := dmg.Options{
        VolumeName: "MyApp",
        OutputPath: "MyApp.dmg",
        Files: map[string]string{
            "MyApp.app": "/path/to/MyApp.app",
        },
        AddApplicationsSymlink: true,
        Window: dmg.WindowConfig{
            X: 200, Y: 200,
            Width: 540, Height: 380,
        },
        Icon: dmg.IconConfig{
            Size: 128, TextSize: 12, GridSpace: 100,
        },
        Background: &dmg.BackgroundConfig{
            File: "/path/to/background.png",
        },
        // Pin specific icons (others are auto-positioned).
        IconPositions: map[string]dmg.IconPosition{
            "MyApp.app":    {X: 150, Y: 180},
            "Applications": {X: 390, Y: 180},
        },
        VolumeIcon: "icon.png", // icon when the DMG is mounted
        FileIcon:   "icon.png", // icon of the .dmg file in Finder
    }

    if err := dmg.Build(opts); err != nil {
        panic(err)
    }
}
```

## Backends

`Options.Backend` selects how the disk image is created:

| Backend | Behaviour | Platforms |
|---------|-----------|-----------|
| `BackendAuto` (default) | `hdiutil` on macOS, pure-Go elsewhere | any |
| `BackendNative` | Pure-Go `hfsplus` + `udif` | any (cross-compiles) |
| `BackendHdiutil` | macOS `hdiutil` / `ditto` | macOS only |

The **native** backend produces a case-insensitive **HFS+** volume by default,
or a case-sensitive **HFSX** volume when `Filesystem` is `FSHFSX`, in a **UDZO
(zlib)** container. It supports files, nested folders, symlinks, the background
image, volume icon, `.DS_Store` styling, and `IconPositions`. Code signatures
on `.app` bundles are preserved (verified with `codesign --verify`).

To build a macOS DMG on Linux/Windows CI, just force the native backend:

```go
opts.Backend = dmg.BackendNative
if err := dmg.Build(opts); err != nil { /* ... */ }
```

Native-backend limitations (use `BackendHdiutil` on macOS if you need these):

- UDZO format only (no LZFSE/LZMA/bzip2)
- HFS+ and HFSX only (no APFS)
- Case-insensitive volumes currently require ASCII file names; non-ASCII names
  need a case-sensitive volume (a full Unicode fold table is planned)
- The icon on the `.dmg` file itself (`FileIcon`) is applied only on macOS

## Icons

Two distinct icon types:

| Icon Type | What It Is | Option |
|-----------|-----------|--------|
| **Volume Icon** | Shown when the DMG is mounted in Finder | `VolumeIcon` |
| **File Icon** | Shown on the `.dmg` file itself in Finder | `FileIcon` |

Both accept any of:

| Format | Extension | Notes |
|--------|-----------|-------|
| ICNS | `.icns` | Used directly |
| PNG | `.png` | Auto-converted to ICNS |
| JPEG | `.jpg`, `.jpeg` | Auto-converted to ICNS |
| Liquid Glass | `.icon` (directory) | macOS Tahoe format — layers composited and converted to ICNS |

The volume icon is set via `.VolumeIcon.icns` plus the `kHasCustomIcon` Finder
flag (encoded into the HFS+ catalog by the native backend, or set via
`com.apple.FinderInfo` by the hdiutil backend). The DMG file icon uses a resource
fork in the `com.apple.ResourceFork` extended attribute (macOS only).

## Sub-packages

These are usable on their own:

```go
import (
    "github.com/leaanthony/dmg/hfsplus" // pure-Go HFS+/HFSX volume writer
    "github.com/leaanthony/dmg/udif"    // pure-Go UDIF (.dmg) container writer
    "github.com/leaanthony/dmg/icns"    // pure-Go ICNS encoder / .icon reader
)
```

Build a `.dmg` directly from a file tree, with no macOS dependency:

```go
vol := &hfsplus.Volume{
    Name: "Install",
    Children: []*hfsplus.Entry{
        {Name: "MyApp.app", Kind: hfsplus.KindDir, Children: /* ... */},
        {Name: "Applications", Kind: hfsplus.KindSymlink, Target: "/Applications"},
    },
}
img, _ := hfsplus.Build(vol)          // raw HFS+ volume image
f, _ := os.Create("Install.dmg")
_ = udif.Build(f, img, "whole disk (Apple_HFS : 0)") // wrap as a .dmg
```

The `icns` sub-package creates Apple Icon Image files from Go images:

```go
err := icns.EncodeFile("output.icns", "source.png") // PNG/JPEG -> ICNS
data, err := icns.IcnsFromImage(img)                // image.Image -> ICNS bytes
img, err := icns.ReadDotIcon("AppIcon.icon")        // read a Tahoe .icon package
```

The encoder generates standard ICNS entries for all sizes from 16×16 to
1024×1024 using a box-filter downscale.

## Examples

Runnable example programs live in [`examples/`](./examples):

```sh
# Pure-Go DMG from a directory (works on Linux/Windows/macOS):
go run ./examples/purego-dmg ./MyApp.app MyApp.dmg MyApp

# Styled DMG via the high-level API (macOS):
go run ./examples/styled-dmg ./MyApp.app MyApp.dmg

# PNG -> ICNS:
go run ./examples/png-to-icns icon.png AppIcon.icns
```

Package-level `Example` functions (visible on pkg.go.dev) demonstrate the API
for each package.

## API Reference

### Options

| Field | Type | Description |
|-------|------|-------------|
| `VolumeName` | `string` | Volume name displayed in Finder title bar |
| `OutputPath` | `string` | Output path for the final DMG file |
| `Files` | `map[string]string` | Files to include (destination name → source path) |
| `AddApplicationsSymlink` | `bool` | Create `/Applications` symlink for drag-to-install |
| `IconPositions` | `map[string]IconPosition` | Pin Finder icon locations by name (others auto-positioned) |
| `Window` | `WindowConfig` | Finder window position and size |
| `Icon` | `IconConfig` | Icon appearance settings |
| `Background` | `*BackgroundConfig` | Window background (nil for default white) |
| `Format` | `Format` | Compression format (default: `FormatUDZO`) |
| `Filesystem` | `Filesystem` | `FSHFSPlus` (default), `FSHFSX` (case-sensitive), or `FSAPFS` (hdiutil backend) |
| `SizeHint` | `int` | Writable DMG size in MB (0 = auto; hdiutil backend) |
| `VolumeIcon` | `string` | Path to icon file for the mounted volume |
| `FileIcon` | `string` | Path to icon file for the .dmg file in Finder |
| `Backend` | `Backend` | `BackendAuto` (default), `BackendNative`, or `BackendHdiutil` |

### WindowConfig

| Field | Type | Description |
|-------|------|-------------|
| `X`, `Y` | `int` | Window position on screen |
| `Width`, `Height` | `int` | Window size in pixels |

### IconConfig

| Field | Type | Description |
|-------|------|-------------|
| `Size` | `int` | Icon size in pixels (default: 128) |
| `TextSize` | `float64` | Label text size in points (default: 12) |
| `GridSpace` | `float64` | Grid spacing between icons (default: 100, max: 100) |

> **Note:** macOS Finder silently rejects all icon view settings if `GridSpace`
> exceeds 100.

### IconPosition

| Field | Type | Description |
|-------|------|-------------|
| `X`, `Y` | `int` | Icon centre point, in window pixels |

### BackgroundConfig

| Field | Type | Description |
|-------|------|-------------|
| `File` | `string` | Path to background image (PNG, JPEG, etc.) |
| `Color` | `*ColorRGB` | Solid color background (used when `File` is empty; components 0.0–1.0) |

When neither `File` nor `Color` is set, the DMG uses a default white background.

### Background Image Helpers

```go
// Solid color background
dmg.SolidBackground(width, height int, c color.Color, path string) error

// Gradient background
dmg.GradientBackground(width, height int, from, to color.Color,
    direction dmg.GradientDirection, path string) error

// Installer-style background with icon wells and arrow
dmg.InstallerBackground(width, height int, from, to color.Color,
    slots []dmg.IconSlot, iconSize int, path string) error
```

## Compression Formats

The native backend produces `FormatUDZO` only. The hdiutil backend (macOS)
supports all of:

| Format | Constant | Notes |
|--------|----------|-------|
| zlib | `FormatUDZO` | Maximum compatibility (default; pure-Go capable) |
| LZFSE | `FormatULFO` | Good balance, macOS 10.11+ (hdiutil) |
| LZMA | `FormatULMO` | Smallest output, slower (hdiutil) |
| bzip2 | `FormatUDBZ` | Deprecated (hdiutil) |
| Uncompressed | `FormatUDRW` | For development (hdiutil) |

## CLI Tool

The `mkdmg` command provides a quick way to create DMGs:

```bash
go install github.com/leaanthony/dmg/cmd/mkdmg@latest

mkdmg -volname MyApp -app /path/to/MyApp.app -output MyApp.dmg \
  -volume-icon icon.png \
  -file-icon icon.png \
  -background bg.png
```

Or use a JSON config file:

```json
{
  "volume_name": "MyApp",
  "output": "MyApp.dmg",
  "files": { "MyApp.app": "/path/to/MyApp.app" },
  "applications_symlink": true,
  "volume_icon": "icon.png",
  "file_icon": "icon.png",
  "background": { "file": "background.png" },
  "window": { "x": 200, "y": 200, "width": 540, "height": 380 },
  "icon": { "size": 128, "text_size": 12, "grid_space": 100 }
}
```

```bash
mkdmg -config dmg.json
```

### HFSX and CLI backend selection

Use HFSX for non-ASCII filenames with the native backend:

```sh
mkdmg -backend native -fs HFSX -volname "Example" -app "Example.app" -output "Example.dmg"
```

Both backends support HFSX. Unsupported filesystem names return an error.
JSON configuration also accepts `"backend": "native"` (`auto` and `hdiutil`
are supported) and explicit positions, for example:

```json
{
  "volume_name": "Example",
  "output": "Example.dmg",
  "filesystem": "HFSX",
  "backend": "native",
  "files": {"Example.app": "Example.app"},
  "applications_symlink": true,
  "icon_positions": {
    "Example.app": {"x": 170, "y": 160},
    "Applications": {"x": 470, "y": 160}
  }
}
```

## Platform support

| Task | Pure-Go (native) backend | hdiutil backend |
|------|--------------------------|-----------------|
| Create a styled `.dmg` | any OS (cross-compiles) | macOS only |
| Output format | UDZO / HFS+ & HFSX | all formats, HFS+, HFSX & APFS |
| `.DS_Store` + ICNS generation | pure Go | pure Go |

The pure-Go backend has no cgo and no external tool dependencies.

## How It Works

Instead of AppleScript or Finder automation (slow and fragile), this library
generates `.DS_Store` files directly in pure Go. The `.DS_Store` binary format
uses a buddy allocator with a B-tree index, holding records that control
Finder's window appearance: `bwsp` (window position/size), `icvp` (icon view
settings), `icvl` (view type), `vSrn` (version marker), and `Iloc` (individual
icon positions).

The native backend additionally writes the HFS+ volume and the UDIF (`.dmg`)
container itself in pure Go (clean-room implementations from Apple's TN1150 and
the public UDIF format), so no `hdiutil` is required and the whole pipeline
cross-compiles to any platform.

## Contributing

Issues are not accepted for this project. Fixes are welcome as pull requests.

If you encounter a bug but cannot provide a fix, please open a pull request
that adds a focused failing test demonstrating the problem. This gives the
issue a reproducible form and makes it straightforward for a contributor to
implement and verify the fix.

## License

MIT
