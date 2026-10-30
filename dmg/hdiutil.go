package dmg

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// hdiutil wraps macOS hdiutil commands for DMG manipulation.

// hdiutilCreate creates a writable DMG file.
func hdiutilCreate(volName string, fs Filesystem, sizeMB int, outputPath string) error {
	if fs == "" {
		fs = FSHFSPlus
	}
	fsName := string(fs)
	if fs == FSHFSX {
		fsName = "Case-sensitive HFS+"
	}
	args := []string{
		"create",
		"-volname", volName,
		"-fs", fsName,
		"-size", fmt.Sprintf("%dm", sizeMB),
		outputPath,
	}
	return runHdiutil(args...)
}

// hdiutilAttach mounts a DMG and returns the mount point path.
func hdiutilAttach(dmgPath string) (string, error) {
	args := []string{
		"attach",
		dmgPath,
		"-nobrowse", // Don't show in Finder
		"-owners", "off",
	}

	out, err := exec.Command("hdiutil", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("hdiutil attach: %w\n%s", err, out)
	}

	// Parse mount point from output.
	// hdiutil attach output has lines like:
	//   /dev/disk5          	Apple_partition_scheme
	//   /dev/disk5s1        	Apple_partition_map
	//   /dev/disk5s2        	Apple_HFS          	/Volumes/My App
	// The mount point is the last field of the last line.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Find the mount point — it starts with /Volumes/
		idx := strings.Index(line, "/Volumes/")
		if idx >= 0 {
			return strings.TrimSpace(line[idx:]), nil
		}
	}

	return "", fmt.Errorf("could not find mount point in hdiutil output:\n%s", out)
}

// hdiutilDetach unmounts a DMG with retry logic.
// Finder sometimes holds the volume briefly, so we retry with backoff.
func hdiutilDetach(mountPoint string) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		args := []string{"detach", mountPoint}
		if attempt >= 3 {
			args = append(args, "-force")
		}

		err := runHdiutil(args...)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("hdiutil detach failed after 5 attempts: %w", lastErr)
}

// hdiutilConvert converts a DMG to the specified format.
func hdiutilConvert(srcPath, dstPath string, format Format) error {
	if format == "" {
		format = FormatUDZO
	}
	args := []string{
		"convert",
		srcPath,
		"-format", string(format),
		"-o", dstPath,
	}

	// Add compression level for zlib
	if format == FormatUDZO {
		args = append(args, "-imagekey", "zlib-level=9")
	}

	return runHdiutil(args...)
}

// hdiutilResize shrinks a writable DMG to its minimum size.
func hdiutilResize(dmgPath string) error {
	return runHdiutil("resize", "-size", "min", dmgPath)
}

// runHdiutil executes an hdiutil command and returns any error.
func runHdiutil(args ...string) error {
	cmd := exec.Command("hdiutil", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hdiutil %s: %w\n%s", args[0], err, out)
	}
	return nil
}
