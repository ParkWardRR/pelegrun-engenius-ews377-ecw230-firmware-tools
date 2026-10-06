package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCap invokes run() capturing stdout/stderr and the exit code.
func runCap(args ...string) (code int, out, errout string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestVersionAndPlanAndHelp(t *testing.T) {
	if code, out, _ := runCap("version"); code != 0 || !strings.Contains(out, "pelegrun "+Version) {
		t.Errorf("version: code=%d out=%q", code, out)
	}
	if code, out, _ := runCap("plan"); code != 0 || !strings.Contains(out, "APPEND-ONLY") {
		t.Errorf("plan: code=%d out=%q", code, out)
	}
	for _, h := range []string{"help", "-h", "--help"} {
		if code, out, _ := runCap(h); code != 0 || !strings.Contains(out, "USAGE:") {
			t.Errorf("%s: code=%d out=%q", h, code, out)
		}
	}
}

func TestNoArgsPrintsUsageWhenNotTTY(t *testing.T) {
	// In `go test`, stdout is not a terminal, so runTUI prints usage instead of
	// launching the Bubble Tea program.
	code, out, _ := runCap()
	if code != 0 || !strings.Contains(out, "USAGE:") {
		t.Errorf("no-args: code=%d out=%q", code, out)
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	code, _, errout := runCap("frobnicate")
	if code != 2 || !strings.Contains(errout, "unknown command") {
		t.Errorf("unknown: code=%d err=%q", code, errout)
	}
}

func TestSerialAndSnextra(t *testing.T) {
	code, out, _ := runCap("serial", "--model", "X42", "--prefix", "SWLW", "--suffix", "0001")
	if code != 0 || strings.TrimSpace(out) != "SWLWX420001T" {
		t.Errorf("serial: code=%d out=%q", code, out)
	}
	// Missing --model is a usage error.
	if code, _, errout := runCap("serial"); code != 1 || !strings.Contains(errout, "--model") {
		t.Errorf("serial no-model: code=%d err=%q", code, errout)
	}
	// Bad model length surfaces the library error.
	if code, _, errout := runCap("serial", "--model", "TOOLONG"); code != 1 || errout == "" {
		t.Errorf("serial bad-model: code=%d err=%q", code, errout)
	}
	if code, out, _ := runCap("snextra", "--model", "X42"); code != 0 || len(strings.TrimSpace(out)) != 20 {
		t.Errorf("snextra: code=%d out=%q", code, out)
	}
	if code, _, errout := runCap("snextra"); code != 1 || !strings.Contains(errout, "--model") {
		t.Errorf("snextra no-model: code=%d err=%q", code, errout)
	}
}

func TestCheck(t *testing.T) {
	// A hardware-verified valid serial.
	code, out, _ := runCap("check", "EPC1X4200011")
	if code != 0 || !strings.Contains(out, "valid=true") || !strings.Contains(out, "model_code=X42") {
		t.Errorf("check valid: code=%d out=%q", code, out)
	}
	// Wrong check char → exit 1 and valid=false reported.
	code, out, errout := runCap("check", "EPC1X4200012")
	if code != 1 || !strings.Contains(out, "valid=false") || !strings.Contains(errout, "check character") {
		t.Errorf("check invalid: code=%d out=%q err=%q", code, out, errout)
	}
	if code, _, errout := runCap("check"); code != 1 || !strings.Contains(errout, "required") {
		t.Errorf("check no-arg: code=%d err=%q", code, errout)
	}
}

func TestEnvcheck(t *testing.T) {
	dir := t.TempDir()
	complete := filepath.Join(dir, "good.txt")
	os.WriteFile(complete, []byte("bootcmd=bootipq\nactive_fw=0\napp_part=0\nrootfsname=rootfs\n"), 0o644)
	if code, out, _ := runCap("envcheck", complete); code != 0 || !strings.Contains(out, "COMPLETE") {
		t.Errorf("envcheck complete: code=%d out=%q", code, out)
	}

	incomplete := filepath.Join(dir, "bad.txt")
	os.WriteFile(incomplete, []byte("ethaddr=00:03:7f:12:3e:87\n"), 0o644)
	code, out, errout := runCap("envcheck", incomplete)
	if code != 1 || !strings.Contains(out, "INCOMPLETE") || !strings.Contains(errout, "incomplete env") {
		t.Errorf("envcheck incomplete: code=%d out=%q err=%q", code, out, errout)
	}

	// The incomplete-env advice must not recommend `env default -a` blindly:
	// this env still has identity vars (ethaddr) that command would erase.
	if strings.Contains(out, "run `env default -a` over UART first") || !strings.Contains(out, "do NOT run `env default -a`") {
		t.Errorf("unsafe recovery advice: %q", out)
	}

	// Healthy EWS377-FIT env (u-boot 2.1.0, no rootfsname) is accepted (#2);
	// --strict (u-boot 2.0.0 profile) still demands rootfsname.
	fit := filepath.Join("..", "..", "internal", "hood", "testdata", "ews377fit-printenv.txt")
	if code, out, errout := runCap("envcheck", fit); code != 0 || !strings.Contains(out, "COMPLETE") || !strings.Contains(out, "hw_id=0101012B") {
		t.Errorf("envcheck FIT: code=%d out=%q err=%q", code, out, errout)
	}
	if code, out, _ := runCap("envcheck", "--strict", fit); code != 1 || !strings.Contains(out, "rootfsname") {
		t.Errorf("envcheck --strict FIT: code=%d out=%q", code, out)
	}

	// Nonexistent file is an I/O error.
	if code, _, errout := runCap("envcheck", filepath.Join(dir, "nope.txt")); code != 1 || errout == "" {
		t.Errorf("envcheck missing-file: code=%d err=%q", code, errout)
	}
}

func TestDiscover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<script src="/static/js/main.abc.chunk.js"></script>`))
	}))
	defer srv.Close()

	code, out, _ := runCap("discover", srv.URL)
	if code != 0 || !strings.Contains(out, "family=cloud") || !strings.Contains(out, "access=") {
		t.Errorf("discover: code=%d out=%q", code, out)
	}
	if code, _, errout := runCap("discover"); code != 1 || !strings.Contains(errout, "url") {
		t.Errorf("discover no-url: code=%d err=%q", code, errout)
	}
}
