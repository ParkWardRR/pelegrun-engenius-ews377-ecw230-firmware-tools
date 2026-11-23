// Package hood is the safety gate that keeps the raptor calm.
//
// It parses a u-boot environment, refuses to act on an incomplete one, and only
// ever plans APPEND-ONLY single-field writes. It has no function that can erase
// the env or save a partial one — the two things that brick a board (Constitution
// clause I). All logic here is pure; hood emits command strings for an accessor to
// run, and never does I/O itself.
package hood

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Env is a parsed u-boot environment (key -> value).
type Env map[string]string

// RequiredBootVars must all be present for the env to be considered bootable.
// This is the set every known ap-hk07 bootloader generation needs: `bootipq`
// reads active_fw/app_part to pick the A/B slot. Their absence is exactly what
// turns a valid-but-incomplete env into a brick.
//
// `rootfsname` is deliberately NOT here: the 2.0.0 bootloader (EWS377AP v3 /
// ECW230v3) carries it, but the 2016.01 V2.1.0 bootloader (EWS377-FIT) boots
// fine without it. See LegacyBootVars and Profile.
var RequiredBootVars = []string{"bootcmd", "active_fw", "app_part"}

// LegacyBootVars are the extra variables the older u-boot 2.0.0 generation
// carries. They are only demanded under ProfileLegacy (strict mode).
var LegacyBootVars = []string{"rootfsname"}

// IdentityVars are the per-unit identity fields. They are NOT recoverable from
// the compiled-in defaults, which is why `env default -a` is dangerous on a unit
// that still has them (the stock cloud_guard cross-checks them against the
// `cert` partition).
//
// On some units the env is the ONLY copy. An ECW336 prints
// `eth0..eth5 MAC Address from ART is not valid` at boot and falls back to the
// Qualcomm dummy 00:03:7f:ba:db:ad, so its real EnGenius-OUI MAC exists nowhere
// but `ethaddr` here — erasing the env is permanent identity loss, not a
// recoverable reset. Capture a printenv before touching anything.
var IdentityVars = []string{"ethaddr", "hw_id", "sn", "snextra"}

// ABSlotVar is the u-boot variable that selects which firmware slot boots.
// Settled on hardware: u-boot reads `active_fw` and prints
// `Booting from NAND FW<n>`. `app_part` sits alongside it and is NOT the
// selector (it stayed 0 across a verified slot switch), and the rootfs's
// `setconfig 38` / /proc/boot_info code is generic QSDK that is inert here —
// so a slot switch must write `active_fw` and nothing else.
const ABSlotVar = "active_fw"

// CloudIDVar is the u-boot variable that selects which controller stack the
// ECW firmware's init talks to. CloudIDPrivate routes it at a self-hosted
// controller.
//
// The gate: /etc/init.d/ezmcloud reads this via cloudmode.sh and sources
// minicloud-init.sh (the FIT path) only when it is "3" — otherwise it sources
// ezmcloud-init.sh (the public-cloud path), which never loads the code that
// reads force_ac, DHCP option 43 or mDNS. A stock unit has no cloud_id at all
// and so defaults to 0, which is why controller-side overrides appear to be
// ignored: nothing is reading them. In mode 3 the AP never contacts EnGenius
// Cloud, so a previous owner's cloud ownership stops mattering.
const (
	CloudIDVar     = "cloud_id"
	CloudIDPrivate = "3" // private cloud / self-hosted controller
	CloudIDPublic  = "0" // EnGenius Cloud (the stock default, when unset)
)

// Profile selects which required-variable set Missing() checks.
type Profile int

const (
	// ProfileAuto demands the common core only; rootfsname is not required.
	ProfileAuto Profile = iota
	// ProfileLegacy additionally demands LegacyBootVars (u-boot 2.0.0 units).
	ProfileLegacy
)

var (
	// ErrIncomplete means the env is missing a required boot variable.
	ErrIncomplete = errors.New("env is incomplete; refusing to write (would risk a brick)")
	// ErrEmptyValue means a write had an empty value — in u-boot that DELETES the
	// variable, which violates the append-only invariant.
	ErrEmptyValue = errors.New("empty value would delete the variable; refused (append-only)")
	// ErrEmptyKey means a blank key was given.
	ErrEmptyKey = errors.New("empty key")
)

// ParsePrintenv parses the output of `fw_printenv` / u-boot `printenv`
// (one `key=value` per line; blank lines and comments ignored). Values may
// contain '='.
func ParsePrintenv(s string) Env {
	e := make(Env)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		e[strings.TrimSpace(line[:i])] = line[i+1:]
	}
	return e
}

// Missing returns the required boot variables absent from e, sorted
// (ProfileAuto: the common core only).
func (e Env) Missing() []string { return e.MissingFor(ProfileAuto) }

// MissingFor returns the boot variables absent from e under profile p, sorted.
func (e Env) MissingFor(p Profile) []string {
	req := RequiredBootVars
	if p == ProfileLegacy {
		req = append(append([]string(nil), req...), LegacyBootVars...)
	}
	var m []string
	for _, k := range req {
		if _, ok := e[k]; !ok {
			m = append(m, k)
		}
	}
	sort.Strings(m)
	return m
}

// IsComplete reports whether every RequiredBootVars is present.
func (e Env) IsComplete() bool { return len(e.Missing()) == 0 }

// HasIdentity reports whether any per-unit identity variable is still present.
func (e Env) HasIdentity() bool {
	for _, k := range IdentityVars {
		if _, ok := e[k]; ok {
			return true
		}
	}
	return false
}

// LooksWiped distinguishes "env was wiped / reset to defaults" from "a healthy
// env that merely lacks a key this bootloader generation doesn't use". A wiped
// env has lost its identity variables AND its boot command is absent or still
// the compiled-in network default (`bootp`).
func (e Env) LooksWiped() bool {
	bc, ok := e["bootcmd"]
	return !e.HasIdentity() && (!ok || strings.HasPrefix(strings.TrimSpace(bc), "bootp"))
}

// Recovery returns the operator advice for an incomplete env. It never offers
// `env default -a` as a blanket fix: on a unit that still has identity
// variables that command would erase them.
func (e Env) Recovery() string {
	if e.HasIdentity() {
		return "this env still holds identity variables (ethaddr/hw_id/sn/snextra): " +
			"do NOT run `env default -a` (it resets them to compiled-in defaults). " +
			"Restore the saved printenv / APPSBLENV backup instead"
	}
	if e.LooksWiped() {
		return "env looks wiped (no identity vars, default bootcmd): restore the saved printenv / " +
			"APPSBLENV backup; `env default -a` over UART is the last resort and loses ethaddr/hw_id/sn/snextra"
	}
	return "restore the saved printenv / APPSBLENV backup over UART"
}

// PlanSet returns the single append-only command to set key=value, but ONLY if
// the current env is already complete and the value is non-empty. This is the one
// sanctioned way to mutate the env; there is deliberately no PlanErase / PlanReset.
func (e Env) PlanSet(key, value string) (string, error) {
	if key == "" {
		return "", ErrEmptyKey
	}
	if value == "" {
		return "", ErrEmptyValue
	}
	if !e.IsComplete() {
		return "", fmt.Errorf("%w: missing %v — %s", ErrIncomplete, e.Missing(), e.Recovery())
	}
	return fmt.Sprintf("fw_setenv %s %s", key, value), nil
}

// PlanSetUboot is PlanSet rendered for the u-boot CLI instead of a running
// Linux userspace. It carries the same completeness gate and the same
// append-only guarantee; the only difference is the console it is typed at.
//
// Needed because the ECW cloud firmware family exposes no usable shell (see
// eyas.Family.NeedsUART), so on those units there is no fw_setenv to reach over
// the network and the env can only be touched from the bootloader prompt.
func (e Env) PlanSetUboot(key, value string) (string, error) {
	if _, err := e.PlanSet(key, value); err != nil {
		return "", err
	}
	return fmt.Sprintf("setenv %s %s ; saveenv", key, value), nil
}

// Slot is an A/B firmware slot.
type Slot int

const (
	// SlotUnknown means active_fw held something other than 0 or 1.
	SlotUnknown Slot = -1
	// SlotA is active_fw=0: u-boot's FW1.
	SlotA Slot = 0
	// SlotB is active_fw=1: u-boot's FW2.
	SlotB Slot = 1
)

// String names the slot the way u-boot does.
func (s Slot) String() string {
	switch s {
	case SlotA:
		return "FW1"
	case SlotB:
		return "FW2"
	default:
		return "unknown"
	}
}

// Value is the active_fw value that selects this slot.
func (s Slot) Value() string { return strconv.Itoa(int(s)) }

// Other returns the opposite slot — the one a flash must be written to.
func (s Slot) Other() Slot {
	switch s {
	case SlotA:
		return SlotB
	case SlotB:
		return SlotA
	default:
		return SlotUnknown
	}
}

// ErrUnknownSlot means active_fw is absent or holds a value that is not 0 or 1,
// so which slot is running cannot be established — and a flash must then not
// proceed, because "the inactive slot" is unknown.
var ErrUnknownSlot = errors.New("cannot determine the active A/B slot from " + ABSlotVar)

// ActiveSlot reports which slot is currently booting.
func (e Env) ActiveSlot() (Slot, error) {
	switch strings.TrimSpace(e[ABSlotVar]) {
	case "0":
		return SlotA, nil
	case "1":
		return SlotB, nil
	default:
		return SlotUnknown, fmt.Errorf("%w (%s=%q)", ErrUnknownSlot, ABSlotVar, e[ABSlotVar])
	}
}

// InactiveSlot reports the slot a flash may be written to (invariant 1: never
// write the running slot, so a bad image is undone by booting the other one).
func (e Env) InactiveSlot() (Slot, error) {
	active, err := e.ActiveSlot()
	if err != nil {
		return SlotUnknown, err
	}
	return active.Other(), nil
}

// PlanSwitchSlot returns the append-only command that points the bootloader at
// the other slot — the post-flash commit, and equally the rollback after a flash
// that came up wrong. It writes only ABSlotVar: app_part is deliberately left
// alone.
//
// MTD partition names are SLOT-RELATIVE on these units: booted from FW2, u-boot
// calls the FW2 rootfs `rootfs` and the FW1 one `rootfs_1` — the labels swap, so
// `rootfs` always means "the running slot". Never map a partition name to a
// fixed offset, and re-read /proc/mtd after a slot switch.
func (e Env) PlanSwitchSlot() (string, error) {
	target, err := e.InactiveSlot()
	if err != nil {
		return "", err
	}
	return e.PlanSet(ABSlotVar, target.Value())
}

// PlanPrivateCloud returns the append-only command that routes an ECW-line
// unit's init at a self-hosted controller (see CloudIDVar). It adds one variable
// to an env already verified complete, and is reverted by setting CloudIDVar
// back to CloudIDPublic — no erase, nothing rebuilt.
//
// This is only the routing gate. The controller side still has to accept the
// check-in: register the device with the `preshared_initial` secret so the AP's
// pre-shared-key signature validates, and make sure the AP's clock is right
// (a stale header gets a distinct timestamp rejection, not a 404).
func (e Env) PlanPrivateCloud() (string, error) {
	return e.PlanSet(CloudIDVar, CloudIDPrivate)
}

// VerifyAfterSet checks that a re-read env reflects the intended write AND is
// still complete. "Prove, don't assume" (Constitution clause II).
func VerifyAfterSet(after Env, key, want string) error {
	if !after.IsComplete() {
		return fmt.Errorf("%w after write: missing %v", ErrIncomplete, after.Missing())
	}
	if got := after[key]; got != want {
		return fmt.Errorf("verify failed: %s = %q, wanted %q", key, got, want)
	}
	return nil
}
