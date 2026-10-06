//! # quarry
//!
//! Pure, I/O-free core for **Senao/EnGenius** firmware images on the `ap-hk07`
//! (Qualcomm IPQ807x) board family — the shared hardware behind the EWS377AP v3,
//! EWS377-FIT and ECW230v3.
//!
//! Two jobs, both deterministic and heavily unit-tested:
//! - [`header`] — parse/validate the Senao image header and perform the
//!   **one-field `product_id` re-head** that lets a sibling's image pass another
//!   model's upload gate.
//! - [`serial`] — the **Code27** check-character math for 12-char serials and the
//!   20-char "extra serial" (`snextra`) helper.
//!
//! This crate never touches a device, a network, or a bootloader environment.
//! It only transforms bytes and strings you hand it. See `SAFETY.md`.
//!
//! **Unofficial / not affiliated with EnGenius or Senao.** For interoperability
//! and self-hosting on hardware you own.

pub mod header;
pub mod serial;
pub mod ubi;

/// Crate-wide error type.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Error {
    /// Image is shorter than the fields we need to read.
    TooShort { need: usize, got: usize },
    /// Magic value at offset 0x5C was not `0x12345678` (Senao header).
    BadMagic { got: u32 },
    /// A serial/model-code string had the wrong length.
    BadLength {
        field: &'static str,
        want: usize,
        got: usize,
    },
    /// A serial/model-code contained a character outside the allowed set.
    BadChar { field: &'static str },
    /// No UBI erase-counter header magic (`UBI#`) at the start of the image.
    NotUbiImage,
    /// The UBI image parsed, but no volume with the given name was found.
    VolumeNotFound { name: String },
    /// Expected a flattened device tree (FDT) blob but the magic didn't match.
    NotFdt,
    /// A user-supplied header XOR key was malformed or does not decode the image.
    BadKey { reason: String },
}

impl core::fmt::Display for Error {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Error::TooShort { need, got } => {
                write!(f, "image too short: need {need} bytes, got {got}")
            }
            Error::BadMagic { got } => {
                write!(f, "bad Senao magic: expected 0x12345678, got {got:#010x}")
            }
            Error::BadLength { field, want, got } => {
                write!(f, "{field}: expected {want} chars, got {got}")
            }
            Error::BadChar { field } => write!(f, "{field}: contains a disallowed character"),
            Error::NotUbiImage => write!(f, "not a UBI image: missing 'UBI#' EC header magic"),
            Error::VolumeNotFound { name } => write!(f, "UBI volume not found: {name}"),
            Error::NotFdt => write!(f, "not a flattened device tree: bad or missing magic"),
            Error::BadKey { reason } => write!(f, "header key: {reason}"),
        }
    }
}

impl std::error::Error for Error {}
