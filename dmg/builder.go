package dmg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/leaanthony/dmg/dsstore"
)

// Build creates a styled DMG disk image according to the provided options.
//
// The backend is chosen by opts.Backend (default BackendAuto): macOS hdiutil
// when available, otherwise the pure-Go hfsplus+udif backend, which needs no
// macOS tools and works when cross-compiled to any platform.
func Build(opts Options) error {
	if err := validateOptions(&opts); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}
	switch resolveBackend(opts) {
	case BackendNative:
		return buildNative(opts)
	default:
		return buildHdiutil(opts)
	}
}

// resolveBackend picks the concrete backend for the given options.
func resolveBackend(opts Options) Backend {
	switch opts.Backend {
	case BackendNative:
		return BackendNative
	case BackendHdiutil:
		return BackendHdiutil
	default: // BackendAuto
		if runtime.GOOS == "darwin" {
			if _, err := exec.LookPath("hdiutil"); err == nil {
				return BackendHdiutil
			}
		}
		return BackendNative
	}
}

// BuildWithContents creates a DMG using a simpler content-item based API.
// This is a convenience wrapper around Build for the common case.
func BuildWithContents(volumeName, outputPath string, contents []ContentItem, opts *Options) error {
	o := Options{
		VolumeName:             volumeName,
		OutputPath:             outputPath,
		Files:                  make(map[string]string),
		AddApplicationsSymlink: false,
		Format:                 FormatUDZO,
		Filesystem:             FSHFSPlus,
		Window: WindowConfig{
			X: 100, Y: 100, Width: 540, Height: 380,
		},
		Icon: IconConfig{
			Size: 128, TextSize: 12, GridSpace: 100,
		},
	}

	if opts != nil {
		if opts.Format != "" {
			o.Format = opts.Format
		}
		if opts.Filesystem != "" {
			o.Filesystem = opts.Filesystem
		}
		if opts.Window.Width > 0 {
			o.Window = opts.Window
		}
		if opts.Icon.Size > 0 {
			o.Icon = opts.Icon
		}
		if opts.Background != nil {
			o.Background = opts.Background
		}
		if opts.VolumeIcon != "" {
			o.VolumeIcon = opts.VolumeIcon
		}
		if opts.FileIcon != "" {
			o.FileIcon = opts.FileIcon
		}
		if opts.SizeHint > 0 {
			o.SizeHint = opts.SizeHint
		}
		o.Backend = opts.Backend
		o.IconPositions = opts.IconPositions
	}

	setPosition := func(name string, item ContentItem) {
		if item.X == 0 && item.Y == 0 {
			return
		}
		if o.IconPositions == nil {
			o.IconPositions = make(map[string]IconPosition)
		}
		o.IconPositions[name] = IconPosition{X: item.X, Y: item.Y}
	}

	for _, item := range contents {
		name := item.Name
		if name == "" {
			name = filepath.Base(item.Path)
		}
		if item.Link != "" {
			// Links other than the Applications drop-link are not modelled here,
			// but a pinned position is still recorded by name.
			setPosition(name, item)
			continue
		}
		o.Files[name] = item.Path
		setPosition(name, item)
	}

	return Build(o)
}

// validateOptions checks and applies defaults to the options.
func validateOptions(opts *Options) error {
	if opts.VolumeName == "" {
		return fmt.Errorf("volume name is required")
	}
	if opts.OutputPath == "" {
		return fmt.Errorf("output path is required")
	}
	if opts.Format == "" {
		opts.Format = FormatUDZO
	}
	if opts.Filesystem == "" {
		opts.Filesystem = FSHFSPlus
	}
	switch opts.Filesystem {
	case FSHFSPlus, FSHFSX, FSAPFS:
	default:
		return fmt.Errorf("unsupported filesystem %q", opts.Filesystem)
	}
	if opts.Window.Width == 0 {
		opts.Window = WindowConfig{X: 100, Y: 100, Width: 540, Height: 380}
	}
	if opts.Icon.Size == 0 {
		opts.Icon = IconConfig{Size: 128, TextSize: 12, GridSpace: 100}
	}
	return nil
}

// calculateSize estimates the DMG size needed for the given files.
func calculateSize(opts Options) int {
	var totalBytes int64
	for _, src := range opts.Files {
		totalBytes += dirSize(src)
	}
	if opts.Background != nil && opts.Background.File != "" {
		info, err := os.Stat(opts.Background.File)
		if err == nil {
			totalBytes += info.Size()
		}
	}
	// Add overhead: 32MB base + 2x file size
	sizeMB := int(totalBytes/(1024*1024))*2 + 32
	if sizeMB < 32 {
		sizeMB = 32
	}
	return sizeMB
}

// dirSize returns the total size of a file or directory in bytes.
func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// sortedFileNames returns file names from opts.Files in sorted order.
func sortedFileNames(opts Options) []string {
	names := make([]string, 0, len(opts.Files))
	for name := range opts.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// newStyledStore builds the .DS_Store contents (window geometry, icon view
// options, background, and icon positions) shared by both backends. aliasInfo
// is used only when bgRelativePath is set.
func newStyledStore(opts Options, bgRelativePath string, aliasInfo *dsstore.AliasFileInfo) *dsstore.Store {
	s := dsstore.New()

	s.SetWindowSettings(dsstore.WindowSettings{
		Top:    opts.Window.Y,
		Left:   opts.Window.X,
		Bottom: opts.Window.Y + opts.Window.Height,
		Right:  opts.Window.X + opts.Window.Width,
	})

	iconViewOpts := &dsstore.IconViewOptions{
		IconSize:      float64(opts.Icon.Size),
		TextSize:      opts.Icon.TextSize,
		GridSpace:     opts.Icon.GridSpace,
		LabelOnBottom: true,
	}

	if bgRelativePath != "" {
		s.SetBackgroundImage(opts.VolumeName, bgRelativePath, iconViewOpts, aliasInfo)
	} else if opts.Background != nil && opts.Background.Color != nil {
		iconViewOpts.BackgroundType = dsstore.BackgroundColor
		iconViewOpts.BackgroundRed = opts.Background.Color.R
		iconViewOpts.BackgroundGreen = opts.Background.Color.G
		iconViewOpts.BackgroundBlue = opts.Background.Color.B
		s.SetIconViewOptions(*iconViewOpts)
	} else {
		s.SetIconViewOptions(*iconViewOpts)
	}

	items := sortedFileNames(opts)
	if opts.AddApplicationsSymlink {
		items = append(items, "Applications")
	}
	positionIcons(s, items, opts.Window.Width, opts.Window.Height, opts.IconPositions)

	return s
}

// positionIcons sets icon positions for items. Items listed in explicit are
// pinned to the given coordinates; the rest are distributed evenly across the
// window width at ~45% of the window height.
func positionIcons(s *dsstore.Store, items []string, windowWidth, windowHeight int, explicit map[string]IconPosition) {
	if len(items) == 0 {
		return
	}
	y := windowHeight * 45 / 100 // slightly above center for label space
	spacing := windowWidth / (len(items) + 1)
	for i, name := range items {
		if pos, ok := explicit[name]; ok {
			s.SetIconPosition(name, pos.X, pos.Y)
			continue
		}
		x := spacing * (i + 1)
		s.SetIconPosition(name, x, y)
	}
}

// unixToHFS converts a Unix timestamp to a Mac HFS timestamp.
// HFS epoch is 1904-01-01, Unix epoch is 1970-01-01. Difference = 2082844800 seconds.
func unixToHFS(unixTime int64) uint32 {
	const hfsEpochDelta = 2082844800
	return uint32(unixTime + hfsEpochDelta)
}
