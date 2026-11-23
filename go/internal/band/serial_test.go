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
