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
- For the first flash on a new model, keep a UART adapter attached.

## Scope & legal
Unofficial; **not affiliated with, endorsed by, or supported by EnGenius or
Senao.** Those names are used only to identify the affected products. For
**interoperability and self-hosting on hardware you own**. Do **not** use this for
warranty fraud, evading paid licensing on hardware you don't own, or defeating
theft protection. No vendor firmware is distributed here — you supply your own
images. Cross-flashing and synthetic serials are **unsupported/internal** and may
void warranty/support. No warranty; use at your own risk. Licensed under Blue Oak
Model License 1.0.0.
