// Package band provisions device identity — like ringing a bird.
//
// It mirrors the Rust `quarry` Code27 math in pure Go (so `pelegrun` needs no
// subprocess) and adds a collision-checked local inventory so every physical AP
// gets a unique, valid serial (Constitution: fleet identity is 1:1, fail closed).
package band

import (
	"errors"
	"fmt"
	"strings"
)

// Code27 alphabet: index 0..27 -> check character.
const Code27 = "1DK3R5FPWME47GTX8VL2J6C9NQH"

const (
	SerialLen  = 12 // 11 body + 1 check char
	SnextraLen = 20 // u-boot "extra serial" (config field 19)
)

var (
	ErrLength = errors.New("wrong length")
	ErrChar   = errors.New("disallowed character")
)

func alnum(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

// CheckChar computes the Code27 check character over an 11-char body.
func CheckChar(body string) (byte, error) {
	if len(body) != SerialLen-1 {
		return 0, fmt.Errorf("%w: body must be %d chars", ErrLength, SerialLen-1)
	}
	if !alnum(body) {
		return 0, ErrChar
	}
	sum := 0
	for i := 0; i < len(body); i++ {
		sum += int(body[i])
	}
	return Code27[sum%27], nil
}

// MakeSerial builds a valid 12-char serial from prefix(4)+model(3)+suffix(4).
func MakeSerial(prefix4, model3, suffix4 string) (string, error) {
	if len(prefix4) != 4 || len(model3) != 3 || len(suffix4) != 4 {
		return "", fmt.Errorf("%w: need 4+3+4", ErrLength)
	}
	body := prefix4 + model3 + suffix4
	c, err := CheckChar(body)
	if err != nil {
		return "", err
	}
	return body + string(c), nil
}

// ValidateSerial reports whether s is a 12-char serial with a matching check char.
func ValidateSerial(s string) bool {
	if len(s) != SerialLen {
		return false
	}
	c, err := CheckChar(s[:SerialLen-1])
	return err == nil && s[SerialLen-1] == c
}

// ModelCode returns the 3-char model code (positions 5-7) of a serial/snextra.
func ModelCode(s string) (string, error) {
	if len(s) < 7 {
		return "", ErrLength
	}
	return s[4:7], nil
}

// ModelCodes maps a 3-char model code (serial positions 5-7) to its model.
// Mirrors quarry's serial::MODEL_CODES.
//
// The codes are NOT all X-prefixed: the ap-hk07 line uses X4n, which made `X__`
// look like the rule, but the ECW336's code is E4T (read off a real unit's env
// snextra and matching the EPC model table). Nothing may assume a leading X.
var ModelCodes = map[string]string{
	"X41": "ECW230",
	"X42": "ECW230v3",
	"X44": "EWS377AP v3",
	"X45": "EWS377-FIT",
	"E4T": "ECW336",
}

// ModelName returns the model a code names, and whether the code is one we have
// actually seen on hardware. Unknown codes return false rather than a guess.
func ModelName(code string) (string, bool) {
	name, ok := ModelCodes[strings.ToUpper(code)]
	return name, ok
}

// MakeSnextra builds a 20-char field-19 value with model3 at positions 5-7,
// zero-padded. prefix defaults to "SWLW".
func MakeSnextra(prefix, model3 string) (string, error) {
	if len(model3) != 3 {
		return "", fmt.Errorf("%w: model code must be 3 chars", ErrLength)
	}
	if prefix == "" {
		prefix = "SWLW"
	}
	if len(prefix) != 4 || !alnum(prefix) {
		return "", ErrChar
	}
	s := prefix + model3
	return s + strings.Repeat("0", SnextraLen-len(s)), nil
}
