package hfsplus

import (
	"encoding/binary"
	"time"
	"unicode/utf16"
)

var be = binary.BigEndian

// --- Volume header ---

type volumeHeaderParams struct {
	volumeName    string
	createdAt     time.Time
	caseSensitive bool
	totalBlocks   uint32
	freeBlocks    uint32
	nextAlloc     uint32
	nextCatalog   uint32
	fileCount     uint32
	folderCount   uint32
	allocFork     fork
	extentsFork   fork
	catalogFork   fork
}

func buildVolumeHeader(p volumeHeaderParams) []byte {
	b := make([]byte, 512)
	t := hfsTime(p.createdAt)

	sig, ver := uint16(sigHFSPlus), uint16(versionHFSPlus)
	if p.caseSensitive {
		sig, ver = sigHFSX, versionHFSX
	}
	be.PutUint16(b[0:2], sig)
	be.PutUint16(b[2:4], ver)
	be.PutUint32(b[4:8], volUnmounted)
	copy(b[8:12], []byte("10.0")) // lastMountedVersion (informational)
	be.PutUint32(b[12:16], 0)     // journalInfoBlock
	be.PutUint32(b[16:20], t)     // createDate
	be.PutUint32(b[20:24], t)     // modifyDate
	be.PutUint32(b[24:28], 0)     // backupDate
	be.PutUint32(b[28:32], t)     // checkedDate
	be.PutUint32(b[32:36], p.fileCount)
	be.PutUint32(b[36:40], p.folderCount)
	be.PutUint32(b[40:44], blockSize)
	be.PutUint32(b[44:48], p.totalBlocks)
	be.PutUint32(b[48:52], p.freeBlocks)
	be.PutUint32(b[52:56], p.nextAlloc)
	be.PutUint32(b[56:60], 65536) // rsrcClumpSize
	be.PutUint32(b[60:64], 65536) // dataClumpSize
	be.PutUint32(b[64:68], p.nextCatalog)
	be.PutUint32(b[68:72], 0) // writeCount
	be.PutUint64(b[72:80], 1) // encodingsBitmap (encoding 0 = MacRoman)
	// finderInfo[8] at 80..112 = zeros
	putFork(b[112:192], p.allocFork)
	putFork(b[192:272], p.extentsFork)
	putFork(b[272:352], p.catalogFork)
	// attributesFile (352..432) and startupFile (432..512) left zero
	return b
}

func putFork(b []byte, f fork) {
	be.PutUint64(b[0:8], f.logicalSize)
	be.PutUint32(b[8:12], uint32(f.logicalSize)) // clumpSize
	be.PutUint32(b[12:16], f.totalBlocks)
	for i, e := range f.extents {
		if i >= 8 {
			break
		}
		be.PutUint32(b[16+i*8:], e.startBlock)
		be.PutUint32(b[16+i*8+4:], e.blockCount)
	}
}

// --- B-tree node assembly ---

// buildNode packs records into a single nodeSize-byte B-tree node with the
// trailing offset table that HFS+ uses to locate records.
func buildNode(kind, height int, fLink, bLink uint32, records [][]byte) []byte {
	node := make([]byte, nodeSize)
	be.PutUint32(node[0:4], fLink)
	be.PutUint32(node[4:8], bLink)
	node[8] = byte(int8(kind))
	node[9] = byte(height)
	be.PutUint16(node[10:12], uint16(len(records)))

	off := 14
	offsets := []int{14}
	for _, r := range records {
		copy(node[off:], r)
		off += len(r)
		offsets = append(offsets, off)
	}
	for i, o := range offsets {
		be.PutUint16(node[nodeSize-2*(i+1):], uint16(o))
	}
	return node
}

// btHeaderParams configures a B-tree header record.
type btHeaderParams struct {
	treeDepth      uint16
	rootNode       uint32
	leafRecords    uint32
	firstLeaf      uint32
	lastLeaf       uint32
	totalNodes     uint32
	freeNodes      uint32
	maxKeyLength   uint16
	keyCompareType uint8
	attributes     uint32
}

func buildBTHeaderRec(p btHeaderParams) []byte {
	b := make([]byte, 106)
	be.PutUint16(b[0:2], p.treeDepth)
	be.PutUint32(b[2:6], p.rootNode)
	be.PutUint32(b[6:10], p.leafRecords)
	be.PutUint32(b[10:14], p.firstLeaf)
	be.PutUint32(b[14:18], p.lastLeaf)
	be.PutUint16(b[18:20], nodeSize)
	be.PutUint16(b[20:22], p.maxKeyLength)
	be.PutUint32(b[22:26], p.totalNodes)
	be.PutUint32(b[26:30], p.freeNodes)
	be.PutUint16(b[30:32], 0) // reserved1
	be.PutUint32(b[32:36], 0) // clumpSize
	b[36] = 0                 // btreeType = kHFSBTreeType
	b[37] = p.keyCompareType
	be.PutUint32(b[38:42], p.attributes)
	// reserved3[16] = zeros
	return b
}

// buildHeaderNode builds B-tree node 0 (the header node): the header record,
// a 128-byte reserved user record, and a node-usage bitmap record.
func buildHeaderNode(h btHeaderParams, usedNodes int) []byte {
	headerRec := buildBTHeaderRec(h)
	userRec := make([]byte, 128)

	// The map record fills the rest of the node; it is a bitmap of used nodes.
	mapLen := nodeSize - 14 - len(headerRec) - len(userRec) - 2*4 // 4 offset entries
	mapRec := make([]byte, mapLen)
	for n := 0; n < usedNodes; n++ {
		mapRec[n/8] |= 0x80 >> (uint(n) % 8)
	}
	return buildNode(kindHeader, 0, 0, 0, [][]byte{headerRec, userRec, mapRec})
}

// buildEmptyBTree builds a B-tree containing only a header node (no records),
// occupying `blocks` allocation blocks.
func buildEmptyBTree(blocks int, maxKeyLen uint16, compareType uint8) []byte {
	totalNodes := uint32(blocks * blockSize / nodeSize)
	h := btHeaderParams{
		treeDepth:      0,
		rootNode:       0,
		leafRecords:    0,
		firstLeaf:      0,
		lastLeaf:       0,
		totalNodes:     totalNodes,
		freeNodes:      totalNodes - 1, // header node used
		maxKeyLength:   maxKeyLen,
		keyCompareType: compareType,
		attributes:     0,
	}
	out := make([]byte, blocks*blockSize)
	copy(out, buildHeaderNode(h, 1))
	return out
}

// --- Catalog ---

// catalogKey builds a catalog B-tree key (parentID + name).
func catalogKey(parentID uint32, name string) []byte {
	u := utf16.Encode(decomposeName(name))
	keyLen := 4 + 2 + 2*len(u) // parentID + nameLength + name
	b := make([]byte, 2+keyLen)
	be.PutUint16(b[0:2], uint16(keyLen))
	be.PutUint32(b[2:6], parentID)
	be.PutUint16(b[6:8], uint16(len(u)))
	for i, c := range u {
		be.PutUint16(b[8+i*2:], c)
	}
	return b
}

// kHFSThreadExistsMask: every file/folder in HFS+ has a thread record.
const flagThreadExists = 0x0002

func catalogFolderRecord(parentID uint32, name string, folderID uint32, valence uint32, createdAt time.Time, finderFlags uint16) []byte {
	key := catalogKey(parentID, name)
	data := make([]byte, 88)
	t := hfsTime(createdAt)
	be.PutUint16(data[0:2], recFolder)
	be.PutUint16(data[2:4], flagThreadExists)
	be.PutUint32(data[4:8], valence)
	be.PutUint32(data[8:12], folderID)
	be.PutUint32(data[12:16], t) // createDate
	be.PutUint32(data[16:20], t) // contentModDate
	be.PutUint32(data[20:24], t) // attributeModDate
	be.PutUint32(data[24:28], t) // accessDate
	be.PutUint32(data[28:32], 0) // backupDate
	putBSDInfo(data[32:48], 0040755)
	// userInfo (FolderInfo) at 48..64: finderFlags is at offset 8 of FolderInfo.
	be.PutUint16(data[48+8:48+10], finderFlags)
	// finderInfo (ExtendedFolderInfo) 64..80 zero; textEncoding, reserved zero
	return append(key, data...)
}

// fileForkInfo describes a file's data fork for the catalog file record.
type fileForkInfo struct {
	logicalSize uint64
	startBlock  uint32
	blockCount  uint32
}

// catalogFileRecord builds an HFSPlusCatalogFile record (248-byte body).
// fileType/fileCreator populate the FileInfo (used to mark symlinks); mode is
// the POSIX file mode (e.g. 0100644 for a file, 0120755 for a symlink).
func catalogFileRecord(parentID uint32, name string, fileID uint32, createdAt time.Time,
	mode uint16, fileType, fileCreator uint32, finderFlags uint16, dataFork fileForkInfo) []byte {
	key := catalogKey(parentID, name)
	data := make([]byte, 248)
	t := hfsTime(createdAt)
	be.PutUint16(data[0:2], recFile)
	be.PutUint16(data[2:4], flagThreadExists)
	be.PutUint32(data[4:8], 0) // reserved1
	be.PutUint32(data[8:12], fileID)
	be.PutUint32(data[12:16], t) // createDate
	be.PutUint32(data[16:20], t) // contentModDate
	be.PutUint32(data[20:24], t) // attributeModDate
	be.PutUint32(data[24:28], t) // accessDate
	be.PutUint32(data[28:32], 0) // backupDate
	putBSDInfo(data[32:48], mode)
	// userInfo (FileInfo) 48..64: fileType(4), fileCreator(4), finderFlags(2), ...
	be.PutUint32(data[48:52], fileType)
	be.PutUint32(data[52:56], fileCreator)
	be.PutUint16(data[56:58], finderFlags)
	// finderInfo (ExtendedFileInfo) 64..80 zero; textEncoding 80, reserved2 84
	// dataFork HFSPlusForkData at 88..168
	putForkData(data[88:168], dataFork)
	// resourceFork at 168..248 left zero
	return append(key, data...)
}

func putForkData(b []byte, f fileForkInfo) {
	be.PutUint64(b[0:8], f.logicalSize)
	be.PutUint32(b[8:12], uint32(f.logicalSize)) // clumpSize
	be.PutUint32(b[12:16], f.blockCount)
	if f.blockCount > 0 {
		be.PutUint32(b[16:20], f.startBlock)
		be.PutUint32(b[20:24], f.blockCount)
	}
}

func catalogThreadRecord(ownID uint32, recordType uint16, parentID uint32, name string) []byte {
	key := catalogKey(ownID, "")
	u := utf16.Encode(decomposeName(name))
	data := make([]byte, 8+2+2*len(u))
	be.PutUint16(data[0:2], recordType)
	be.PutUint16(data[2:4], 0) // reserved
	be.PutUint32(data[4:8], parentID)
	be.PutUint16(data[8:10], uint16(len(u)))
	for i, c := range u {
		be.PutUint16(data[10+i*2:], c)
	}
	return append(key, data...)
}

// putBSDInfo writes an HFSPlusBSDInfo with the given POSIX file mode.
func putBSDInfo(b []byte, mode uint16) {
	be.PutUint32(b[0:4], 0) // ownerID
	be.PutUint32(b[4:8], 0) // groupID
	b[8] = 0                // adminFlags
	b[9] = 0                // ownerFlags
	be.PutUint16(b[10:12], mode)
	be.PutUint32(b[12:16], 0) // special
}
