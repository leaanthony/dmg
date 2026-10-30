package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leaanthony/dmg/dmg"
)

func TestLoadConfigHFSX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dmg.json")
	data := []byte(`{"volume_name":"Example","output":"out.dmg","filesystem":"HFSX","backend":"native","files":{"Example.app":"app"},"icon_positions":{"Example.app":{"x":170,"y":160},"Applications":{"x":470,"y":160}}}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Filesystem != dmg.FSHFSX || opts.Backend != dmg.BackendNative || opts.Files["Example.app"] != "app" || !opts.AddApplicationsSymlink {
		t.Fatalf("config was lost: %+v", opts)
	}
	if opts.IconPositions["Example.app"] != (dmg.IconPosition{X: 170, Y: 160}) || opts.IconPositions["Applications"] != (dmg.IconPosition{X: 470, Y: 160}) {
		t.Fatalf("positions were lost: %+v", opts.IconPositions)
	}
}

func TestParseBackend(t *testing.T) {
	for _, tc := range []struct {
		name string
		want dmg.Backend
	}{{"", dmg.BackendAuto}, {"auto", dmg.BackendAuto}, {"native", dmg.BackendNative}, {"hdiutil", dmg.BackendHdiutil}} {
		got, err := parseBackend(tc.name)
		if err != nil || got != tc.want {
			t.Fatalf("%q: got %v, %v", tc.name, got, err)
		}
	}
	if _, err := parseBackend("naive"); err == nil {
		t.Fatal("invalid backend accepted")
	}
}
