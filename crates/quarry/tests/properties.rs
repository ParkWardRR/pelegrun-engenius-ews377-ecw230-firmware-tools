//! Property-style tests for quarry's pure transforms.
//!
//! No external crates and no real randomness (a fixed-seed LCG keeps runs
//! reproducible and CI-stable): we generate many structured inputs and assert
//! invariants that must hold for *every* input, not just the hand-picked vectors
//! in the unit tests.

use quarry::header::{self, MAGIC, OFF_MAGIC, OFF_MODEL, OFF_PRODUCT_ID};
use quarry::serial::{
    check_char, make_serial, make_snextra, model_code, validate_serial, validate_snextra, CODE27,
    SERIAL_LEN, SNEXTRA_LEN,
};

/// Small deterministic PRNG (PCG-XSH-RR style constants) so each run is identical.
struct Lcg(u64);
impl Lcg {
    fn new(seed: u64) -> Self {
        Lcg(seed ^ 0x9E37_79B9_7F4A_7C15)
    }
    fn next_u32(&mut self) -> u32 {
        self.0 = self
            .0
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        (self.0 >> 33) as u32
    }
    fn below(&mut self, n: u32) -> u32 {
        self.next_u32() % n
    }
}

const ALNUM: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";

fn rand_alnum(r: &mut Lcg, len: usize) -> String {
    (0..len)
        .map(|_| ALNUM[r.below(ALNUM.len() as u32) as usize] as char)
        .collect()
}

const ITERS: usize = 5000;

#[test]
fn check_char_makes_serials_that_validate() {
    let mut r = Lcg::new(1);
    for _ in 0..ITERS {
        let body = rand_alnum(&mut r, SERIAL_LEN - 1);
        let c = check_char(&body).expect("alnum body must have a check char");
        // The check char is always drawn from the Code27 alphabet.
        assert!(CODE27.contains(&(c as u8)));
        // body + its check char always validates.
        let serial = format!("{body}{c}");
        assert!(validate_serial(&serial), "should validate: {serial}");
        // check_char is a pure function of the body — recompute is stable.
        assert_eq!(check_char(&body).unwrap(), c);
    }
}

#[test]
fn a_wrong_check_char_never_validates() {
    let mut r = Lcg::new(2);
    for _ in 0..ITERS {
        let body = rand_alnum(&mut r, SERIAL_LEN - 1);
        let good = check_char(&body).unwrap();
        // Any *other* alphabet character must fail validation (check is exact).
        for &b in CODE27.iter() {
            let cand = b as char;
            if cand == good {
                continue;
            }
            assert!(!validate_serial(&format!("{body}{cand}")));
        }
    }
}

#[test]
fn make_serial_roundtrips_model_code() {
    let mut r = Lcg::new(3);
    for _ in 0..ITERS {
        let prefix = rand_alnum(&mut r, 4);
        let model = rand_alnum(&mut r, 3);
        let suffix = rand_alnum(&mut r, 4);
        let s = make_serial(&prefix, &model, &suffix).unwrap();
        assert_eq!(s.len(), SERIAL_LEN);
        assert!(validate_serial(&s));
        assert_eq!(model_code(&s).unwrap(), model, "model code at 5..7");
    }
}

#[test]
fn snextra_is_well_formed_and_carries_model() {
    let mut r = Lcg::new(4);
    for _ in 0..ITERS {
        let prefix = rand_alnum(&mut r, 4);
        let model = rand_alnum(&mut r, 3);
        let suffix = rand_alnum(&mut r, 4);
        let x = make_snextra(&prefix, &model, &suffix).unwrap();
        assert_eq!(x.len(), SNEXTRA_LEN);
        assert!(validate_snextra(&x));
        assert_eq!(model_code(&x).unwrap(), model);
        // Whatever the inputs, the field holds a complete, valid serial.
        assert!(validate_serial(&x[..12]));
        assert!(x.ends_with("********"));
    }
}

#[test]
fn seq4_is_total_and_stays_in_the_alphabet() {
    let mut r = Lcg::new(11);
    for _ in 0..ITERS {
        let n = r.next_u32();
        let s = quarry::serial::seq4(n);
        assert_eq!(s.len(), 4);
        assert!(s.bytes().all(|b| CODE27.contains(&b)));
        // Wrapping is modular, exactly like the vendor's `% 531441`.
        assert_eq!(s, quarry::serial::seq4(n % quarry::serial::SEQ_SPACE));
    }
}

/// A minimal synthetic Senao header (mirrors the in-crate test helper).
fn synth(vendor: u32, product: u32) -> Vec<u8> {
    let mut d = vec![0u8; 0x100];
    d[4..8].copy_from_slice(&vendor.to_be_bytes());
    d[OFF_PRODUCT_ID..OFF_PRODUCT_ID + 4].copy_from_slice(&product.to_be_bytes());
    d[OFF_MAGIC..OFF_MAGIC + 4].copy_from_slice(&MAGIC.to_be_bytes());
    let mb = b"MODELSTRING";
    d[OFF_MODEL..OFF_MODEL + mb.len()].copy_from_slice(mb);
    d
}

#[test]
fn rehead_only_changes_product_id_and_is_reversible() {
    let mut r = Lcg::new(5);
    for _ in 0..ITERS {
        let vendor = r.next_u32();
        let old_id = r.next_u32();
        let new_id = r.next_u32();
        let mut data = synth(vendor, old_id);
        let untouched = data.clone();

        let returned_old = header::rehead(&mut data, new_id).unwrap();
        assert_eq!(
            returned_old, old_id,
            "rehead returns the previous product_id"
        );

        // Exactly the 4 product_id bytes changed — nothing else.
        for (i, (a, b)) in untouched.iter().zip(data.iter()).enumerate() {
            let in_field = (OFF_PRODUCT_ID..OFF_PRODUCT_ID + 4).contains(&i);
            if in_field {
                continue;
            }
            assert_eq!(a, b, "byte {i} outside product_id must not change");
        }

        // Re-parse sees the new id; vendor + magic survive.
        let h = header::parse(&data).unwrap();
        assert_eq!(h.product_id, new_id);
        assert_eq!(h.vendor_id, vendor);
        assert_eq!(h.magic, MAGIC);

        // rehead-ing back restores the original bytes.
        header::rehead(&mut data, old_id).unwrap();
        assert_eq!(data, untouched, "reheading back is a perfect inverse");
    }
}
