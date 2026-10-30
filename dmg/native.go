package dmg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/leaanthony/dmg/dsstore"
	"github.com/leaanthony/dmg/hfsplus"
	"github.com/leaanthony/dmg/udif"
)

// kHasCustomIcon is the Finder flag bit marking an item as having a custom icon.
const kHasCustomIcon = 0x0400

// buildNative creates the DMG entirely in Go (hfsplus + udif), with no macOS
// tools, so it works when cross-compiled to any platform. It produces a
// HFS+ or case-sensitive HFSX volume in a UDZO container.
func buildNative(opts Options) error {
	if opts.Format != "" && opts.Format != FormatUDZO {
		return fmt.Errorf("dmg: native backend only supports UDZO format (got %s); use BackendHdiutil", opts.Format)
	}
	if opts.Filesystem == FSAPFS {
		return fmt.Errorf("dmg: native backend does not support APFS; use BackendHdiutil")
	}

	vol := &hfsplus.Volume{
		Name: opts.VolumeName, CreatedAt: time.Now(),
		CaseSensitive: opts.Filesystem == FSHFSX,
	}

	// Files (in sorted order, matching the .DS_Store icon layout).
	for _, name := range sortedFileNames(opts) {
		ent, err := pathToEntry(name, opts.Files[name])
		if err != nil {
			return fmt.Errorf("dmg: add %q: %w", name, err)
		}
		vol.Children = append(vol.Children, ent)
	}

	// Background image, copied to the volume root as ".background.<ext>".
	var bgRel string
	if opts.Background != nil && opts.Background.File != "" {
		data, err := os.ReadFile(opts.Background.File)
		if err != nil {
			return fmt.Errorf("dmg: read background: %w", err)
		}
		bgRel = ".background" + filepath.Ext(opts.Background.File)
		vol.Children = append(vol.Children, &hfsplus.Entry{Name: bgRel, Kind: hfsplus.KindFile, Data: data})
	}

	// Volume icon: write .VolumeIcon.icns and flag the root folder.
	if opts.VolumeIcon != "" {
		icnsData, err := readIconData(opts.VolumeIcon)
		if err != nil {
			return fmt.Errorf("dmg: read volume icon: %w", err)
		}
		vol.Children = append(vol.Children, &hfsplus.Entry{Name: ".VolumeIcon.icns", Kind: hfsplus.KindFile, Data: icnsData})
		vol.RootFinderFlags |= kHasCustomIcon
	}

	// Applications drag-link.
	if opts.AddApplicationsSymlink {
		vol.Children = append(vol.Children, &hfsplus.Entry{Name: "Applications", Kind: hfsplus.KindSymlink, Target: "/Applications"})
	}

	// .DS_Store (added last so it does not shift earlier CNIDs).
	dsData, err := nativeDSStore(opts, vol, bgRel)
	if err != nil {
		return fmt.Errorf("dmg: generate .DS_Store: %w", err)
	}
	vol.Children = append(vol.Children, &hfsplus.Entry{Name: ".DS_Store", Kind: hfsplus.KindFile, Data: dsData})

	img, err := hfsplus.Build(vol)
	if err != nil {
		return fmt.Errorf("dmg: build HFS+ volume: %w", err)
	}

	os.Remove(opts.OutputPath)
	f, err := os.Create(opts.OutputPath)
	if err != nil {
		return fmt.Errorf("dmg: create output: %w", err)
	}
	if err := udif.Build(f, img, "whole disk (Apple_HFS : 0)"); err != nil {
		f.Close()
		os.Remove(opts.OutputPath)
		return fmt.Errorf("dmg: write image: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}

	// The icon on the .dmg file itself is a host extended-attribute operation,
	// so it is only applied on macOS.
	if opts.FileIcon != "" && runtime.GOOS == "darwin" {
		icnsData, err := readIconData(opts.FileIcon)
		if err != nil {
			return fmt.Errorf("dmg: read file icon: %w", err)
		}
		if err := setFileIcon(opts.OutputPath, icnsData); err != nil {
			return fmt.Errorf("dmg: set file icon: %w", err)
		}
	}

	return nil
}

// nativeDSStore builds the .DS_Store bytes for the native backend. When a
// background is set it computes the background file's CNID (via AssignCNIDs) so
// the alias record matches the volume that will be built.
func nativeDSStore(opts Options, vol *hfsplus.Volume, bgRel string) ([]byte, error) {
	var alias *dsstore.AliasFileInfo
	if bgRel != "" {
		hfsplus.AssignCNIDs(vol)
		var bgCNID uint32
		for _, c := range vol.Children {
			if c.Name == bgRel {
				bgCNID = c.CNID
			}
		}
		hfsDate := unixToHFS(vol.CreatedAt.Unix())
		alias = &dsstore.AliasFileInfo{
			DriveType:        0,
			VolCreationDate:  hfsDate,
			FileCreationDate: hfsDate,
			FileCNID:         bgCNID,
			ParentCNID:       2, // root folder
		}
	}

	s := newStyledStore(opts, bgRel, alias)
	var buf bytes.Buffer
	if err := s.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pathToEntry converts a source path into an hfsplus.Entry under the given name,
// preserving directories, symlinks, and file permissions.
func pathToEntry(name, src string) (*hfsplus.Entry, error) {
	fi, err := os.Lstat(src)
	if err != nil {
		return nil, err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return nil, err
		}
		return &hfsplus.Entry{Name: name, Kind: hfsplus.KindSymlink, Target: target}, nil
	case fi.IsDir():
		kids, err := dirEntries(src)
		if err != nil {
			return nil, err
		}
		return &hfsplus.Entry{Name: name, Kind: hfsplus.KindDir, Children: kids}, nil
	default:
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		return &hfsplus.Entry{Name: name, Kind: hfsplus.KindFile, Data: data, Mode: uint16(fi.Mode().Perm())}, nil
	}
}

func dirEntries(dir string) ([]*hfsplus.Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*hfsplus.Entry, 0, len(des))
	for _, de := range des {
		ent, err := pathToEntry(de.Name(), filepath.Join(dir, de.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, ent)
	}
	return out, nil
}
