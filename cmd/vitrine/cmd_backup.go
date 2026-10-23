package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/usevitrine/vitrine/internal/backup"
)

// vitrine backup: a backup of the library database now (also while the
// server runs).
func cmdBackup(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("backup", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vitrine backup [flags]\n\nCopies the library database (tags, subscriptions, settings, deletion records) to the\nbackup folder, safely even while Vitrine runs. Thumbnails and site credentials are left out.")
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	keep := fset.Int("keep", 0, "keep only the newest N backups (0 = keep all)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	dbPath := filepath.Join(c.DataDir, "meta.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no library at %s", c.DataDir)
	}
	b, err := backup.New(dbPath, c.BackupDir).Create(ctx, *keep)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s (%.1f MB)\n", filepath.Join(c.BackupDir, b.Name), float64(b.Size)/(1<<20))
	return nil
}

// vitrine restore <file>: checks a backup and puts it in place the next time
// Vitrine starts.
func cmdRestore(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("restore", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vitrine restore [flags] <backup file>\n\nChecks a backup (a .db.gz from \"vitrine backup\" or Settings > Backups) and puts it in place\nof the library database the next time Vitrine starts (the current one is kept as\nmeta.db.before-restore). Restart Vitrine to finish.")
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		fset.Usage()
		return flag.ErrHelp
	}
	c, err := load()
	if err != nil {
		return err
	}
	f, err := os.Open(fset.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return err
	}
	if err := backup.New(filepath.Join(c.DataDir, "meta.db"), c.BackupDir).Stage(f); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "The backup is checked and ready: it replaces the library the next time Vitrine starts.")
	return nil
}
