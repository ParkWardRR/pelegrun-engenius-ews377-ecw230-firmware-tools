// Command pelegrun — cross-flash and recover EnGenius/Senao ap-hk07 (IPQ807x) APs.
// No arguments launches the TUI; subcommands are script/CI friendly.
// Unofficial; not affiliated with EnGenius or Senao. See README.md / SAFETY.md.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/band"
	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/eyas"
	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/hood"
	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/jess"
	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/tui"

	"golang.org/x/term"
)

// Version is stamped via -ldflags "-X main.Version=...".
var Version = "0.5.2"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point: it dispatches a subcommand, writing normal
// output to out and diagnostics to errw, and returns the process exit code
// (0 ok, 1 command error, 2 unknown command).
func run(args []string, out, errw io.Writer) int {
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	var err error
	switch cmd {
	case "", "tui":
		err = runTUI(out)
	case "version":
		fmt.Fprintf(out, "pelegrun %s\n", Version)
	case "plan":
		fmt.Fprint(out, planText)
	case "serial":
		err = cmdSerial(out, args)
	case "snextra":
		err = cmdSnextra(out, args)
	case "check":
		err = cmdCheck(out, args)
	case "envcheck":
		err = cmdEnvcheck(out, args)
	case "discover":
		err = cmdDiscover(out, args)
	case "redact":
		err = cmdRedact(out, args)
	case "-h", "--help", "help":
		fmt.Fprint(out, usageText)
	default:
		fmt.Fprintf(errw, "pelegrun: unknown command %q\n", cmd)
		return 2
	}
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	return 0
}

func runTUI(out io.Writer) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprint(out, usageText)
		return nil
	}
	return tui.Run(Version)
}

func argVal(a []string, key string) string {
	for i, v := range a {
		if v == key && i+1 < len(a) {
			return a[i+1]
		}
	}
	return ""
}

func or(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func cmdSerial(out io.Writer, a []string) error {
	model := argVal(a, "--model")
	if model == "" {
		return fmt.Errorf("serial: --model <CODE> required (e.g. X42)")
	}
	s, err := band.MakeSerial(or(argVal(a, "--prefix"), "SWLW"), model, or(argVal(a, "--suffix"), "0001"))
	if err != nil {
		return err
	}
	fmt.Fprintln(out, s)
	return nil
}

func cmdSnextra(out io.Writer, a []string) error {
	model := argVal(a, "--model")
	if model == "" {
		return fmt.Errorf("snextra: --model <CODE> required")
	}
	s, err := band.MakeSnextra(argVal(a, "--prefix"), model)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, s)
	return nil
}

func cmdCheck(out io.Writer, a []string) error {
	if len(a) == 0 {
		return fmt.Errorf("check: <serial> required")
	}
	mc, _ := band.ModelCode(a[0])
	name, known := band.ModelName(mc)
	if !known {
		name = "unknown code"
	}
	fmt.Fprintf(out, "serial=%s valid=%t model_code=%s model=%s\n",
		a[0], band.ValidateSerial(a[0]), mc, name)
	if !band.ValidateSerial(a[0]) {
		return fmt.Errorf("check character does not match")
	}
	return nil
}

func cmdEnvcheck(out io.Writer, a []string) error {
	var data []byte
	var err error
	if src := firstPositional(a); src != "" && src != "-" {
		data, err = os.ReadFile(src)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		return err
	}
	e := hood.ParsePrintenv(string(data))
	profile := hood.ProfileAuto
	if has(a, "--strict") {
		profile = hood.ProfileLegacy // also demand rootfsname (u-boot 2.0.0 units)
	}
	if miss := e.MissingFor(profile); len(miss) > 0 {
		fmt.Fprintf(out, "env: INCOMPLETE — missing %v\nrefuse writes; %s\n", miss, e.Recovery())
		return fmt.Errorf("incomplete env")
	}
	fmt.Fprintln(out, "env: COMPLETE — safe to append individual fields")
	for _, k := range []string{"hw_id", "hw_ver", "pro_id", "machid"} {
		if v, ok := e[k]; ok {
			fmt.Fprintf(out, "  %s=%s\n", k, v)
		}
	}
	reportSlot(out, e)
	reportController(out, e)
	return nil
}

// reportSlot names the running slot and the one a flash may target. Writing the
// inactive slot is invariant 1, so the operator should never have to work this
// out by hand.
func reportSlot(out io.Writer, e hood.Env) {
	active, err := e.ActiveSlot()
	if err != nil {
		fmt.Fprintf(out, "  A/B slot: %v — do not flash until this is resolved\n", err)
		return
	}
	spare, _ := e.InactiveSlot()
	cmd, err := e.PlanSwitchSlot()
	if err != nil {
		return
	}
	fmt.Fprintf(out, "  A/B slot: running %s (%s=%s), flash target %s\n",
		active, hood.ABSlotVar, active.Value(), spare)
	fmt.Fprintf(out, "            commit/rollback: %s\n", cmd)
	fmt.Fprintf(out, "            partition names are slot-relative: %q is the running slot, %q the target\n",
		"rootfs", "rootfs_1")
}

// reportController shows which controller stack an ECW-line unit's init will
// talk to. A stock unit has no cloud_id at all, which is the silent default
// that makes every controller-side override look broken.
func reportController(out io.Writer, e hood.Env) {
	v, set := e[hood.CloudIDVar]
	switch {
	case set && strings.TrimSpace(v) == hood.CloudIDPrivate:
		fmt.Fprintf(out, "  controller: %s=%s — private cloud (self-hosted); never contacts EnGenius Cloud\n",
			hood.CloudIDVar, hood.CloudIDPrivate)
	case set:
		fmt.Fprintf(out, "  controller: %s=%s — EnGenius Cloud path\n", hood.CloudIDVar, v)
	default:
		fmt.Fprintf(out, "  controller: %s unset → defaults to EnGenius Cloud; force_ac/DHCP-opt43/mDNS are NOT read in that mode\n",
			hood.CloudIDVar)
	}
	if !set || strings.TrimSpace(v) != hood.CloudIDPrivate {
		if cmd, err := e.PlanPrivateCloud(); err == nil {
			fmt.Fprintf(out, "            to adopt onto a self-hosted controller: %s\n", cmd)
		}
		if cmd, err := e.PlanSetUboot(hood.CloudIDVar, hood.CloudIDPrivate); err == nil {
			fmt.Fprintf(out, "            no shell on cloud firmware — at the u-boot prompt: %s\n", cmd)
		}
	}
}

func cmdDiscover(out io.Writer, a []string) error {
	if len(a) == 0 {
		return fmt.Errorf("discover: <url> required (e.g. http://192.168.1.1)")
	}
	fam, err := eyas.Fingerprint(context.Background(), jess.InsecureClient(10*time.Second), a[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "family=%s\naccess=%s\nneeds_uart=%t\n", fam, fam.AccessHint(), fam.NeedsUART())
	if fam.NeedsUART() {
		fmt.Fprintln(out, "note: env changes on this family (including the cloud_id controller gate) "+
			"cannot be made over the network — attach a serial console.")
	}
	return nil
}

const usageText = "pelegrun — cross-flash & recover EnGenius/Senao IPQ807x APs (unofficial)\n\n" +
	"USAGE:\n" +
	"  pelegrun                 launch the TUI (default)\n" +
	"  pelegrun version | plan\n" +
	"  pelegrun discover <url>            fingerprint firmware family (eyas)\n" +
	"  pelegrun serial  --model X42 [--prefix P --suffix S]   Code27 serial (band)\n" +
	"  pelegrun snextra --model X42 [--prefix P]              20-char field-19 value\n" +
	"  pelegrun check   <serial>                              validate a serial\n" +
	"  pelegrun envcheck [file|-] [--strict]                  hood env completeness gate, A/B slot + controller mode\n" +
	"                                                         (--strict also needs rootfsname)\n" +
	"  pelegrun redact  [file|-] [--mac] [--value S]...       scrub secrets from a bundle/log\n\n" +
	"Model codes: X44 EWS377AP v3 · X45 EWS377-FIT · X42 ECW230v3 · E4T ECW336 (not all are X-prefixed).\n" +
	"Image re-head and the SKU/board table ship as the quarry binary (Rust): `quarry products`.\n" +
	"Unofficial; hardware you own only.\n"

const planText = "Safety ladder (why UART is usually unnecessary):\n\n" +
	"  1. network flash        no UART — dual A/B slot means a bad image never bricks\n" +
	"  2. network env-repair   no UART — append-only fw_setenv on a verified env\n" +
	"  3. UART env-repair      gated: restore the saved printenv backup; `env default -a`\n" +
	"                          only as a last resort (it erases ethaddr/hw_id/sn/snextra)\n" +
	"  4. UART TFTP re-flash   truly dead board — pull it back over the wire\n\n" +
	"Invariants the tool cannot break: write the INACTIVE slot; env is APPEND-ONLY.\n\n" +
	"Exception — the ECW cloud firmware family (stock ECW336, stock ECW230v3) has no\n" +
	"usable shell, so rungs 1-2 are unavailable and every env change starts at UART.\n" +
	"Two things are worse there, not better:\n" +
	"  · the real MAC exists ONLY in the env (u-boot reports the ART copy invalid and\n" +
	"    falls back to 00:03:7f:ba:db:ad) — an env erase is permanent identity loss\n" +
	"  · the Senao boot menu puts `9` (TFTP write bootloader) and `e` (erase env) one\n" +
	"    keystroke from `4` (the CLI you want). Capture a printenv first.\n" +
	"What is better: the ECW336's u-boot has a working TFTP recovery path (menu 2),\n" +
	"which the ap-hk07 units did not.\n"
