//! `quarry` — CLI for the Senao/EnGenius header re-head + Code27 serial core.
//!
//! Unofficial. Operates only on image files you supply; touches no device.

use std::process::ExitCode;

fn usage() -> &'static str {
    "quarry — Senao/EnGenius ap-hk07 firmware header + serial tool (unofficial)

USAGE:
  quarry inspect    <image.bin> [--key <16hex>]
  quarry rehead     <in.bin> <out.bin> --to <product_id> [--key <16hex>]  # e.g. --to 282
  quarry verify-ubi <factory.ubi> [--board <name>[,<name>...]]            # e.g. --board hk07,hk08
  quarry serial     --model <CODE> [--prefix PPPP] [--suffix SSSS]
  quarry snextra    --model <CODE> [--prefix PPPP]        # 20-char field 19
  quarry check      <serial12>

Product ids: 282 = EWS377AP v3, 300 = EWS377-FIT, 284 = ECW230v3.
Model codes:  X44 = EWS377AP v3, X45 = EWS377-FIT, X42 = ECW230v3.
inspect/rehead auto-detect the XOR-obfuscated header of OpenWrt-built
senao-factory.bin images (known key 783c9ecf67b359ac); pass --key for another.
verify-ubi checks a factory.ubi's 'kernel' volume for FIT config nodes named
'config@<board>' — ALL boards listed must be present (the ECW230v3 image carries
both config@hk07 and config@hk08). Without --board it just lists the configs.
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
    let mut data = std::fs::read(input).map_err(|e| format!("read {input}: {e}"))?;
    let old = quarry::header::rehead_with_key(&mut data, to, key.as_ref())
        .map_err(|e| with_key_hint(e.to_string()))?;
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

fn cmd_serial(a: &[String]) -> Result<(), String> {
    let model = arg_val(a, "--model").ok_or("serial: missing --model <CODE>")?;
    let prefix = arg_val(a, "--prefix").unwrap_or_else(|| "SWLW".to_string());
    let suffix = arg_val(a, "--suffix").unwrap_or_else(|| "0001".to_string());
    let s = quarry::serial::make_serial(&prefix, &model, &suffix).map_err(|e| e.to_string())?;
    println!("{s}");
    Ok(())
}

fn cmd_snextra(a: &[String]) -> Result<(), String> {
    let model = arg_val(a, "--model").ok_or("snextra: missing --model <CODE>")?;
    let prefix = arg_val(a, "--prefix").unwrap_or_default();
    let s = quarry::serial::make_snextra(&prefix, &model).map_err(|e| e.to_string())?;
    println!("{s}");
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
    if ok {
        Ok(())
    } else {
        Err("check character does not match".into())
    }
}
