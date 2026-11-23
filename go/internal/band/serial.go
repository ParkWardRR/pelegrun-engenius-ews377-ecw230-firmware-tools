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
	SerialLen  = 12                // 11 body + 1 check char
	SnextraLen = 20                // u-boot "extra serial" (config field 19)
	SeqSpace   = 27 * 27 * 27 * 27 // sequence numbers that fit positions 8-11 (531441)
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
	"C21": "ECW120",
	"C2W": "ECW115",
	"C41": "ECW130",
	"C42": "EWS375-FIT",
	"CC2": "ENH1350EXT",
	"CCE": "ECW160",
	"E4T": "ECW336",
	"G21": "EWS7928P-FIT",
	"G2F": "EWS7928FP-FIT",
	"G41": "EWS7952P-FIT",
	"G4F": "EWS7952FP-FIT",
	"G81": "ECS1008P",
	"G9F": "ECS1112FP",
	"GC1": "EWS2910P-FIT",
	"GCF": "EWS2910FP-FIT",
	"H21": "ECS1528P",
	"H22": "ECS1528",
	"H25": "ECS1528T",
	"H2F": "ECS1528FP",
	"H41": "ECS1552P",
	"H42": "ECS1552",
	"H4F": "ECS1552FP",
	"J52": "ECS205L",
	"J82": "ECS208L",
	"J92": "ECS2512",
	"J9F": "ECS2512FP",
	"L4Q": "ESG610",
	"L62": "ECS5512F",
	"L8J": "ESG620",
	"L92": "ECS5512",
	"L9F": "ECS5512FP",
	"M2F": "ECS2528FP",
	"M4F": "ECS2552FP",
	"N4Q": "ESG510",
	"P4G": "ESG320M",
	"P4H": "ESG320",
	"RC2": "EWS2510-FIT",
	"RCF": "ECS2510FP",
	"V21": "ECW220S",
	"V41": "ECW230S",
	"W21": "ECW536",
	"W22": "ECW516L",
	"W23": "ECW510",
	"W24": "ECW520",
	"W25": "ECW515L",
	"W41": "ECW526",
	"X21": "ECW220",
	"X22": "ECW220v2",
	"X23": "EWS357APv2",
	"X24": "EWS357APv3",
	"X25": "EWS356-FIT",
	"X26": "EWS355AP",
	"X27": "ECW220v3",
	"X28": "EWS357APv4",
	"X29": "EWS357-FIT",
	"X2D": "ECW201L-POE",
	"X2E": "ECW201L-AC",
	"X2L": "ECW210L",
	"X2W": "ECW215",
	"X41": "ECW230",
	"X42": "ECW230v3",
	"X43": "EWS377APv2",
	"X44": "EWS377APv3",
	"X45": "EWS377-FIT",
	"X46": "EWS276-FIT",
	"X4L": "ECW212L",
	"XC2": "EWS850AP",
	"XC3": "EWS850-FIT",
	"XC4": "ENH500-AX",
	"XC5": "EnStation6",
	"XC6": "ENS621EXT",
	"XCE": "ECW260",
	"XDE": "ECW270",
}

// ModelName returns the model a code names, and whether the code is one we have
// actually seen on hardware. Unknown codes return false rather than a guess.
func ModelName(code string) (string, bool) {
	name, ok := ModelCodes[strings.ToUpper(code)]
	return name, ok
}

// Seq4 encodes a unit sequence number as the 4 base-27 digits the factory
// writes at serial positions 8-11, most significant digit first.
//
// Mirrors sn_seq_convert() in the stock firmware's /usr/sbin/snconverter.sh,
// wrap included: the vendor takes sequence % 531441 rather than erroring,
// because the legacy 6-digit decimal field outruns the 27^4 space. Index 0 of
// Code27 is the character '1', so sequence 1 is "111D" and sequence 0 "1111".
func Seq4(n uint32) string {
	n %= SeqSpace
	b := make([]byte, 4)
	for i, p := range [4]uint32{19683, 729, 27, 1} {
		b[i] = Code27[(n/p)%27]
	}
	return string(b)
}

// MakeSnextra builds a 20-char field-19 value: a complete 12-char serial (check
// character included) followed by eight '*', which is the shape real FIT units
// hold. prefix defaults to "SWLW".
//
// It used to pad prefix+model with '0' to 20 chars, which put no serial and no
// check character in the field; the stock firmware's own /usr/sbin/sncheck.sh
// reports NG on a value like that.
func MakeSnextra(prefix, model3, suffix4 string) (string, error) {
	if len(model3) != 3 {
		return "", fmt.Errorf("%w: model code must be 3 chars", ErrLength)
	}
	if prefix == "" {
		prefix = "SWLW"
	}
	if len(prefix) != 4 || !alnum(prefix) {
		return "", ErrChar
	}
	serial, err := MakeSerial(prefix, model3, suffix4)
	if err != nil {
		return "", err
	}
	return serial + strings.Repeat("*", SnextraLen-SerialLen), nil
}
