# Using Pelegrún

A practical walkthrough: install, the guided TUI, the individual commands, and
how the safe-flash and recovery flows fit together.

> **Unofficial — not affiliated with EnGenius or Senao.** For interoperability
> and self-hosting on hardware you own. Cross-flashing can brick hardware; read
> [`../SAFETY.md`](../SAFETY.md) first.

## Install

### Prebuilt binaries (recommended)

Grab the archive for your platform from the
[latest release](https://github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/releases/latest),
then **verify the checksum** before running:

```sh
# example: Apple Silicon macOS
curl -LO https://github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/releases/latest/download/pelegrun-darwin-arm64
curl -LO https://github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/releases/latest/download/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing   # or: sha256sum -c
chmod +x pelegrun-darwin-arm64
./pelegrun-darwin-arm64
```

Binaries are published for `darwin/{arm64,amd64}`, `linux/{amd64,arm64}`, and
`windows/amd64`.

### From source

```sh
git clone https://github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools
cd pelegrun-ap-hk07-firmware-tools
cd go && go build -o pelegrun ./cmd/pelegrun    # the TUI/CLI (Go)
cargo build -p quarry --release                 # image re-head + serial core (Rust)
```

## The guided TUI

Run `pelegrun` with no arguments to open the dashboard. The sidebar walks the job
in plain steps; each screen shows **live output from the real logic** — including
the safety refusals — not mock data.

| Step | What it does |
|------|--------------|
| **Discover** | Fingerprints the firmware family (Cloud · EWS/LuCI · FIT) from its web UI |
| **Connect** | Shows how to reach each family — SSH is on **:8822**, not 22 |
| **Back Up** | The required read-only evidence bundle to capture *before* flashing |
| **Safeguards** | Why the tool can't brick: the env gate refuses wiped/empty writes |
| **Identity** | Mints a unique, collision-checked serial for the target model |
| **Install** | The no-UART A/B flash: write the spare slot, reboot, re-verify |
| **Verify** | Confirms the device came back as intended; rollback if not |

Navigate with `↑ ↓` (or `j k`), jump with `g` / `G`, quit with `q`.

## Commands

Everything the TUI shows is also scriptable:

```sh
pelegrun discover http://192.168.1.1     # → family + which access adapter to use
pelegrun serial  --model X42 --prefix SWLW --suffix 0001   # → SWLWX420001T
pelegrun snextra --model X42             # → 20-char u-boot field-19 value
pelegrun check   EPC1X4200011            # → serial=… valid=true model_code=X42
pelegrun envcheck env.txt                # completeness gate; refuses if incomplete
pelegrun redact  bundle.txt --mac --value <serial>   # scrub keys, tokens, serials, identity
pelegrun plan                            # print the ordered, gated flash plan
pelegrun version
```

`pelegrun envcheck` reads a `fw_printenv` dump (file, or `-` for stdin) and exits
non-zero if the bootloader env is incomplete — the one state that bricks:

```console
$ printf 'ethaddr=00:03:7f:12:3e:87\n' | pelegrun envcheck -
env: INCOMPLETE — missing [active_fw app_part bootcmd]
refuse writes; this env still holds identity variables (ethaddr/hw_id/sn/snextra): do NOT run `env default -a` (it resets them to compiled-in defaults). Restore the saved printenv / APPSBLENV backup instead
```

The required set is `bootcmd`, `active_fw`, `app_part` — what every bootloader
generation needs (the EWS377-FIT's u-boot 2016.01 V2.1.0 has no `rootfsname`).
Pass `--strict` for the older u-boot 2.0.0 units to also require `rootfsname`.
On success it echoes `hw_id`/`hw_ver`/`pro_id`/`machid` when present.

`pelegrun redact` always scrubs passwords/tokens, PEM private keys (even
truncated ones), bare base64 key bodies, `snextra=`/`sn=` values, and the
`cert` partition's `SN/MAC/HWID` record; `--mac` additionally scrubs MAC
addresses. **Backups are secrets** — see [`SAFETY.md`](../SAFETY.md#backups-are-secrets);
never attach a raw dump to an issue.

Model codes (the 3 chars at serial positions 5–7): `X42` ECW230v3 · `X44`
EWS377AP v3 · `X45` EWS377-FIT. See the field guide's
[model-codes](https://github.com/ParkWardRR/engenius-field-guide/blob/main/model-codes.md)
for the full table.

### Image re-head (quarry)

The one-field `product_id` patch that lets a sibling's image pass another model's
upload gate ships as the standalone `quarry` binary:

```sh
quarry inspect firmware.bin             # parse + validate the Senao header
quarry rehead firmware.bin patched.bin --to 284   # ECW230v3; writes only 4 bytes at 0x08
quarry inspect  openwrt-...-senao-factory.bin     # OpenWrt-built image: header auto-decoded
quarry verify-ubi factory.ubi --board hk07,hk08   # every listed FIT config must exist
quarry verify-ubi factory.ubi                     # no --board: just list the FIT configs
```

**OpenWrt-built `senao-factory.bin`.** The OpenWrt `senao-header` build step
XOR-obfuscates the header with an 8-byte key (`-m` in `ipq807x.mk`,
`783c9ecf67b359ac`); the on-wire "magic" at `0x5C` is then the key's tail
(`0x67b359ac`) instead of `0x12345678`. `inspect` and `rehead` detect this
automatically with the built-in key, show `xor key`, and `rehead` writes the new
`product_id` XOR'd with the same key (still only 4 bytes) and re-parses the result
to prove it. A different key can be supplied with `--key <16 hex digits>`; a key
that does not decode the header (wrong tail, or an unreadable model string) is
refused rather than silently misdecoding.

**`verify-ubi --board`** takes one name or a comma-separated list and requires
**all** of them; the ECW230v3 image carries two FIT configs (`config@hk07` and
`config@hk08`, since its stock FIT defaults to hk08). Without `--board` it only
lists the configs.

> **Re-heading across SKUs changes only `product_id`.** The payload keeps the
> donor SKU's DTS model, `qcom,ath11k-calibration-variant` and Wi-Fi
> `board-2.bin`. Such an image can boot and bring radios up (on the donor's RF
> data) while reporting the wrong model — fine for a test, **not for a release**.
> The converse also bites: an image with the FIT's variant but another SKU's board
> file has **no radios** (`failed to fetch board data … variant=…`). `quarry` does
> not inspect the payload's variant/board-file identity yet.

**Header `product_id`s** (read from real vendor images; `vendor_id` is `257`
across the line): `282` EWS377AP v3 · `300` EWS377-FIT · `284` ECW230v3 are the
three cross-flash **siblings** (the values you pass to `--to`). Also recognised
for identification only: `275` ECW230, `182` EWS377AP v2, `285` ECW230S — the
last is a related cloud AP, **not** a verified cross-flash target. The parser is
regression-tested against real firmware with `make test-firmware FW=<dir>`.

### Hardware variants

`hw_id` and the `product_id` above are **different namespaces** (see below), and
recovery steps differ by hardware revision:

| | EWS377AP v3 unit (earlier) | EWS377-FIT unit |
|---|---|---|
| RAM | 1 GiB | **512 MiB** |
| u-boot | 2.0.0, `=>`, 5 s "press a key" | **2016.01 V2.1.0, `IPQ807x#`, boot menu — press `4`** (never `9` / `e`), `bootdelay=2` |
| MAC source | ART | **u-boot env + `cert`**; ART has placeholders |
| Stock console | — | **password-protected**; the documented `admin` was rejected on this unit |
| `rootfsname` env var | present | **absent** (healthy) |
| extra partitions | — | `cert`, `userconfig`, `crashdump` (so ART is `mtd12`) |

Source: the first physical EWS377-FIT, see the
[hardware variants](https://github.com/ParkWardRR/openwrt-engenius-ews377ap-ecw230-ews377fit/blob/main/docs/hardware-variants.md)
write-up in the OpenWrt repo.

### Identity lives in two places

On the FIT the unit's identity is in **both** the u-boot env (`ethaddr`,
`hw_id`, `hw_ver`, `sn`, `snextra` = 12-char serial + 8 `*`) and the `cert`
partition (`SN/MAC/HWID` text record + the cloud RSA key); the stock
`cloud_guard` compares them at boot (`uboot(SN/MAC/HWID)[…], cert(SN/MAC/HWID)[…],
no problem.`). Things to know:

- **Env `ethaddr`, not ART, is the MAC source** on this hardware. ART starts
  `0xff` with placeholder MACs; u-boot's SROM MAC is the placeholder
  `00:03:7f:ba:db:ad`. Any "preserve the MAC" step must read the env.
- **Untested:** if a new `snextra`/`hw_id` is written to the env (append-only)
  but not to the `cert` record, whether `cloud_guard` / adoption treats env≠cert
  as a failure. Do **not** recommend synthetic serials on the FIT path until
  that is checked on hardware. `pelegrun envcheck` echoes `hw_id`/`hw_ver`/
  `pro_id`/`machid` so you can compare the env against your `cert` record.
- The real FIT serial validates (`quarry check` → `valid: true`, `model_code:
  X45`), so the Code27 math and the `X45` code are confirmed on hardware.
  `quarry`'s `snextra` validation accepts the real `serial + ********` form.

### `hw_id` vs `product_id` (open data request, [#8](https://github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/issues/8))

The FIT's env reports `hw_id=0101012B`: vendor `0x0101` (= the Senao `vendor_id`
257) and a low half `0x012B` = **299**, while its Senao header `product_id` is
**300** (`0x012C`). Whether that is a separate hardware numbering, an off-by-one,
or a board-revision difference is unknown. If you own another SKU, please add
read-only values for `printenv hw_id hw_ver pro_id machid`, the `product_id`
`quarry inspect` shows on that unit's own official image, and whether `upload.cgi`
accepted a re-headed image on that exact unit. Until then treat `hw_id` as
informational; `pelegrun envcheck` displays it.

## The two invariants (why it won't brick)

1. **Writes go to the `INACTIVE` A/B slot.** The running slot stays bootable — a
   bad image is undone with a factory-reset hold.
2. **The bootloader env is append-only.** The tool only adds fields to an env it
   has verified complete; it can't erase it or save a partial one. A
   valid-but-incomplete env is the single thing that bricks, and the code is
   structurally incapable of producing one.

## If it won't boot (UART recovery)

Rarely needed, but if a board is truly dead you'll want a USB-TTL adapter on the
console header. The recovery steps are:

1. **Env repair:** restore the saved `printenv` / APPSBLENV backup first.
   `env default -a` is a last resort — it resets `ethaddr`, `hw_id`, `sn` and
   `snextra` to compiled-in defaults (the stock `cloud_guard` cross-checks them
   against the `cert` partition). If you must: `env default -a` (RAM only) →
   re-enter the identity vars from your backup → `env save` → `env load` →
   verify → **cold boot** (full power removal, not just `reset`).
2. **TFTP re-flash:** point the board's u-boot at your machine and serve it a
   known-good image via a TFTP server.

### UART notes for the newer (EWS377-FIT, u-boot 2.1.0) hardware

- **Catch the boot menu:** send `4` repeatedly from the moment `U-Boot 2016.01`
  appears. The usual "press any key" spam does nothing; never press `9` or `e`.
- **Chunk NAND reads.** Do not `nand read` a whole 111 MiB slot at once on a
  512 MiB unit: `fdtcontroladdr=4a970ec0` lies inside `0x44000000 + 0x6f00000`, so
  the read overwrites u-boot's own FDT and the AP resets mid-backup (to stock,
  harmlessly). Use chunks ≤ 32 MiB to `0x44000000` and join them on the server.
- **No `saveenv` if `active_fw` is already `0`.** `nand write` needs none, which
  keeps the env (and the MAC/identity vars) untouched. Verify with `nand read` +
  `cmp.b` instead.
- **The stock console rejected the documented `admin` login** on this unit, and
  SSH `:8822` on stock was not tested — treat "root/admin on reset units" as
  **unverified** for the FIT.
- Power-cycling into u-boot stops the stock OS and its Wi-Fi, which matters if the
  unit is on a LAN you care about.

See [`../SAFETY.md`](../SAFETY.md) and the field guide's
[cross-flash walkthrough](https://github.com/ParkWardRR/engenius-field-guide/blob/main/crossflash-ews377apv3-walkthrough.md)
for the full hardware procedure and evidence checklist.
