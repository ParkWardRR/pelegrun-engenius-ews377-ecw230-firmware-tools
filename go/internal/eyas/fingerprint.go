// Package eyas discovers an AP and fingerprints its firmware family — the
// nestling hawk that spots the quarry. Pure HTTP; testable without a device.
package eyas

import (
	"context"
	"io"
	"net/http"
	"strings"
)

// Family is the firmware family running on a Senao IPQ807x device.
type Family int

const (
	Unknown Family = iota
	EwsLuCI        // ezMaster/EnSky EWS firmware — LuCI web, SSH exec on :8822
	Cloud          // ECW cloud firmware (ECW230v3, ECW336) — React GUI + JSON API
	Fit            // EWS377-FIT firmware — FitController/EPC managed
)

func (f Family) String() string {
	switch f {
	case EwsLuCI:
		return "ews-luci"
	case Cloud:
		return "cloud"
	case Fit:
		return "fit"
	default:
		return "unknown"
	}
}

// AccessHint tells the caller which jess adapter to use for this family.
func (f Family) AccessHint() string {
	switch f {
	case EwsLuCI:
		return "SSH exec on :8822 (root / web-admin pw) + LuCI flashops"
	case Cloud:
		// Measured on a stock ECW336 (1.8.118-12): POST /api/mgm/ssh
		// {"enable":true} really does start dropbear and :22 answers
		// SSH-2.0-dropbear, but every login is refused at the account level
		// ("Not support for root account."), so there is no exec channel to
		// drive. Bootloader-env work on this family needs UART.
		return "cloud local JSON API (admin/admin) — no usable shell (dropbear starts, accounts refused); budget UART for env writes"
	case Fit:
		return "cloud/FIT local JSON API (admin/admin) — no shell"
	default:
		return "unknown"
	}
}

// NeedsUART reports whether changing the bootloader env on this firmware family
// requires a serial console. Only the ECW cloud family does: it exposes no
// usable shell, so there is no fw_setenv to reach over the network. This is the
// one exception to the project's network-only workflow — and it is a property of
// the firmware line, not of the board.
func (f Family) NeedsUART() bool { return f == Cloud }

// classify inspects a fetched HTML/JS body for family markers. Exposed for tests.
func Classify(body string) Family {
	b := strings.ToLower(body)
	switch {
	case strings.Contains(b, "static/js/main"): // React SPA shell
		return Cloud
	case strings.Contains(b, "md5.js") || strings.Contains(b, "password_plain_text") || strings.Contains(b, "cgi-bin/luci"):
		return EwsLuCI
	case strings.Contains(b, "ews377-fit") || strings.Contains(b, "fitcontroller") || strings.Contains(b, "/fit/"):
		return Fit
	default:
		return Unknown
	}
}

// Fingerprint fetches the device web root (and the LuCI login as a fallback) and
// classifies the firmware family. Uses the provided client so callers can pass an
// InsecureSkipVerify transport for the self-signed cloud/HTTPS endpoints.
func Fingerprint(ctx context.Context, client *http.Client, baseURL string) (Family, error) {
	base := strings.TrimRight(baseURL, "/")
	for _, path := range []string{"/", "/cgi-bin/luci"} {
		fam, err := fetchClassify(ctx, client, base+path)
		if err != nil {
			continue
		}
		if fam != Unknown {
			return fam, nil
		}
	}
	return Unknown, nil
}

func fetchClassify(ctx context.Context, client *http.Client, url string) (Family, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Unknown, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Unknown, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return Unknown, err
	}
	return Classify(string(body)), nil
}
