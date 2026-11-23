package band

import "testing"

// The model code is NOT always `X`+2: the ap-hk07 line's X4n codes made that
// look like a rule, and the ECW336's code is E4T.
func TestModelNameIncludesTheEcw336(t *testing.T) {
	for code, want := range map[string]string{
		"E4T": "ECW336",
		"e4t": "ECW336",
		"X42": "ECW230v3",
		"X45": "EWS377-FIT",
	} {
		got, ok := ModelName(code)
		if !ok || got != want {
			t.Errorf("ModelName(%q) = %q, %t; want %q, true", code, got, ok, want)
		}
	}
	// An unrecognised code is reported as unknown, never guessed.
	if got, ok := ModelName("ZZZ"); ok || got != "" {
		t.Errorf("ModelName(\"ZZZ\") = %q, %t; want \"\", false", got, ok)
	}
}

// The ECW336's serial uses the same Code27 math as the rest of the line —
// confirmed against a real unit's env; this is a synthetic stand-in, because
// the real serial is identity material (the controller pre-shared key is
// derived from mac+serial).
func TestEcw336SerialRoundTrips(t *testing.T) {
	s, err := MakeSerial("2290", "E4T", "0001")
	if err != nil {
		t.Fatalf("MakeSerial: %v", err)
	}
	if !ValidateSerial(s) {
		t.Errorf("%q should validate", s)
	}
	code, err := ModelCode(s)
	if err != nil || code != "E4T" {
		t.Errorf("ModelCode(%q) = %q, %v; want E4T", s, code, err)
	}
	if name, ok := ModelName(code); !ok || name != "ECW336" {
		t.Errorf("code %q resolved to %q, %t", code, name, ok)
	}
}
