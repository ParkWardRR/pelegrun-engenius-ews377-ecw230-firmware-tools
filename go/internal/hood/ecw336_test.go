package hood

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadEcw336(t *testing.T) Env {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ecw336-printenv.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return ParsePrintenv(string(b))
}

// A stock ECW336 env must pass the gate: this bootloader generation boots
// without rootfsname, exactly like the EWS377-FIT.
func TestEcw336EnvIsCompleteWithoutRootfsname(t *testing.T) {
	e := loadEcw336(t)
	if miss := e.Missing(); len(miss) != 0 {
		t.Errorf("stock ECW336 env should be complete, missing %v", miss)
	}
	if miss := e.MissingFor(ProfileLegacy); len(miss) == 0 {
		t.Error("ProfileLegacy should still demand rootfsname")
	}
	if !e.HasIdentity() {
		t.Error("fixture should carry identity vars")
	}
	if e.LooksWiped() {
		t.Error("a healthy env must not look wiped")
	}
	if got := e["hw_id"]; got != "01010123" {
		t.Errorf("hw_id = %q, want 01010123 (vendor 257 + product 291)", got)
	}
}

func TestActiveAndInactiveSlot(t *testing.T) {
	e := loadEcw336(t)
	active, err := e.ActiveSlot()
	if err != nil || active != SlotA {
		t.Fatalf("ActiveSlot = %v, %v; want FW1", active, err)
	}
	spare, err := e.InactiveSlot()
	if err != nil || spare != SlotB {
		t.Fatalf("InactiveSlot = %v, %v; want FW2", spare, err)
	}
	if active.String() != "FW1" || spare.String() != "FW2" {
		t.Errorf("slot names: %q / %q", active, spare)
	}

	// Booted from FW2, the target is FW1 — the pair is symmetric.
	e[ABSlotVar] = "1"
	if active, _ := e.ActiveSlot(); active != SlotB {
		t.Errorf("active_fw=1 should be FW2, got %v", active)
	}
	if spare, _ := e.InactiveSlot(); spare != SlotA {
		t.Errorf("flash target from FW2 should be FW1, got %v", spare)
	}
}

// If active_fw cannot be read, "the inactive slot" is unknown and the plan must
// fail closed rather than pick one.
func TestUnknownSlotFailsClosed(t *testing.T) {
	e := loadEcw336(t)
	for _, bad := range []string{"", "2", "FW1", "yes"} {
		e[ABSlotVar] = bad
		if _, err := e.ActiveSlot(); !errors.Is(err, ErrUnknownSlot) {
			t.Errorf("active_fw=%q: got %v, want ErrUnknownSlot", bad, err)
		}
		if _, err := e.PlanSwitchSlot(); err == nil {
			t.Errorf("active_fw=%q: PlanSwitchSlot must refuse", bad)
		}
	}
	// Absent entirely: also refused (and the env is then incomplete anyway).
	delete(e, ABSlotVar)
	if _, err := e.ActiveSlot(); !errors.Is(err, ErrUnknownSlot) {
		t.Errorf("missing active_fw: got %v, want ErrUnknownSlot", err)
	}
}

func TestPlanSwitchSlotTouchesOnlyActiveFw(t *testing.T) {
	e := loadEcw336(t)
	cmd, err := e.PlanSwitchSlot()
	if err != nil {
		t.Fatalf("PlanSwitchSlot: %v", err)
	}
	if cmd != "fw_setenv active_fw 1" {
		t.Errorf("got %q, want `fw_setenv active_fw 1`", cmd)
	}
	// app_part is NOT the selector and must not appear in the plan.
	if strings.Contains(cmd, "app_part") {
		t.Errorf("plan must not touch app_part: %q", cmd)
	}
}

// The slot switch inherits the completeness gate: no writes planned against an
// env that is missing boot variables.
func TestSlotAndCloudPlansRespectTheCompletenessGate(t *testing.T) {
	broken := ParsePrintenv("ethaddr=00:03:7f:ba:db:ad\nactive_fw=0\n") // no bootcmd/app_part
	if _, err := broken.PlanSwitchSlot(); !errors.Is(err, ErrIncomplete) {
		t.Errorf("PlanSwitchSlot on an incomplete env: got %v, want ErrIncomplete", err)
	}
	if _, err := broken.PlanPrivateCloud(); !errors.Is(err, ErrIncomplete) {
		t.Errorf("PlanPrivateCloud on an incomplete env: got %v, want ErrIncomplete", err)
	}
	if _, err := broken.PlanSetUboot(CloudIDVar, CloudIDPrivate); !errors.Is(err, ErrIncomplete) {
		t.Errorf("PlanSetUboot on an incomplete env: got %v, want ErrIncomplete", err)
	}
}

// cloud_id is absent on a stock unit — the silent default that routes the init
// at EnGenius Cloud, where force_ac/opt-43/mDNS are never read.
func TestPrivateCloudGate(t *testing.T) {
	e := loadEcw336(t)
	if _, set := e[CloudIDVar]; set {
		t.Fatal("fixture should model a stock unit, which has no cloud_id")
	}
	cmd, err := e.PlanPrivateCloud()
	if err != nil {
		t.Fatalf("PlanPrivateCloud: %v", err)
	}
	if cmd != "fw_setenv cloud_id 3" {
		t.Errorf("got %q, want `fw_setenv cloud_id 3`", cmd)
	}

	// The same write from the bootloader prompt, which is the only console the
	// cloud firmware family offers.
	ub, err := e.PlanSetUboot(CloudIDVar, CloudIDPrivate)
	if err != nil {
		t.Fatalf("PlanSetUboot: %v", err)
	}
	if ub != "setenv cloud_id 3 ; saveenv" {
		t.Errorf("got %q, want `setenv cloud_id 3 ; saveenv`", ub)
	}

	// It is append-only and reversible: writing it back is one more field set,
	// never an erase. (There is deliberately no PlanErase to call.)
	e[CloudIDVar] = CloudIDPrivate
	back, err := e.PlanSet(CloudIDVar, CloudIDPublic)
	if err != nil || back != "fw_setenv cloud_id 0" {
		t.Errorf("revert: %q, %v", back, err)
	}
	if err := VerifyAfterSet(e, CloudIDVar, CloudIDPrivate); err != nil {
		t.Errorf("VerifyAfterSet: %v", err)
	}
}

// An empty value deletes a u-boot variable, so it stays refused on every path
// including the new u-boot form.
func TestUbootPlanKeepsTheAppendOnlyGuards(t *testing.T) {
	e := loadEcw336(t)
	if _, err := e.PlanSetUboot(CloudIDVar, ""); !errors.Is(err, ErrEmptyValue) {
		t.Error("empty value must be refused (it deletes the variable)")
	}
	if _, err := e.PlanSetUboot("", "3"); !errors.Is(err, ErrEmptyKey) {
		t.Error("empty key must be refused")
	}
}
