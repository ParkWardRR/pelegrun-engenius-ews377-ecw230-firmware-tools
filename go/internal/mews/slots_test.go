package mews

import (
	"errors"
	"strings"
	"testing"
)

// A real ECW336 /proc/mtd, booted from FW1. ART lands on mtd12 here (mtd11 on
// an EWS377AP v3), which is why nothing may resolve by index.
const ecw336ProcMtdFW1 = `dev:    size   erasesize  name
mtd0: 00100000 00020000 "0:SBL1"
mtd1: 00100000 00020000 "0:MIBIB"
mtd2: 00300000 00020000 "0:QSEE"
mtd3: 00080000 00020000 "0:DEVCFG"
mtd4: 00080000 00020000 "0:APDP"
mtd5: 00080000 00020000 "0:RPM"
mtd6: 00080000 00020000 "0:CDT"
mtd7: 00080000 00020000 "0:APPSBLENV"
mtd8: 006a0000 00020000 "0:APPSBL"
mtd9: 00060000 00020000 "cert"
mtd10: 000a0000 00020000 "userconfig"
mtd11: 00060000 00020000 "crashdump"
mtd12: 00080000 00020000 "0:ART"
mtd13: 06ec0000 00020000 "rootfs"
mtd14: 00900000 00020000 "0:WIFIFW"
mtd15: 06ec0000 00020000 "rootfs_1"
mtd16: 00900000 00020000 "0:WIFIFW_1"
mtd17: 00080000 00020000 "0:ETHPHYFW"
`

// The same unit booted from FW2: the labels swap. `rootfs` is at the FW2 offset
// now and `rootfs_1` points back at FW1. This is the trap the names exist to
// avoid — a plan that remembered an offset would overwrite the running slot.
const ecw336ProcMtdFW2 = `dev:    size   erasesize  name
mtd7: 00080000 00020000 "0:APPSBLENV"
mtd8: 006a0000 00020000 "0:APPSBL"
mtd12: 00080000 00020000 "0:ART"
mtd13: 06ec0000 00020000 "rootfs_1"
mtd15: 06ec0000 00020000 "rootfs"
mtd17: 00080000 00020000 "0:ETHPHYFW"
`

func TestRootfsResolvesTheSpareSlotByName(t *testing.T) {
	running, spare, err := Rootfs(ecw336ProcMtdFW1)
	if err != nil {
		t.Fatalf("Rootfs: %v", err)
	}
	if running.Index != 13 || spare.Index != 15 {
		t.Errorf("booted FW1: running=mtd%d spare=mtd%d, want mtd13/mtd15", running.Index, spare.Index)
	}

	// Booted from the other slot, the same names resolve to the other devices.
	running2, spare2, err := Rootfs(ecw336ProcMtdFW2)
	if err != nil {
		t.Fatalf("Rootfs FW2: %v", err)
	}
	if running2.Index != 15 || spare2.Index != 13 {
		t.Errorf("booted FW2: running=mtd%d spare=mtd%d, want mtd15/mtd13", running2.Index, spare2.Index)
	}
	if running.Index == running2.Index {
		t.Error("the rootfs labels must track the active slot, not a fixed index")
	}
}

func TestRootfsFailsClosedWithoutASpareSlot(t *testing.T) {
	single := "mtd13: 06ec0000 00020000 \"rootfs\"\n"
	if _, _, err := Rootfs(single); !errors.Is(err, ErrNoSpareSlot) {
		t.Errorf("got %v, want ErrNoSpareSlot", err)
	}

	// Mismatched sizes are not a matched A/B pair.
	odd := "mtd13: 06ec0000 00020000 \"rootfs\"\nmtd15: 01000000 00020000 \"rootfs_1\"\n"
	_, _, err := Rootfs(odd)
	if err == nil || !strings.Contains(err.Error(), "sizes differ") {
		t.Errorf("mismatched slot sizes: got %v, want a refusal", err)
	}

	// Duplicate names must not be guessed between.
	dup := "mtd13: 06ec0000 00020000 \"rootfs\"\nmtd14: 06ec0000 00020000 \"rootfs\"\nmtd15: 06ec0000 00020000 \"rootfs_1\"\n"
	if _, _, err := Rootfs(dup); err == nil {
		t.Error("ambiguous rootfs must be refused")
	}
}

// The ECW336 carries 0:ETHPHYFW (the Aquantia 10G PHY firmware u-boot loads
// before the uplink works). It must be captured when present and must not make
// the plan fail on boards that lack it.
func TestEthphyfwIsCapturedWhenPresentAndOptionalWhenNot(t *testing.T) {
	arts, err := Resolve(ecw336ProcMtdFW1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var found *Artifact
	for i := range arts {
		if strings.HasPrefix(arts[i].Name, "ethphyfw-") {
			found = &arts[i]
		}
	}
	if found == nil {
		t.Fatal("ETHPHYFW present in /proc/mtd but not captured")
	}
	if found.Command != "dd if=/dev/mtd17 bs=64k" {
		t.Errorf("command = %q", found.Command)
	}
	if found.Critical {
		t.Error("ethphyfw must stay optional — ap-hk07 boards have no such partition")
	}

	// An ap-hk07-shaped table with no ETHPHYFW still resolves.
	hk07 := `mtd7: 00080000 00020000 "0:APPSBLENV"
mtd8: 006a0000 00020000 "0:APPSBL"
mtd11: 00080000 00020000 "0:ART"
`
	if _, err := Resolve(hk07); err != nil {
		t.Errorf("a board without ETHPHYFW must still resolve: %v", err)
	}
}

// ART is mtd12 on this unit and mtd11 on an EWS377AP v3 — same name, different
// index. Resolution must follow the name.
func TestArtIndexIsNotAssumed(t *testing.T) {
	arts, err := Resolve(ecw336ProcMtdFW1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, a := range arts {
		if strings.HasPrefix(a.Name, "art-") {
			if a.Command != "dd if=/dev/mtd12 bs=64k" {
				t.Errorf("ART resolved to %q, want mtd12 on this layout", a.Command)
			}
			return
		}
	}
	t.Error("ART not in the resolved plan")
}
