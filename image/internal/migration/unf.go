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

// UniFi OS's Inform Host Override in Network's database dump, as BSON: the boolean turned on, and the
// address it points to. A NUL ends each key, so the first can't match the second.
var (
	informOverrideOn       = []byte("\x08override_inform_host\x00\x01")
	informOverrideOff      = []byte("\x08override_inform_host\x00\x00")
	informOverrideLocation = []byte("\x02override_inform_host_location\x00")
)

// Converted is a backup made from a console backup.
type Converted struct {
	Backup []byte
	// InformOverrideOff tells that UniFi OS's Inform Host Override was on and is now off: it pointed
	// to the console, and Network here would send devices to UniFi OS's container name instead.
	InformOverrideOff bool
	// InformOverrideWas is the address it pointed to, if any.
	InformOverrideWas string
}

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
func FromConsole(data []byte) (Converted, error) {
	archive, err := decryptConsole(data)
	if err != nil {
		return Converted{}, err
	}
	zipped, converted, err := networkZip(archive)
	if err != nil {
		return Converted{}, err
	}
	converted.Backup, err = encrypt(zipped)
	return converted, err
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
// The backup it returns is still to be encrypted.
func networkZip(archive []byte) ([]byte, Converted, error) {
	var converted Converted
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, converted, fmt.Errorf("it is damaged, or not a console backup: %w", err)
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
			return nil, converted, fmt.Errorf("it is damaged: %w", err)
		}
		name, inside := strings.CutPrefix(path.Clean(header.Name)+dirSuffix(header), consoleNetworkDir)
		if !inside || name == "" {
			continue
		}
		found = true
		var content io.Reader = entries
		if name == "db.gz" && header.Typeflag == tar.TypeReg {
			database, err := io.ReadAll(entries)
			if err != nil {
				return nil, converted, fmt.Errorf("it is damaged: %w", err)
			}
			if database, err = turnOffInformOverride(database, &converted); err != nil {
				return nil, converted, err
			}
			content = bytes.NewReader(database)
		}
		if err := copyEntry(zipped, name, header, content); err != nil {
			return nil, converted, err
		}
	}
	if !found {
		return nil, converted, errors.New("it holds no Network data")
	}
	if err := zipped.Close(); err != nil {
		return nil, converted, err
	}
	return out.Bytes(), converted, nil
}

// turnOffInformOverride turns UniFi OS's Inform Host Override off in Network's gzipped database dump,
// and notes what it pointed to. Each change keeps the dump's length, so nothing else moves.
func turnOffInformOverride(compressed []byte, converted *Converted) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("its database is damaged: %w", err)
	}
	database, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("its database is damaged: %w", err)
	}
	if !bytes.Contains(database, informOverrideOn) {
		return compressed, nil
	}
	converted.InformOverrideOff = true
	converted.InformOverrideWas = bsonString(database, informOverrideLocation)
	database = bytes.ReplaceAll(database, informOverrideOn, informOverrideOff)
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write(database); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// turnOffInformOverrideInBackup does the same in a Network backup. One it can't read is left as it
// is, for CheckBackup to explain.
func turnOffInformOverrideInBackup(data []byte, converted *Converted) ([]byte, error) {
	plain, err := decrypt(data)
	if err != nil {
		return data, nil
	}
	archive, err := repairZip(plain)
	if err != nil {
		return data, nil
	}
	var out bytes.Buffer
	zipped := zip.NewWriter(&out)
	for _, file := range archive.File {
		content, err := readFile(file)
		if err != nil {
			return data, nil
		}
		if file.Name == "db.gz" {
			if content, err = turnOffInformOverride(content, converted); err != nil {
				return data, nil
			}
		}
		entry, err := zipped.Create(file.Name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(content); err != nil {
			return nil, err
		}
	}
	if !converted.InformOverrideOff {
		return data, nil
	}
	if err := zipped.Close(); err != nil {
		return nil, err
	}
	return encrypt(out.Bytes())
}

func readFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(reader)
}

// bsonString reads the first BSON string after its type and key, or nothing.
func bsonString(data, typeAndKey []byte) string {
	start := bytes.Index(data, typeAndKey)
	if start < 0 {
		return ""
	}
	start += len(typeAndKey)
	if start+4 > len(data) {
		return ""
	}
	size := int(binary.LittleEndian.Uint32(data[start:]))
	if size < 1 || start+4+size > len(data) {
		return ""
	}
	return string(data[start+4 : start+4+size-1])
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
