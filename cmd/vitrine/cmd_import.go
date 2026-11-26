package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"runtime"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
)

func cmdImport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("import", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vitrine import [flags] <file or folder>...\n\nFolders are searched recursively; unsupported files are skipped.")
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	verbose := fset.Bool("v", false, "list every file")
	noThumbs := fset.Bool("no-thumbs", false, "don't make thumbnails now (run \"vitrine process\" later)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() == 0 {
		fset.Usage()
		return flag.ErrHelp
	}
	c, err := load()
	if err != nil {
		return err
	}

	lib, closeLib, err := openLibrary(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()

	var added, already, deleted, unsupported, failed int
	var newIDs []uint32
	importOne := func(path string) {
		f, err := lib.ImportFile(ctx, path)
		switch {
		case errors.Is(err, media.ErrUnsupported):
			unsupported++
			if *verbose {
				fmt.Fprintf(stdout, "skip     %s (not a supported file type)\n", path)
			}
		case errors.Is(err, library.ErrDeleted):
			deleted++
			if *verbose {
				fmt.Fprintf(stdout, "deleted  %s (deleted from the library before)\n", path)
			}
		case err != nil:
			failed++
			fmt.Fprintf(stderr, "error    %s: %v\n", path, err)
		case f.New:
			added++
			newIDs = append(newIDs, f.ID)
			if *verbose {
				fmt.Fprintf(stdout, "added    %s -> #%d %s\n", path, f.ID, f.Type)
			}
		default:
			already++
			if *verbose {
				fmt.Fprintf(stdout, "have     %s (#%d)\n", path, f.ID)
			}
		}
	}

	for _, root := range fset.Args() {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				failed++
				fmt.Fprintf(stderr, "error    %s: %v\n", path, err)
				return nil
			}
			if d.Type().IsRegular() {
				importOne(path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(stdout, "stopped: %v\n", err)
			break
		}
	}

	fmt.Fprintf(stdout, "%d added, %d already in the library, %d previously deleted, %d unsupported, %d failed\n",
		added, already, deleted, unsupported, failed)

	if len(newIDs) > 0 && !*noThumbs && ctx.Err() == nil {
		if err := processFiles(ctx, lib, newIDs, 0, stdout, stderr); err != nil {
			fmt.Fprintln(stdout, err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d files failed", failed)
	}
	return nil
}

func cmdProcess(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("process", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vitrine process [flags]\n\nMakes thumbnails and reads media details for files that don't have them yet.")
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	retry := fset.Bool("retry", false, "also retry files that failed before")
	workers := fset.Int("j", 0, "files processed at once (default: half the CPU cores)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	c, err := load()
	if err != nil {
		return err
	}
	lib, closeLib, err := openLibrary(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()
	ids, err := lib.PendingMedia(ctx, *retry)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "nothing to do: every file has its thumbnail")
		return nil
	}
	return processFiles(ctx, lib, ids, *workers, stdout, stderr)
}

// processFiles makes thumbnails for ids, printing progress now and then.
func processFiles(ctx context.Context, lib *library.Library, ids []uint32, workers int, stdout, stderr io.Writer) error {
	if workers <= 0 {
		workers = max(1, runtime.NumCPU()/2)
	}
	start, last := time.Now(), time.Now()
	var done, failed int
	var noTools error
	lib.ProcessMany(ctx, ids, workers, func(id uint32, err error) {
		done++
		switch {
		case errors.Is(err, library.ErrNoMediaTools):
			noTools = err
		case err != nil:
			failed++
			fmt.Fprintf(stderr, "error    %v\n", err)
		}
		if time.Since(last) > 2*time.Second {
			last = time.Now()
			fmt.Fprintf(stdout, "thumbnails: %d of %d...\n", done, len(ids))
		}
	})
	if noTools != nil {
		return fmt.Errorf("thumbnails skipped: %v", noTools)
	}
	fmt.Fprintf(stdout, "thumbnails: %d made, %d failed, in %v\n", done-failed, failed, time.Since(start).Round(time.Millisecond))
	return nil
}

func cmdImportComic(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("import-comic", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vitrine import-comic [flags] <folder or zip/cbz>...\n\nEach folder or archive becomes one comic on the Comics page: its pictures, in name\norder, are the pages (never in the library's grid). The title is the folder's or\narchive's name unless -title gives one.")
		fset.PrintDefaults()
	}
	load := configFlags(fset)
	title := fset.String("title", "", "the comic's title (one folder or archive only)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() == 0 || (*title != "" && fset.NArg() > 1) {
		fset.Usage()
		return flag.ErrHelp
	}
	c, err := load()
	if err != nil {
		return err
	}
	lib, closeLib, err := openLibrary(ctx, c)
	if err != nil {
		return err
	}
	defer closeLib()
	var failed int
	for _, src := range fset.Args() {
		id, n, err := lib.ImportComic(ctx, src, *title)
		if err != nil {
			failed++
			fmt.Fprintf(stderr, "error    %s: %v\n", src, err)
			continue
		}
		fmt.Fprintf(stdout, "comic #%d: %d pages from %s\n", id, n, src)
	}
	// Thumbnails now (the server makes any left over).
	if ids, err := lib.PendingMedia(ctx, false); err == nil {
		lib.ProcessMany(ctx, ids, max(1, runtime.NumCPU()/2), nil)
	}
	if failed > 0 {
		return fmt.Errorf("%d could not be imported", failed)
	}
	return nil
}
