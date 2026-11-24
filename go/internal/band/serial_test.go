package band

import (
	"strings"
	"testing"
)

func TestCheckCharKnown(t *testing.T) {
	c, err := CheckChar("EPC1X420001")
	if err != nil || c != '1' {
		t.Fatalf("got %q err %v (want '1')", c, err)
	}
}

func TestSerialRoundtrip(t *testing.T) {
	s, err := MakeSerial("EPC1", "X42", "0001")
	if err != nil || s != "EPC1X4200011" {
		t.Fatalf("got %q err %v", s, err)
	}
	if !ValidateSerial(s) {
		t.Fatal("should validate")
	}
	if ValidateSerial("EPC1X4200012") {
		t.Fatal("bad check char should fail")
	}
	mc, _ := ModelCode(s)
	if mc != "X42" {
		t.Fatalf("model code %q", mc)
	}
}

func TestSnextra(t *testing.T) {
	x, err := MakeSnextra("EPC1", "X42", "0001")
	if err != nil || x != "EPC1X4200011********" || len(x) != SnextraLen {
		t.Fatalf("got %q err %v", x, err)
	}
	// field 19 must carry a whole serial, check character included: the
	// device's own /usr/sbin/sncheck.sh splits off char 12 and compares it.
	if !ValidateSerial(x[:SerialLen]) {
		t.Errorf("%q does not hold a valid serial", x)
	}
}

// Seq4 mirrors sn_seq_convert() in the stock /usr/sbin/snconverter.sh.
func TestSeq4MatchesTheVendorEncoder(t *testing.T) {
	for n, want := range map[uint32]string{0: "1111", 1: "111D", 26: "111H", 27: "11D1"} {
		if got := Seq4(n); got != want {
			t.Errorf("Seq4(%d) = %q, want %q", n, got, want)
		}
	}
	if Seq4(SeqSpace) != Seq4(0) || Seq4(SeqSpace+1) != Seq4(1) {
		t.Error("Seq4 must wrap modulo 27^4, like the vendor's % 531441")
	}
	for _, n := range []uint32{0, 1, 999, 531440} {
		for _, c := range Seq4(n) {
			if !strings.ContainsRune(Code27, c) {
				t.Errorf("Seq4(%d) = %q has %q, outside the Code27 alphabet", n, Seq4(n), c)
			}
		}
	}
}

// The worked migration example: one date, one sequence, three identities --
// only the model code and the check character differ.
func TestMigrationSerials(t *testing.T) {
	for code, want := range map[string]string{"X44": "26A0X44111D3", "X45": "26A0X45111DR", "X42": "26A0X42111DD"} {
		got, err := MakeSerial("26A0", code, Seq4(1))
		if err != nil || got != want {
			t.Errorf("MakeSerial(26A0, %s, 111D) = %q (%v), want %q", code, got, err, want)
		}
		if !ValidateSerial(got) {
			t.Errorf("%q should validate", got)
		}
	}
}

// The vendor's own script, run on the ECW336's extracted rootfs, turns
// `228001234 EWS377APv3` into `2280X441DL2L`. Parity with that output.
func TestLegacyConversionMatchesTheVendorScript(t *testing.T) {
	code, ok := CodeForModelName("EWS377APv3")
	if !ok || code != "X44" {
		t.Fatalf("CodeForModelName(EWS377APv3) = %q, %t; want X44, true", code, ok)
	}
	got, err := ConvertLegacySerial("228001234", code)
	if err != nil || got != "2280X441DL2L" {
		t.Fatalf("ConvertLegacySerial = %q (%v); want 2280X441DL2L", got, err)
	}
	if !ValidateSerial(got) {
		t.Errorf("%q should validate", got)
	}
	// Fails closed instead of emitting the vendor's XXX placeholder.
	if _, ok := CodeForModelName("NoSuchAP"); ok {
		t.Error("CodeForModelName should not guess")
	}
	for _, bad := range []string{"22800123", "22X001234", "2280012A4", ""} {
		if _, err := ConvertLegacySerial(bad, "X44"); err == nil {
			t.Errorf("ConvertLegacySerial(%q) should fail", bad)
		}
	}
}

// Name -> code is safe to merge across the two tables only while they never
// disagree about a name; code -> name is the ambiguous direction.
func TestLegacyAndCurrentTablesAgreeOnSharedNames(t *testing.T) {
	for name, legacy := range LegacyModelCodes {
		for code, current := range ModelCodes {
			if strings.EqualFold(current, name) && code != legacy {
				t.Errorf("%s is %s in snconverter.sh but %s in the EPC table", name, legacy, code)
			}
		}
	}
	// The code-level collisions stay unreachable through name lookup.
	if c, _ := CodeForModelName("EWS330AP"); c != "X25" {
		t.Errorf("EWS330AP = %q, want X25", c)
	}
	if c, _ := CodeForModelName("EWS356-FIT"); c != "X25" {
		t.Errorf("EWS356-FIT = %q, want X25", c)
	}
	if n, _ := ModelName("X25"); n != "EWS356-FIT" {
		t.Errorf("ModelName(X25) = %q, want the current answer EWS356-FIT", n)
	}
}

func TestShapeNotesFlagHandBuiltSerials(t *testing.T) {
	if n := ShapeNotes("26A0X45111DR"); len(n) != 0 {
		t.Errorf("factory-shaped serial flagged: %v", n)
	}
	// What the old --suffix 0001 default produced: valid, not factory-shaped.
	hand, _ := MakeSerial("SWLW", "X42", "0001")
	if !ValidateSerial(hand) {
		t.Fatal("precondition: hand-built serial should still validate")
	}
	if n := ShapeNotes(hand); len(n) != 4 {
		t.Errorf("ShapeNotes(%q) = %v; want 4 notes", hand, n)
	}
	// A date-shaped prefix with a minted sequence leaves just the one note.
	mixed, _ := MakeSerial("26A0", "X45", "0001")
	n := ShapeNotes(mixed)
	if len(n) != 1 || !strings.Contains(n[0], "Code27 alphabet") {
		t.Errorf("ShapeNotes(%q) = %v; want the sequence-field note", mixed, n)
	}
	if len(ShapeNotes("short")) != 1 {
		t.Error("a wrong-length serial should produce exactly one note")
	}
}
