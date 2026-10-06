package hood

import (
	"os"
	"strings"
	"testing"
)

const good = `bootcmd=bootipq
active_fw=0
app_part=0
rootfsname=rootfs
ethaddr=00:11:22:33:44:55
snextra=EPC1X420000000000000
`

func TestParseAndComplete(t *testing.T) {
	e := ParsePrintenv(good)
	if !e.IsComplete() {
		t.Fatalf("expected complete, missing %v", e.Missing())
	}
	if e["bootcmd"] != "bootipq" {
		t.Fatalf("bootcmd = %q", e["bootcmd"])
	}
}

func TestIncompleteMissing(t *testing.T) {
	e := ParsePrintenv("bootcmd=bootipq\nethaddr=x\n")
	if e.IsComplete() {
		t.Fatal("should be incomplete")
	}
	got := e.Missing()
	// active_fw, app_part missing
	if len(got) != 2 {
		t.Fatalf("missing = %v", got)
	}
}

func TestPlanSetAppendOnly(t *testing.T) {
	e := ParsePrintenv(good)
	cmd, err := e.PlanSet("snextra", "EPC1X420000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "fw_setenv snextra EPC1X420000000000000" {
		t.Fatalf("cmd = %q", cmd)
	}
}

func TestPlanSetRefusesEmptyValue(t *testing.T) {
	e := ParsePrintenv(good)
	if _, err := e.PlanSet("snextra", ""); err != ErrEmptyValue {
		t.Fatalf("want ErrEmptyValue, got %v", err) // empty value = delete = brick vector
	}
}

func TestPlanSetRefusesIncompleteEnv(t *testing.T) {
	e := ParsePrintenv("ethaddr=x\n")
	if _, err := e.PlanSet("snextra", "y"); err == nil {
		t.Fatal("must refuse writes on an incomplete env")
	}
}

func TestVerifyAfterSet(t *testing.T) {
	e := ParsePrintenv(good)
	if err := VerifyAfterSet(e, "snextra", "EPC1X420000000000000"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAfterSet(e, "snextra", "WRONG"); err == nil {
		t.Fatal("should fail on mismatch")
	}
}

// Real EWS377-FIT env (u-boot 2016.01 V2.1.0, identity values masked): healthy,
// boots stock + OpenWrt, and has NO rootfsname. It must not be rejected (#2).
func TestFITEnvIsComplete(t *testing.T) {
	data, err := os.ReadFile("testdata/ews377fit-printenv.txt")
	if err != nil {
		t.Fatal(err)
	}
	e := ParsePrintenv(string(data))
	if !e.IsComplete() {
		t.Fatalf("healthy FIT env rejected, missing %v", e.Missing())
	}
	if _, ok := e["rootfsname"]; ok {
		t.Fatal("fixture must model a unit without rootfsname")
	}
	if _, err := e.PlanSet("snextra", "X"); err != nil {
		t.Fatalf("append on a healthy FIT env must be allowed: %v", err)
	}
	if e.LooksWiped() {
		t.Fatal("healthy env must not look wiped")
	}
	// Strict (u-boot 2.0.0) profile still demands rootfsname.
	if got := e.MissingFor(ProfileLegacy); len(got) != 1 || got[0] != "rootfsname" {
		t.Fatalf("legacy profile missing = %v", got)
	}
}

func TestWipedVsIdentityBearing(t *testing.T) {
	wiped := ParsePrintenv("bootcmd=bootp\nbaudrate=115200\n")
	if !wiped.LooksWiped() || wiped.HasIdentity() {
		t.Fatal("default env should look wiped")
	}
	damaged := ParsePrintenv("ethaddr=88:DC:97:00:00:00\nhw_id=0101012B\nbootcmd=bootipq\n")
	if damaged.LooksWiped() || !damaged.HasIdentity() {
		t.Fatal("env with identity vars is not wiped")
	}
}

// `env default -a` erases identity vars; the advice must never recommend it
// as a generic fix for an env that still holds them (#2).
func TestRecoveryNeverBlanketDefaultAll(t *testing.T) {
	e := ParsePrintenv("ethaddr=88:DC:97:00:00:00\nhw_id=0101012B\n")
	r := e.Recovery()
	if !strings.Contains(r, "do NOT run `env default -a`") || !strings.Contains(r, "backup") {
		t.Fatalf("unsafe recovery advice: %q", r)
	}
	_, err := e.PlanSet("snextra", "y")
	if err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("PlanSet error should point at the backup: %v", err)
	}
}
