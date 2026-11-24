//! `quarry` — CLI for the Senao/EnGenius header re-head + Code27 serial core.
//!
//! Unofficial. Operates only on image files you supply; touches no device.

use std::process::ExitCode;

fn usage() -> &'static str {
    "quarry — Senao/EnGenius IPQ807x firmware header + serial tool (unofficial)

USAGE:
  quarry inspect    <image.bin> [--key <16hex>]
  quarry rehead     <in.bin> <out.bin> --to <product_id> [--key <16hex>]  # e.g. --to 282
                    [--force-cross-board]
  quarry verify-ubi <factory.ubi> [--board <name>[,<name>...]]            # e.g. --board hk07,hk08
  quarry serial     --model <CODE|NAME> [--prefix PPPP] [--seq N | --suffix SSSS]
  quarry serial     --model <CODE|NAME> --from-legacy <serial9>   # old format -> new
  quarry snextra    --model <CODE|NAME> [--prefix PPPP] [--seq N | --suffix SSSS]
                                                          # serial + 8 '*' = field 19
  quarry check      <serial12>                            # validity + factory shape
  quarry products                                         # SKU / board / model-code table

Product ids: 282 = EWS377AP v3, 300 = EWS377-FIT, 284 = ECW230v3, 291 = ECW336.
Model codes:  X44 = EWS377AP v3, X45 = EWS377-FIT, X42 = ECW230v3, E4T = ECW336
              (note: not every code is X-prefixed).
A serial is YY + month(1-9,A-C) + factory digit + model code + 4 base-27
sequence digits + check char. --seq encodes the sequence for you (--seq 1 =>
111D); --suffix passes 4 chars through verbatim. For a MIGRATION, keep the
unit's own prefix and sequence and change only --model.
--from-legacy converts a 9-char serial (YY + month + 6 decimal digits) the way
the stock firmware's /usr/sbin/snconverter.sh does, and fails closed on a model
name it cannot resolve instead of emitting the vendor's XXX placeholder.
`check` also reports whether a serial matches that layout: only the check
character is ever validated, so a hand-built serial can be valid yet plainly
synthetic. That report is advisory and never changes the exit status.
rehead REFUSES a cross-board patch — 282/300/284 share the ap-hk07 board, the
ECW336 (291) is ap-hk01-c6, and the payload keeps the donor's device tree.
inspect/rehead auto-detect the XOR-obfuscated header of OpenWrt-built
senao-factory.bin images (known key 783c9ecf67b359ac); pass --key for another.
verify-ubi checks a factory.ubi's 'kernel' volume for FIT config nodes named
'config@<board>' — ALL boards listed must be present (the ECW230v3 image carries
both config@hk07 and config@hk08). Without --board it just lists the configs.
A config slot name is NOT a board identity: stock ECW336 firmware boots out of
config@hk07 too, and both vendors ship the same ~25 QSDK reference slots. Read
the DTB's `compatible` to identify hardware.
The OpenWrt EWS377AP v3 port only boots from NAND with a board-matched config
name (see openwrt-ews377ap-v3/results-2026-09-06/ in the repo history for why)."
}

fn arg_val(args: &[String], key: &str) -> Option<String> {
    args.iter()
        .position(|a| a == key)
        .and_then(|i| args.get(i + 1).cloned())
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let cmd = args.first().map(String::as_str).unwrap_or("");
    let rest = if args.is_empty() { &[][..] } else { &args[1..] };

    let result: Result<(), String> = match cmd {
        "inspect" => cmd_inspect(rest),
        "rehead" => cmd_rehead(rest),
        "verify-ubi" => cmd_verify_ubi(rest),
        "serial" => cmd_serial(rest),
        "snextra" => cmd_snextra(rest),
        "check" => cmd_check(rest),
        "products" => cmd_products(),
        "-h" | "--help" | "help" | "" => {
            println!("{}", usage());
            Ok(())
        }
        other => Err(format!("unknown command: {other}\n\n{}", usage())),
    };

    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("error: {e}");
            ExitCode::FAILURE
        }
    }
}

fn key_arg(a: &[String]) -> Result<Option<quarry::header::Key>, String> {
    arg_val(a, "--key")
        .map(|k| quarry::header::parse_key(&k).map_err(|e| e.to_string()))
        .transpose()
}

fn hex_key(k: &quarry::header::Key) -> String {
    k.iter().map(|b| format!("{b:02x}")).collect()
}

fn cmd_inspect(a: &[String]) -> Result<(), String> {
    let path = a.first().ok_or("inspect: missing <image.bin>")?;
    let key = key_arg(a)?;
    let data = std::fs::read(path).map_err(|e| format!("read {path}: {e}"))?;
    let h = quarry::header::parse_with_key(&data, key.as_ref())
        .map_err(|e| with_key_hint(e.to_string()))?;
    println!("file        : {path} ({} bytes)", data.len());
    println!("vendor_id   : {}", h.vendor_id);
    println!("product_id  : {} ({})", h.product_id, h.product().label());
    let board = h.product().board();
    println!("board       : {} ({})", board.label(), board.compatible());
    println!("fw_type     : {}", h.firmware_type);
    println!("model       : {}", h.model);
    match h.key {
        None => println!("magic       : {:#010x} (ok)", h.magic),
        Some(k) => {
            println!(
                "magic       : {:#010x} (key tail; header is XOR-obfuscated)",
                h.magic
            );
            println!("xor key     : {}", hex_key(&k));
        }
    }
    Ok(())
}

/// Adds the override hint to a cross-board refusal; otherwise falls through to
/// the XOR-key hint so `rehead` keeps both diagnostics.
fn cross_board_hint(msg: String) -> String {
    if msg.starts_with("refusing a cross-board re-head") {
        format!(
            "{msg}\nhint: `quarry products` lists which SKUs share a board. --force-cross-board\n      overrides this, but only do that on the INACTIVE A/B slot with UART attached."
        )
    } else {
        with_key_hint(msg)
    }
}

fn with_key_hint(msg: String) -> String {
    if msg.starts_with("bad Senao magic") {
        format!("{msg}\nhint: an OpenWrt-built senao-factory.bin is XOR-obfuscated; if it uses a key other than the built-in one, pass --key <16 hex digits> (mksenaofw -m)")
    } else {
        msg
    }
}

fn cmd_rehead(a: &[String]) -> Result<(), String> {
    let input = a.first().ok_or("rehead: missing <in.bin>")?;
    let output = a.get(1).ok_or("rehead: missing <out.bin>")?;
    let to: u32 = arg_val(a, "--to")
        .ok_or("rehead: missing --to <product_id>")?
        .parse()
        .map_err(|_| "rehead: --to must be a number")?;
    let key = key_arg(a)?;
    let force_cross_board = a.iter().any(|v| v == "--force-cross-board");
    let mut data = std::fs::read(input).map_err(|e| format!("read {input}: {e}"))?;
    let rehead = if force_cross_board {
        quarry::header::rehead_cross_board_with_key
    } else {
        quarry::header::rehead_with_key
    };
    let old = rehead(&mut data, to, key.as_ref()).map_err(|e| cross_board_hint(e.to_string()))?;
    std::fs::write(output, &data).map_err(|e| format!("write {output}: {e}"))?;
    println!(
        "re-headed product_id {} -> {} : {} bytes -> {}",
        old,
        to,
        data.len(),
        output
    );
    println!("note: verify on a recoverable A/B slot; the tool never asserts a flash succeeded.");
    println!(
        "warning: re-heading changes ONLY product_id. The payload keeps the donor SKU's DTS model,\n         ath11k calibration variant and Wi-Fi board-2.bin: it can boot and bring radios up on\n         the donor's RF data while reporting the wrong model. Fine for a test, not for a release."
    );
    Ok(())
}

fn cmd_verify_ubi(a: &[String]) -> Result<(), String> {
    let path = a.first().ok_or("verify-ubi: missing <factory.ubi>")?;
    let boards: Vec<String> = arg_val(a, "--board")
        .map(|v| {
            v.split(',')
                .map(|b| b.trim().to_string())
                .filter(|b| !b.is_empty())
                .collect()
        })
        .unwrap_or_default();
    let data = std::fs::read(path).map_err(|e| format!("read {path}: {e}"))?;
    // The first board (or a dummy) drives the call; `configs` carries them all.
    let probe = boards.first().map(String::as_str).unwrap_or("");
    let result = quarry::ubi::check_factory_ubi(&data, probe).map_err(|e| e.to_string())?;
    println!("file             : {path} ({} bytes)", data.len());
    println!("kernel volume    : {} bytes", result.kernel_volume_bytes);
    println!("fit configs      : {}", result.configs.join(", "));
    let mut missing = Vec::new();
    for b in &boards {
        let present = result.configs.contains(&format!("config@{b}"));
        println!(
            "config@{b:<9}: {}",
            if present { "present" } else { "MISSING" }
        );
        if !present {
            missing.push(b.as_str());
        }
    }
    if missing.is_empty() {
        Ok(())
    } else {
        Err(format!(
            "kernel volume has no /configurations/config@{} node — bootipq will refuse to boot this image from NAND",
            missing.join(", config@")
        ))
    }
}

/// The sequence field (serial positions 8–11): `--suffix` verbatim, or `--seq`
/// encoded as the vendor's 4 base-27 digits. Defaults to sequence 1 (`111D`) —
/// a shape a factory serial can actually have, unlike the old `0001` default
/// (`0` is not in the Code27 alphabet).
fn seq_arg(a: &[String], cmd: &str) -> Result<String, String> {
    match (arg_val(a, "--suffix"), arg_val(a, "--seq")) {
        (Some(_), Some(_)) => Err(format!("{cmd}: pass --suffix or --seq, not both")),
        (Some(s), None) => Ok(s),
        (None, Some(n)) => {
            let seq: u32 = n
                .parse()
                .map_err(|_| format!("{cmd}: --seq wants a non-negative integer, got {n:?}"))?;
            Ok(quarry::serial::seq4(seq))
        }
        (None, None) => Ok(quarry::serial::seq4(1)),
    }
}

/// `--model` takes a 3-char code, or a product name we can resolve to one
/// (current controller table first, then the vendor's legacy table).
fn model_code_arg(a: &[String], cmd: &str) -> Result<String, String> {
    let m = arg_val(a, "--model").ok_or(format!("{cmd}: missing --model <CODE|NAME>"))?;
    if m.len() == 3 {
        return Ok(m);
    }
    quarry::serial::code_for_model_name(&m)
        .map(str::to_string)
        .ok_or(format!(
            "{cmd}: {m:?} is neither a 3-char model code nor a product name this build knows \
         (try `quarry products`, or pass the code)"
        ))
}

fn cmd_serial(a: &[String]) -> Result<(), String> {
    let model = model_code_arg(a, "serial")?;
    if let Some(old) = arg_val(a, "--from-legacy") {
        if arg_val(a, "--prefix").is_some() || arg_val(a, "--seq").is_some() {
            return Err("serial: --from-legacy carries its own date and sequence; \
                        drop --prefix/--seq"
                .into());
        }
        let s = quarry::serial::convert_legacy_serial(&old, &model).map_err(|e| e.to_string())?;
        println!("{s}");
        return Ok(());
    }
    let prefix = arg_val(a, "--prefix").unwrap_or_else(|| "SWLW".to_string());
    let suffix = seq_arg(a, "serial")?;
    let s = quarry::serial::make_serial(&prefix, &model, &suffix).map_err(|e| e.to_string())?;
    println!("{s}");
    Ok(())
}

fn cmd_snextra(a: &[String]) -> Result<(), String> {
    let model = model_code_arg(a, "snextra")?;
    let prefix = arg_val(a, "--prefix").unwrap_or_default();
    let suffix = seq_arg(a, "snextra")?;
    let s = quarry::serial::make_snextra(&prefix, &model, &suffix).map_err(|e| e.to_string())?;
    println!("{s}");
    Ok(())
}

/// The SKU table: header `product_id`, host board, and serial model code. Boards
/// are only named where a real DTB was diffed; everything else reads `unknown`
/// rather than being guessed, and `rehead` will not refuse on an unknown board.
fn cmd_products() -> Result<(), String> {
    use quarry::header::Product;
    println!(
        "{:<5} {:<12} {:<12} {:<4} compatible",
        "id", "model", "board", "code"
    );
    for p in [
        Product::Ews377ApV2,
        Product::Ecw230,
        Product::Ews377ApV3,
        Product::Ecw230V3,
        Product::Ecw230S,
        Product::Ecw336,
        Product::Ews377Fit,
    ] {
        let code = quarry::serial::MODEL_CODES
            .iter()
            .find(|(_, name)| *name == p.label())
            .map(|(c, _)| *c)
            .unwrap_or("-");
        println!(
            "{:<5} {:<12} {:<12} {:<4} {}",
            p.id(),
            p.label(),
            p.board().label(),
            code,
            p.board().compatible()
        );
    }
    println!(
        "\nA re-head only rewrites the SKU gate, so it is a plausible cross-flash only\nbetween SKUs on the SAME board. 282/300/284 are one board (ap-hk07); the\nECW336 is ap-hk01-c6 — same IPQ8072A silicon, different ethernet topology\n(dp4+dp5), an Aquantia 10G PHY and a QCN9074 6 GHz card. `unknown` means no\nDTB was diffed, not \"compatible\"."
    );
    Ok(())
}

fn cmd_check(a: &[String]) -> Result<(), String> {
    let s = a.first().ok_or("check: missing <serial12>")?;
    let ok = quarry::serial::validate_serial(s);
    let mc = quarry::serial::model_code(s)
        .map(str::to_string)
        .unwrap_or_else(|_| "?".into());
    println!("serial    : {s}");
    println!("valid     : {ok}");
    println!("model_code: {mc}");
    println!(
        "model     : {}",
        quarry::serial::model_name(&mc).unwrap_or("unknown code")
    );
    // Advisory only: the check character is the sole thing that *validates*, so
    // a hand-built serial can be valid and still obviously synthetic.
    let notes = quarry::serial::shape_notes(s);
    if notes.is_empty() {
        println!("shape     : matches the factory layout");
    } else {
        println!("shape     : NOT factory-shaped (advisory)");
        for n in notes {
            println!("            - {n}");
        }
    }
    if ok {
        Ok(())
    } else {
        Err("check character does not match".into())
    }
}
