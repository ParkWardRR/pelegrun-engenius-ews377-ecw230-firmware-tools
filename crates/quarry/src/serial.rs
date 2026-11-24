//! Code27 serial math and the 20-char `snextra` helper.
//!
//! A cloud/FIT device is identified by a serial whose **3-char model code** sits
//! at positions 5–7 (0-indexed `4..7`). A controller maps that code to a model,
//! so a converted device must present a serial with the *target* model's code.

use crate::Error;

/// The Code27 alphabet — index `0..27` maps to a check character.
pub const CODE27: &[u8; 27] = b"1DK3R5FPWME47GTX8VL2J6C9NQH";

/// Length of a standard 12-char serial (11 body + 1 check char).
pub const SERIAL_LEN: usize = 12;
/// Length of the u-boot "extra serial" (`snextra`, config field 19).
pub const SNEXTRA_LEN: usize = 20;
/// How many sequence numbers fit the 4 base-27 digits at serial positions 8–11.
pub const SEQ_SPACE: u32 = 27 * 27 * 27 * 27; // 531441

/// Encode a unit sequence number as the 4 base-27 digits the factory writes at
/// serial positions 8–11, most significant digit first.
///
/// Mirrors `sn_seq_convert()` in the stock firmware's
/// `/usr/sbin/snconverter.sh`, wrap included: the vendor takes
/// `sequence % 531441` rather than erroring, because the legacy 6-digit decimal
/// field (max 999999) outruns the 27⁴ space.
///
/// Index 0 of [`CODE27`] is the character `1`, so sequence 1 encodes as `111D`
/// and sequence 0 as `1111`.
pub fn seq4(n: u32) -> String {
    let n = n % SEQ_SPACE;
    [19683u32, 729, 27, 1]
        .iter()
        .map(|p| CODE27[((n / p) % 27) as usize] as char)
        .collect()
}

/// Compute the Code27 check character over an 11-char body.
///
/// `check = CODE27[(sum of the body's byte values) % 27]`.
pub fn check_char(body: &str) -> Result<char, Error> {
    if body.len() != SERIAL_LEN - 1 {
        return Err(Error::BadLength {
            field: "serial body",
            want: SERIAL_LEN - 1,
            got: body.len(),
        });
    }
    if !body.bytes().all(|b| b.is_ascii_alphanumeric()) {
        return Err(Error::BadChar {
            field: "serial body",
        });
    }
    let sum: usize = body.bytes().map(|b| b as usize).sum();
    Ok(CODE27[sum % 27] as char)
}

/// Build a valid 12-char serial from a 4-char prefix, 3-char model code, and
/// 4-char suffix, appending the Code27 check character.
pub fn make_serial(prefix4: &str, model_code3: &str, suffix4: &str) -> Result<String, Error> {
    if prefix4.len() != 4 {
        return Err(Error::BadLength {
            field: "prefix",
            want: 4,
            got: prefix4.len(),
        });
    }
    if model_code3.len() != 3 {
        return Err(Error::BadLength {
            field: "model_code",
            want: 3,
            got: model_code3.len(),
        });
    }
    if suffix4.len() != 4 {
        return Err(Error::BadLength {
            field: "suffix",
            want: 4,
            got: suffix4.len(),
        });
    }
    let body = format!("{prefix4}{model_code3}{suffix4}");
    let c = check_char(&body)?;
    Ok(format!("{body}{c}"))
}

/// True if `s` is a well-formed 12-char serial with a matching check character.
pub fn validate_serial(s: &str) -> bool {
    if s.len() != SERIAL_LEN {
        return false;
    }
    let (body, tail) = s.split_at(SERIAL_LEN - 1);
    match check_char(body) {
        Ok(c) => tail.starts_with(c),
        Err(_) => false,
    }
}

/// Known 3-char model codes (serial positions 5–7) and the model each maps to.
///
/// **The codes are not all `X`-prefixed.** The ap-hk07 line happens to use
/// `X4n`, which made `X__` look like the rule; the ECW336's code is `E4T`
/// (read off a real unit's env `snextra`, and matching the EPC model table), so
/// nothing here may assume a leading `X`.
///
/// Source: an EnGenius Private Cloud 1.10.0 `models` collection
/// (`/app/default-data/models.json` in `epc-api`) — the controller's own values,
/// not guesses. `X42`, `X44`, `X45` and `E4T` are additionally confirmed against
/// real hardware serials.
///
/// **A code is not unique across eras.** The stock firmware's legacy
/// `snconverter.sh` table names `X25` EWS330AP and `CCE` ENS620EXT, where the
/// controller now says EWS356-FIT and ECW160. `CCE` is one board under two
/// series names; `X25` is a retire-and-reuse. Treat a lookup here as "what a
/// current controller thinks", not "what this code has always meant".
pub const MODEL_CODES: &[(&str, &str)] = &[
    ("C21", "ECW120"),
    ("C2W", "ECW115"),
    ("C41", "ECW130"),
    ("C42", "EWS375-FIT"),
    ("CC2", "ENH1350EXT"),
    ("CCE", "ECW160"),
    ("E4T", "ECW336"),
    ("G21", "EWS7928P-FIT"),
    ("G2F", "EWS7928FP-FIT"),
    ("G41", "EWS7952P-FIT"),
    ("G4F", "EWS7952FP-FIT"),
    ("G81", "ECS1008P"),
    ("G9F", "ECS1112FP"),
    ("GC1", "EWS2910P-FIT"),
    ("GCF", "EWS2910FP-FIT"),
    ("H21", "ECS1528P"),
    ("H22", "ECS1528"),
    ("H25", "ECS1528T"),
    ("H2F", "ECS1528FP"),
    ("H41", "ECS1552P"),
    ("H42", "ECS1552"),
    ("H4F", "ECS1552FP"),
    ("J52", "ECS205L"),
    ("J82", "ECS208L"),
    ("J92", "ECS2512"),
    ("J9F", "ECS2512FP"),
    ("L4Q", "ESG610"),
    ("L62", "ECS5512F"),
    ("L8J", "ESG620"),
    ("L92", "ECS5512"),
    ("L9F", "ECS5512FP"),
    ("M2F", "ECS2528FP"),
    ("M4F", "ECS2552FP"),
    ("N4Q", "ESG510"),
    ("P4G", "ESG320M"),
    ("P4H", "ESG320"),
    ("RC2", "EWS2510-FIT"),
    ("RCF", "ECS2510FP"),
    ("V21", "ECW220S"),
    ("V41", "ECW230S"),
    ("W21", "ECW536"),
    ("W22", "ECW516L"),
    ("W23", "ECW510"),
    ("W24", "ECW520"),
    ("W25", "ECW515L"),
    ("W41", "ECW526"),
    ("X21", "ECW220"),
    ("X22", "ECW220v2"),
    ("X23", "EWS357APv2"),
    ("X24", "EWS357APv3"),
    ("X25", "EWS356-FIT"),
    ("X26", "EWS355AP"),
    ("X27", "ECW220v3"),
    ("X28", "EWS357APv4"),
    ("X29", "EWS357-FIT"),
    ("X2D", "ECW201L-POE"),
    ("X2E", "ECW201L-AC"),
    ("X2L", "ECW210L"),
    ("X2W", "ECW215"),
    ("X41", "ECW230"),
    ("X42", "ECW230v3"),
    ("X43", "EWS377APv2"),
    ("X44", "EWS377APv3"),
    ("X45", "EWS377-FIT"),
    ("X46", "EWS276-FIT"),
    ("X4L", "ECW212L"),
    ("XC2", "EWS850AP"),
    ("XC3", "EWS850-FIT"),
    ("XC4", "ENH500-AX"),
    ("XC5", "EnStation6"),
    ("XC6", "ENS621EXT"),
    ("XCE", "ECW260"),
    ("XDE", "ECW270"),
];
/// The model a 3-char code names, if it is one we have seen on hardware.
/// Case-insensitive; `None` for anything unrecognised (never a guess).
pub fn model_name(code: &str) -> Option<&'static str> {
    MODEL_CODES
        .iter()
        .find(|(c, _)| c.eq_ignore_ascii_case(code))
        .map(|(_, name)| *name)
}

/// The vendor's own **legacy** product-name → code table, transcribed from
/// `modelCode()` in `/usr/sbin/snconverter.sh` (byte-identical in Cloud6E
/// ECW336 1.8.118-12 and Fit6 EWS377-FIT 1.1.65-2 images). It covers only the
/// ezMaster-era products whose 9-char serials were migrated to the current
/// format, which is why half of these have no entry in [`MODEL_CODES`] at all.
///
/// **This is deliberately name → code, and nothing resolves a code to a name
/// through it.** Two of its codes have since been reassigned (`X25` was
/// EWS330AP, the controller now says EWS356-FIT; `CCE` was ENS620EXT, now
/// ECW160), so using it in the other direction would produce a second,
/// contradictory answer for a code. Where both tables know a *name* they agree
/// on its code — enforced by a test.
pub const LEGACY_MODEL_CODES: &[(&str, &str)] = &[
    ("EWS357AP", "X23"),
    ("EWS357APv2", "X23"),
    ("EWS357APv3", "X24"),
    ("EWS357APv4", "X28"),
    ("EWS850AP", "XC2"),
    ("EWS377AP", "X43"),
    ("EWS377APv2", "X43"),
    ("EWS377APv3", "X44"),
    ("EWS330AP", "X25"),
    ("EWS355AP", "X26"),
    ("ENH1350EXT", "CC2"),
    ("ENS620EXT", "CCE"),
];

/// The 3-char code for a product *name*, current table first, then the legacy
/// one. Case-insensitive; `None` rather than a guess.
///
/// Safe in a way the reverse direction is not: distinct names may share a code
/// (one board, two series), but no name maps to two different codes.
pub fn code_for_model_name(name: &str) -> Option<&'static str> {
    MODEL_CODES
        .iter()
        .find(|(_, n)| n.eq_ignore_ascii_case(name))
        .map(|(c, _)| *c)
        .or_else(|| {
            LEGACY_MODEL_CODES
                .iter()
                .find(|(n, _)| n.eq_ignore_ascii_case(name))
                .map(|(_, c)| *c)
        })
}

/// The factory digit at serial position 4. `0` on every unit and every image
/// seen so far — `snconverter.sh` hardcodes it.
pub const FACTORY_DIGIT: char = '0';

/// Why a serial does not look factory-made, as a list of notes. Empty means it
/// matches the vendor's layout.
///
/// A serial is only ever *validated* by its check character, so a hand-built one
/// passes [`validate_serial`] while still being obviously synthetic to anyone
/// who knows the layout: `YY` + month (`1`–`9`, `A`–`C`) + [`FACTORY_DIGIT`] +
/// model code + 4 base-27 sequence digits. These notes are advisory — a real
/// unit could in principle carry something unexpected — so callers should report
/// them, not refuse on them.
pub fn shape_notes(serial: &str) -> Vec<&'static str> {
    let mut notes = Vec::new();
    if serial.len() != SERIAL_LEN {
        notes.push("not 12 characters, so the factory layout cannot be read");
        return notes;
    }
    let b = serial.as_bytes();
    if !b[0].is_ascii_digit() || !b[1].is_ascii_digit() {
        notes.push("positions 1-2 are not a 2-digit year");
    }
    if !matches!(b[2], b'1'..=b'9' | b'A'..=b'C' | b'a'..=b'c') {
        notes.push("position 3 is not a month (1-9 then A/B/C for Oct-Dec)");
    }
    if b[3] != FACTORY_DIGIT as u8 {
        notes.push("position 4 is not the factory digit 0");
    }
    if !b[7..11]
        .iter()
        .all(|c| CODE27.contains(&c.to_ascii_uppercase()))
    {
        notes.push("positions 8-11 fall outside the Code27 alphabet, so the sequence was not machine-generated");
    }
    notes
}

/// Re-encode a **legacy 9-char serial** into the current 12-char format, the
/// way `/usr/sbin/snconverter.sh` does: keep the 3 date characters, insert the
/// factory digit, place the target model code, re-encode the decimal sequence as
/// 4 base-27 digits, append the check character.
///
/// The legacy format is `YY` + month (`1`–`9`, `A`–`C`) + a 6-digit decimal
/// sequence. Unlike the vendor's script — which emits the model code `XXX` for
/// any name it does not know — this fails closed; resolve names with
/// [`code_for_model_name`] first.
pub fn convert_legacy_serial(old9: &str, model_code3: &str) -> Result<String, Error> {
    if old9.len() != 9 {
        return Err(Error::BadLength {
            field: "legacy serial",
            want: 9,
            got: old9.len(),
        });
    }
    let b = old9.as_bytes();
    let date_ok = b[0].is_ascii_digit()
        && b[1].is_ascii_digit()
        && matches!(b[2], b'1'..=b'9' | b'A'..=b'C' | b'a'..=b'c');
    if !date_ok || !b[3..9].iter().all(u8::is_ascii_digit) {
        return Err(Error::BadChar {
            field: "legacy serial (want YY + month 1-9/A-C + 6 decimal digits)",
        });
    }
    let seq: u32 = old9[3..9].parse().map_err(|_| Error::BadChar {
        field: "legacy serial sequence",
    })?;
    let prefix = format!("{}{}", &old9[..3], FACTORY_DIGIT);
    make_serial(&prefix, model_code3, &seq4(seq))
}

/// The 3-char model code (positions 5–7) of a serial or `snextra`.
pub fn model_code(s: &str) -> Result<&str, Error> {
    if s.len() < 7 {
        return Err(Error::BadLength {
            field: "serial",
            want: 7,
            got: s.len(),
        });
    }
    Ok(&s[4..7])
}

/// Build a 20-char `snextra` (u-boot config field 19): a complete 12-char
/// serial — check character included — followed by eight `*`. This is the value
/// the cloud firmware reads via `setconfig -g 19` and sends as `sn=` at
/// check-in, and the shape real FIT units hold.
///
/// The prefix defaults to `"SWLW"` when empty.
///
/// It used to pad `prefix + code` with `0` to 20 chars, which put no serial and
/// no check character in the field; the stock firmware's own
/// `/usr/sbin/sncheck.sh` (which splits off the 12th character and compares)
/// reports `NG` on a value like that.
pub fn make_snextra(prefix: &str, model_code3: &str, suffix4: &str) -> Result<String, Error> {
    if model_code3.len() != 3 {
        return Err(Error::BadLength {
            field: "model_code",
            want: 3,
            got: model_code3.len(),
        });
    }
    let prefix = if prefix.is_empty() { "SWLW" } else { prefix };
    if prefix.len() != 4 || !prefix.bytes().all(|b| b.is_ascii_alphanumeric()) {
        return Err(Error::BadChar { field: "prefix" });
    }
    let serial = make_serial(prefix, model_code3, suffix4)?;
    let mut s = String::with_capacity(SNEXTRA_LEN);
    s.push_str(&serial);
    while s.len() < SNEXTRA_LEN {
        s.push('*');
    }
    Ok(s)
}

/// True if `s` is a plausible 20-char `snextra` (correct length, ASCII-alnum
/// with optional trailing `*` padding, and the model code at 5–7 is extractable).
///
/// Real FIT units store `snextra` as the 12-char serial followed by 8 `*`
/// (12 + 8 = 20), so trailing `*` is accepted; `*` anywhere inside the body is not.
///
/// **ECW336 caveat:** on the one unit whose env was captured, `snextra` read as
/// the bare 12-char serial and the `*` padding was not confirmed. Rather than
/// loosen the length rule on a single observation, validate an ECW336 env value
/// with [`validate_serial`] and read its code with [`model_code`] (which works
/// on any string of 7+ chars) instead of calling this.
pub fn validate_snextra(s: &str) -> bool {
    let body = s.trim_end_matches('*');
    s.len() == SNEXTRA_LEN
        && body.len() >= 7
        && body.bytes().all(|b| b.is_ascii_alphanumeric())
        && model_code(s).is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    // Verified on real hardware this project came from: EPC1X420001 -> check '1'.
    #[test]
    fn check_char_known_vector() {
        assert_eq!(check_char("EPC1X420001").unwrap(), '1');
    }

    #[test]
    fn validate_known_serial() {
        assert!(validate_serial("EPC1X4200011"));
        assert!(!validate_serial("EPC1X4200012")); // wrong check char
        assert!(!validate_serial("EPC1X420001")); // too short
    }

    #[test]
    fn make_and_validate_roundtrip() {
        let s = make_serial("EPC1", "X42", "0001").unwrap();
        assert_eq!(s, "EPC1X4200011");
        assert!(validate_serial(&s));
        assert_eq!(model_code(&s).unwrap(), "X42");
    }

    #[test]
    fn model_code_positions() {
        assert_eq!(model_code("EPC1X4200011").unwrap(), "X42"); // ECW230v3
        assert_eq!(model_code("AAAAX45BBBBc").unwrap(), "X45"); // EWS377-FIT
    }

    #[test]
    fn snextra_shape() {
        let x = make_snextra("EPC1", "X42", "0001").unwrap();
        assert_eq!(x.len(), SNEXTRA_LEN);
        assert_eq!(&x[4..7], "X42");
        assert!(validate_snextra(&x));
        assert!(!validate_snextra("EPC1X42")); // too short
        assert!(!validate_snextra("EPC1X420000000000!0")); // bad char
    }

    // What `snextra` emits has to be a *whole* serial plus `*` padding: the
    // device's own /usr/sbin/sncheck.sh splits off the 12th character and
    // compares it, so a field with no check character in it reads `NG`.
    #[test]
    fn snextra_contains_a_valid_serial_not_zero_padding() {
        let x = make_snextra("26A0", "X45", "111D").unwrap();
        assert_eq!(x, "26A0X45111DR********");
        assert!(validate_serial(&x[..SERIAL_LEN]));
        assert_eq!(x.matches('*').count(), SNEXTRA_LEN - SERIAL_LEN);
        // The old zero-padded shape is exactly what must no longer appear.
        assert_ne!(x, "26A0X450000000000000");
    }

    // sn_seq_convert() from the stock snconverter.sh: 4 base-27 digits, index 0
    // is the character `1`, and the vendor wraps at 27^4 instead of erroring.
    #[test]
    fn seq4_matches_the_vendor_encoder() {
        assert_eq!(seq4(0), "1111");
        assert_eq!(seq4(1), "111D");
        assert_eq!(seq4(26), "111H");
        assert_eq!(seq4(27), "11D1");
        assert_eq!(seq4(SEQ_SPACE), seq4(0)); // wraps, like `% 531441`
        assert_eq!(seq4(SEQ_SPACE + 1), seq4(1));
        // Every digit comes out of the Code27 alphabet, so `0`/`A`/`B` etc.
        // can never appear in a factory sequence field.
        for n in [0, 1, 999, 531440] {
            assert!(seq4(n).bytes().all(|b| CODE27.contains(&b)));
        }
    }

    // The vendor's own script, run on the ECW336's extracted rootfs, turns
    // `228001234 EWS377APv3` into `2280X441DL2L`. This is parity with that
    // output, not with our own re-derivation of it.
    #[test]
    fn legacy_conversion_matches_the_vendor_script() {
        assert_eq!(code_for_model_name("EWS377APv3"), Some("X44"));
        assert_eq!(
            convert_legacy_serial("228001234", "X44").unwrap(),
            "2280X441DL2L"
        );
        assert!(validate_serial(
            &convert_legacy_serial("228001234", "X44").unwrap()
        ));

        // Fails closed where the vendor emits the placeholder `XXX`.
        assert_eq!(code_for_model_name("ECW336"), Some("E4T")); // current table
        assert_eq!(code_for_model_name("NoSuchAP"), None);
        // Shape errors rather than silent truncation.
        assert!(convert_legacy_serial("22800123", "X44").is_err()); // 8 chars
        assert!(convert_legacy_serial("22X001234", "X44").is_err()); // bad month
        assert!(convert_legacy_serial("2280012A4", "X44").is_err()); // non-digit seq
    }

    // Name -> code is safe to merge across the two tables only while they never
    // disagree about a name. Code -> name is the direction that is ambiguous.
    #[test]
    fn legacy_and_current_tables_agree_on_every_shared_name() {
        for (name, legacy_code) in LEGACY_MODEL_CODES {
            if let Some((current_code, _)) = MODEL_CODES
                .iter()
                .find(|(_, n)| n.eq_ignore_ascii_case(name))
            {
                assert_eq!(
                    current_code, legacy_code,
                    "{name} is {legacy_code} in snconverter.sh but {current_code} in the EPC table"
                );
            }
        }
        // The known code-level collisions are *not* reachable through name lookup.
        assert_eq!(code_for_model_name("EWS330AP"), Some("X25"));
        assert_eq!(code_for_model_name("EWS356-FIT"), Some("X25"));
        assert_eq!(model_name("X25"), Some("EWS356-FIT")); // one answer, the current one
    }

    #[test]
    fn shape_notes_flag_hand_built_serials() {
        // A factory-shaped serial: 2026-10, factory digit 0, sequence 1.
        assert!(shape_notes("26A0X45111DR").is_empty());
        // What the old `--suffix 0001` default produced: valid, not factory-shaped.
        let hand = make_serial("SWLW", "X42", "0001").unwrap();
        assert!(validate_serial(&hand));
        let notes = shape_notes(&hand);
        assert_eq!(notes.len(), 4, "got {notes:?}");
        // The sequence-field note is the one that catches a minted serial whose
        // prefix happens to look like a date.
        let mixed = make_serial("26A0", "X45", "0001").unwrap();
        assert_eq!(shape_notes(&mixed).len(), 1);
        assert!(shape_notes(&mixed)[0].contains("Code27 alphabet"));
        assert_eq!(shape_notes("short").len(), 1);
    }

    // The worked migration example: one date, one sequence, three identities.
    #[test]
    fn migration_serials_differ_only_in_code_and_check_char() {
        let seq = seq4(1);
        let v3 = make_serial("26A0", "X44", &seq).unwrap();
        let fit = make_serial("26A0", "X45", &seq).unwrap();
        let ecw = make_serial("26A0", "X42", &seq).unwrap();
        assert_eq!(v3, "26A0X44111D3");
        assert_eq!(fit, "26A0X45111DR");
        assert_eq!(ecw, "26A0X42111DD");
        for s in [&v3, &fit, &ecw] {
            assert!(validate_serial(s));
            assert_eq!(&s[..4], "26A0"); // same date + factory digit
            assert_eq!(&s[7..11], "111D"); // same sequence
        }
    }

    // Structure of the first physical EWS377-FIT's serial (issue #5): YYWW + X45
    // + nnnn + Code27 check char, and an env `snextra` = serial + 8 `*`. This is a
    // synthetic, masked stand-in (the real serial is not published); it pins the
    // structure and that the FIT's model code is X45.
    #[test]
    fn fit_serial_structure_and_star_padded_snextra() {
        let serial = make_serial("2540", "X45", "0001").unwrap();
        assert!(validate_serial(&serial));
        assert_eq!(model_code(&serial).unwrap(), "X45");
        let snextra = format!("{serial}********");
        assert_eq!(snextra.len(), SNEXTRA_LEN);
        assert!(validate_snextra(&snextra));
        assert_eq!(model_code(&snextra).unwrap(), "X45");
        assert!(!validate_snextra("2540X4*00010000000000")); // `*` inside the body
        assert!(!validate_snextra(&"*".repeat(SNEXTRA_LEN)));
    }

    // The ECW336's model code is `E4T` — not `X__`. Its real serial lives in
    // the u-boot env `snextra` and is NOT published here (identity: the EPC
    // pre-shared key is derived from mac+serial); this is a synthetic stand-in
    // with the same shape. The Code27 check math was confirmed against the real
    // 12-char value on hardware, so no separate alphabet is needed for ECW.
    #[test]
    fn ecw336_model_code_is_not_x_prefixed() {
        assert_eq!(model_name("E4T"), Some("ECW336"));
        assert_eq!(model_name("e4t"), Some("ECW336"));
        assert_eq!(model_name("X45"), Some("EWS377-FIT"));
        assert_eq!(model_name("ZZZ"), None);

        let serial = make_serial("2290", "E4T", "0001").unwrap();
        assert!(validate_serial(&serial));
        assert_eq!(model_code(&serial).unwrap(), "E4T");
        // A bare 12-char serial is what an ECW336 env held: too short for
        // validate_snextra, but the model code is still readable.
        assert!(!validate_snextra(&serial));
        assert_eq!(model_code(&serial).unwrap(), "E4T");
        // And the padded form a 20-char field would carry.
        assert!(validate_snextra(&format!("{serial}********")));
    }

    #[test]
    fn make_serial_length_guards() {
        assert!(make_serial("EPC", "X42", "0001").is_err());
        assert!(make_serial("EPC1", "X4", "0001").is_err());
    }
}
