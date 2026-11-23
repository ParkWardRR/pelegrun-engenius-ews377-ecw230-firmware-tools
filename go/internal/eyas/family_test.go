package eyas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFamilyStringAndAccessHint(t *testing.T) {
	cases := []struct {
		f          Family
		str        string
		hintSubstr string
		needsUART  bool
	}{
		// The cloud family is the one that has to go through UART: on a stock
		// ECW336 dropbear can be started but every account is refused.
		{Cloud, "cloud", "UART", true},
		{EwsLuCI, "ews-luci", "8822", false},
		{Fit, "fit", "no shell", false},
		{Unknown, "unknown", "unknown", false},
	}
	for _, c := range cases {
		if got := c.f.String(); got != c.str {
			t.Errorf("String(%d) = %q, want %q", c.f, got, c.str)
		}
		if got := c.f.AccessHint(); !strings.Contains(got, c.hintSubstr) {
			t.Errorf("AccessHint(%s) = %q, want substring %q", c.str, got, c.hintSubstr)
		}
		if got := c.f.NeedsUART(); got != c.needsUART {
			t.Errorf("NeedsUART(%s) = %t, want %t", c.str, got, c.needsUART)
		}
	}
}

func TestFingerprintUnreachableReturnsUnknown(t *testing.T) {
	// A closed server: both probe paths error, so Fingerprint returns Unknown
	// with no error (best-effort discovery).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // now nothing is listening

	fam, err := Fingerprint(context.Background(), &http.Client{Timeout: time.Second}, url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fam != Unknown {
		t.Errorf("got %v, want Unknown", fam)
	}
}

func TestFingerprintServerErrorStillClassifiesBody(t *testing.T) {
	// Even a 500 with a recognizable body should classify (we read the body).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<title>EWS377-FIT</title>`))
	}))
	defer srv.Close()

	fam, err := Fingerprint(context.Background(), srv.Client(), srv.URL)
	if err != nil || fam != Fit {
		t.Errorf("got %v err %v, want Fit", fam, err)
	}
}
