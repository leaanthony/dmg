// Package hfsplus writes raw HFS+/HFSX volume images in pure Go.
//
// The output is a sector-aligned filesystem image suitable for wrapping in a
// UDIF container (see the sibling udif package) to produce a mountable .dmg.
// It makes no host OS calls, so it works when cross-compiled to any platform —
// the goal being to build macOS disk images on Linux/Windows CI.
//
// The writer is build-once and append-only: the entire directory tree is known
// up front, so there is no deletion, no node splitting, no journal, and no free
// space reuse. B-trees are bulk-loaded from pre-sorted keys. This is a clean-room
// implementation from Apple's Technote TN1150 (HFS Plus Volume Format); it does
// not derive from any GPL-licensed source.
//
// By default volumes are written as case-insensitive HFS+ (signature "H+"),
// matching the platform convention for distribution disk images. The current
// case-insensitive comparator handles ASCII names (which covers app bundles);
// non-ASCII names require the case-sensitive HFSX mode (signature "HX", binary
// comparison) until the full TN1150 fold table is implemented.
package hfsplus

import (
	"fmt"
	"time"
)

const (
	blockSize = 4096 // allocation block size in bytes
	nodeSize  = 4096 // B-tree node size in bytes
	sectorSz  = 512

	sigHFSPlus     = 0x482b // 'H+' (case-insensitive)
	versionHFSPlus = 4
	sigHFSX        = 0x4858 // 'HX' (case-sensitive when keyCompareType is binary)
	versionHFSX    = 5

	cnidRootParent = 1
	cnidRootFolder = 2
	cnidFirstUser  = 16

	kindLeaf   = -1
	kindIndex  = 0
	kindHeader = 1
	kindMap    = 2

	recFolder       = 1
	recFile         = 2
	recFolderThread = 3
	recFileThread   = 4

	keyCompareBinary  = 0xBC // HFSX case-sensitive
	keyCompareFolding = 0xCF // HFS+ case-insensitive (folding)

	btBigKeysMask           = 0x00000002
	btVariableIndexKeysMask = 0x00000004

	maxCatalogKeyLen = 516
	maxExtentKeyLen  = 10

	volUnmounted = 0x00000100

	// POSIX mode helpers.
	modeDir     = 0040755
	modeFile    = 0100644
	modeSymlink = 0120755

	// Symlink FileInfo type/creator (so Finder recognises it).
	symlinkType    = 0x736c6e6b // 'slnk'
	symlinkCreator = 0x72686170 // 'rhap'

	minVolumeBytes = 1 << 20 // floor so tiny volumes still mount cleanly
)

const hfsEpochDelta = 2082844800

func hfsTime(t time.Time) uint32 { return uint32(t.Unix() + hfsEpochDelta) }

// Kind distinguishes the three entry types a volume can hold.
type Kind int

const (
	KindDir Kind = iota
	KindFile
	KindSymlink
)

// Entry is one node in the directory tree.
type Entry struct {
	Name string
	Kind Kind

	Children []*Entry // KindDir
	Data     []byte   // KindFile
	Target   string   // KindSymlink

	// Mode is the POSIX permission bits (without the type bits). 0 selects a
	// sensible default (0755 for dirs/symlinks, 0644 for files).
	Mode uint16
	// FinderFlags sets the Finder flag bits (e.g. kHasCustomIcon = 0x0400).
	FinderFlags uint16

	// CNID is set by Build (and AssignCNIDs) to the catalog node ID this entry
	// received. It is output only; any value set before Build is overwritten.
	CNID uint32
}

// Volume describes a complete HFS+ volume to write.
type Volume struct {
	Name      string
	CreatedAt time.Time
	Children  []*Entry

	// RootFinderFlags sets the Finder flag bits on the root folder (e.g.
	// kHasCustomIcon = 0x0400 when the volume has a .VolumeIcon.icns).
	RootFinderFlags uint16

	// CaseSensitive selects a case-sensitive HFSX volume. The default (false)
	// writes a case-insensitive HFS+ volume, matching the platform convention
	// for distribution disk images.
	//
	// The case-insensitive comparator currently supports ASCII names only (it
	// folds A–Z, which matches macOS exactly for ASCII); a name containing a
	// non-ASCII character returns an error suggesting CaseSensitive. A full
	// Unicode (TN1150) fold table is a planned follow-up. Case-sensitive volumes
	// accept any name.
	CaseSensitive bool
}

// AssignCNIDs walks the volume in the same order Build uses and records each
// entry's catalog node ID in Entry.CNID, without building the image. This lets
// callers reference files by CNID (e.g. when constructing a .DS_Store alias)
// before the volume is built. Appending entries after calling AssignCNIDs does
// not change the CNIDs already assigned to earlier entries.
func AssignCNIDs(vol *Volume) {
	next := uint32(cnidFirstUser)
	var walk func(children []*Entry)
	walk = func(children []*Entry) {
		for _, e := range children {
			e.CNID = next
			next++
			if e.Kind == KindDir {
				walk(e.Children)
			}
		}
	}
	walk(vol.Children)
}

// isASCII reports whether s contains only ASCII (< 0x80) characters.
func isASCII(s string) bool {
	for _, r := range s {
		if r >= 0x80 {
			return false
		}
	}
	return true
}

type extent struct{ startBlock, blockCount uint32 }

type fork struct {
	logicalSize uint64
	totalBlocks uint32
	extents     []extent
}

// fileSlot tracks a file/symlink that needs data-fork blocks allocated.
type fileSlot struct {
	recIndex   int
	keyLen     int
	data       []byte
	blockCount uint32
	startBlock uint32
}

// Build writes a complete HFSX volume image for the given tree and returns the
// raw bytes, ready to be wrapped by udif.Build.
func Build(vol *Volume) ([]byte, error) {
	if vol.Name == "" {
		return nil, fmt.Errorf("hfsplus: volume name is required")
	}
	if !vol.CaseSensitive && !isASCII(vol.Name) {
		return nil, fmt.Errorf("hfsplus: volume name %q has non-ASCII characters; set CaseSensitive", vol.Name)
	}

	var records []catRecord
	var files []*fileSlot
	nextCNID := uint32(cnidFirstUser)
	var fileCount, folderCount uint32

	add := func(r []byte, key []byte) int {
		records = append(records, catRecord{key: key, full: r})
		return len(records) - 1
	}

	// Root folder record + thread.
	rootKey := catalogKey(cnidRootParent, vol.Name)
	if len(rootKey) > 8+2*255 {
		return nil, fmt.Errorf("hfsplus: volume name exceeds 255 UTF-16 units after decomposition")
	}
	add(catalogFolderRecord(cnidRootParent, vol.Name, cnidRootFolder, uint32(len(vol.Children)), vol.CreatedAt, vol.RootFinderFlags), rootKey)
	add(catalogThreadRecord(cnidRootFolder, recFolderThread, cnidRootParent, vol.Name), catalogKey(cnidRootFolder, ""))

	var walk func(parent uint32, children []*Entry) error
	walk = func(parent uint32, children []*Entry) error {
		names := make(map[string]bool, len(children))
		for _, e := range children {
			if e.Name == "" {
				return fmt.Errorf("hfsplus: entry under CNID %d has empty name", parent)
			}
			if !vol.CaseSensitive && !isASCII(e.Name) {
				return fmt.Errorf("hfsplus: name %q has non-ASCII characters; set CaseSensitive", e.Name)
			}
			key := catalogKey(parent, e.Name)
			if len(key) > 8+2*255 {
				return fmt.Errorf("hfsplus: name %q exceeds 255 UTF-16 units after decomposition", e.Name)
			}
			if names[string(key)] {
				return fmt.Errorf("hfsplus: duplicate name %q after decomposition under CNID %d", e.Name, parent)
			}
			names[string(key)] = true
			cnid := nextCNID
			nextCNID++
			e.CNID = cnid
			switch e.Kind {
			case KindDir:
				folderCount++
				add(catalogFolderRecord(parent, e.Name, cnid, uint32(len(e.Children)), vol.CreatedAt, e.FinderFlags), key)
				add(catalogThreadRecord(cnid, recFolderThread, parent, e.Name), catalogKey(cnid, ""))
				if err := walk(cnid, e.Children); err != nil {
					return err
				}
			case KindFile, KindSymlink:
				fileCount++
				data := e.Data
				mode := e.Mode
				var ftype, fcreator uint32
				if e.Kind == KindSymlink {
					data = []byte(e.Target)
					if mode == 0 {
						mode = modeSymlink & 0777
					}
					mode |= 0120000
					ftype, fcreator = symlinkType, symlinkCreator
				} else {
					if mode == 0 {
						mode = modeFile & 0777
					}
					mode |= 0100000
				}
				blockCount := uint32((len(data) + blockSize - 1) / blockSize)
				rec := catalogFileRecord(parent, e.Name, cnid, vol.CreatedAt, mode, ftype, fcreator, e.FinderFlags,
					fileForkInfo{logicalSize: uint64(len(data)), startBlock: 0, blockCount: blockCount})
				idx := add(rec, key)
				files = append(files, &fileSlot{recIndex: idx, keyLen: len(key), data: data, blockCount: blockCount})
				add(catalogThreadRecord(cnid, recFileThread, parent, e.Name), catalogKey(cnid, ""))
			default:
				return fmt.Errorf("hfsplus: %q has unknown kind %d", e.Name, e.Kind)
			}
		}
		return nil
	}
	if err := walk(cnidRootFolder, vol.Children); err != nil {
		return nil, err
	}

	// Build the catalog once to learn its node count (record sizes are final;
	// only file-fork start blocks remain to be patched in).
	_, catalogNodes := buildCatalogBTree(records, vol.CaseSensitive)
	catalogBlocks := catalogNodes // nodeSize == blockSize

	var totalFileBlocks uint32
	for _, f := range files {
		totalFileBlocks += f.blockCount
	}

	// Resolve the block layout, iterating until the bitmap size stabilises.
	const extentsBlocks = 1
	bitmapBlocks := 1
	var allocStart, catalogStart, extentsStart, dataStart, dataEnd, totalBlocks uint32
	for {
		allocStart = 1
		catalogStart = allocStart + uint32(bitmapBlocks)
		extentsStart = catalogStart + uint32(catalogBlocks)
		dataStart = extentsStart + extentsBlocks
		dataEnd = dataStart + totalFileBlocks
		totalBlocks = dataEnd + 1 // last block holds the alternate volume header
		if min := uint32(minVolumeBytes / blockSize); totalBlocks < min {
			totalBlocks = min
		}
		need := int((totalBlocks/8 + uint32(blockSize) - 1) / uint32(blockSize))
		bitmapBytes := (int(totalBlocks) + 7) / 8
		need = (bitmapBytes + blockSize - 1) / blockSize
		if need == bitmapBlocks {
			break
		}
		bitmapBlocks = need
	}

	// Assign data blocks and patch file records.
	nextData := dataStart
	for _, f := range files {
		if f.blockCount == 0 {
			continue
		}
		f.startBlock = nextData
		nextData += f.blockCount
		// dataFork.extents[0].startBlock lives at full[keyLen + 88 + 16].
		off := f.keyLen + 88 + 16
		be.PutUint32(records[f.recIndex].full[off:off+4], f.startBlock)
	}

	catalog, catalogNodes2 := buildCatalogBTree(records, vol.CaseSensitive)
	if catalogNodes2 != catalogNodes {
		return nil, fmt.Errorf("hfsplus: catalog size changed after layout (%d != %d)", catalogNodes2, catalogNodes)
	}

	// Assemble the image.
	img := make([]byte, int(totalBlocks)*blockSize)

	// Allocation bitmap: mark the metadata region, all file blocks, and the
	// alternate-header block as used.
	bitmap := make([]byte, bitmapBlocks*blockSize)
	markUsed := func(b uint32) { bitmap[b/8] |= 0x80 >> (b % 8) }
	for b := uint32(0); b < dataEnd; b++ {
		markUsed(b)
	}
	markUsed(totalBlocks - 1)
	copy(img[allocStart*blockSize:], bitmap)
	usedCount := dataEnd + 1
	freeBlocks := totalBlocks - usedCount

	copy(img[catalogStart*blockSize:], catalog)

	extents := buildEmptyBTree(extentsBlocks, maxExtentKeyLen, 0)
	copy(img[extentsStart*blockSize:], extents)

	for _, f := range files {
		if f.blockCount == 0 {
			continue
		}
		copy(img[f.startBlock*blockSize:], f.data)
	}

	vh := buildVolumeHeader(volumeHeaderParams{
		volumeName:    vol.Name,
		createdAt:     vol.CreatedAt,
		caseSensitive: vol.CaseSensitive,
		totalBlocks:   totalBlocks,
		freeBlocks:    freeBlocks,
		nextAlloc:     dataEnd,
		nextCatalog:   nextCNID,
		fileCount:     fileCount,
		folderCount:   folderCount,
		allocFork:     fork{logicalSize: uint64(bitmapBlocks) * blockSize, totalBlocks: uint32(bitmapBlocks), extents: []extent{{allocStart, uint32(bitmapBlocks)}}},
		extentsFork:   fork{logicalSize: extentsBlocks * blockSize, totalBlocks: extentsBlocks, extents: []extent{{extentsStart, extentsBlocks}}},
		catalogFork:   fork{logicalSize: uint64(catalogBlocks) * blockSize, totalBlocks: uint32(catalogBlocks), extents: []extent{{catalogStart, uint32(catalogBlocks)}}},
	})
	copy(img[1024:], vh)
	copy(img[len(img)-1024:], vh)

	return img, nil
}

// BuildEmpty writes a minimal empty HFSX volume containing only its root folder.
func BuildEmpty(volumeName string, createdAt time.Time) ([]byte, error) {
	return Build(&Volume{Name: volumeName, CreatedAt: createdAt})
}
