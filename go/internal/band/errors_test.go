package band

import (
	"errors"
	"testing"
)

func TestCheckCharErrors(t *testing.T) {
	if _, err := CheckChar("tooshort"); !errors.Is(err, ErrLength) {
		t.Errorf("short body: want ErrLength, got %v", err)
	}
	// 11 chars but with a disallowed character.
	if _, err := CheckChar("EPC1X42000!"); !errors.Is(err, ErrChar) {
		t.Errorf("bad char: want ErrChar, got %v", err)
	}
}

func TestMakeSerialErrors(t *testing.T) {
	for _, c := range [][3]string{
		{"EPC", "X42", "0001"}, // short prefix
		{"EPC1", "X4", "0001"}, // short model
		{"EPC1", "X42", "001"}, // short suffix
	} {
		if _, err := MakeSerial(c[0], c[1], c[2]); !errors.Is(err, ErrLength) {
			t.Errorf("MakeSerial%v: want ErrLength, got %v", c, err)
		}
	}
	// Right lengths but a disallowed char propagates ErrChar from CheckChar.
	if _, err := MakeSerial("EPC1", "X4!", "0001"); !errors.Is(err, ErrChar) {
		t.Errorf("bad-char serial: want ErrChar, got %v", err)
	}
}

func TestValidateSerialRejectsShapes(t *testing.T) {
	if ValidateSerial("EPC1X420001") { // 11 chars
		t.Error("11-char serial validated")
	}
	if ValidateSerial("EPC1X42000!X") { // bad char in body
		t.Error("bad-char serial validated")
	}
}

func TestModelCodeAndSnextraErrors(t *testing.T) {
	if _, err := ModelCode("EPC1X4"); !errors.Is(err, ErrLength) {
		t.Errorf("short ModelCode: want ErrLength, got %v", err)
	}
	if _, err := MakeSnextra("SWLW", "XX", "0001"); !errors.Is(err, ErrLength) {
		t.Errorf("short model: want ErrLength, got %v", err)
	}
	if _, err := MakeSnextra("BAD!", "X42", "0001"); !errors.Is(err, ErrChar) {
		t.Errorf("bad prefix: want ErrChar, got %v", err)
	}
	// Empty prefix defaults to SWLW.
	x, err := MakeSnextra("", "X42", "0001")
	if err != nil || x[:4] != "SWLW" {
		t.Errorf("default prefix: got %q err %v", x, err)
	}
	// A wrong-length sequence field is a length error, not silent truncation.
	if _, err := MakeSnextra("SWLW", "X42", "001"); !errors.Is(err, ErrLength) {
		t.Errorf("short suffix: want ErrLength, got %v", err)
	}
}
