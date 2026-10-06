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
var IdentityVars = []string{"ethaddr", "hw_id", "sn", "snextra"}

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
