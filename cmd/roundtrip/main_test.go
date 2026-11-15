package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildTool compiles the roundtrip command once and returns its path.
func buildTool(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "roundtrip")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

func runTool(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("can't run the tool: %v", err)
	return "", -1
}

func TestToolRoundTripsFixtures(t *testing.T) {
	bin := buildTool(t)
	fixtures, err := filepath.Glob("../../fixtures/*.mid")
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	dir := t.TempDir()
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			out := filepath.Join(dir, filepath.Base(fixture))
			msg, code := runTool(t, bin, "-in", fixture, "-out", out)
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d: %s", code, msg)
			}
			if _, err := os.Stat(out); err != nil {
				t.Fatalf("no output written: %v", err)
			}
			// The output is itself a valid round trip.
			if msg, code := runTool(t, bin, "-in", out, "-out", out+".again"); code != 0 {
				t.Fatalf("output isn't round-trippable: %d: %s", code, msg)
			}
		})
	}
}

func TestToolRejectsBadFiles(t *testing.T) {
	bin := buildTool(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		data []byte
	}{
		{
			name: "not a midi file",
			data: []byte("hello, this is not midi at all"),
		},
		{
			name: "header truncated",
			data: []byte{'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0},
		},
		{
			name: "track truncated",
			data: []byte{
				'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1, 0, 0x60,
				'M', 'T', 'r', 'k', 0, 0, 0, 20, 0x00, 0xFF, 0x2F, 0x00,
			},
		},
		{
			name: "vlq too long",
			data: []byte{
				'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1, 0, 0x60,
				'M', 'T', 'r', 'k', 0, 0, 0, 8, 0x81, 0x80, 0x80, 0x80, 0x00, 0x90, 0x3C, 0x40,
			},
		},
		{
			name: "meta payload missing",
			data: []byte{
				'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1, 0, 0x60,
				'M', 'T', 'r', 'k', 0, 0, 0, 7, 0x00, 0xFF, 0x58, 0x04, 0x04, 0x02, 0x18,
			},
		},
		{
			name: "data bytes after the meta",
			data: []byte{
				'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1, 0, 0x60,
				'M', 'T', 'r', 'k', 0, 0, 0, 6, 0x00, 0xFF, 0x2F, 0x00, 0x00, 0x3C,
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			in := filepath.Join(dir, "bad.mid")
			if err := os.WriteFile(in, c.data, 0644); err != nil {
				t.Fatal(err)
			}
			msg, code := runTool(t, bin, "-in", in, "-out", filepath.Join(dir, "out.mid"))
			if code == 0 {
				t.Fatalf("expected a non-zero exit code, got 0: %s", msg)
			}
			if msg == "" {
				t.Fatal("expected the failure reason on stderr")
			}
		})
	}
}

func TestToolMissingInput(t *testing.T) {
	bin := buildTool(t)
	if msg, code := runTool(t, bin, "-in", filepath.Join(t.TempDir(), "nope.mid")); code == 0 {
		t.Fatalf("expected a failure for a missing file: %s", msg)
	}
}
