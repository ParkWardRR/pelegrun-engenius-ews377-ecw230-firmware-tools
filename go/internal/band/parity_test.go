package band

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The Go `band` package reimplements quarry's (Rust) Code27 math so pelegrun
// needs no subprocess. This test guards that reimplementation against the Rust
// source of truth: for many inputs, band's output must byte-match the quarry
// binary's. It's opt-in — set QUARRY_BIN or build `target/release/quarry`; if the
// binary isn't found the test skips, so CI (which doesn't build Rust in the Go
// job) stays green.
func findQuarry(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("QUARRY_BIN"); b != "" {
		return b
	}
	// From go/internal/band, the release binary sits at ../../../target/release.
	for _, rel := range []string{
		"../../../target/release/quarry",
		"../../../target/debug/quarry",
	} {
		if p, err := filepath.Abs(rel); err == nil {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	t.Skip("quarry binary not found (set QUARRY_BIN or run `cargo build -p quarry --release`)")
	return ""
}

func quarry(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command(findQuarry(t), args...).Output()
	return strings.TrimSpace(string(out)), err
}

// deterministic input generator (no rand → reproducible)
type lcg uint64

func (s *lcg) next() uint64 {
	*s = *s*6364136223846793005 + 1442695040888963407
	return uint64(*s) >> 33
}

const alnumSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func (s *lcg) str(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alnumSet[s.next()%uint64(len(alnumSet))]
	}
	return string(b)
}

func TestSerialParityWithQuarry(t *testing.T) {
	bin := findQuarry(t) // skips if absent
	t.Logf("comparing band (Go) against %s", bin)

	r := lcg(1)
	for i := 0; i < 300; i++ {
		prefix, model, suffix := r.str(4), r.str(3), r.str(4)

		goSerial, err := MakeSerial(prefix, model, suffix)
		if err != nil {
			t.Fatalf("band.MakeSerial(%q,%q,%q): %v", prefix, model, suffix, err)
		}
		rsSerial, err := quarry(t, "serial", "--prefix", prefix, "--model", model, "--suffix", suffix)
		if err != nil {
			t.Fatalf("quarry serial: %v", err)
		}
		if goSerial != rsSerial {
			t.Fatalf("serial mismatch for %q/%q/%q: band=%q quarry=%q", prefix, model, suffix, goSerial, rsSerial)
		}

		goSnextra, err := MakeSnextra(prefix, model, suffix)
		if err != nil {
			t.Fatalf("band.MakeSnextra: %v", err)
		}
		rsSnextra, err := quarry(t, "snextra", "--prefix", prefix, "--model", model, "--suffix", suffix)
		if err != nil {
			t.Fatalf("quarry snextra: %v", err)
		}
		if goSnextra != rsSnextra {
			t.Fatalf("snextra mismatch for %q/%q: band=%q quarry=%q", prefix, model, goSnextra, rsSnextra)
		}

		// --seq must encode identically on both sides too.
		n := uint32(r.next())
		goSeq := Seq4(n)
		rsSeq, err := quarry(t, "serial", "--prefix", prefix, "--model", model, "--seq", strconv.FormatUint(uint64(n), 10))
		if err != nil {
			t.Fatalf("quarry serial --seq: %v", err)
		}
		if want, _ := MakeSerial(prefix, model, goSeq); want != rsSeq {
			t.Fatalf("--seq mismatch for %d: band=%q quarry=%q", n, want, rsSeq)
		}
	}
}

func TestValidateParityWithQuarry(t *testing.T) {
	findQuarry(t) // skips if absent

	// A mix of valid serials and deliberately-corrupted ones; band.ValidateSerial
	// must agree with `quarry check` (exit 0 == valid) on every case.
	r := lcg(7)
	for i := 0; i < 200; i++ {
		s, _ := MakeSerial(r.str(4), r.str(3), r.str(4))
		if i%2 == 0 {
			// Corrupt the check char so ~half are invalid.
			b := []byte(s)
			b[len(b)-1] = alnumSet[(int(b[len(b)-1])+1)%len(alnumSet)]
			s = string(b)
		}
		goValid := ValidateSerial(s)
		_, err := quarry(t, "check", s)
		rsValid := err == nil // quarry exits non-zero on a bad check char
		if goValid != rsValid {
			t.Fatalf("validate mismatch for %q: band=%v quarry=%v", s, goValid, rsValid)
		}
	}
}
