// Package migration is the tool for leaving another controller: it checks Network's backup files,
// converts console backups to them, and asks a running controller for one.
package migration

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Every Network install uses these, so that anyone with a backup can read it.
var (
	unfKey = []byte("bcyangkmluohmars")
	unfIV  = []byte("ubntenterpriseap")
)

// UniFi OS uses the SHA-256 of this for console backups, with a random IV first in the file.
const consoleSecret = "ubiquiti"

// Network accepts such files, and restores them as a nearly empty controller: tell it plainly.
var (
	errCutShort = errors.New("it is cut short, or not a Network backup. Download it again: Network would accept it, and restore a nearly empty controller")
	errDamaged  = errors.New("it is damaged. Download it again: Network would accept it, and restore a nearly empty controller")
)

// Where a console backup keeps what is, once zipped and encrypted, a full Network backup.
const consoleNetworkDir = "backup/network/"

// CheckBackup tells whether data is a complete, full backup, and returns its Network version.
func CheckBackup(data []byte) (string, error) {
	plain, err := decrypt(data)
	if err != nil {
		return "", err
	}
	archive, err := repairZip(plain)
	if err != nil {
		return "", err
	}
	version, err := readEntry(archive, "version")
	if err != nil {
		return "", errors.New("it has no version: not a Network backup")
	}
	if _, err := archive.Open("system.properties"); err != nil {
		return "", fmt.Errorf("it is a site export of Network %s, which can't be restored", version)
	}
	return version, nil
}

// FromConsole makes a backup from a console backup, taking only Network's data out of it.
func FromConsole(data []byte) ([]byte, error) {
	archive, err := decryptConsole(data)
	if err != nil {
		return nil, err
	}
	zipped, err := networkZip(archive)
	if err != nil {
		return nil, err
	}
	return encrypt(zipped)
}

func decrypt(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errCutShort
	}
	block, err := aes.NewCipher(unfKey)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, unfIV).CryptBlocks(plain, data)
	return plain, nil
}

func encrypt(zipped []byte) ([]byte, error) {
	block, err := aes.NewCipher(unfKey)
	if err != nil {
		return nil, err
	}
	// The cipher has no padding of its own, and works on whole blocks.
	padded := make([]byte, len(zipped)+(aes.BlockSize-len(zipped)%aes.BlockSize)%aes.BlockSize)
	copy(padded, zipped)
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, unfIV).CryptBlocks(encrypted, padded)
	return encrypted, nil
}

// repairZip opens a decrypted backup. Older backups lose up to 15 bytes of the zip's last record
// to their encryption, and newer ones have padding after it, so the record is written again from
// what's in the file, and the checksums of the whole then tell whether that was right. Without the
// start of that record, the file is cut short, which Network wouldn't notice: it would restore
// a nearly empty controller.
func repairZip(plain []byte) (*zip.Reader, error) {
	end := bytes.LastIndex(plain, []byte("PK\x05\x06"))
	if end < 0 {
		return nil, errCutShort
	}
	// The same bytes can appear by chance in compressed data: try each until the checksums match.
	starts := offsets(plain[:end], []byte("PK\x01\x02"))
	for index, start := range starts {
		repaired := withEndRecord(plain[:end], len(starts)-index, end-start, start)
		archive, err := zip.NewReader(bytes.NewReader(repaired), int64(len(repaired)))
		if err == nil && testEntries(archive) == nil {
			return archive, nil
		}
	}
	return nil, errDamaged
}

func offsets(data, pattern []byte) []int {
	var found []int
	for at := 0; ; {
		next := bytes.Index(data[at:], pattern)
		if next < 0 {
			return found
		}
		found = append(found, at+next)
		at += next + 1
	}
}

func withEndRecord(body []byte, entries, directorySize, directoryStart int) []byte {
	repaired := append(bytes.Clone(body), "PK\x05\x06\x00\x00\x00\x00"...)
	repaired = binary.LittleEndian.AppendUint16(repaired, uint16(entries))
	repaired = binary.LittleEndian.AppendUint16(repaired, uint16(entries))
	repaired = binary.LittleEndian.AppendUint32(repaired, uint32(directorySize))
	repaired = binary.LittleEndian.AppendUint32(repaired, uint32(directoryStart))
	return append(repaired, 0, 0)
}

// testEntries reads every entry, which verifies its checksum.
func testEntries(archive *zip.Reader) error {
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, reader)
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func readEntry(archive *zip.Reader, name string) (string, error) {
	reader, err := archive.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()
	content, err := io.ReadAll(reader)
	return strings.TrimSpace(string(content)), err
}

func decryptConsole(data []byte) ([]byte, error) {
	const notConsole = "it is damaged, or not a console backup"
	if len(data) < 2*aes.BlockSize || (len(data)-aes.BlockSize)%aes.BlockSize != 0 {
		return nil, errors.New(notConsole)
	}
	key := sha256.Sum256([]byte(consoleSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	encrypted := data[aes.BlockSize:]
	plain := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, data[:aes.BlockSize]).CryptBlocks(plain, encrypted)
	padding := int(plain[len(plain)-1])
	if padding == 0 || padding > aes.BlockSize {
		return nil, errors.New(notConsole)
	}
	return plain[:len(plain)-padding], nil
}

// networkZip copies Network's folder from a console backup to a zip, with its entries at the root.
func networkZip(archive []byte) ([]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("it is damaged, or not a console backup: %w", err)
	}
	var out bytes.Buffer
	zipped := zip.NewWriter(&out)
	entries := tar.NewReader(compressed)
	found := false
	for {
		header, err := entries.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("it is damaged: %w", err)
		}
		name, inside := strings.CutPrefix(path.Clean(header.Name)+dirSuffix(header), consoleNetworkDir)
		if !inside || name == "" {
			continue
		}
		found = true
		if err := copyEntry(zipped, name, header, entries); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, errors.New("it holds no Network data")
	}
	if err := zipped.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func dirSuffix(header *tar.Header) string {
	if header.Typeflag == tar.TypeDir {
		return "/"
	}
	return ""
}

func copyEntry(zipped *zip.Writer, name string, header *tar.Header, content io.Reader) error {
	// As UniFi OS does: nothing but files and folders.
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
		return fmt.Errorf("it holds an unexpected entry: %s", name)
	}
	entry, err := zipped.Create(name)
	if err != nil {
		return err
	}
	if header.Typeflag == tar.TypeReg {
		if _, err := io.Copy(entry, content); err != nil {
			return fmt.Errorf("it is damaged: %w", err)
		}
	}
	return nil
}
