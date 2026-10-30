package dmg

import (
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeFilesystemHeader(t *testing.T) {
	for _, tc := range []struct {
		fs        Filesystem
		signature string
		version   uint16
	}{{FSHFSPlus, "H+", 4}, {FSHFSX, "HX", 5}} {
		t.Run(string(tc.fs), func(t *testing.T) {
			dir := t.TempDir()
			name := "App.app"
			if tc.fs == FSHFSX {
				name = "\u793a\u4f8b.app"
			}
			app := filepath.Join(dir, name)
			if err := os.Mkdir(app, 0755); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "test.dmg")
			if err := Build(Options{VolumeName: "Test", OutputPath: out, Files: map[string]string{name: app}, Filesystem: tc.fs, Backend: BackendNative}); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(out)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			// This small, mostly empty fixture has a zlib-compressed first chunk.
			r, err := zlib.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			header := make([]byte, 1028)
			if _, err := io.ReadFull(r, header); err != nil {
				t.Fatal(err)
			}
			if string(header[1024:1026]) != tc.signature || binary.BigEndian.Uint16(header[1026:]) != tc.version {
				t.Fatalf("unexpected volume header: %x", header[1024:])
			}
		})
	}
}

func TestRejectUnknownFilesystem(t *testing.T) {
	if err := Build(Options{VolumeName: "Test", OutputPath: filepath.Join(t.TempDir(), "bad.dmg"), Filesystem: "HFXX", Backend: BackendNative}); err == nil {
		t.Fatal("unknown filesystem accepted")
	}
}

func TestHFSXMountedNames(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS mounting")
	}
	for _, backend := range []Backend{BackendNative, BackendHdiutil} {
		t.Run(map[Backend]string{BackendNative: "native", BackendHdiutil: "hdiutil"}[backend], func(t *testing.T) {
			dir := t.TempDir()
			appName := "\u793a\u4f8b-caf\u00e9-\uac00.app"
			app := filepath.Join(dir, appName)
			if err := os.Mkdir(app, 0755); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{appName: app}
			if err := os.WriteFile(filepath.Join(app, "\u6587\u4ef6.txt"), []byte("\u6587\u4ef6.txt"), 0755); err != nil {
				t.Fatal(err)
			}
			// Separate source names also work on case-insensitive host filesystems.
			for i, name := range []string{"Case.txt", "case.txt", "caf\u00e9.txt", "\uac00\uac01.txt", "\U0001d15e.txt", "a\u0301\U0001d165\u0327.txt"} {
				src := filepath.Join(dir, fmt.Sprintf("source-%d", i))
				if err := os.WriteFile(src, []byte(name), 0755); err != nil {
					t.Fatal(err)
				}
				files[name] = src
			}
			out := filepath.Join(dir, "test.dmg")
			if err := Build(Options{VolumeName: "Unicode caf\u00e9 \uac00", OutputPath: out, Files: files, Filesystem: FSHFSX, Backend: backend, AddApplicationsSymlink: true}); err != nil {
				t.Fatal(err)
			}
			if o, err := exec.Command("hdiutil", "verify", out).CombinedOutput(); err != nil {
				t.Fatalf("verify: %v %s", err, o)
			}
			mnt, err := hdiutilAttach(out)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := hdiutilDetach(mnt); err != nil {
					t.Error(err)
				}
			}()
			for _, name := range []string{"\u6587\u4ef6.txt", "Case.txt", "case.txt", "caf\u00e9.txt", "\uac00\uac01.txt", "\U0001d15e.txt", "a\u0301\U0001d165\u0327.txt"} {
				path := filepath.Join(mnt, name)
				if name == "\u6587\u4ef6.txt" {
					path = filepath.Join(mnt, appName, name)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != name {
					t.Fatalf("%s: %q %v", name, data, err)
				}
				fi, err := os.Stat(path)
				if err != nil || fi.Mode().Perm() != 0755 {
					t.Fatalf("permissions: %v %v", fi, err)
				}
			}
			if target, err := os.Readlink(filepath.Join(mnt, "Applications")); err != nil || target != "/Applications" {
				t.Fatalf("Applications: %q %v", target, err)
			}
		})
	}
}
