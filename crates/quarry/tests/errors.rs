//! Error-path and display coverage for quarry's public surface.

use quarry::header::Product;
use quarry::serial::{make_serial, make_snextra, model_code, validate_serial, validate_snextra};
use quarry::Error;

#[test]
fn error_display_is_human_readable() {
    let cases: Vec<(Error, &str)> = vec![
        (Error::TooShort { need: 152, got: 8 }, "too short"),
        (Error::BadMagic { got: 0xdead_beef }, "magic"),
        (
            Error::BadLength {
                field: "serial body",
                want: 11,
                got: 3,
            },
            "serial body",
        ),
        (Error::BadChar { field: "prefix" }, "prefix"),
    ];
    for (e, needle) in cases {
        let s = e.to_string();
        assert!(s.contains(needle), "{s:?} should contain {needle:?}");
    }
    // The magic message renders as hex.
    assert!(Error::BadMagic { got: 0x1234_5678 }
        .to_string()
        .contains("0x12345678"));
}

#[test]
fn product_id_label_roundtrip() {
    for (id, label) in [
        (182u32, "EWS377AP v2"),
        (282, "EWS377AP v3"),
        (300, "EWS377-FIT"),
        (275, "ECW230"),
        (284, "ECW230v3"),
        (285, "ECW230S"),
        (291, "ECW336"),
    ] {
        let p = Product::from_id(id);
        assert_eq!(p.id(), id);
        assert_eq!(p.label(), label);
    }
    // Unknown ids round-trip through Other and label as "unknown".
    let other = Product::from_id(999);
    assert_eq!(other, Product::Other(999));
    assert_eq!(other.id(), 999);
    assert_eq!(other.label(), "unknown");
}

#[test]
fn serial_length_and_char_errors() {
    assert!(matches!(
        make_serial("EPC", "X42", "0001"),
        Err(Error::BadLength { .. })
    ));
    assert!(matches!(
        make_serial("EPC1", "X4", "0001"),
        Err(Error::BadLength { .. })
    ));
    // model_code needs at least 7 chars.
    assert!(matches!(model_code("EPC1X4"), Err(Error::BadLength { .. })));
}

#[test]
fn snextra_errors_and_defaulting() {
    // Wrong model-code length.
    assert!(matches!(
        make_snextra("SWLW", "XX", "0001"),
        Err(Error::BadLength { .. })
    ));
    // Non-alnum prefix.
    assert!(matches!(
        make_snextra("BAD!", "X42", "0001"),
        Err(Error::BadChar { .. })
    ));
    // Wrong sequence-field length.
    assert!(matches!(
        make_snextra("SWLW", "X42", "001"),
        Err(Error::BadLength { .. })
    ));
    // Empty prefix defaults to SWLW.
    let x = make_snextra("", "X42", "0001").unwrap();
    assert_eq!(&x[..4], "SWLW");
    assert!(validate_snextra(&x));
    // The field is a whole serial plus `*` padding, never zero padding.
    assert!(x.ends_with("********"));
    assert!(validate_serial(&x[..12]));
    // Too-short / bad-char snextra fail validation.
    assert!(!validate_snextra("SWLWX42"));
    assert!(!validate_snextra("SWLWX420000000000!00"));
}
