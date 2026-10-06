package redact

import (
	"strings"
	"testing"
)

func TestSecretKVRedaction(t *testing.T) {
	in := strings.Join([]string{
		`password=hunter2`,
		`"api_key": "sk-abc123def"`,
		`wpa_psk=SuperSecretWifi`,
		`sysauth=deadbeefsession`,
		`bootcmd=bootipq`, // NOT a secret — must survive
		`rootfsname=rootfs`,
	}, "\n")
	out := Default().Text(in)
	for _, leak := range []string{"hunter2", "sk-abc123def", "SuperSecretWifi", "deadbeefsession"} {
		if strings.Contains(out, leak) {
			t.Errorf("secret %q leaked through redaction:\n%s", leak, out)
		}
	}
	// Non-secret keys and values are preserved.
	if !strings.Contains(out, "bootcmd=bootipq") || !strings.Contains(out, "rootfsname=rootfs") {
		t.Errorf("non-secret env vars must be preserved:\n%s", out)
	}
}

func TestBearerAndJWT(t *testing.T) {
	in := `Authorization: Bearer abc.def.ghi-token_value
token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature-part-here`
	out := Default().Text(in)
	if strings.Contains(out, "abc.def.ghi-token_value") {
		t.Errorf("bearer token leaked:\n%s", out)
	}
	if strings.Contains(out, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("JWT leaked:\n%s", out)
	}
}

func TestPrivateKeyBlock(t *testing.T) {
	in := "before\n-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA...secret...ZZZZ\n-----END OPENSSH PRIVATE KEY-----\nafter"
	out := Default().Text(in)
	if strings.Contains(out, "secret") {
		t.Fatalf("private key body leaked:\n%s", out)
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Fatalf("surrounding text must be preserved:\n%s", out)
	}
}

func TestMACOptIn(t *testing.T) {
	in := `ethaddr=88:DC:97:04:44:07`
	if strings.Contains(Default().Text(in), "[REDACTED:mac]") {
		t.Fatal("Default must NOT redact MACs")
	}
	out := All().Text(in)
	if strings.Contains(out, "88:DC:97:04:44:07") || !strings.Contains(out, "[REDACTED:mac]") {
		t.Fatalf("All should redact MACs:\n%s", out)
	}
}

func TestExplicitValues(t *testing.T) {
	in := `serial#=SWLWX420001Q reported by device SWLWX420001Q`
	out := Default().WithValues("SWLWX420001Q").Text(in)
	if strings.Contains(out, "SWLWX420001Q") {
		t.Fatalf("explicit serial value should be scrubbed everywhere:\n%s", out)
	}
	if strings.Count(out, "[REDACTED:value]") != 2 {
		t.Fatalf("both occurrences should be redacted:\n%s", out)
	}
}

func TestDeterministicAndValuesCaseInsensitive(t *testing.T) {
	r := Default().WithValues("MyHost")
	a := r.Text("connect to myhost and MYHOST")
	b := r.Text("connect to myhost and MYHOST")
	if a != b {
		t.Fatal("redaction must be deterministic")
	}
	if strings.Contains(strings.ToLower(a), "myhost") {
		t.Fatalf("case-insensitive value redaction failed:\n%s", a)
	}
}

func TestWithValuesDoesNotMutateOriginal(t *testing.T) {
	base := Default()
	_ = base.WithValues("x")
	if len(base.Values) != 0 {
		t.Fatal("WithValues must not mutate the receiver")
	}
}

// A fake key (never a real one): cert-partition-style dump from the EWS377-FIT.
const fakeCert = "SN/MAC/HWID\n39\nFAKESERIAL12/88:DC:97:00:00:00/0101012B\n" +
	"-----BEGIN RSA PRIVATE KEY-----\n" +
	"MIIEowIBAAKCAQEAfakefakefakefakefakefakefakefakefakefakefakefakefake1\n" +
	"fakefakefakefakefakefakefakefakefakefakefakefakefakefakefakefakefake2\n" +
	"fakefake+/==\n" +
	"-----END RSA PRIVATE KEY-----\n"

func TestCertPartitionDump(t *testing.T) {
	out := Default().Text(fakeCert)
	for _, leak := range []string{"FAKESERIAL12", "88:DC:97:00:00:00", "0101012B", "fakefakefake", "MIIEow"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked from cert dump:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "[REDACTED:private-key]") || !strings.Contains(out, "[REDACTED:identity]") {
		t.Errorf("expected both placeholders:\n%s", out)
	}
}

func TestTruncatedPEMAndBareBase64(t *testing.T) {
	// BEGIN with no END (dump cut off).
	trunc := "x\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAfakefakefakefakefakefakefakefakefakefakefakefake\nmore"
	out := Default().Text(trunc)
	if strings.Contains(out, "MIIEow") || strings.Contains(out, "more") || !strings.HasPrefix(out, "x\n") {
		t.Errorf("truncated PEM leaked or ate preceding text:\n%s", out)
	}
	// A middle chunk of a key printed without markers.
	body := "ok\nMIIEowIBAAKCAQEAfakefakefakefakefakefakefakefakefakefakefakefakefake1\n" +
		"fakefakefakefakefakefakefakefakefakefakefakefakefakefakefakefakefake2\nSHORT+tail=\nafter"
	out = Default().Text(body)
	if strings.Contains(out, "fakefake") || strings.Contains(out, "SHORT") {
		t.Errorf("bare base64 body leaked:\n%s", out)
	}
	if !strings.Contains(out, "ok\n") || !strings.HasSuffix(out, "after") {
		t.Errorf("surrounding text must survive:\n%s", out)
	}
}

func TestBase64HeuristicSparesHashesAndProse(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	in := sha + "\n" + sha + "\nThis is an ordinary sentence of prose that is long enough to be a line.\n"
	if out := Default().Text(in); out != in {
		t.Errorf("hex checksums / prose must be untouched:\n%s", out)
	}
}

func TestSnextraAndCloudGuardLog(t *testing.T) {
	in := "snextra=ABCDEFGHIJKL********\nsn=000000001\nbootcmd=bootipq\n" +
		"uboot(SN/MAC/HWID)[ABCDEFGHIJKL/88:DC:97:00:00:00/0101012B], cert(SN/MAC/HWID)[ABCDEFGHIJKL/88:DC:97:00:00:00/0101012B], no problem.\n"
	out := Default().Text(in)
	for _, leak := range []string{"ABCDEFGHIJKL", "88:DC:97", "000000001", "0101012B"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "bootcmd=bootipq") || !strings.Contains(out, "no problem") {
		t.Errorf("non-secret text must be preserved:\n%s", out)
	}
}
