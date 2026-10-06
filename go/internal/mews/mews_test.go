package mews

import (
	"bytes"
	"strings"
	"testing"
)

// 11-partition layout: EWS377AP v3 (ART = mtd11).
const procMtdV3 = `dev:    size   erasesize  name
mtd0: 00100000 00020000 "0:sbl1"
mtd1: 00080000 00020000 "0:mibib"
mtd2: 00300000 00020000 "0:qsee"
mtd3: 00080000 00020000 "0:devcfg"
mtd4: 00080000 00020000 "0:cdt"
mtd5: 00080000 00020000 "0:ddr"
mtd6: 00080000 00020000 "0:tz"
mtd7: 00080000 00020000 "0:appsblenv"
mtd8: 006a0000 00020000 "0:appsbl"
mtd9: 00080000 00020000 "0:art_bak"
mtd10: 00080000 00020000 "0:oem"
mtd11: 00080000 00020000 "0:art"
mtd12: 06f00000 00020000 "rootfs"
`

// 17-partition layout: EWS377-FIT. mtd11 is crashdump; ART is mtd12 (#3).
const procMtdFIT = `dev:    size   erasesize  name
mtd0: 00100000 00020000 "0:sbl1"
mtd1: 00080000 00020000 "0:mibib"
mtd2: 00300000 00020000 "0:qsee"
mtd3: 00080000 00020000 "0:devcfg"
mtd4: 00080000 00020000 "0:cdt"
mtd5: 00080000 00020000 "0:ddr"
mtd6: 00080000 00020000 "0:tz"
mtd7: 00080000 00020000 "0:appsblenv"
mtd8: 006a0000 00020000 "0:appsbl"
mtd9: 00060000 00020000 "cert"
mtd10: 000a0000 00020000 "userconfig"
mtd11: 00060000 00020000 "crashdump"
mtd12: 00080000 00020000 "0:art"
mtd13: 06f00000 00020000 "rootfs"
`

func find(arts []Artifact, slug string) *Artifact {
	for i := range arts {
		if strings.HasPrefix(arts[i].Name, slug+"-") {
			return &arts[i]
		}
	}
	return nil
}

func TestResolveByName_BothLayouts(t *testing.T) {
	for _, tc := range []struct {
		name, proc string
		art, env   string
	}{
		{"v3", procMtdV3, "dd if=/dev/mtd11 bs=64k", "dd if=/dev/mtd7 bs=64k"},
		{"fit", procMtdFIT, "dd if=/dev/mtd12 bs=64k", "dd if=/dev/mtd7 bs=64k"},
	} {
		arts, err := Resolve(tc.proc)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if a := find(arts, "art"); a == nil || a.Command != tc.art || !a.Critical || a.Secret {
			t.Errorf("%s: art = %+v", tc.name, a)
		}
		if a := find(arts, "appsblenv"); a == nil || a.Command != tc.env {
			t.Errorf("%s: appsblenv = %+v", tc.name, a)
		}
	}
}

// The #3 bug: on the FIT, an index-based plan dumped crashdump as ART.
func TestFITNeverDumpsCrashdumpAsArt(t *testing.T) {
	arts, _ := Resolve(procMtdFIT)
	for _, a := range arts {
		if strings.Contains(a.Command, "/dev/mtd11 ") && a.Critical {
			t.Fatalf("critical artifact %s reads crashdump (mtd11)", a.Name)
		}
		if strings.HasSuffix(a.Name, ".bin") && strings.Contains(a.Name, "mtd") {
			t.Errorf("artifact %q must be named by partition, not mtd index", a.Name)
		}
	}
	if a := find(arts, "art"); a == nil || !strings.Contains(a.ProcLine, `"0:art"`) {
		t.Errorf("proc line not recorded next to the dump: %+v", a)
	}
}

func TestResolveFailsClosed(t *testing.T) {
	missing := strings.Replace(procMtdFIT, `"0:art"`, `"calibration"`, 1)
	if _, err := Resolve(missing); err == nil || !strings.Contains(err.Error(), "0:art") {
		t.Fatalf("missing ART must fail closed, got %v", err)
	}
	dup := procMtdFIT + `mtd14: 00080000 00020000 "ART"` + "\n"
	if _, err := Resolve(dup); err == nil || !strings.Contains(err.Error(), "refusing to guess") {
		t.Fatalf("ambiguous ART must fail closed, got %v", err)
	}
	if _, err := Resolve("garbage"); err == nil {
		t.Fatal("unparseable /proc/mtd must fail")
	}
}

func TestSecretPartitionsSeparated(t *testing.T) {
	arts, _ := Resolve(procMtdFIT)
	for _, slug := range []string{"cert", "userconfig"} {
		a := find(arts, slug)
		if a == nil || !a.Secret || a.Critical || a.Dir() != "secret" {
			t.Errorf("%s must be an optional secret artifact in secret/: %+v", slug, a)
		}
	}
	// A unit without cert/userconfig (v3) simply skips them.
	arts, _ = Resolve(procMtdV3)
	if find(arts, "cert") != nil {
		t.Error("v3 has no cert partition")
	}
	if !strings.Contains(SecretWarning, "NEVER publish") {
		t.Error("loud warning text missing")
	}
}

func TestPlanReadOnly(t *testing.T) {
	for _, a := range Plan() {
		if strings.Contains(a.Command, "of=/dev/mtd") {
			t.Fatalf("backup plan must never WRITE an mtd: %q", a.Command)
		}
	}
	arts, _ := Resolve(procMtdFIT)
	for _, a := range arts {
		if strings.Contains(a.Command, "of=/dev/mtd") {
			t.Fatalf("resolved plan must never WRITE an mtd: %q", a.Command)
		}
	}
}

func TestSanityArt(t *testing.T) {
	art := bytes.Repeat([]byte{0xff}, 512*1024)
	if err := Sanity("art", art); err == nil {
		t.Fatal("all-0xff ART must be rejected")
	}
	art[0x1000] = 0x05
	if err := Sanity("art", art); err != nil {
		t.Fatal(err)
	}
	if err := Sanity("art", make([]byte, 384*1024)); err == nil {
		t.Fatal("a 384 KiB crashdump must not pass as ART")
	}
}

func TestSanityEnv(t *testing.T) {
	good := append([]byte{0xde, 0xad, 0xbe, 0xef}, []byte("bootcmd=bootipq\x00active_fw=0\x00\x00")...)
	if err := Sanity("appsblenv", good); err != nil {
		t.Fatal(err)
	}
	redundant := append([]byte{0xde, 0xad, 0xbe, 0xef, 0x01}, []byte("bootcmd=bootipq\x00\x00")...)
	if err := Sanity("appsblenv", redundant); err != nil {
		t.Fatalf("redundant-env flag byte should be tolerated: %v", err)
	}
	if err := Sanity("appsblenv", bytes.Repeat([]byte{0xff}, 4096)); err == nil {
		t.Fatal("erased flash is not an env")
	}
}

func TestBundleName(t *testing.T) {
	got := BundleName("2026-09-02", "Scuderia Toro Rosso", "88:DC:97:04:44:07")
	if got != "2026-09-02_Scuderia-Toro-Rosso_88-dc-97-04-44-07" {
		t.Fatalf("bundle = %q", got)
	}
}

func TestMissingCritical(t *testing.T) {
	arts, _ := Resolve(procMtdFIT)
	captured := map[string]bool{}
	for _, a := range arts {
		if a.Critical {
			captured[a.Name] = true
		}
	}
	if m := MissingCritical(arts, captured); len(m) != 0 {
		t.Fatalf("missing = %v", m)
	}
	art := find(arts, "art")
	delete(captured, art.Name)
	if m := MissingCritical(arts, captured); len(m) != 1 || m[0] != art.Name {
		t.Fatalf("missing = %v", m)
	}
}
