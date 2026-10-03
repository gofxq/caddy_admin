package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	caddyadapter "github.com/gofxq/caddy_admin/internal/adapter/caddy"
	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/config"
	"github.com/gofxq/caddy_admin/internal/domain"
	"github.com/gofxq/caddy_admin/internal/supervisor"
)

func containerCommands(c config.Config, executable string, initialized bool) []*exec.Cmd {
	commands := []*exec.Cmd{}
	if initialized && c.AdminURL == "" {
		commands = append(commands, exec.Command(c.CaddyBinary, "run", "--config", c.ActivePath()))
	}
	commands = append(commands, exec.Command(executable, "serve"))
	for _, cmd := range commands {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	if len(commands) > 0 {
		commands[len(commands)-1].Env = append(os.Environ(), "MANAGER_SUPERVISED=true")
	}
	return commands
}

func needsCloudflareToken(c config.Config, certificateMode domain.CertificateMode) bool {
	return c.AdminURL == "" && !c.TestTLS && certificateMode == domain.CertificateModeCloudflare
}

func runContainer(c config.Config) error {
	store, err := gormstore.Open(filepath.Join(c.DataDir, "state.db"))
	if err != nil {
		return err
	}
	initialized, err := store.IsInitialized(context.Background())
	certificateMode := domain.CertificateModeBootstrapInternal
	if err == nil && initialized {
		settings, settingsErr := store.ManagedSettings(context.Background())
		if settingsErr != nil {
			err = settingsErr
		} else {
			err = config.ApplyManagedSettings(&c, settings)
		}
		if err == nil && c.AdminURL == "" {
			err = reconcileCertificateActivation(store, c)
		}
		if err == nil {
			status, statusErr := store.CertificateStatus(context.Background())
			if statusErr != nil {
				err = statusErr
			} else {
				certificateMode = status.Mode
			}
		}
	}
	_ = store.Close()
	if err != nil {
		return err
	}
	if initialized && c.AdminURL == "" {
		if err := caddyadapter.EnsureBootstrapCertificate(filepath.Join(c.DataDir, "secrets", "setup_tls.crt"), filepath.Join(c.DataDir, "secrets", "setup_tls.key"), "https://127.0.0.1"); err != nil {
			return err
		}
		raw, err := os.ReadFile(c.ActivePath())
		if err != nil {
			return fmt.Errorf("read embedded snapshot: %w", err)
		}
		var snapshot struct {
			Admin struct {
				Listen string `json:"listen"`
			} `json:"admin"`
			Storage struct {
				Module string `json:"module"`
				Root   string `json:"root"`
			} `json:"storage"`
		}
		if json.Unmarshal(raw, &snapshot) != nil || snapshot.Admin.Listen != "unix/"+c.Socket || snapshot.Storage.Module != "file_system" || snapshot.Storage.Root != c.CaddyStorage {
			return fmt.Errorf("incompatible embedded snapshot: restore an embedded-mode backup or initialize a separate deployment; external snapshots cannot be started locally")
		}
	}
	if initialized && needsCloudflareToken(c, certificateMode) {
		if err := loadCloudflareSecret(); err != nil {
			return err
		}
		if err := domain.ValidateCloudflareToken(os.Getenv("CLOUDFLARE_API_TOKEN")); err != nil {
			return err
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return supervisor.Run(ctx, 15*time.Second, containerCommands(c, executable, initialized)...)
}

func reconcileCertificateActivation(store *gormstore.Store, c config.Config) error {
	status, err := store.CertificateStatus(context.Background())
	if err != nil {
		return err
	}
	if status.ActivationStatus != "applying" && status.ActivationStatus != "uncertain" {
		return nil
	}
	raw, err := os.ReadFile(c.ActivePath())
	if err != nil {
		return err
	}
	activeHash := domain.Fingerprint(raw)
	if status.CandidateHash != "" && activeHash == status.CandidateHash {
		status.Mode = domain.CertificateModeCloudflare
		status.ActivationStatus = "success"
		status.PublicStatus = "pending"
		status.LastErrorClass = ""
		status.BeforeHash, status.CandidateHash = "", ""
	} else if status.BeforeHash != "" && activeHash == status.BeforeHash {
		// The manager run supervisor starts Caddy from this snapshot after this
		// check. Therefore the old bootstrap configuration is now authoritative.
		status.Mode = domain.CertificateModeBootstrapInternal
		status.ActivationStatus = "failed"
		status.PublicStatus = "unknown"
		status.LastErrorClass = "unavailable"
		status.BeforeHash, status.CandidateHash = "", ""
	} else {
		// Neither known fingerprint is authoritative: fail closed and preserve
		// the intent hashes for operator reconciliation.
		status.ActivationStatus = "uncertain"
	}
	return store.SetCertificateStatusAudit(context.Background(), status, "system", status.ActivationStatus)
}
