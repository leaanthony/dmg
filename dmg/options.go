// Package dmg creates styled macOS disk images (DMGs) with custom Finder
// window appearance including background images, icon positions, and window geometry.
//
// It uses the dsstore package to generate .DS_Store files and hdiutil for
// disk image creation and conversion.
package dmg

// Format specifies the DMG compression format.
type Format string

const (
	FormatUDRW Format = "UDRW" // Read-write (uncompressed, for development)
	FormatUDZO Format = "UDZO" // zlib compressed (maximum compatibility)
	FormatULFO Format = "ULFO" // LZFSE compressed (macOS 10.11+, good balance)
	FormatULMO Format = "ULMO" // LZMA compressed (smallest, slower decompression)
	FormatUDBZ Format = "UDBZ" // bzip2 compressed (deprecated)
)

// Filesystem specifies the DMG filesystem type.
type Filesystem string

const (
	FSHFSPlus Filesystem = "HFS+"
	FSHFSX    Filesystem = "HFSX" // Case-sensitive; supports non-ASCII names in the native backend.
	FSAPFS    Filesystem = "APFS"
)

// Backend selects how the disk image is created.
type Backend int

const (
	// BackendAuto uses macOS hdiutil when available (on darwin) and otherwise
	// falls back to the pure-Go backend. This is the default.
	BackendAuto Backend = iota
	// BackendNative uses the pure-Go hfsplus+udif backend. It needs no macOS
	// tools, so it works when cross-compiled to any platform. It produces a
	// UDZO, HFS+/HFSX image (other formats/filesystems require BackendHdiutil).
	BackendNative
	// BackendHdiutil uses macOS hdiutil/ditto (darwin only).
	BackendHdiutil
)

// Options configures the DMG creation process.
type Options struct {
	// Volume name displayed in Finder title bar
	VolumeName string

	// Output path for the final DMG file
	OutputPath string

	// Files to include in the DMG. Keys are destination names inside the volume,
	// values are source paths on disk.
	Files map[string]string

	// AddApplicationsSymlink creates a symlink to /Applications in the DMG.
	AddApplicationsSymlink bool

	// IconPositions optionally pins Finder icon locations by item name.
	// Coordinates are in Finder window pixels and refer to the icon centre.
	// Names not listed here are auto-positioned. Honored by both backends.
	IconPositions map[string]IconPosition

	// Window configures the Finder window geometry.
	Window WindowConfig

	// Icon configures icon appearance.
	Icon IconConfig

	// Background configures the Finder window background.
	Background *BackgroundConfig

	// Format for the final DMG (default: FormatUDZO)
	Format Format

	// Filesystem type (default: FSHFSPlus)
	Filesystem Filesystem

	// SizeHint is the writable DMG size in megabytes.
	// If 0, it's calculated automatically from the file sizes + overhead.
	SizeHint int

	// VolumeIcon is the path to an icon file for the mounted volume (optional).
	// Accepts .icns files directly, or .png/.jpg files which are auto-converted.
	VolumeIcon string

	// FileIcon is the path to an icon file for the DMG file itself in Finder (optional).
	// This sets the icon shown on the .dmg file before it's mounted.
	// Accepts .icns files directly, or .png/.jpg files which are auto-converted.
	FileIcon string

	// Backend selects the image-creation backend (default BackendAuto).
	Backend Backend
}

// WindowConfig defines the Finder window position and size.
type WindowConfig struct {
	X      int // Window X position on screen
	Y      int // Window Y position on screen
	Width  int // Window width in pixels
	Height int // Window height in pixels
}

// IconConfig defines icon appearance settings.
type IconConfig struct {
	Size      int     // Icon size in pixels (default: 128)
	TextSize  float64 // Label text size in points (default: 12)
	GridSpace float64 // Grid spacing between icons (default: 100, max: 100)
}

// IconPosition defines a Finder icon's centre point, in window pixels.
type IconPosition struct {
	X, Y int
}

// BackgroundConfig defines the Finder window background.
type BackgroundConfig struct {
	// Path to the background image file on disk.
	// The image will be copied into the DMG's .background/ folder.
	File string

	// Color sets a solid color background instead of an image.
	// Only used if File is empty. Values are 0.0-1.0.
	Color *ColorRGB
}

// ColorRGB represents an RGB color with float components (0.0-1.0).
type ColorRGB struct {
	R, G, B float64
}

// ContentItem defines a file or link to include in the DMG with a positioned icon.
type ContentItem struct {
	// Source path on disk (for files/directories)
	Path string

	// Name inside the DMG volume (defaults to base name of Path)
	Name string

	// Icon position in the Finder window
	X, Y int

	// Link makes this a symlink to the given target instead of copying a file.
	// Set to "/Applications" for the standard app-drop-link.
	Link string
}

// DefaultOptions returns Options with sensible defaults for a typical app DMG.
func DefaultOptions(appPath, outputPath string) Options {
	return Options{
		VolumeName: "Application",
		OutputPath: outputPath,
		Files:      map[string]string{},
		Window: WindowConfig{
			X:      100,
			Y:      100,
			Width:  540,
			Height: 380,
		},
		Icon: IconConfig{
			Size:      128,
			TextSize:  12,
			GridSpace: 100,
		},
		Format:                 FormatUDZO,
		Filesystem:             FSHFSPlus,
		AddApplicationsSymlink: true,
	}
}
