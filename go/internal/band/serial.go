// Package band provisions device identity — like ringing a bird.
//
// It mirrors the Rust `quarry` Code27 math in pure Go (so `pelegrun` needs no
// subprocess) and adds a collision-checked local inventory so every physical AP
// gets a unique, valid serial (Constitution: fleet identity is 1:1, fail closed).
package band

import (
	"errors"
	"fmt"
	"strconv"
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

// LegacyModelCodes is the vendor's own legacy product-name -> code table,
// transcribed from modelCode() in /usr/sbin/snconverter.sh (byte-identical in
// Cloud6E ECW336 1.8.118-12 and Fit6 EWS377-FIT 1.1.65-2 images). It covers
// only the ezMaster-era products whose 9-char serials were migrated to the
// current format, which is why half of these have no ModelCodes entry.
//
// Deliberately name -> code only: two of its codes were later reassigned (X25
// was EWS330AP, the controller now says EWS356-FIT; CCE was ENS620EXT, now
// ECW160), so the reverse direction would give a code a second, contradictory
// name. Where both tables know a name they agree on its code (test-enforced).
var LegacyModelCodes = map[string]string{
	"EWS357AP":   "X23",
	"EWS357APV2": "X23",
	"EWS357APV3": "X24",
	"EWS357APV4": "X28",
	"EWS850AP":   "XC2",
	"EWS377AP":   "X43",
	"EWS377APV2": "X43",
	"EWS377APV3": "X44",
	"EWS330AP":   "X25",
	"EWS355AP":   "X26",
	"ENH1350EXT": "CC2",
	"ENS620EXT":  "CCE",
}

// FactoryDigit is serial position 4: '0' on every unit and image seen so far,
// and hardcoded in snconverter.sh.
const FactoryDigit = '0'

// CodeForModelName returns the 3-char code for a product name, current table
// first then the legacy one, and whether it resolved. Case-insensitive.
//
// Safe in a way the reverse direction is not: distinct names may share a code
// (one board, two series), but no name maps to two different codes.
func CodeForModelName(name string) (string, bool) {
	up := strings.ToUpper(name)
	for code, n := range ModelCodes {
		if strings.ToUpper(n) == up {
			return code, true
		}
	}
	if code, ok := LegacyModelCodes[up]; ok {
		return code, true
	}
	return "", false
}

// ShapeNotes lists the reasons a serial does not look factory-made. An empty
// slice means it matches the vendor's layout: YY + month (1-9 then A/B/C) +
// FactoryDigit + model code + 4 base-27 sequence digits.
//
// Only the check character ever *validates* a serial, so a hand-built one
// passes ValidateSerial while still being obviously synthetic. These notes are
// advisory -- report them, do not refuse on them.
func ShapeNotes(serial string) []string {
	if len(serial) != SerialLen {
		return []string{"not 12 characters, so the factory layout cannot be read"}
	}
	var notes []string
	digit := func(b byte) bool { return b >= '0' && b <= '9' }
	if !digit(serial[0]) || !digit(serial[1]) {
		notes = append(notes, "positions 1-2 are not a 2-digit year")
	}
	m := serial[2]
	if !((m >= '1' && m <= '9') || (m >= 'A' && m <= 'C') || (m >= 'a' && m <= 'c')) {
		notes = append(notes, "position 3 is not a month (1-9 then A/B/C for Oct-Dec)")
	}
	if serial[3] != FactoryDigit {
		notes = append(notes, "position 4 is not the factory digit 0")
	}
	for i := 7; i < 11; i++ {
		if !strings.ContainsRune(Code27, rune(upper(serial[i]))) {
			notes = append(notes, "positions 8-11 fall outside the Code27 alphabet, so the sequence was not machine-generated")
			break
		}
	}
	return notes
}

func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}

// ConvertLegacySerial re-encodes a legacy 9-char serial into the current
// 12-char format the way /usr/sbin/snconverter.sh does: keep the 3 date
// characters, insert the factory digit, place the target model code, re-encode
// the decimal sequence as 4 base-27 digits, append the check character.
//
// The legacy format is YY + month (1-9, A-C) + a 6-digit decimal sequence.
// Unlike the vendor's script -- which emits the code XXX for an unknown name --
// this fails closed; resolve names with CodeForModelName first.
func ConvertLegacySerial(old9, model3 string) (string, error) {
	if len(old9) != 9 {
		return "", fmt.Errorf("%w: legacy serial must be 9 chars", ErrLength)
	}
	digit := func(b byte) bool { return b >= '0' && b <= '9' }
	m := old9[2]
	ok := digit(old9[0]) && digit(old9[1]) &&
		((m >= '1' && m <= '9') || (m >= 'A' && m <= 'C') || (m >= 'a' && m <= 'c'))
	for i := 3; i < 9 && ok; i++ {
		ok = digit(old9[i])
	}
	if !ok {
		return "", fmt.Errorf("%w: legacy serial wants YY + month 1-9/A-C + 6 decimal digits", ErrChar)
	}
	seq, err := strconv.ParseUint(old9[3:9], 10, 32)
	if err != nil {
		return "", fmt.Errorf("%w: legacy sequence: %v", ErrChar, err)
	}
	return MakeSerial(old9[:3]+string(FactoryDigit), model3, Seq4(uint32(seq)))
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
