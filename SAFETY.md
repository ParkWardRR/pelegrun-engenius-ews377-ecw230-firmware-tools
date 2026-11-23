# SAFETY

> **Work in progress.** This software is largely untested in the field. The
> core logic and unit tests pass, but end-to-end device testing is ongoing.
> Treat every operation as experimental until this notice is removed. Feedback
> and bug reports are welcome.

Cross-flashing can **brick hardware**. This tool is built to make that nearly
impossible, but firmware work is never zero-risk. Read this before you use it.

## The two invariants (why UART is usually unnecessary)
1. **Writes go to the INACTIVE A/B slot.** The working slot stays bootable, so a
   bad image is undone by a factory-reset-button hold — no UART.
2. **The bootloader env is APPEND-ONLY.** The tool only adds/updates individual
   fields on an env it has verified COMPLETE. It **cannot** erase the env or save
   a partial one. A *valid-but-incomplete* env is the single thing that turns a
   running AP into a bootloader brick — so the tool is structurally unable to make
   one. (This is the exact mistake the project was born from.)

## Always
- Let the tool take its **backup bundle** (`0:appsblenv`, `0:appsbl`, `0:art` +
  config + hashes) first. Partitions are resolved **by name** from `/proc/mtd`,
  never by index — indices differ between units (the EWS377-FIT has extra
  `cert`/`userconfig`/`crashdump` partitions, so ART is `mtd12` there and `mtd11`
  on the EWS377AP v3; the ECW336 has those three plus `0:ETHPHYFW` and
  `0:WIFIFW`/`0:WIFIFW_1`). The plan fails closed if a critical name is missing
  or ambiguous, and dumps are named by partition + size, not `mtdN`.
- **A name is not a position either.** The A/B rootfs labels are *slot-relative*:
  `rootfs` always means the slot currently booted and `rootfs_1` the spare, so
  they swap after a slot switch. Re-read `/proc/mtd` after one, and never map a
  partition name to a remembered offset — on an ECW336 `rootfs` was `0x1000000`
  booted from FW1 and `0x87c0000` booted from FW2.
- Treat **ART** (calibration + factory MACs) as **read-only** — never write it.
  And do not assume it *has* the MACs: an ECW336's u-boot reports the ART copy
  invalid, so there the env is the only copy (see below).
- Keep pristine, unpatched stock images as your rollback path.
- For the first flash on a new model, keep a UART adapter attached. Recovery
  differs by hardware revision (the EWS377-FIT's u-boot 2.1.0 has a boot menu,
  512 MiB RAM and chunked-read limits) — read
  [`docs/USAGE.md`](docs/USAGE.md#uart-notes-for-the-newer-ews377-fit-u-boot-210-hardware)
  before you open the case.
- **Identity vars:** never `env default -a` a unit that still has `ethaddr` /
  `hw_id` / `sn` / `snextra`; restore from the saved `printenv` backup instead.

## On the ECW336, the env is the only copy of the MAC
A stock ECW336 boots with `eth0..eth5 MAC Address from ART is not valid` and
falls back to the Qualcomm dummy `00:03:7f:ba:db:ad`. Its real EnGenius-OUI MAC
lives **only** in env `ethaddr` (the VAP MACs are `ethaddr`+1/+2/+3). On the FIT
an env wipe was recoverable from the `cert` record; here it is permanent
identity loss, and the controller pre-shared key is derived from the MAC and
serial, so losing them loses adoption too.

Consequences for how you work on one:

- **Capture a `printenv` before anything else**, and treat it as a secret (see
  below) — it is the only backup of the unit's identity that exists.
- The Senao boot menu puts `9` (TFTP write **bootloader**) and `e` (**erase
  env**) one keystroke from `4`, the CLI you actually want — and `4` needs a
  CR, within a ~2 s countdown. Treat `9`/`e` as keys that do not exist.
- There is no network path to the env on this firmware line: the ECW cloud
  builds expose no usable shell (`pelegrun discover` reports
  `needs_uart=true`), so rungs 1–2 of `pelegrun plan` are unavailable and every
  change starts at UART.
- What *is* better than the ap-hk07: its u-boot has a working TFTP recovery path
  (boot-menu option `2`), so a bad rootfs is recoverable over the wire.

## Scan for an open BSS after every flash or reset
A factory-default EnGenius ECW AP brings up its Multi-AP/mesh onboarding temp
WDS link as an **unencrypted** BSS — no Privacy bit, no RSN, no WPA — alongside
the secured one, on a hidden SSID. A hidden SSID is not security. After any
flash, factory reset or config change, scan and **parse** for BSSes lacking
`RSN:`/`WPA:` rather than eyeballing SSID names, and match the unit's own MAC
*and* its locally-administered variants (base `ethaddr`, VAPs at +1/+2/+3, and
`6a:`/`8e:`-prefixed forms). Adopting the AP to a controller is the proper fix,
because it replaces the onboarding state with a managed config; disabling
mesh/Multi-AP is the direct mitigation.

## Backups are secrets
Raw flash dumps are **not safe to share**. On the EWS377-FIT the `cert`
partition holds the unit's **cloud RSA private key** (a PEM
`-----BEGIN RSA PRIVATE KEY-----` block) plus a `SN/MAC/HWID` identity record,
and `userconfig` can hold credentials. Anything that dumps the boot region
(`0x0–0x1000000`) or "all partitions" includes them.

- **Never** post a backup, a raw dump, or `strings`/`grep` output of one to a
  GitHub issue, pastebin, forum, or chat. If you need to share a log, run it
  through `pelegrun redact` first (it scrubs PEM private keys — including
  truncated ones — bare base64 key bodies, `SN/MAC/HWID` records,
  `snextra=`/`sn=` values, and controller secrets including the prefixed
  `epc_secret=`/`cloud_secret=` forms) and eyeball the result.
- **A MAC plus a serial is a credential on the ECW line**, not just identity:
  the controller pre-shared key is derived from them. So `--mac` is not merely
  privacy hygiene on a printenv from one of these units, and a "harmless"
  console transcript that includes a `printenv` puts the whole identity on the
  wire.
- The backup bundle keeps `cert`/`userconfig` in a separate `secret/` directory,
  apart from the shareable evidence (`proc-mtd.txt`, `env-good.txt`, ART,
  APPSBL/APPSBLENV). Treat `secret/` as a password-grade secret.
- Encrypt the bundle at rest and store it offline, e.g.
  `tar c <bundle> | age -p > <bundle>.tar.age` (or `gpg -c`).
- Be careful with ART too: it carries factory calibration (and, on older units,
  MACs). It is not a key, but there is no reason to publish it.

## Scope & legal
Unofficial; **not affiliated with, endorsed by, or supported by EnGenius or
Senao.** Those names are used only to identify the affected products. For
**interoperability and self-hosting on hardware you own**. Do **not** use this for
warranty fraud, evading paid licensing on hardware you don't own, or defeating
theft protection. No vendor firmware is distributed here — you supply your own
images. Cross-flashing and synthetic serials are **unsupported/internal** and may
void warranty/support. No warranty; use at your own risk. Licensed under Blue Oak
Model License 1.0.0.
