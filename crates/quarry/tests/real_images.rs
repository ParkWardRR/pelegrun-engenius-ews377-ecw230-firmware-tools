//! Regression test against **real vendor images** — opt-in, nothing committed.
//!
//! Set `QUARRY_FIRMWARE_DIR` to a folder of `.bin` firmware and run:
//!
//! ```sh
//! QUARRY_FIRMWARE_DIR=~/Downloads cargo test -p quarry --test real_images -- --nocapture
//! ```
//!
//! Without the env var the test is a no-op, so CI (which has no firmware) stays
//! green and no vendor bytes ever enter the repo. It asserts quarry's header
//! parser holds on genuine ap-hk07 images: valid magic, the family vendor id,
//! and a `product_id` that round-trips through `Product`.

use std::fs;
use std::path::PathBuf;

use quarry::header::{self, Product};

#[test]
fn parses_real_firmware_headers() {
    let dir = match std::env::var("QUARRY_FIRMWARE_DIR") {
        Ok(d) => PathBuf::from(shellexpand_tilde(&d)),
        Err(_) => {
            eprintln!("skip: set QUARRY_FIRMWARE_DIR to run the real-image test");
            return;
        }
    };

    let mut parsed = 0usize;
    let mut skipped = 0usize;
    for entry in fs::read_dir(&dir).expect("read firmware dir") {
        let path = entry.unwrap().path();
        if path.extension().and_then(|e| e.to_str()) != Some("bin") {
            continue;
        }
        // Read only the header window — we never need the multi-MB payload.
        let data = read_prefix(&path, header::MIN_LEN);
        let h = match header::parse(&data) {
            Ok(h) => h,
            Err(_) => {
                skipped += 1; // not a Senao image (e.g. a u-boot env backup)
                continue;
            }
        };
        let name = path.file_name().unwrap().to_string_lossy();

        assert_eq!(h.magic, header::MAGIC, "{name}: magic");
        assert_eq!(
            h.vendor_id, 257,
            "{name}: vendor_id (the whole Senao IPQ807x line is 257)"
        );
        // product_id must round-trip through the Product enum.
        assert_eq!(
            Product::from_id(h.product_id).id(),
            h.product_id,
            "{name}: product_id {} does not round-trip",
            h.product_id
        );
        // A known id must carry a non-"unknown" label.
        if matches!(h.product_id, 182 | 275 | 282 | 284 | 285 | 291 | 300) {
            assert_ne!(
                h.product().label(),
                "unknown",
                "{name}: known id lost its label"
            );
        }
        eprintln!(
            "  ok  {name:<52} vendor={} product={} ({}) model={:?}",
            h.vendor_id,
            h.product_id,
            h.product().label(),
            h.model
        );
        parsed += 1;
    }

    eprintln!("parsed {parsed} Senao image(s), skipped {skipped} non-Senao .bin");
    assert!(parsed > 0, "no Senao images parsed from {}", dir.display());
}

fn read_prefix(path: &std::path::Path, n: usize) -> Vec<u8> {
    use std::io::Read;
    let mut f = fs::File::open(path).expect("open image");
    let mut buf = vec![0u8; n];
    let got = f.read(&mut buf).expect("read header");
    buf.truncate(got);
    buf
}

/// Minimal `~` expansion (no external crate).
fn shellexpand_tilde(p: &str) -> String {
    if let Some(rest) = p.strip_prefix("~/") {
        if let Ok(home) = std::env::var("HOME") {
            return format!("{home}/{rest}");
        }
    }
    p.to_string()
}
