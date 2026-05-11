package stup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

const (
	installBinPath  = "/usr/local/bin/infra-helper"
	installUnitPath = "/etc/systemd/system/infra-helper-stup.service"
	installUnitName = "infra-helper-stup.service"

	installBinPerm  os.FileMode = 0o755
	installUnitPerm os.FileMode = 0o644
)

const unitTemplate = `[Unit]
Description=infra-helper stup (static stub sign-in page)
After=network.target

[Service]
Type=simple
ExecStart=%s stup --no-metrics --listen %s
Restart=on-failure
RestartSec=2
DynamicUser=yes
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
`

var errInstallNeedsRoot = errors.New("install requires root (run with sudo)")

// Install copies the running binary to /usr/local/bin, writes a systemd unit,
// reloads systemd, and enables+starts the unit.
func Install(listen string) error {
	if os.Geteuid() != 0 {
		return errInstallNeedsRoot
	}

	src, err := resolveSelfPath()
	if err != nil {
		return err
	}

	binErr := installBinary(src)
	if binErr != nil {
		return binErr
	}

	unitErr := writeUnit(listen)
	if unitErr != nil {
		return unitErr
	}

	reloadErr := runSystemctl("daemon-reload")
	if reloadErr != nil {
		return reloadErr
	}

	enableErr := runSystemctl("enable", installUnitName)
	if enableErr != nil {
		return enableErr
	}

	restartErr := runSystemctl("restart", installUnitName)
	if restartErr != nil {
		return restartErr
	}

	log.Info().Str("unit", installUnitName).Msg("service enabled and (re)started")

	return nil
}

func resolveSelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate self: %w", err)
	}

	resolved, evalErr := filepath.EvalSymlinks(exe)
	if evalErr != nil {
		return "", fmt.Errorf("resolve self path: %w", evalErr)
	}

	return resolved, nil
}

func installBinary(src string) error {
	if src == installBinPath {
		log.Info().Str("path", installBinPath).Msg("binary already at destination")

		return nil
	}

	copyErr := copyFileAtomic(src, installBinPath, installBinPerm)
	if copyErr != nil {
		return fmt.Errorf("copy binary to %s: %w", installBinPath, copyErr)
	}

	log.Info().Str("src", src).Str("dst", installBinPath).Msg("binary installed")

	return nil
}

func writeUnit(listen string) error {
	unit := fmt.Sprintf(unitTemplate, installBinPath, listen)

	writeErr := os.WriteFile(installUnitPath, []byte(unit), installUnitPerm)
	if writeErr != nil {
		return fmt.Errorf("write unit file: %w", writeErr)
	}

	log.Info().Str("unit", installUnitPath).Msg("unit file written")

	return nil
}

func copyFileAtomic(src, dst string, perm os.FileMode) error {
	srcFile, openErr := os.Open(src)
	if openErr != nil {
		return fmt.Errorf("open src: %w", openErr)
	}
	defer func() { _ = srcFile.Close() }()

	dir := filepath.Dir(dst)

	tmp, tmpErr := os.CreateTemp(dir, ".infra-helper.*")
	if tmpErr != nil {
		return fmt.Errorf("create tmp: %w", tmpErr)
	}

	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	_, copyErr := io.Copy(tmp, srcFile)
	if copyErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("copy bytes: %w", copyErr)
	}

	chmodErr := tmp.Chmod(perm)
	if chmodErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("chmod tmp: %w", chmodErr)
	}

	closeErr := tmp.Close()
	if closeErr != nil {
		return fmt.Errorf("close tmp: %w", closeErr)
	}

	renameErr := os.Rename(tmpPath, dst)
	if renameErr != nil {
		return fmt.Errorf("rename tmp to dst: %w", renameErr)
	}

	return nil
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	if runErr != nil {
		return fmt.Errorf("systemctl %v: %w", args, runErr)
	}

	return nil
}
