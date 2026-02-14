// Package listupdater contains the implementation of the list-updater subcommand.
package listupdater

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	routercommon "github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

const (
	plainFilePerm os.FileMode = 0o644
	plainDirPerm  os.FileMode = 0o750
)

var (
	errCategoryNotFound    = errors.New("category not found")
	errCategoryUnsupported = errors.New("categories unsupported")
)

func ensurePlain(originalPath string, plainPath string) error {
	//nolint:gosec // file path is derived from trusted config (no user input).
	origBytes, err := os.ReadFile(originalPath)
	if err != nil {
		return fmt.Errorf("read original: %w", err)
	}

	lines, ok := tryDecodeGeoSiteDat(origBytes)
	if ok {
		return writeLinesAtomically(plainPath, lines)
	}

	return copyFileAtomically(originalPath, plainPath)
}

func ensurePlainCategory(originalPath string, plainPath string, category string) error {
	//nolint:gosec // file path is derived from trusted config (no user input).
	origBytes, err := os.ReadFile(originalPath)
	if err != nil {
		return fmt.Errorf("read original: %w", err)
	}

	lines, ok, found := tryDecodeGeoSiteDatCategory(origBytes, category)
	if !ok {
		return errCategoryUnsupported
	}

	if !found {
		return fmt.Errorf("%w: %s", errCategoryNotFound, category)
	}

	return writeLinesAtomically(plainPath, lines)
}

func listGeoSiteCategoriesFromFile(originalPath string) ([]string, bool, error) {
	//nolint:gosec // file path is derived from trusted config (no user input).
	origBytes, err := os.ReadFile(originalPath)
	if err != nil {
		return nil, false, fmt.Errorf("read original: %w", err)
	}

	categories, ok := tryListGeoSiteCategories(origBytes)

	return categories, ok, nil
}

func tryDecodeGeoSiteDat(b []byte) ([]string, bool) {
	var geoList routercommon.GeoSiteList
	if proto.Unmarshal(b, &geoList) != nil {
		return nil, false
	}

	entries := geoList.GetEntry()
	if len(entries) == 0 {
		return nil, false
	}

	seen := make(map[string]struct{})

	for _, site := range entries {
		domains := site.GetDomain()
		for _, dom := range domains {
			value := normalizePlainValue(dom.GetValue())
			if value == "" {
				continue
			}

			value = strings.ToLower(value)
			seen[value] = struct{}{}
		}
	}

	lines := make([]string, 0, len(seen))
	for v := range seen {
		lines = append(lines, v)
	}

	sort.Strings(lines)

	return lines, true
}

func tryDecodeGeoSiteDatCategory(b []byte, category string) ([]string, bool, bool) {
	var geoList routercommon.GeoSiteList
	if proto.Unmarshal(b, &geoList) != nil {
		return nil, false, false
	}

	entries := geoList.GetEntry()
	if len(entries) == 0 {
		return nil, false, false
	}

	catLower := strings.ToLower(strings.TrimSpace(category))
	if catLower == "" {
		return nil, true, false
	}

	found := false
	seen := make(map[string]struct{})

	for _, site := range entries {
		codeLower := strings.ToLower(site.GetCountryCode())
		if codeLower != catLower {
			continue
		}

		found = true

		domains := site.GetDomain()
		for _, dom := range domains {
			value := normalizePlainValue(dom.GetValue())
			if value == "" {
				continue
			}

			value = strings.ToLower(value)
			seen[value] = struct{}{}
		}
	}

	lines := make([]string, 0, len(seen))
	for v := range seen {
		lines = append(lines, v)
	}

	sort.Strings(lines)

	return lines, true, found
}

func tryListGeoSiteCategories(b []byte) ([]string, bool) {
	var geoList routercommon.GeoSiteList
	if proto.Unmarshal(b, &geoList) != nil {
		return nil, false
	}

	entries := geoList.GetEntry()
	if len(entries) == 0 {
		return nil, false
	}

	seen := make(map[string]struct{})

	for _, site := range entries {
		code := strings.TrimSpace(site.GetCountryCode())
		if code == "" {
			continue
		}

		code = strings.ToLower(code)
		seen[code] = struct{}{}
	}

	categories := make([]string, 0, len(seen))
	for c := range seen {
		categories = append(categories, c)
	}

	sort.Strings(categories)

	return categories, true
}

func normalizePlainValue(value string) string {
	value = strings.TrimSpace(value)

	// Some lists contain quoted values; plain output must not.
	value = strings.Trim(value, "\"'")

	// If value looks like "domain:example.com" (plaintext export format),
	// strip the prefix and keep only the domain/IP.
	if i := strings.IndexByte(value, ':'); i > 0 {
		prefix := value[:i]
		switch prefix {
		case "domain", "full", "regexp", "keyword", "ip", "cidr":
			value = strings.TrimSpace(value[i+1:])
			value = strings.Trim(value, "\"'")
		}
	}

	return value
}

func writeLinesAtomically(path string, lines []string) error {
	buf := &bytes.Buffer{}
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	dir := filepath.Dir(path)

	mkdirErr := os.MkdirAll(dir, plainDirPerm)
	if mkdirErr != nil {
		return fmt.Errorf("mkdir plain dir: %w", mkdirErr)
	}

	tmpPath := path + ".tmp"

	writeErr := os.WriteFile(tmpPath, buf.Bytes(), plainFilePerm)
	if writeErr != nil {
		return fmt.Errorf("write plain: %w", writeErr)
	}

	renameErr := os.Rename(tmpPath, path)
	if renameErr != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename plain: %w", renameErr)
	}

	return nil
}

func copyFileAtomically(srcPath string, dstPath string) error {
	//nolint:gosec // file path is derived from trusted config (no user input).
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open original: %w", err)
	}

	defer func() { _ = src.Close() }()

	dir := filepath.Dir(dstPath)

	mkdirErr := os.MkdirAll(dir, plainDirPerm)
	if mkdirErr != nil {
		return fmt.Errorf("mkdir plain dir: %w", mkdirErr)
	}

	tmpPath := dstPath + ".tmp"
	//nolint:gosec // file path is derived from trusted config (no user input).
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, plainFilePerm)
	if err != nil {
		return fmt.Errorf("open plain tmp: %w", err)
	}

	copyErr := func() error {
		_, err = io.Copy(dst, src)
		if err != nil {
			return fmt.Errorf("copy to plain: %w", err)
		}

		return nil
	}()

	closeErr := dst.Close()

	if copyErr != nil {
		_ = os.Remove(tmpPath)

		return copyErr
	}

	if closeErr != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("close plain tmp: %w", closeErr)
	}

	renameErr := os.Rename(tmpPath, dstPath)
	if renameErr != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename plain: %w", renameErr)
	}

	return nil
}
