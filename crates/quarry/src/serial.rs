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

/// Build a 20-char `snextra` (u-boot config field 19) with the given 3-char model
/// code at positions 5–7, zero-padded. This is the value the cloud firmware reads
/// via `setconfig -g 19` and sends as `sn=` at check-in.
///
/// The prefix defaults to `"SWLW"` when empty. NOTE: `snextra` is a raw 20-char
/// field; the Code27 check character applies to the 12-char serial form, not here.
pub fn make_snextra(prefix: &str, model_code3: &str) -> Result<String, Error> {
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
    let mut s = String::with_capacity(SNEXTRA_LEN);
    s.push_str(prefix);
    s.push_str(model_code3);
    while s.len() < SNEXTRA_LEN {
        s.push('0');
    }
    Ok(s)
}

/// True if `s` is a plausible 20-char `snextra` (correct length, ASCII-alnum
/// with optional trailing `*` padding, and the model code at 5–7 is extractable).
///
/// Real FIT units store `snextra` as the 12-char serial followed by 8 `*`
/// (12 + 8 = 20), so trailing `*` is accepted; `*` anywhere inside the body is not.
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
        let x = make_snextra("EPC1", "X42").unwrap();
        assert_eq!(x.len(), SNEXTRA_LEN);
        assert_eq!(&x[4..7], "X42");
        assert!(validate_snextra(&x));
        assert!(!validate_snextra("EPC1X42")); // too short
        assert!(!validate_snextra("EPC1X420000000000!0")); // bad char
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

    #[test]
    fn make_serial_length_guards() {
        assert!(make_serial("EPC", "X42", "0001").is_err());
        assert!(make_serial("EPC1", "X4", "0001").is_err());
    }
}
