// Package mews builds the per-device backup/evidence bundle — the shelter you can
// always return the bird to. It plans the (read-only) capture commands and names
// the bundle deterministically; running the commands is the accessor's job.
//
// Constitution III: the bundle is a HARD gate before any destructive step, and
// ART is read-only-backup, never write.
//
// MTD partition indices are NOT stable across units/firmware (the EWS377-FIT has
// extra cert/userconfig/crashdump partitions, so ART is mtd12 there, mtd11 on the
// EWS377AP v3). Partitions are therefore resolved BY NAME from /proc/mtd and the
// plan fails closed unless each critical name matches exactly once.
package mews

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Partition is one `/proc/mtd` row.
type Partition struct {
	Index  int
	Size   uint64
	Name   string
	Offset int64  // not in /proc/mtd; -1 unless supplied by the caller
	Line   string // the raw /proc/mtd line, recorded next to the dump
}

var procMtdLine = regexp.MustCompile(`^mtd(\d+):\s+([0-9a-fA-F]+)\s+([0-9a-fA-F]+)\s+"([^"]*)"`)

// ParseProcMtd parses the contents of /proc/mtd.
func ParseProcMtd(s string) ([]Partition, error) {
	var parts []Partition
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		m := procMtdLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		size, err := strconv.ParseUint(m[2], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("bad size in %q: %w", line, err)
		}
		parts = append(parts, Partition{Index: idx, Size: size, Name: m[4], Offset: -1, Line: line})
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no partitions found in /proc/mtd")
	}
	return parts, nil
}

// Artifact is one file the backup must contain, with how to capture it.
type Artifact struct {
	Name     string   // file name inside the bundle
	Command  string   // read-only command that produces it (over the accessor)
	Critical bool     // if true, the pipeline must abort when this is missing
	Secret   bool     // holds key material/identity — never publish or attach to an issue
	Names    []string // accepted /proc/mtd partition names (lowercased); nil for non-MTD captures
	ProcLine string   // the /proc/mtd line of the resolved partition
}

// Dir is the bundle sub-directory the artifact is written to. Secret artifacts
// are kept apart from the shareable bundle.
func (a Artifact) Dir() string {
	if a.Secret {
		return "secret"
	}
	return "."
}

// partitionSpec is a named partition the backup wants.
type partitionSpec struct {
	slug     string
	names    []string
	critical bool
	secret   bool
}

var specs = []partitionSpec{
	{slug: "appsblenv", names: []string{"0:appsblenv", "appsblenv"}, critical: true},
	{slug: "appsbl", names: []string{"0:appsbl", "appsbl"}, critical: true},
	{slug: "art", names: []string{"0:art", "art"}, critical: true},
	// cert holds the unit's cloud RSA private key + SN/MAC/HWID record; useful
	// for restore, but a secret. userconfig can hold credentials.
	{slug: "cert", names: []string{"cert"}, secret: true},
	{slug: "userconfig", names: []string{"userconfig"}, secret: true},
}

// Plan returns the layout-independent capture plan, for display. Real commands
// come from Resolve once /proc/mtd has been read.
func Plan() []Artifact {
	arts := []Artifact{
		{Name: "proc-mtd.txt", Command: "cat /proc/mtd", Critical: true},
		{Name: "env-good.txt", Command: "fw_printenv", Critical: true},
	}
	for _, sp := range specs {
		arts = append(arts, Artifact{
			Name:     fileName(sp.slug, nil),
			Command:  fmt.Sprintf("dd if=/dev/mtdN bs=64k  # N = %q in /proc/mtd", sp.names[0]),
			Critical: sp.critical,
			Secret:   sp.secret,
			Names:    sp.names,
		})
	}
	return append(arts, Artifact{Name: "dmesg.txt", Command: "dmesg"})
}

// fileName names a dump by partition name (+ index-independent slug), never by
// bare mtd index. With a resolved partition it includes the size, so a layout
// change shows up in the file name.
func fileName(slug string, p *Partition) string {
	if p == nil {
		return slug + ".bin"
	}
	return fmt.Sprintf("%s-%#x.bin", slug, p.Size)
}

// Resolve maps the plan onto a real /proc/mtd table. It FAILS CLOSED: every
// critical partition must be found exactly once by name, otherwise it returns an
// error and no commands (an operator must never believe ART is saved when the
// wrong partition was dumped). Optional partitions that are absent are skipped;
// ambiguous ones are an error.
func Resolve(procMtd string) ([]Artifact, error) {
	parts, err := ParseProcMtd(procMtd)
	if err != nil {
		return nil, err
	}
	var out []Artifact
	for _, a := range Plan() {
		if a.Names == nil {
			out = append(out, a)
			continue
		}
		sp := specFor(a)
		var hits []Partition
		for _, p := range parts {
			for _, n := range sp.names {
				if strings.EqualFold(p.Name, n) {
					hits = append(hits, p)
					break
				}
			}
		}
		switch {
		case len(hits) > 1:
			return nil, fmt.Errorf("partition %q matches %d /proc/mtd entries; refusing to guess", sp.names[0], len(hits))
		case len(hits) == 0 && sp.critical:
			return nil, fmt.Errorf("critical partition %q not found in /proc/mtd; refusing to back up by index", sp.names[0])
		case len(hits) == 0:
			continue
		}
		p := hits[0]
		a.Name = fileName(sp.slug, &p)
		a.Command = fmt.Sprintf("dd if=/dev/mtd%d bs=64k", p.Index)
		a.ProcLine = p.Line
		out = append(out, a)
	}
	return out, nil
}

func specFor(a Artifact) partitionSpec {
	for _, sp := range specs {
		if sp.names[0] == a.Names[0] {
			return sp
		}
	}
	return partitionSpec{}
}

// Sanity checks a captured dump for the partition it claims to be. It returns an
// error if the content cannot be that partition (e.g. a crashdump captured as ART).
func Sanity(slug string, data []byte) error {
	switch slug {
	case "art":
		if len(data) != 512*1024 {
			return fmt.Errorf("ART should be 512 KiB, got %d bytes", len(data))
		}
		blk := data[0x1000 : 0x1000+0x200]
		if bytes.Count(blk, []byte{0xff}) == len(blk) {
			return fmt.Errorf("ART calibration block at 0x1000 is all 0xff — not calibration data")
		}
	case "appsblenv":
		if len(data) < 8 {
			return fmt.Errorf("APPSBLENV too short (%d bytes)", len(data))
		}
		// 4-byte CRC, then key=value\0 pairs. (Redundant-env layouts add a flag
		// byte; tolerate one.)
		body := data[4:]
		for _, off := range []int{0, 1} {
			if looksLikeEnv(body[off:]) {
				return nil
			}
		}
		return fmt.Errorf("APPSBLENV does not start with a 4-byte CRC followed by key=value pairs")
	}
	return nil
}

var envKey = regexp.MustCompile(`^[A-Za-z0-9_:#.-]+=`)

func looksLikeEnv(b []byte) bool {
	end := bytes.IndexByte(b, 0)
	if end <= 0 {
		return false
	}
	return envKey.Match(b[:end])
}

// SecretWarning is printed whenever secret artifacts are captured.
const SecretWarning = "The files in secret/ hold the unit's cloud RSA private key and identity (SN/MAC/HWID). " +
	"NEVER publish them or attach them to a GitHub issue, pastebin, or forum post; " +
	"store them encrypted (age/gpg) and offline."

// BundleName is the deterministic, sortable bundle directory for a device.
// Example: 2026-09-02_ScuderiaToroRosso_88-DC-97-04-44-07
func BundleName(date, asset, mac string) string {
	safeMac := strings.NewReplacer(":", "-", " ", "").Replace(strings.ToLower(mac))
	safeAsset := strings.NewReplacer(" ", "-", "/", "-").Replace(asset)
	return fmt.Sprintf("%s_%s_%s", date, safeAsset, safeMac)
}

// Manifest records what was captured and its integrity hashes.
type Manifest struct {
	Bundle    string            `json:"bundle"`
	Device    string            `json:"device"`
	MAC       string            `json:"mac"`
	Capdate   string            `json:"captured"`
	SHA256    map[string]string `json:"sha256"` // artifact name -> hex digest
	Firmware  string            `json:"firmware,omitempty"`
	SrcImage  string            `json:"src_image_sha256,omitempty"`
	PatchedTo string            `json:"patched_product_id,omitempty"`
}

// MissingCritical returns the critical artifacts absent from a captured set —
// the pipeline must refuse to continue if this is non-empty. Names are the
// resolved artifact names from Resolve.
func MissingCritical(plan []Artifact, captured map[string]bool) []string {
	var miss []string
	for _, a := range plan {
		if a.Critical && !captured[a.Name] {
			miss = append(miss, a.Name)
		}
	}
	return miss
}
