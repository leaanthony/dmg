package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leaanthony/dmg/dmg"
)

func main() {
	// Basic options
	volName := flag.String("volname", "", "Volume name displayed in Finder title bar (required)")
	output := flag.String("output", "", "Output DMG file path (required)")
	format := flag.String("format", "UDZO", "DMG format: UDZO (zlib), ULFO (lzfse), ULMO (lzma), UDBZ (bzip2), UDRW (raw)")
	fs := flag.String("fs", "HFS+", "Filesystem: HFS+, HFSX (case-sensitive), or APFS")

	backend := flag.String("backend", "auto", "Backend: auto, native, or hdiutil")

	// Files
	app := flag.String("app", "", "Path to .app bundle to include")
	addApps := flag.Bool("applications-symlink", true, "Add /Applications symlink for drag-to-install")

	// Window
	winWidth := flag.Int("width", 540, "Finder window width")
	winHeight := flag.Int("height", 380, "Finder window height")
	winX := flag.Int("x", 100, "Finder window X position")
	winY := flag.Int("y", 100, "Finder window Y position")

	// Icons
	iconSize := flag.Int("icon-size", 128, "Icon size in pixels")
	textSize := flag.Float64("text-size", 12, "Icon label text size in points")
	gridSpace := flag.Float64("grid-space", 100, "Grid spacing between icons")

	// Background
	bgImage := flag.String("background", "", "Path to background image file")
	bgColor := flag.String("bg-color", "", "Background color as R,G,B floats (e.g. 0.9,0.9,0.9)")

	// Volume icon
	volumeIcon := flag.String("volume-icon", "", "Path to icon file (.icns, .png, .jpg) for mounted volume")

	// File icon (icon of the .dmg file itself in Finder)
	fileIcon := flag.String("file-icon", "", "Path to icon file (.icns, .png, .jpg) for the DMG file in Finder")

	// Size
	sizeHint := flag.Int("size", 0, "DMG size hint in MB (0 = auto-calculate)")

	// JSON config
	configFile := flag.String("config", "", "JSON config file (overrides other flags)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "mkdmg - Create styled macOS DMG disk images\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  mkdmg -app /path/to/MyApp.app -output MyApp.dmg\n")
		fmt.Fprintf(os.Stderr, "  mkdmg -config dmg.json\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	var opts dmg.Options

	if *configFile != "" {
		var err error
		opts, err = loadConfig(*configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		opts = buildOptionsFromFlags(
			*volName, *output, *format, *fs,
			*app, *addApps,
			*winX, *winY, *winWidth, *winHeight,
			*iconSize, *textSize, *gridSpace,
			*bgImage, *bgColor,
			*volumeIcon, *fileIcon, *sizeHint,
		)
	}

	if *configFile == "" {
		var err error
		opts.Backend, err = parseBackend(*backend)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	// Validate minimum required fields
	if opts.OutputPath == "" {
		fmt.Fprintf(os.Stderr, "error: -output is required\n")
		flag.Usage()
		os.Exit(1)
	}
	if len(opts.Files) == 0 {
		fmt.Fprintf(os.Stderr, "error: at least one file is required (-app or -config)\n")
		flag.Usage()
		os.Exit(1)
	}

	if opts.VolumeName == "" {
		// Derive from first file
		for name := range opts.Files {
			opts.VolumeName = strings.TrimSuffix(name, filepath.Ext(name))
			break
		}
	}

	fmt.Fprintf(os.Stderr, "Creating DMG: %s\n", opts.OutputPath)
	fmt.Fprintf(os.Stderr, "  Volume: %s\n", opts.VolumeName)
	fmt.Fprintf(os.Stderr, "  Format: %s\n", opts.Format)
	for name, src := range opts.Files {
		fmt.Fprintf(os.Stderr, "  File: %s -> %s\n", src, name)
	}

	if err := dmg.Build(opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	info, _ := os.Stat(opts.OutputPath)
	fmt.Fprintf(os.Stderr, "Done: %s (%.1f MB)\n", opts.OutputPath, float64(info.Size())/(1024*1024))
}

func buildOptionsFromFlags(
	volName, output, format, fs string,
	app string, addApps bool,
	winX, winY, winWidth, winHeight int,
	iconSize int, textSize, gridSpace float64,
	bgImage, bgColor string,
	volumeIcon, fileIcon string, sizeHint int,
) dmg.Options {
	opts := dmg.Options{
		VolumeName:             volName,
		OutputPath:             output,
		Files:                  make(map[string]string),
		AddApplicationsSymlink: addApps,
		Format:                 dmg.Format(format),
		Filesystem:             dmg.Filesystem(fs),
		Window: dmg.WindowConfig{
			X: winX, Y: winY, Width: winWidth, Height: winHeight,
		},
		Icon: dmg.IconConfig{
			Size: iconSize, TextSize: textSize, GridSpace: gridSpace,
		},
		VolumeIcon: volumeIcon,
		FileIcon:   fileIcon,
		SizeHint:   sizeHint,
	}

	if app != "" {
		name := filepath.Base(app)
		opts.Files[name] = app
	}

	if bgImage != "" {
		opts.Background = &dmg.BackgroundConfig{File: bgImage}
	} else if bgColor != "" {
		c, err := parseColor(bgColor)
		if err == nil {
			opts.Background = &dmg.BackgroundConfig{Color: c}
		}
	}

	return opts
}

func parseColor(s string) (*dmg.ColorRGB, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return nil, fmt.Errorf("color must be R,G,B (e.g. 0.9,0.9,0.9)")
	}
	var r, g, b float64
	if _, err := fmt.Sscanf(parts[0], "%f", &r); err != nil {
		return nil, err
	}
	if _, err := fmt.Sscanf(parts[1], "%f", &g); err != nil {
		return nil, err
	}
	if _, err := fmt.Sscanf(parts[2], "%f", &b); err != nil {
		return nil, err
	}
	return &dmg.ColorRGB{R: r, G: g, B: b}, nil
}

func parseBackend(s string) (dmg.Backend, error) {
	switch s {
	case "", "auto":
		return dmg.BackendAuto, nil
	case "native":
		return dmg.BackendNative, nil
	case "hdiutil":
		return dmg.BackendHdiutil, nil
	default:
		return dmg.BackendAuto, fmt.Errorf("unsupported backend %q", s)
	}
}

// JSON config structure
type jsonConfig struct {
	Backend                string                      `json:"backend"`
	IconPositions          map[string]dmg.IconPosition `json:"icon_positions"`
	VolumeName             string                      `json:"volume_name"`
	OutputPath             string                      `json:"output"`
	Format                 string                      `json:"format"`
	Filesystem             string                      `json:"filesystem"`
	Files                  map[string]string           `json:"files"`
	AddApplicationsSymlink *bool                       `json:"applications_symlink"`
	Background             *jsonBackground             `json:"background"`
	VolumeIcon             string                      `json:"volume_icon"`
	FileIcon               string                      `json:"file_icon"`
	SizeHint               int                         `json:"size_hint"`
	Window                 *jsonWindow                 `json:"window"`
	Icon                   *jsonIcon                   `json:"icon"`
}

type jsonBackground struct {
	File  string      `json:"file"`
	Color *[3]float64 `json:"color"`
}

type jsonWindow struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type jsonIcon struct {
	Size      int     `json:"size"`
	TextSize  float64 `json:"text_size"`
	GridSpace float64 `json:"grid_space"`
}

func loadConfig(path string) (dmg.Options, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return dmg.Options{}, fmt.Errorf("read config: %w", err)
	}

	var cfg jsonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return dmg.Options{}, fmt.Errorf("parse config: %w", err)
	}

	opts := dmg.Options{
		VolumeName:    cfg.VolumeName,
		IconPositions: cfg.IconPositions,
		OutputPath:    cfg.OutputPath,
		Files:         cfg.Files,
		VolumeIcon:    cfg.VolumeIcon,
		FileIcon:      cfg.FileIcon,
		SizeHint:      cfg.SizeHint,
	}

	opts.Backend, err = parseBackend(cfg.Backend)
	if err != nil {
		return dmg.Options{}, err
	}

	if cfg.Format != "" {
		opts.Format = dmg.Format(cfg.Format)
	}
	if cfg.Filesystem != "" {
		opts.Filesystem = dmg.Filesystem(cfg.Filesystem)
	}

	if cfg.AddApplicationsSymlink != nil {
		opts.AddApplicationsSymlink = *cfg.AddApplicationsSymlink
	} else {
		opts.AddApplicationsSymlink = true
	}

	if cfg.Window != nil {
		opts.Window = dmg.WindowConfig{
			X: cfg.Window.X, Y: cfg.Window.Y,
			Width: cfg.Window.Width, Height: cfg.Window.Height,
		}
	}
	if cfg.Icon != nil {
		opts.Icon = dmg.IconConfig{
			Size: cfg.Icon.Size, TextSize: cfg.Icon.TextSize, GridSpace: cfg.Icon.GridSpace,
		}
	}

	if cfg.Background != nil {
		bg := &dmg.BackgroundConfig{}
		if cfg.Background.File != "" {
			bg.File = cfg.Background.File
		}
		if cfg.Background.Color != nil {
			bg.Color = &dmg.ColorRGB{
				R: cfg.Background.Color[0],
				G: cfg.Background.Color[1],
				B: cfg.Background.Color[2],
			}
		}
		opts.Background = bg
	}

	if opts.Files == nil {
		opts.Files = make(map[string]string)
	}

	return opts, nil
}
