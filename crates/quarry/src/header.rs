//! Senao image header: parse, validate, and one-field `product_id` re-head.
//!
//! All multi-byte fields are **big-endian**. Offsets are absolute file offsets,
//! verified on real EWS377AP v3 / EWS377-FIT / ECW230v3 images.

use crate::Error;

/// `vendor_id` (u32 BE). `257` across the ap-hk07 line.
pub const OFF_VENDOR_ID: usize = 0x04;
/// `product_id` (u32 BE) — **the one field the re-head patches**.
pub const OFF_PRODUCT_ID: usize = 0x08;
/// `firmware_type` (u32 BE) — `0` = combo image (has CAPWAP sub-header).
pub const OFF_FIRMWARE_TYPE: usize = 0x1C;
/// `md5sum` (16 bytes) — over the payload, unaffected by a header re-head.
pub const OFF_MD5SUM: usize = 0x28;
/// `chksum` (u32 BE) — vendor-proprietary; not read by the upgrade path.
pub const OFF_CHKSUM: usize = 0x58;
/// `magic` (u32 BE) — must be `0x12345678`.
pub const OFF_MAGIC: usize = 0x5C;
/// `model` string (ASCII, NUL/garbage-terminated).
pub const OFF_MODEL: usize = 0x88;

/// The Senao magic value.
pub const MAGIC: u32 = 0x1234_5678;

/// Smallest image length we can fully parse (covers the model string field).
pub const MIN_LEN: usize = OFF_MODEL + 16;

/// An 8-byte header XOR key (`mksenaofw -m`).
pub type Key = [u8; 8];

/// The key the OpenWrt `senao-header` build step uses for the per-SKU
/// `senao-factory.bin` images (`-m 783c9ecf67b359ac` in `ipq807x.mk`).
pub const DEFAULT_KEY: Key = [0x78, 0x3c, 0x9e, 0xcf, 0x67, 0xb3, 0x59, 0xac];

/// Keys tried automatically when the magic is not `0x12345678`.
pub const KNOWN_KEYS: &[Key] = &[DEFAULT_KEY];

/// Parse a `--key` value: 16 hex digits, optional `0x` prefix.
pub fn parse_key(s: &str) -> Result<Key, Error> {
    let h = s.trim().trim_start_matches("0x");
    if h.len() != 16 || !h.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err(Error::BadKey {
            reason: "expected 16 hex digits (8 bytes)".into(),
        });
    }
    let mut k = [0u8; 8];
    for (i, b) in k.iter_mut().enumerate() {
        *b = u8::from_str_radix(&h[i * 2..i * 2 + 2], 16).expect("validated hex");
    }
    Ok(k)
}

/// Known `product_id` values on the ap-hk07 board family.
///
/// The three cross-flash siblings are `282`/`300`/`284`. The other ids were read
/// from real vendor images (see `tests/real_images.rs`): `275` base ECW230,
/// `182` the older EWS377AP v2 / `ews377ap-all` line, and `285` ECW230S (a
/// related cloud AP with an extra scanning radio — labelled, but NOT a verified
/// cross-flash target).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Product {
    Ews377ApV2, // 182
    Ews377ApV3, // 282
    Ews377Fit,  // 300
    Ecw230,     // 275
    Ecw230V3,   // 284
    Ecw230S,    // 285
    Other(u32),
}

impl Product {
    pub fn from_id(id: u32) -> Self {
        match id {
            182 => Product::Ews377ApV2,
            282 => Product::Ews377ApV3,
            300 => Product::Ews377Fit,
            275 => Product::Ecw230,
            284 => Product::Ecw230V3,
            285 => Product::Ecw230S,
            other => Product::Other(other),
        }
    }
    pub fn id(self) -> u32 {
        match self {
            Product::Ews377ApV2 => 182,
            Product::Ews377ApV3 => 282,
            Product::Ews377Fit => 300,
            Product::Ecw230 => 275,
            Product::Ecw230V3 => 284,
            Product::Ecw230S => 285,
            Product::Other(v) => v,
        }
    }
    pub fn label(self) -> &'static str {
        match self {
            Product::Ews377ApV2 => "EWS377AP v2",
            Product::Ews377ApV3 => "EWS377AP v3",
            Product::Ews377Fit => "EWS377-FIT",
            Product::Ecw230 => "ECW230",
            Product::Ecw230V3 => "ECW230v3",
            Product::Ecw230S => "ECW230S",
            Product::Other(_) => "unknown",
        }
    }
}

/// A parsed, validated Senao header.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Header {
    pub vendor_id: u32,
    pub product_id: u32,
    pub firmware_type: u32,
    pub chksum: u32,
    /// The on-wire magic. For an obfuscated header this is the key's last four
    /// bytes, not `0x12345678`.
    pub magic: u32,
    pub model: String,
    /// `Some(key)` if the header was XOR-obfuscated (OpenWrt `senao-header`)
    /// and was decoded with this key; `None` for a plain OEM header.
    pub key: Option<Key>,
}

impl Header {
    pub fn product(&self) -> Product {
        Product::from_id(self.product_id)
    }
}

fn be_u32(data: &[u8], off: usize) -> Result<u32, Error> {
    let end = off + 4;
    if data.len() < end {
        return Err(Error::TooShort {
            need: end,
            got: data.len(),
        });
    }
    Ok(u32::from_be_bytes([
        data[off],
        data[off + 1],
        data[off + 2],
        data[off + 3],
    ]))
}

fn model_of(hdr: &[u8]) -> String {
    let model_bytes = &hdr[OFF_MODEL..OFF_MODEL + 16];
    let end = model_bytes
        .iter()
        .position(|&b| b == 0 || !(0x20..=0x7e).contains(&b))
        .unwrap_or(model_bytes.len());
    String::from_utf8_lossy(&model_bytes[..end]).into_owned()
}

fn header_from(hdr: &[u8], magic: u32, key: Option<Key>) -> Result<Header, Error> {
    Ok(Header {
        vendor_id: be_u32(hdr, OFF_VENDOR_ID)?,
        product_id: be_u32(hdr, OFF_PRODUCT_ID)?,
        firmware_type: be_u32(hdr, OFF_FIRMWARE_TYPE)?,
        chksum: be_u32(hdr, OFF_CHKSUM)?,
        magic,
        model: model_of(hdr),
        key,
    })
}

/// XOR the first [`MIN_LEN`] bytes of `data` with `key[i % 8]`.
fn xor_header(buf: &mut [u8], key: &Key) {
    for (i, b) in buf.iter_mut().enumerate().take(MIN_LEN) {
        *b ^= key[i % 8];
    }
}

/// Parse and validate a Senao image header, auto-detecting the OpenWrt
/// XOR-obfuscated form with the [`KNOWN_KEYS`]. Errors if the image is too short
/// or isn't a Senao image.
pub fn parse(data: &[u8]) -> Result<Header, Error> {
    parse_with_key(data, None)
}

/// Like [`parse`], optionally with an explicit XOR `key` (`--key`).
///
/// The OpenWrt `senao-header` step XORs the header with an 8-byte key (byte `i`
/// with `key[i % 8]`) and the on-wire "magic" field (offset 0x5C, `key[4..8]`)
/// carries the key's tail instead of `0x12345678`. A plain OEM header is the
/// degenerate case with a zero key head. If the magic is not `0x12345678`, each
/// candidate key whose tail equals the on-wire magic is tried, and accepted only
/// if the decoded header has a plausible model string — so a wrong key fails
/// instead of silently decoding garbage.
pub fn parse_with_key(data: &[u8], key: Option<&Key>) -> Result<Header, Error> {
    if data.len() < MIN_LEN {
        return Err(Error::TooShort {
            need: MIN_LEN,
            got: data.len(),
        });
    }
    let magic = be_u32(data, OFF_MAGIC)?;
    if magic == MAGIC {
        return header_from(data, magic, None);
    }
    let tail = magic.to_be_bytes();
    let candidates: Vec<Key> = match key {
        Some(k) => vec![*k],
        None => KNOWN_KEYS.to_vec(),
    };
    for k in &candidates {
        if k[4..8] != tail {
            continue;
        }
        let mut hdr = data[..MIN_LEN].to_vec();
        xor_header(&mut hdr, k);
        if model_of(&hdr).len() >= 3 {
            return header_from(&hdr, magic, Some(*k));
        }
    }
    match key {
        Some(_) => Err(Error::BadKey {
            reason: format!(
                "key does not decode this header (on-wire magic {magic:#010x} must equal the key's last 4 bytes, and the decoded model string must be readable)"
            ),
        }),
        None => Err(Error::BadMagic { got: magic }),
    }
}

/// The **one-field re-head**: overwrite `product_id` (4 bytes at 0x08) in place so
/// the image passes a sibling model's upload gate. Validates the header first,
/// returns the previous `product_id`. Obfuscated headers are handled
/// transparently with the [`KNOWN_KEYS`].
///
/// The payload `md5sum` covers the untouched payload (stays valid) and `chksum`
/// is never read, so nothing else needs fixing.
pub fn rehead(data: &mut [u8], new_product_id: u32) -> Result<u32, Error> {
    rehead_with_key(data, new_product_id, None)
}

/// Like [`rehead`], optionally with an explicit XOR `key`. For an obfuscated
/// header the new id is written XOR'd with the key, and the result is re-parsed
/// to prove it decodes to the requested id.
pub fn rehead_with_key(
    data: &mut [u8],
    new_product_id: u32,
    key: Option<&Key>,
) -> Result<u32, Error> {
    let h = parse_with_key(data, key)?;
    let new = new_product_id.to_be_bytes();
    for (i, nb) in new.iter().enumerate() {
        let off = OFF_PRODUCT_ID + i;
        data[off] = nb ^ h.key.map_or(0, |k| k[off % 8]);
    }
    let after = parse_with_key(data, h.key.as_ref())?;
    debug_assert_eq!(after.product_id, new_product_id);
    Ok(h.product_id)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Build a minimal synthetic Senao header for tests.
    fn synth(vendor: u32, product: u32, model: &str) -> Vec<u8> {
        let mut d = vec![0u8; 0x100];
        d[OFF_VENDOR_ID..OFF_VENDOR_ID + 4].copy_from_slice(&vendor.to_be_bytes());
        d[OFF_PRODUCT_ID..OFF_PRODUCT_ID + 4].copy_from_slice(&product.to_be_bytes());
        d[OFF_MAGIC..OFF_MAGIC + 4].copy_from_slice(&MAGIC.to_be_bytes());
        let mb = model.as_bytes();
        d[OFF_MODEL..OFF_MODEL + mb.len()].copy_from_slice(mb);
        d
    }

    #[test]
    fn parse_ok() {
        let d = synth(257, 284, "ECW230v3");
        let h = parse(&d).unwrap();
        assert_eq!(h.vendor_id, 257);
        assert_eq!(h.product_id, 284);
        assert_eq!(h.model, "ECW230v3");
        assert_eq!(h.product(), Product::Ecw230V3);
    }

    #[test]
    fn bad_magic_rejected() {
        let mut d = synth(257, 284, "ECW230v3");
        d[OFF_MAGIC] = 0xFF;
        assert!(matches!(parse(&d), Err(Error::BadMagic { .. })));
    }

    #[test]
    fn too_short_rejected() {
        assert!(matches!(parse(&[0u8; 8]), Err(Error::TooShort { .. })));
    }

    #[test]
    fn rehead_284_to_282() {
        // ECW230v3 (284) -> pass the EWS377AP v3 (282) gate.
        let mut d = synth(257, 284, "ECW230v3");
        let old = rehead(&mut d, 282).unwrap();
        assert_eq!(old, 284);
        assert_eq!(parse(&d).unwrap().product_id, 282);
        assert_eq!(parse(&d).unwrap().product(), Product::Ews377ApV3);
    }

    #[test]
    fn rehead_leaves_other_fields_intact() {
        let mut d = synth(257, 300, "EWS377-FIT");
        let before_magic = be_u32(&d, OFF_MAGIC).unwrap();
        rehead(&mut d, 282).unwrap();
        assert_eq!(be_u32(&d, OFF_MAGIC).unwrap(), before_magic);
        assert_eq!(be_u32(&d, OFF_VENDOR_ID).unwrap(), 257);
    }

    /// An OpenWrt-style obfuscated header: plaintext magic field is zero, every
    /// header byte is XOR'd with `key[i % 8]`, so 0x5C carries the key tail.
    fn synth_obfuscated(product: u32, model: &str, key: &Key) -> Vec<u8> {
        let mut d = synth(257, product, model);
        d[OFF_MAGIC..OFF_MAGIC + 4].copy_from_slice(&[0; 4]);
        xor_header(&mut d, key);
        d
    }

    #[test]
    fn obfuscated_header_is_detected_with_default_key() {
        let d = synth_obfuscated(300, "EWS377-FIT", &DEFAULT_KEY);
        assert_eq!(be_u32(&d, OFF_MAGIC).unwrap(), 0x67b3_59ac); // key tail (issue #6)
        let h = parse(&d).unwrap();
        assert_eq!(h.product_id, 300);
        assert_eq!(h.vendor_id, 257);
        assert_eq!(h.model, "EWS377-FIT");
        assert_eq!(h.key, Some(DEFAULT_KEY));
        assert_eq!(h.magic, 0x67b3_59ac);
    }

    #[test]
    fn plain_header_has_no_key() {
        assert_eq!(parse(&synth(257, 284, "ECW230v3")).unwrap().key, None);
    }

    #[test]
    fn obfuscated_rehead_roundtrips_and_touches_only_four_bytes() {
        let orig = synth_obfuscated(300, "EWS377-FIT", &DEFAULT_KEY);
        let mut d = orig.clone();
        assert_eq!(rehead(&mut d, 282).unwrap(), 300);
        assert_eq!(parse(&d).unwrap().product_id, 282);
        let changed: Vec<usize> = (0..d.len()).filter(|&i| d[i] != orig[i]).collect();
        assert!(
            changed.iter().all(|i| (0x08..0x0c).contains(i)),
            "{changed:?}"
        );
        // Raw bytes are the XOR'd form, not plaintext 0x0000011a.
        assert_ne!(&d[0x08..0x0c], &282u32.to_be_bytes());
        // And back again.
        rehead(&mut d, 300).unwrap();
        assert_eq!(d, orig);
    }

    #[test]
    fn explicit_key_for_unknown_key_image() {
        let key: Key = [1, 2, 3, 4, 5, 6, 7, 8];
        let d = synth_obfuscated(284, "ECW230v3", &key);
        // Unknown key: refused, not misdecoded.
        assert!(matches!(parse(&d), Err(Error::BadMagic { .. })));
        let h = parse_with_key(&d, Some(&key)).unwrap();
        assert_eq!((h.product_id, h.key), (284, Some(key)));
        let mut d2 = d.clone();
        rehead_with_key(&mut d2, 282, Some(&key)).unwrap();
        assert_eq!(parse_with_key(&d2, Some(&key)).unwrap().product_id, 282);
    }

    #[test]
    fn wrong_explicit_key_is_rejected() {
        let d = synth_obfuscated(300, "EWS377-FIT", &DEFAULT_KEY);
        // Right tail, wrong head: decodes to garbage model -> refused.
        let bad: Key = [9, 9, 9, 9, 0x67, 0xb3, 0x59, 0xac];
        assert!(matches!(
            parse_with_key(&d, Some(&bad)),
            Err(Error::BadKey { .. })
        ));
        // Wrong tail.
        let bad2: Key = [0x78, 0x3c, 0x9e, 0xcf, 0, 0, 0, 0];
        assert!(matches!(
            parse_with_key(&d, Some(&bad2)),
            Err(Error::BadKey { .. })
        ));
        let mut d2 = d.clone();
        assert!(rehead_with_key(&mut d2, 282, Some(&bad)).is_err());
        assert_eq!(d2, d, "a refused rehead must not modify the image");
    }

    #[test]
    fn parse_key_validates() {
        assert_eq!(parse_key("783c9ecf67b359ac").unwrap(), DEFAULT_KEY);
        assert_eq!(parse_key("0x783C9ECF67B359AC").unwrap(), DEFAULT_KEY);
        assert!(parse_key("783c9ecf").is_err());
        assert!(parse_key("zzzzzzzzzzzzzzzz").is_err());
    }

    #[test]
    fn rehead_rejects_non_senao() {
        let mut junk = vec![0u8; 0x100];
        assert!(rehead(&mut junk, 282).is_err());
    }
}
