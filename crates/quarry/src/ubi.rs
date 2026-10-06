//! Raw UBI image reader + a bounded FDT (FIT) structure-block walker.
//!
//! Purpose-built for one check: does a `factory.ubi` image contain a `kernel`
//! volume whose payload is a FIT image with a `/configurations/config@hk07`
//! node? That specific field is the one that bricked the EWS377AP v3 OpenWrt
//! port on real hardware — the bootloader's `bootipq` selects a FIT config by
//! board name, and a build with only the generic `config@1` fails to boot
//! from NAND even though the image itself is otherwise valid (see
//! `openwrt-ews377ap-v3/results-2026-09-06/FLASH-STAGE2-RESULT.md` in this
//! repo's history for the real-hardware trace).
//!
//! This is **not** a general UBI/UBIFS implementation. It reads exactly enough
//! of the UBI erase-block/volume-table structure to reassemble one named
//! volume's payload by logical erase block (LEB), then walks that payload as
//! a flattened device tree (FDT) looking for one node. It does not validate
//! UBI header CRCs and does not handle bad-PEB remapping. It also does not
//! rely on the VID header's `data_size` field: that field is only meaningful
//! for **static** UBI volumes, and factory-image volumes such as `kernel` are
//! dynamic, so every LEB (including the last) is treated as full-size. The
//! FDT walk below is self-bounding via the FDT header's own size fields, so
//! trailing padding bytes in the final LEB are harmless.

use crate::Error;

const PEB_SIZE: usize = 0x20000; // 128 KiB — matches the EWS377AP v3 NAND geometry.
const EC_MAGIC: [u8; 4] = *b"UBI#";
const VID_MAGIC: [u8; 4] = *b"UBI!";
const LAYOUT_VOL_ID: u32 = 0x7fff_efff;
const VTBL_RECORD_SIZE: usize = 172;

// EC header layout (64 bytes, big-endian fields), per `struct ubi_ec_hdr` in
// linux/mtd/ubi-media.h:
//   magic[4] version[1] padding[3] ec[8] vid_hdr_offset[4] data_offset[4] image_seq[4] ...
const EC_OFF_VID_HDR_OFFSET: usize = 0x10;
const EC_OFF_DATA_OFFSET: usize = 0x14;

// VID header layout (64 bytes), relative to `vid_hdr_offset` within the PEB,
// per `struct ubi_vid_hdr`. We only need `vol_id`/`lnum` (`data_size` is
// static-volume-only — see the module doc above).
const VID_OFF_VOL_ID: usize = 0x08;
const VID_OFF_LNUM: usize = 0x0c;

// Volume table record layout (172 bytes):
//   reserved_pebs[4] alignment[4] data_pad[4] vol_type[1] upd_marker[1]
//   name_len[2] name[128] flags[1] ... crc[4]
const VTBL_OFF_NAME_LEN: usize = 0x0e;
const VTBL_OFF_NAME: usize = 0x10;
const VTBL_NAME_MAX: usize = 128;

fn be_u32_at(data: &[u8], off: usize) -> Option<u32> {
    data.get(off..off + 4)
        .map(|b| u32::from_be_bytes([b[0], b[1], b[2], b[3]]))
}

/// One physical erase block's UBI headers, if it carries valid ones.
struct PebInfo {
    data_offset: usize,
    vol_id: u32,
    lnum: u32,
}

fn read_peb(data: &[u8], peb_index: usize) -> Option<PebInfo> {
    let start = peb_index * PEB_SIZE;
    let peb = data.get(start..start + PEB_SIZE)?;
    if peb.get(0..4)? != EC_MAGIC {
        return None; // free/erased or non-UBI block
    }
    let vid_hdr_offset = be_u32_at(peb, EC_OFF_VID_HDR_OFFSET)? as usize;
    let data_offset = be_u32_at(peb, EC_OFF_DATA_OFFSET)? as usize;
    let vid = peb.get(vid_hdr_offset..vid_hdr_offset + 64)?;
    if vid.get(0..4)? != VID_MAGIC {
        return None; // EC header present but PEB not yet mapped to a volume
    }
    let vol_id = be_u32_at(vid, VID_OFF_VOL_ID)?;
    let lnum = be_u32_at(vid, VID_OFF_LNUM)?;
    Some(PebInfo {
        data_offset,
        vol_id,
        lnum,
    })
}

/// Read the volume-name table (the UBI "layout volume") and return the
/// `vol_id` whose name matches `want_name`, if present.
fn find_volume_id(data: &[u8], want_name: &str) -> Result<u32, Error> {
    let num_pebs = data.len() / PEB_SIZE;
    // The layout volume is small and conventionally lives at lnum 0/1 near the
    // front of the image; scan every PEB rather than assuming a position.
    for i in 0..num_pebs {
        let Some(peb) = read_peb(data, i) else {
            continue;
        };
        if peb.vol_id != LAYOUT_VOL_ID {
            continue;
        }
        let start = i * PEB_SIZE + peb.data_offset;
        let leb = data
            .get(start..start + (PEB_SIZE - peb.data_offset))
            .ok_or(Error::TooShort {
                need: start + (PEB_SIZE - peb.data_offset),
                got: data.len(),
            })?;
        let max_records = leb.len() / VTBL_RECORD_SIZE;
        for vol_id in 0..max_records {
            let rec = &leb[vol_id * VTBL_RECORD_SIZE..(vol_id + 1) * VTBL_RECORD_SIZE];
            let name_len =
                u16::from_be_bytes([rec[VTBL_OFF_NAME_LEN], rec[VTBL_OFF_NAME_LEN + 1]]) as usize;
            if name_len == 0 || name_len > VTBL_NAME_MAX {
                continue;
            }
            let name_bytes = &rec[VTBL_OFF_NAME..VTBL_OFF_NAME + name_len];
            if name_bytes == want_name.as_bytes() {
                return Ok(vol_id as u32);
            }
        }
        // Found *a* layout-volume copy; that's enough to answer from.
        return Err(Error::VolumeNotFound {
            name: want_name.to_string(),
        });
    }
    Err(Error::NotUbiImage)
}

/// Reassemble a named volume's payload by walking every PEB, collecting the
/// ones tagged with the target `vol_id`, ordering them by `lnum`, and
/// concatenating each LEB's **full** data region (dynamic volumes don't carry
/// a meaningful trailing-LEB size at the UBI layer — see module doc).
fn read_volume(data: &[u8], vol_id: u32) -> Result<Vec<u8>, Error> {
    let num_pebs = data.len() / PEB_SIZE;
    let mut lebs: Vec<(u32, usize, usize)> = Vec::new(); // (lnum, peb_index, data_offset)
    for i in 0..num_pebs {
        let Some(peb) = read_peb(data, i) else {
            continue;
        };
        if peb.vol_id != vol_id {
            continue;
        }
        lebs.push((peb.lnum, i, peb.data_offset));
    }
    if lebs.is_empty() {
        return Err(Error::VolumeNotFound {
            name: format!("vol_id {vol_id}"),
        });
    }
    lebs.sort_by_key(|&(lnum, _, _)| lnum);

    let mut out = Vec::new();
    for &(_, peb_index, data_offset) in &lebs {
        let leb_size = PEB_SIZE - data_offset;
        let start = peb_index * PEB_SIZE + data_offset;
        let leb = data.get(start..start + leb_size).ok_or(Error::TooShort {
            need: start + leb_size,
            got: data.len(),
        })?;
        out.extend_from_slice(leb);
    }
    Ok(out)
}

// --- FDT (flattened device tree) structure-block walk -----------------

const FDT_MAGIC: u32 = 0xd00d_feed;
const FDT_BEGIN_NODE: u32 = 0x0000_0001;
const FDT_END_NODE: u32 = 0x0000_0002;
const FDT_PROP: u32 = 0x0000_0003;
const FDT_NOP: u32 = 0x0000_0004;
const FDT_END: u32 = 0x0000_0009;

/// Does this FDT blob contain `/configurations/<child_name>`?
///
/// Walks the structure block token-by-token, tracking the current node-name
/// stack, and reports whether `child_name` appears as an immediate child of a
/// node literally named `"configurations"`. Bounded by `off_dt_struct` +
/// `size_dt_struct` from the header — never reads past the declared struct
/// block, so trailing padding in a reassembled volume is harmless.
#[cfg(test)]
fn fdt_has_config_child(fdt: &[u8], child_name: &str) -> Result<bool, Error> {
    Ok(fdt_config_children(fdt)?.iter().any(|n| n == child_name))
}

/// Every immediate child of `/configurations`, in tree order.
fn fdt_config_children(fdt: &[u8]) -> Result<Vec<String>, Error> {
    if fdt.len() < 40 || be_u32_at(fdt, 0) != Some(FDT_MAGIC) {
        return Err(Error::NotFdt);
    }
    let off_dt_struct = be_u32_at(fdt, 8).ok_or(Error::NotFdt)? as usize;
    let size_dt_struct = be_u32_at(fdt, 36).ok_or(Error::NotFdt)? as usize;
    let struct_end = off_dt_struct
        .checked_add(size_dt_struct)
        .filter(|&e| e <= fdt.len())
        .ok_or(Error::NotFdt)?;

    let mut pos = off_dt_struct;
    let mut node_stack: Vec<String> = Vec::new();
    let mut found: Vec<String> = Vec::new();

    while pos + 4 <= struct_end {
        let token = be_u32_at(fdt, pos).ok_or(Error::NotFdt)?;
        pos += 4;
        match token {
            FDT_BEGIN_NODE => {
                let name_start = pos;
                let name_end = fdt[name_start..struct_end]
                    .iter()
                    .position(|&b| b == 0)
                    .map(|p| name_start + p)
                    .ok_or(Error::NotFdt)?;
                let name = String::from_utf8_lossy(&fdt[name_start..name_end]).into_owned();
                pos = align4(name_end + 1);

                if node_stack.last().map(String::as_str) == Some("configurations") {
                    found.push(name.clone());
                }
                node_stack.push(name);
            }
            FDT_END_NODE => {
                node_stack.pop();
            }
            FDT_PROP => {
                let len = be_u32_at(fdt, pos).ok_or(Error::NotFdt)? as usize;
                pos += 8; // len[4] + nameoff[4]
                pos = align4(pos + len);
            }
            FDT_NOP => {}
            FDT_END => break,
            _ => return Err(Error::NotFdt),
        }
    }
    Ok(found)
}

fn align4(n: usize) -> usize {
    (n + 3) & !3
}

/// Result of checking a `factory.ubi` for the `config@<board>` FIT config node.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FactoryUbiCheck {
    pub kernel_volume_bytes: usize,
    /// `config@<board>` was present (the single board passed to the check).
    pub has_config: bool,
    /// Every `/configurations/*` child node name in the FIT (e.g. `config@hk07`,
    /// `config@hk08`), so callers can check for any/all or just list them.
    pub configs: Vec<String>,
}

/// Confirm `data` is a UBI image, locate its `kernel` volume, and check
/// whether that volume's FIT payload declares `/configurations/config@<board>`.
///
/// `board` is the FIT config-node suffix to require, e.g. `"hk07"` for this
/// board family — pass whatever `DEVICE_DTS_CONFIG` names on the target.
pub fn check_factory_ubi(data: &[u8], board: &str) -> Result<FactoryUbiCheck, Error> {
    if data.len() < PEB_SIZE || data[0..4] != EC_MAGIC {
        return Err(Error::NotUbiImage);
    }
    let kernel_vol_id = find_volume_id(data, "kernel")?;
    let kernel = read_volume(data, kernel_vol_id)?;
    let configs = fdt_config_children(&kernel)?;
    let config_name = format!("config@{board}");
    Ok(FactoryUbiCheck {
        kernel_volume_bytes: kernel.len(),
        has_config: configs.contains(&config_name),
        configs,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Build a minimal synthetic FDT with `/configurations/{names}`.
    fn synth_fdt(config_names: &[&str]) -> Vec<u8> {
        let mut strings = Vec::new();
        let mut structure = Vec::new();

        fn push_token(s: &mut Vec<u8>, t: u32) {
            s.extend_from_slice(&t.to_be_bytes());
        }
        fn push_begin_node(s: &mut Vec<u8>, name: &str) {
            push_token(s, FDT_BEGIN_NODE);
            s.extend_from_slice(name.as_bytes());
            s.push(0);
            while !s.len().is_multiple_of(4) {
                s.push(0);
            }
        }
        fn push_end_node(s: &mut Vec<u8>) {
            push_token(s, FDT_END_NODE);
        }

        push_begin_node(&mut structure, ""); // root
        push_begin_node(&mut structure, "configurations");
        for name in config_names {
            push_begin_node(&mut structure, name);
            push_end_node(&mut structure);
        }
        push_end_node(&mut structure); // /configurations
        push_end_node(&mut structure); // root
        push_token(&mut structure, FDT_END);
        strings.push(0); // empty string table is fine; we never reference nameoff

        let header_len = 40usize;
        let off_dt_struct = header_len;
        let off_dt_strings = off_dt_struct + structure.len();
        let mut fdt = vec![0u8; off_dt_strings + strings.len()];
        fdt[0..4].copy_from_slice(&FDT_MAGIC.to_be_bytes());
        let totalsize = fdt.len() as u32;
        fdt[4..8].copy_from_slice(&totalsize.to_be_bytes());
        fdt[8..12].copy_from_slice(&(off_dt_struct as u32).to_be_bytes());
        fdt[12..16].copy_from_slice(&(off_dt_strings as u32).to_be_bytes());
        fdt[36..40].copy_from_slice(&(structure.len() as u32).to_be_bytes());
        fdt[off_dt_struct..off_dt_struct + structure.len()].copy_from_slice(&structure);
        fdt[off_dt_strings..].copy_from_slice(&strings);
        fdt
    }

    #[test]
    fn fdt_finds_matching_config_child() {
        let fdt = synth_fdt(&["config@1", "config@hk07"]);
        assert!(fdt_has_config_child(&fdt, "config@hk07").unwrap());
        assert!(!fdt_has_config_child(&fdt, "config@ap8220").unwrap());
    }

    #[test]
    fn lists_all_config_children() {
        // ECW230v3 image carries two FIT configs (issue #6).
        let fdt = synth_fdt(&["config@hk07", "config@hk08"]);
        assert_eq!(
            fdt_config_children(&fdt).unwrap(),
            vec!["config@hk07", "config@hk08"]
        );
        let img = synth_ubi(&fdt);
        let r = check_factory_ubi(&img, "hk08").unwrap();
        assert!(r.has_config);
        assert_eq!(r.configs.len(), 2);
    }

    #[test]
    fn fdt_rejects_bad_magic() {
        let mut fdt = synth_fdt(&["config@hk07"]);
        fdt[0] = 0;
        assert!(matches!(
            fdt_has_config_child(&fdt, "config@hk07"),
            Err(Error::NotFdt)
        ));
    }

    /// Build a minimal synthetic UBI image: a layout-volume PEB (vol_id
    /// 0x7fffefff) with one vtbl record naming "kernel" -> vol_id 1, followed
    /// by one or more PEBs carrying vol_id 1's LEBs with `payload`.
    fn synth_ubi(payload: &[u8]) -> Vec<u8> {
        fn write_ec_header(peb: &mut [u8], vid_hdr_offset: u32, data_offset: u32) {
            peb[0..4].copy_from_slice(&EC_MAGIC);
            peb[EC_OFF_VID_HDR_OFFSET..EC_OFF_VID_HDR_OFFSET + 4]
                .copy_from_slice(&vid_hdr_offset.to_be_bytes());
            peb[EC_OFF_DATA_OFFSET..EC_OFF_DATA_OFFSET + 4]
                .copy_from_slice(&data_offset.to_be_bytes());
        }
        fn write_vid_header(peb: &mut [u8], vid_hdr_offset: usize, vol_id: u32, lnum: u32) {
            let vid = &mut peb[vid_hdr_offset..vid_hdr_offset + 64];
            vid[0..4].copy_from_slice(&VID_MAGIC);
            vid[VID_OFF_VOL_ID..VID_OFF_VOL_ID + 4].copy_from_slice(&vol_id.to_be_bytes());
            vid[VID_OFF_LNUM..VID_OFF_LNUM + 4].copy_from_slice(&lnum.to_be_bytes());
        }

        const VID_HDR_OFFSET: u32 = 2048;
        const DATA_OFFSET: u32 = 4096;
        let leb_size = PEB_SIZE - DATA_OFFSET as usize;
        let n_data_pebs = payload.len().div_ceil(leb_size).max(1);
        let n_pebs = 1 + n_data_pebs; // 1 layout-volume PEB + data PEBs

        let mut img = vec![0xffu8; n_pebs * PEB_SIZE];

        // PEB 0: layout volume, one vtbl record for "kernel" at vol_id 1.
        {
            let peb = &mut img[0..PEB_SIZE];
            write_ec_header(peb, VID_HDR_OFFSET, DATA_OFFSET);
            write_vid_header(peb, VID_HDR_OFFSET as usize, LAYOUT_VOL_ID, 0);
            let leb_start = DATA_OFFSET as usize;
            let rec_start = leb_start + VTBL_RECORD_SIZE; // record for vol_id 1
            let name = b"kernel";
            peb[rec_start + VTBL_OFF_NAME_LEN..rec_start + VTBL_OFF_NAME_LEN + 2]
                .copy_from_slice(&(name.len() as u16).to_be_bytes());
            peb[rec_start + VTBL_OFF_NAME..rec_start + VTBL_OFF_NAME + name.len()]
                .copy_from_slice(name);
        }

        // PEBs 1.. : vol_id 1 ("kernel"), one LEB per PEB, in order. The final
        // chunk is padded to a full LEB with zero bytes — dynamic volumes
        // don't shrink their last LEB, and the FDT walk is self-bounding.
        for (lnum, chunk) in payload.chunks(leb_size).enumerate() {
            let peb_index = 1 + lnum;
            let peb = &mut img[peb_index * PEB_SIZE..(peb_index + 1) * PEB_SIZE];
            write_ec_header(peb, VID_HDR_OFFSET, DATA_OFFSET);
            write_vid_header(peb, VID_HDR_OFFSET as usize, 1, lnum as u32);
            let leb_start = DATA_OFFSET as usize;
            peb[leb_start..leb_start + chunk.len()].copy_from_slice(chunk);
        }

        img
    }

    #[test]
    fn check_factory_ubi_finds_config_hk07() {
        let fdt = synth_fdt(&["config@1", "config@hk07"]);
        let img = synth_ubi(&fdt);
        let result = check_factory_ubi(&img, "hk07").unwrap();
        assert!(result.has_config);
    }

    #[test]
    fn check_factory_ubi_missing_config_reports_false() {
        let fdt = synth_fdt(&["config@1"]); // no config@hk07
        let img = synth_ubi(&fdt);
        let result = check_factory_ubi(&img, "hk07").unwrap();
        assert!(!result.has_config);
    }

    #[test]
    fn check_factory_ubi_spans_multiple_lebs() {
        // Payload bigger than one LEB, to exercise multi-PEB reassembly.
        let leb_size = PEB_SIZE - 4096;
        let mut names: Vec<String> = (0..8000).map(|i| format!("config@pad{i}")).collect();
        names.push("config@hk07".to_string());
        let name_refs: Vec<&str> = names.iter().map(String::as_str).collect();
        let fdt = synth_fdt(&name_refs);
        assert!(fdt.len() > leb_size, "test fixture should span >1 LEB");
        let img = synth_ubi(&fdt);
        assert!(
            img.len() > 2 * PEB_SIZE,
            "test fixture should span >1 data PEB"
        );
        let result = check_factory_ubi(&img, "hk07").unwrap();
        assert!(result.has_config);
    }

    #[test]
    fn check_factory_ubi_rejects_non_ubi() {
        let junk = vec![0u8; PEB_SIZE * 2];
        assert!(matches!(
            check_factory_ubi(&junk, "hk07"),
            Err(Error::NotUbiImage)
        ));
    }

    #[test]
    fn check_factory_ubi_rejects_missing_kernel_volume() {
        // Layout volume PEB with no "kernel" record at all.
        let mut img = vec![0xffu8; PEB_SIZE];
        img[0..4].copy_from_slice(&EC_MAGIC);
        img[EC_OFF_VID_HDR_OFFSET..EC_OFF_VID_HDR_OFFSET + 4]
            .copy_from_slice(&2048u32.to_be_bytes());
        img[EC_OFF_DATA_OFFSET..EC_OFF_DATA_OFFSET + 4].copy_from_slice(&4096u32.to_be_bytes());
        let vid = &mut img[2048..2048 + 64];
        vid[0..4].copy_from_slice(&VID_MAGIC);
        vid[VID_OFF_VOL_ID..VID_OFF_VOL_ID + 4].copy_from_slice(&LAYOUT_VOL_ID.to_be_bytes());
        assert!(matches!(
            check_factory_ubi(&img, "hk07"),
            Err(Error::VolumeNotFound { .. })
        ));
    }
}
