package migration

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../tests/fixture/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCheckFullBackup(t *testing.T) {
	version, err := CheckBackup(fixture(t, "sample-10.6.109.unf"))
	if err != nil {
		t.Fatal(err)
	}
	if version != "10.6.109" {
		t.Errorf("got version %q", version)
	}
}

// Network accepts such files, and restores them as a nearly empty controller.
func TestCheckRefusesDamagedFiles(t *testing.T) {
	sample := fixture(t, "sample-10.6.109.unf")
	flipped := bytes.Clone(sample)
	flipped[4000] ^= 0xff
	tests := map[string][]byte{
		"empty":            {},
		"not a block size": sample[:3000],
		"cut on a block":   sample[:4096],
		"cut near the end": sample[:len(sample)-16],
		"a byte flipped":   flipped,
		"not a backup":     bytes.Repeat([]byte{1}, 1600),
		"a console backup": fixture(t, "sample-console.unifi"),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if version, err := CheckBackup(data); err == nil {
				t.Errorf("accepted, as version %q", version)
			}
		})
	}
}

func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	zipped := zip.NewWriter(&out)
	for name, content := range entries {
		entry, err := zipped.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	encrypted, err := encrypt(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return encrypted
}

func TestCheckRefusesABackupWithoutVersion(t *testing.T) {
	_, err := CheckBackup(zipOf(t, map[string]string{"db.gz": "x"}))
	if err == nil || !strings.Contains(err.Error(), "no version") {
		t.Errorf("got %v", err)
	}
}

func TestFromConsole(t *testing.T) {
	converted, err := FromConsole(fixture(t, "sample-console.unifi"))
	if err != nil {
		t.Fatal(err)
	}
	version, err := CheckBackup(converted)
	if err != nil || version != "10.6.109" {
		t.Fatalf("got %q, %v", version, err)
	}
	plain, err := decrypt(converted)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"unifi-core.key", "not a real key", "metadata.json"} {
		if bytes.Contains(plain, []byte(secret)) {
			t.Errorf("the console's own data came along: %q", secret)
		}
	}
}

func TestFromConsoleRefusesDamagedFiles(t *testing.T) {
	sample := fixture(t, "sample-console.unifi")
	for name, data := range map[string][]byte{
		"empty":          {},
		"not a block":    sample[:1000],
		"cut on a block": sample[:16+1600],
		"a plain backup": fixture(t, "sample-10.6.109.unf"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := FromConsole(data); err == nil {
				t.Error("accepted")
			}
		})
	}
}

func consoleBackup(t *testing.T, write func(*tar.Writer)) []byte {
	t.Helper()
	var archive bytes.Buffer
	compressed := gzip.NewWriter(&archive)
	entries := tar.NewWriter(compressed)
	write(entries)
	if err := entries.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	plain := archive.Bytes()
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	key := sha256.Sum256([]byte(consoleSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	iv := bytes.Repeat([]byte{7}, aes.BlockSize)
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, plain)
	return append(iv, encrypted...)
}

func TestFromConsoleRefusesLinks(t *testing.T) {
	data := consoleBackup(t, func(entries *tar.Writer) {
		if err := entries.WriteHeader(&tar.Header{
			Name: "backup/network/version", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd",
		}); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := FromConsole(data); err == nil || !strings.Contains(err.Error(), "unexpected entry") {
		t.Errorf("got %v", err)
	}
}

func TestFromConsoleWithoutNetworkData(t *testing.T) {
	data := consoleBackup(t, func(entries *tar.Writer) {
		if err := entries.WriteHeader(&tar.Header{
			Name: "backup/metadata.json", Typeflag: tar.TypeDir,
		}); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := FromConsole(data); err == nil || !strings.Contains(err.Error(), "no Network data") {
		t.Errorf("got %v", err)
	}
}
