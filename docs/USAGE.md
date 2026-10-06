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
pelegrun redact  bundle.txt --mac --value <serial>   # scrub secrets
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

Model codes (the 3 chars at serial positions 5–7): `X42` ECW230v3 · `X44`
EWS377AP v3 · `X45` EWS377-FIT. See the field guide's
[model-codes](https://github.com/ParkWardRR/engenius-field-guide/blob/main/model-codes.md)
for the full table.

### Image re-head (quarry)

The one-field `product_id` patch that lets a sibling's image pass another model's
upload gate ships as the standalone `quarry` binary:

```sh
quarry inspect firmware.bin             # parse + validate the Senao header
quarry rehead firmware.bin --to 284     # ECW230v3; writes only 4 bytes at 0x08
```

**Header `product_id`s** (read from real vendor images; `vendor_id` is `257`
across the line): `282` EWS377AP v3 · `300` EWS377-FIT · `284` ECW230v3 are the
three cross-flash **siblings** (the values you pass to `--to`). Also recognised
for identification only: `275` ECW230, `182` EWS377AP v2, `285` ECW230S — the
last is a related cloud AP, **not** a verified cross-flash target. The parser is
regression-tested against real firmware with `make test-firmware FW=<dir>`.

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

See [`../SAFETY.md`](../SAFETY.md) and the field guide's
[cross-flash walkthrough](https://github.com/ParkWardRR/engenius-field-guide/blob/main/crossflash-ews377apv3-walkthrough.md)
for the full hardware procedure and evidence checklist.
