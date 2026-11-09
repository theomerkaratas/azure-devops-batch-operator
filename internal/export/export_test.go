package export

import (
	"bytes"
	"testing"
)

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		`Deploy\Services`: "Deploy_Services",
		"  a:b*c?  ":      "a_b_c_",
		`x"<>|/y`:         "x_____y",
		"Plain Name":      "Plain Name",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestUniqueName(t *testing.T) {
	used := map[string]bool{}
	got := []string{
		UniqueName("Deploy", used),
		UniqueName("deploy", used),
		UniqueName("DEPLOY", used),
		UniqueName("Other", used),
	}
	want := []string{"Deploy", "deploy_2", "DEPLOY_3", "Other"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UniqueName #%d=%q want %q", i, got[i], want[i])
		}
	}
}

func TestEncodeScriptPowerShellHasBOM(t *testing.T) {
	got, err := EncodeScript("Write-Host 'ok'", ".ps1")
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xEF, 0xBB, 0xBF}, "Write-Host 'ok'"...)
	if !bytes.Equal(got, want) {
		t.Errorf("unexpected ps1 bytes: %x", got)
	}
}

func TestEncodeScriptCmdUsesCRLF(t *testing.T) {
	got, err := EncodeScript("x\r\ny\nz", ".cmd")
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte("x\r\ny\r\nz"); !bytes.Equal(got, want) {
		t.Errorf("cmd line endings: %q", got)
	}
}

func TestEncodeScriptCmdUsesCP857(t *testing.T) {
	// Reference bytes produced by Python's cp857 codec for the same Turkish text.
	got, err := EncodeScript("ıİşŞğĞçÇöÖüÜ", ".cmd")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x8D, 0x98, 0x9F, 0x9E, 0xA7, 0xA6, 0x87, 0x80, 0x94, 0x99, 0x81, 0x9A}
	if !bytes.Equal(got, want) {
		t.Errorf("cp857 bytes %x, want %x", got, want)
	}
}

func TestEncodeScriptCmdRejectsUnmappable(t *testing.T) {
	if _, err := EncodeScript("emoji 😀", ".cmd"); err == nil {
		t.Error("expected error for character outside cp857")
	}
}
