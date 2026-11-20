package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"filippo.io/age"

	"xauth/internal/config"
	"xauth/internal/store"
)

func runBackup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	output := flags.String("output", "", "new encrypted backup file")
	recipientKey := flags.String("recipient", os.Getenv("XAUTH_BACKUP_RECIPIENT"), "age X25519 public key (or set XAUTH_BACKUP_RECIPIENT)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *output == "" || *recipientKey == "" {
		return errors.New("usage: xauth backup -output FILE.age -recipient AGE_PUBLIC_KEY")
	}
	recipient, err := age.ParseX25519Recipient(strings.TrimSpace(*recipientKey))
	if err != nil {
		return errors.New("invalid age recipient: expected an X25519 public key beginning with age1")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := publishNewFile(*output, func(dst *os.File) error {
		dir, err := os.MkdirTemp("", "xauth-snapshot-")
		if err != nil {
			return fmt.Errorf("create private snapshot directory: %w", err)
		}
		snapshot := filepath.Join(dir, "snapshot.db")
		defer cleanupStage(snapshot)
		if err := store.BackupDatabase(ctx, backupSourcePath(), snapshot); err != nil {
			return err
		}
		if err := store.ValidateBackupDatabase(ctx, snapshot); err != nil {
			return err
		}
		source, err := os.Open(snapshot)
		if err != nil {
			return err
		}
		defer source.Close()
		out, err := age.Encrypt(dst, recipient)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, backupReader{ctx, source}); err != nil {
			return err
		}
		return out.Close()
	}, nil); err != nil {
		return fmt.Errorf("save encrypted backup: %w", err)
	}
	fmt.Printf("Encrypted SQLite backup saved to %s\n", *output)
	return nil
}

func runRestore(args []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	input := flags.String("input", "", "encrypted age backup file")
	identityPath := flags.String("identity", "", "age X25519 private identity file")
	target := flags.String("db", "", "new database file; existing files are never replaced")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *input == "" || *identityPath == "" || *target == "" {
		return errors.New("usage: xauth restore -input FILE.age -identity KEY_FILE -db NEW_DB")
	}
	keyFile, err := os.Open(*identityPath)
	if err != nil {
		return fmt.Errorf("open age identity: %w", err)
	}
	identities, err := age.ParseIdentities(keyFile)
	keyFile.Close()
	if err != nil {
		return errors.New("invalid age identity file: expected the file produced by age-keygen")
	}
	source, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("open encrypted backup: %w", err)
	}
	defer source.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := publishNewFile(*target, func(dst *os.File) error {
		out, err := age.Decrypt(source, identities...)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, backupReader{ctx, out})
		return err
	}, func(path string) error {
		if err := store.ValidateBackupDatabase(ctx, path); err != nil {
			return err
		}
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if err := requireNewPath(*target + suffix); err != nil {
				return err
			}
		}
		return ctx.Err()
	}); err != nil {
		return fmt.Errorf("restore encrypted backup: %w", err)
	}
	fmt.Printf("Verified SQLite database restored to %s\n", *target)
	return nil
}

// Stage beside the destination and publish without replacing any existing file.
func publishNewFile(path string, write func(*os.File) error, validate func(string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := requireNewPath(path); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(path), ".xauth-stage-")
	if err != nil {
		return err
	}
	stage := filepath.Join(dir, "payload")
	defer cleanupStage(stage)
	f, err := os.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if validate != nil {
		if err := validate(stage); err != nil {
			return err
		}
	}
	return os.Link(stage, path)
}

func requireNewPath(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%q already exists; choose a new path", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func cleanupStage(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
	_ = os.Remove(filepath.Dir(path))
}

type backupReader struct {
	ctx context.Context
	r   io.Reader
}

func (r backupReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func backupSourcePath() string {
	if path := os.Getenv("XAUTH_DB"); path != "" {
		return path
	}
	return config.Defaults().DBPath
}
