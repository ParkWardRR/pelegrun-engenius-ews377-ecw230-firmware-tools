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
  on the EWS377AP v3). The plan fails closed if a critical name is missing or
  ambiguous, and dumps are named by partition + size, not `mtdN`.
- Treat **ART** (calibration + factory MACs) as **read-only** — never write it.
- Keep pristine, unpatched stock images as your rollback path.
- For the first flash on a new model, keep a UART adapter attached. Recovery
  differs by hardware revision (the EWS377-FIT's u-boot 2.1.0 has a boot menu,
  512 MiB RAM and chunked-read limits) — read
  [`docs/USAGE.md`](docs/USAGE.md#uart-notes-for-the-newer-ews377-fit-u-boot-210-hardware)
  before you open the case.
- **Identity vars:** never `env default -a` a unit that still has `ethaddr` /
  `hw_id` / `sn` / `snextra`; restore from the saved `printenv` backup instead.

## Backups are secrets
Raw flash dumps are **not safe to share**. On the EWS377-FIT the `cert`
partition holds the unit's **cloud RSA private key** (a PEM
`-----BEGIN RSA PRIVATE KEY-----` block) plus a `SN/MAC/HWID` identity record,
and `userconfig` can hold credentials. Anything that dumps the boot region
(`0x0–0x1000000`) or "all partitions" includes them.

- **Never** post a backup, a raw dump, or `strings`/`grep` output of one to a
  GitHub issue, pastebin, forum, or chat. If you need to share a log, run it
  through `pelegrun redact` first (it scrubs PEM private keys — including
  truncated ones — bare base64 key bodies, `SN/MAC/HWID` records, and
  `snextra=`/`sn=` values) and eyeball the result.
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
