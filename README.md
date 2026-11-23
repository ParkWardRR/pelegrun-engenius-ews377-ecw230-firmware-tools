<div align="center">

# Pelegrún · ap-hk07 firmware tools

No-brick toolkit to cross-flash and recover **EnGenius/Senao ap-hk07** access
points (EWS377AP v3 · EWS377-FIT · ECW230v3 · IPQ807x) over the network — no
UART required. One-field firmware re-head, Code27 serial provisioning,
bootloader env safety gate, and firmware family fingerprinting. Rust + Go.

Also knows the **ECW336** (`ap-hk01-c6`, Wi-Fi 6E): its SKU id, model code, MTD
layout, A/B selector and controller gate, plus the guard that stops you
re-heading an image onto the wrong board. That one is a **UART** job, not a
network one — [see below](#related-hardware-the-ecw336-and-why-its-path-is-not-network-only).

[![License: Blue Oak 1.0.0](https://img.shields.io/badge/License-Blue_Oak_1.0.0-0a7bbb.svg)](LICENSE)
[![CI: local](https://img.shields.io/badge/CI-local%20(make%20ci)-informational.svg)](Makefile)
[![Release](https://img.shields.io/github/v/release/ParkWardRR/pelegrun-ap-hk07-firmware-tools?color=success)](https://github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/releases)
[![Unofficial](https://img.shields.io/badge/vendor-unofficial-lightgrey.svg)](SAFETY.md)

![Rust](https://img.shields.io/badge/Rust-core-000000?logo=rust&logoColor=white)
![Go](https://img.shields.io/badge/Go-TUI%20%2B%20CLI-00ADD8?logo=go&logoColor=white)

<br/>

<img src="docs/tour.gif" alt="Pelegrún TUI dashboard" width="760"/>

<br/>

[![Bubble Tea](https://img.shields.io/badge/TUI-Bubble%20Tea-ff69b4?logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZD0iTTEyIDJhMTAgMTAgMCAxIDAgMCAyMCAxMCAxMCAwIDAgMCAwLTIweiIgZmlsbD0id2hpdGUiLz48L3N2Zz4=)](https://github.com/charmbracelet/bubbletea)

</div>

### TUI screenshots

<table>
<tr>
<td><img src="docs/screenshots/01-discover.png" alt="Discover — fingerprint firmware family" width="420"/></td>
<td><img src="docs/screenshots/04-safeguards.png" alt="Safeguards — why it can't brick" width="420"/></td>
</tr>
<tr>
<td><img src="docs/screenshots/05-identity.png" alt="Identity — mint a unique serial" width="420"/></td>
<td><img src="docs/screenshots/06-install.png" alt="Install — no-UART A/B flash" width="420"/></td>
</tr>
</table>

<details>
<summary>All 7 TUI screens</summary>

| Step | Screen | What it does |
|------|--------|--------------|
| 1 | Discover | Fingerprints the firmware family (Cloud · EWS/LuCI · FIT) |
| 2 | Connect | How to reach each family — SSH on **:8822**, not 22 |
| 3 | Back Up | Required read-only evidence bundle before flashing |
| 4 | Safeguards | Why the tool can't brick: append-only env gate |
| 5 | Identity | Mint a unique, collision-checked serial |
| 6 | Install | No-UART A/B flash: write the spare slot, reboot |
| 7 | Verify | Confirm the device came back as intended |

Navigate `↑↓` / `jk`, jump `g`/`G`, quit `q`.

</details>

---

> **Cross-flashing can brick hardware.** Pelegrún is designed so the common
> mistakes aren't reachable (see [the two invariants](#the-two-invariants)), but
> firmware work is never zero-risk. Read [`SAFETY.md`](SAFETY.md) and the
> [scope/legal](#scope--legal) notes first. Unofficial — not affiliated with
> EnGenius or Senao. For hardware you own.

## Why

The EWS377AP v3, EWS377-FIT, and ECW230v3 are the **same board** (Qualcomm
IPQ807x, Senao `ap-hk07`). Cross-flashing between them requires changing one
4-byte header field (`product_id`). But the manual path is easy to get wrong: a
stray `setconfig` wipes the bootloader env, a hand-rebuilt env bricks the boot
slot, SSH is on **port 8822**, and adoption fails silently on a blank serial.
Pelegrún encodes the safe path so those mistakes aren't reachable.

**The entire conversion is network-only** — SSH to set the serial, then upload
the patched image via the web GUI. No UART, no physical access needed.

## The two invariants

Two rules keep a failed flash recoverable over the network:

1. **Writes go to the `INACTIVE` A/B slot.** The working slot stays bootable — a
   bad image is undone with a factory-reset hold.
2. **The bootloader env is `APPEND-ONLY`.** The tool only adds single fields to an
   env it has *verified complete*; it **cannot** erase it or save a partial one.

**When UART *is* required:** only when the network path is already gone — a
wiped bootloader env, a rootfs that won't boot, or a first flash of an unproven
model as a safety net. **Plus one firmware-line exception** — see below.

## Related hardware: the ECW336, and why its path is not network-only

The **ECW336** (Wi-Fi 6E tri-band) shares this generation's **SoC — IPQ8074** —
and its Senao container is byte-identical in construction (same XOR key
`783c9ecf67b359ac`, same plaintext header), so the image-side tooling here reads
and patches its firmware fine.

It is **not** the same *board*, though — and the labels actively mislead here.
The DTB a live ECW336 boots sits in the config slot *named* `hk07` and even
carries `model = "…IPQ8074/AP-HK07"`, but its machine-readable id says
otherwise:

```
compatible = qcom,ipq8074-ap-hk01-c6, qcom,ipq8074-hk01, qcom,ipq8074
```

Senao writes its own board content into a generic QSDK slot (the stock kernel
ships ~25 of them), so **neither the slot name nor the `model` string is an
identity** — that same `AP-HK07` model string appears in the EWS377-FIT's DTB.
**Read `compatible`**, as OpenWrt's `board_detect` does: this unit is
`ap-hk01-c6`. Qualcomm's own `firmware_rdp_feature.ini` (shipped in the
ECW336's wifi-firmware volume) agrees — it has a
`#HK01-C6 - Pine RDPs : Pine6G + HK(2G+5G) in HK01 platform` section and **no
`ap-hk07` entry at all**.

Note `machid` won't separate them either: the ECW336 and an EWS377-FIT both
report `machid=0x8010006`, differing only in `hw_id` (`01010123` vs
`0101012B`). Same slot selected, different board content inside it.

Its 6 GHz radio is a **separate QCN9074 over PCIe** (`QCN9000_PCI0`) — Qualcomm
Pine PN02-class — not part of the HK01 host. (The `C6` is board revision 6; it
has nothing to do with 6 GHz, and there is no "6E HK01".) So treat the ECW336 as
*same SoC, adjacent board, extra radio* — not a drop-in sibling.

What does **not** transfer is the network-only workflow, and the reason is the
*firmware line*, not the board:

| firmware line | remote shell | consequence |
|---|---|---|
| FIT / EWS builds (what this tool targets) | **SSH on port 8822**, works | conversion is network-only, as documented above |
| ECW cloud builds (stock ECW336, stock ECW230v3) | **none usable** | UART needed for env changes |

On a stock ECW336 (`1.8.118-12`), `POST /api/mgm/ssh {"enable":true}` really does
start dropbear — port 22 opens and answers `SSH-2.0-dropbear`, which is already
better than older ap-hk07 managed builds where the daemon never started at all.
But **every login is refused**: `root` → `"Not support for root account."`,
`admin` → likewise. It is not a passwd problem (`/etc/passwd` gives both uid 0
and `/bin/ash`) and there is no second port — `/etc/config/dropbear` ships
`Port '22'` with `RootPasswordAuth 'on'`, which is why the password prompt is
accepted and the *account* is then rejected.

Practical upshot if you point this tool at an ECW-line device still on cloud
firmware: **budget a UART session.** Adoption onto a private controller also
needs the u-boot env `cloud_id=3` (the ECW-specific gate; `force_ac` and mDNS
are not substitutes until that is set). Full write-ups:

- [ECW336-firmware-tools](https://github.com/ParkWardRR/ECW336-firmware-tools)
  — the no-brick flash, reproduced on three units, and EPC adoption
- [engenius-field-guide → `ecw-private-cloud-mode`](https://github.com/ParkWardRR/engenius-field-guide/blob/main/ecw-private-cloud-mode.md)
  — the `cloud_id` mechanism and check-in status codes

The two invariants above hold unchanged on that hardware — they were, if
anything, re-confirmed by it: the ECW336 keeps its **only** copy of `ethaddr`,
`sn`, `snextra` and `hw_id` in the bootloader env (u-boot reports the ART copy
invalid and falls back to the Qualcomm dummy `00:03:7f:ba:db:ad`), so an erased
or hand-rebuilt env there is permanent identity loss.

### What the tool knows about it

Three units on the bench turned the ECW336 from a hypothesis into facts the code
now carries:

| | what it is | where |
|---|---|---|
| SKU id | `291`, corroborated by env `hw_id=01010123` (vendor 257 + 291) | `quarry inspect`, `quarry products` |
| board | `ap-hk01-c6` — cross-board re-heads refused | `quarry rehead` |
| model code | `E4T` (not `X__`) | `pelegrun check`, `quarry check` |
| A/B selector | env `active_fw` (`0`=FW1, `1`=FW2); `app_part` is not it | `pelegrun envcheck` |
| controller gate | env `cloud_id=3` = private cloud | `pelegrun envcheck` |
| shell | none usable → `needs_uart=true` | `pelegrun discover` |
| partitions | `cert`/`userconfig`/`crashdump` + `0:ETHPHYFW`; ART is `mtd12` | backup planner |

Two of those are traps rather than features. **The A/B rootfs labels are
slot-relative:** `rootfs` always names the slot you are *running from* and
`rootfs_1` the spare, so the labels swap after a slot switch and a remembered
offset writes the live slot. And the recovery picture is genuinely mixed — the
ECW336's u-boot *does* have a working TFTP recovery path (boot-menu option `2`),
which the ap-hk07 units never offered, but option `9` beside it writes the
bootloader and `e` erases the env that holds the unit's only MAC. The boot menu
wants `4`+CR within ~2 s; a bare `4` does not select it, and the prompt is
`IPQ807x# `, so anything matching on the ap-hk07's `(IPQ) #` will never fire.

Details and the per-unit procedure: [`docs/USAGE.md`](docs/USAGE.md#uart-notes-for-the-ecw336-u-boot-201601-ecw336-v100).

## Quick start

```console
# Patch an ECW230v3 image to flash from EWS firmware (product_id 284 → 282)
$ quarry rehead ecw230v3-v1.8.114-1.bin patched.bin --to 282

# Inspect any Senao image header
$ quarry inspect patched.bin

# Generate a unique serial and snextra for the target model
$ quarry serial  --model X42 --prefix EPC1 --suffix 0001
$ quarry snextra --model X42 --prefix EPC1

# Fingerprint what firmware an AP is running
$ pelegrun discover http://192.168.1.1

# Check bootloader env safety before writing
$ pelegrun envcheck env-dump.txt

# Scrub secrets before sharing a log
$ pelegrun redact bundle.txt --mac --value <serial>

# Launch the TUI dashboard
$ pelegrun
```

**Product ids:** `282` EWS377AP v3 · `300` EWS377-FIT · `284` ECW230v3 · `291` ECW336 · `275` ECW230 · `182` EWS377AP v2 · `285` ECW230S.
**Model codes:** `X44` EWS377AP v3 · `X45` EWS377-FIT · `X42` ECW230v3 · `E4T` ECW336 — **not all are `X`-prefixed.**
`hw_id` in the u-boot env (e.g. `0101012B` on the FIT: vendor `0x0101` + `0x012B`=299) is a
*different* number from the header `product_id` (300) — see
[`docs/USAGE.md`](docs/USAGE.md#hw_id-vs-product_id-open-data-request-8).

`quarry products` prints the SKU → board → model-code table, and **`quarry
rehead` refuses to patch an image across boards**: `282`/`300`/`284` are one
board, `291` is another, and a re-head rewrites only the SKU gate — the payload
keeps the donor's device tree either way.

## Install

Download a prebuilt binary from the
[latest release](https://github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/releases/latest)
and **verify the checksum**:

```console
$ curl -LO .../releases/latest/download/pelegrun-linux-amd64
$ curl -LO .../releases/latest/download/SHA256SUMS
$ sha256sum -c SHA256SUMS --ignore-missing && chmod +x pelegrun-linux-amd64
```

From source: `cargo build --release -p quarry` (Rust) and `cd go && go build ./cmd/pelegrun` (Go).

## Architecture

```mermaid
flowchart LR
  U([operator]) --> Q[quarry · Rust<br/>re-head + serial]
  U --> P[pelegrun · Go]
  P --> E[eyas · fingerprint]
  P --> H[hood · env gate]
  P --> R[redact · scrub]
  H -. fragile? .-> X[[refuse / stop]]
```

| Component | Role | Lang |
|---|---|---|
| **quarry** | Image header re-head, Code27 serial, snextra, inspect | Rust |
| **pelegrun** | TUI dashboard + CLI subcommands | Go |
| **eyas** | Fingerprint firmware family (Cloud · EWS/LuCI · FIT) | Go |
| **hood** | Bootloader env completeness gate | Go |
| **redact** | Secret scrubbing for logs and support bundles | Go |
| **band** | Code27 serial math (Go port of quarry's serial module) | Go |

## OpenWrt

For running **OpenWrt** on the EWS377AP v3, see the dedicated repo:
**[ews377apv3-openwrt](https://github.com/ParkWardRR/ews377apv3-openwrt)** —
porting plan, install guide, hardware reference, and community builds.

## Build & test

CI is **local only** — no GitHub Actions. `make` drives both languages:

```console
make ci            # fmt-check + lint + tests (race)
make test          # cargo test + go test
make dist          # cross-compiled binaries + SHA256SUMS → dist/
make tui           # run the dashboard
```

Run `make hooks` once per clone to gate `git push` on a green `make ci`.

## Scope & legal

Unofficial community tooling for **interoperability and self-hosting on hardware
you own**. "EnGenius" and "Senao" are trademarks of their owners, used only to
name the affected products; no endorsement or affiliation is implied. **No vendor
firmware is redistributed here** — you supply your own images. Do not use this for
warranty fraud, evading paid licensing on hardware you don't own, or defeating
theft protection. Cross-flashing/synthetic serials are unsupported and may void
warranty/support. No warranty; use at your own risk. See [`SAFETY.md`](SAFETY.md).

## Credits

Born from a real cross-flash + recovery saga documented in the
[engenius-field-guide](https://github.com/ParkWardRR/engenius-field-guide),
building on [DaveCorder's EnGenius notes](https://github.com/DaveCorder/EnGenius).
TUI by [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## License

[Blue Oak Model License 1.0.0](LICENSE).
