package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/restic/restic/internal/checker"
	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/restic"
)

var cmdCheck = &cobra.Command{
	Use:   "check [flags]",
	Short: "Check the repository for errors",
	Long: `
The "check" command tests the repository for errors and reports any errors it
finds. It can also be used to read all data and therefore simulate a restore.

By default, the "check" command will always load all data directly from the
repository and not use a local cache.
`,
	DisableAutoGenTag: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheck(checkOptions, globalOptions, args)
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return checkFlags(checkOptions)
	},
}

// CheckOptions bundles all options for the 'check' command.
type CheckOptions struct {
	ReadData       bool
	ReadDataSubset []uint
	CheckUnused    bool
	WithCache      bool
}

var checkOptions CheckOptions

func init() {
	cmdRoot.AddCommand(cmdCheck)

	f := cmdCheck.Flags()
	f.BoolVar(&checkOptions.ReadData, "read-data", false, "read all data blobs")
	f.UintSliceVar(&checkOptions.ReadDataSubset, "read-data-subset", nil, "read subset of data packs")
	f.BoolVar(&checkOptions.CheckUnused, "check-unused", false, "find unused blobs")
	f.BoolVar(&checkOptions.WithCache, "with-cache", false, "use the cache")
}

func checkFlags(opts CheckOptions) error {
	if opts.ReadData && opts.ReadDataSubset != nil {
		return errors.Errorf("check flags --readData and --read-data-subset cannot be used together")
	}
	if opts.ReadDataSubset != nil {
		if len(opts.ReadDataSubset) != 2 {
			return errors.Errorf("check flag --read-data-subset must have two values")
		}
		if opts.ReadDataSubset[0] == 0 || opts.ReadDataSubset[1] == 0 || opts.ReadDataSubset[0] > opts.ReadDataSubset[1] {
			return errors.Errorf("check flag --read-data-subset=n,t values must be positive integers, and n <= t")
		}
	}

	return nil
}

func newReadProgress(gopts GlobalOptions, todo restic.Stat) *restic.Progress {
	if gopts.Quiet {
		return nil
	}

	readProgress := restic.NewProgress()

	readProgress.OnUpdate = func(s restic.Stat, d time.Duration, ticker bool) {
		status := fmt.Sprintf("[%s] %s  %d / %d items",
			formatDuration(d),
			formatPercent(s.Blobs, todo.Blobs),
			s.Blobs, todo.Blobs)

		if w := stdoutTerminalWidth(); w > 0 {
			if len(status) > w {
				max := w - len(status) - 4
				status = status[:max] + "... "
			}
		}

		PrintProgress("%s", status)
	}

	readProgress.OnDone = func(s restic.Stat, d time.Duration, ticker bool) {
		fmt.Printf("\nduration: %s\n", formatDuration(d))
	}

	return readProgress
}

func runCheck(opts CheckOptions, gopts GlobalOptions, args []string) error {
	if len(args) != 0 {
		return errors.Fatal("check has no arguments")
	}

	if !opts.WithCache {
		// do not use a cache for the checker
		gopts.NoCache = true
	}

	repo, err := OpenRepository(gopts)
	if err != nil {
		return err
	}

	if !gopts.NoLock {
		Verbosef("create exclusive lock for repository\n")
		lock, err := lockRepoExclusive(repo)
		defer unlockRepo(lock)
		if err != nil {
			return err
		}
	}

	chkr := checker.New(repo)

	Verbosef("load indexes\n")
	hints, errs := chkr.LoadIndex(gopts.ctx)

	dupFound := false
	for _, hint := range hints {
		Printf("%v\n", hint)
		if _, ok := hint.(checker.ErrDuplicatePacks); ok {
			dupFound = true
		}
	}

	if dupFound {
		Printf("\nrun `restic rebuild-index' to correct this\n")
	}

	if len(errs) > 0 {
		for _, err := range errs {
			Warnf("error: %v\n", err)
		}
		return errors.Fatal("LoadIndex returned errors")
	}

	errorsFound := false
	errChan := make(chan error)

	Verbosef("check all packs\n")
	go chkr.Packs(gopts.ctx, errChan)

	for err := range errChan {
		errorsFound = true
		fmt.Fprintf(os.Stderr, "%v\n", err)
	}

	Verbosef("check snapshots, trees and blobs\n")
	errChan = make(chan error)
	go chkr.Structure(gopts.ctx, errChan)

	for err := range errChan {
		errorsFound = true
		if e, ok := err.(checker.TreeError); ok {
			fmt.Fprintf(os.Stderr, "error for tree %v:\n", e.ID.Str())
			for _, treeErr := range e.Errors {
				fmt.Fprintf(os.Stderr, "  %v\n", treeErr)
			}
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}

	if opts.CheckUnused {
		for _, id := range chkr.UnusedBlobs() {
			Verbosef("unused blob %v\n", id.Str())
			errorsFound = true
		}
	}

	doReadData := func(bucket, totalBuckets uint) {
		packs := restic.IDSet{}
		for pack := range chkr.GetPacks() {
			if (uint(pack[0]) % totalBuckets) == (bucket - 1) {
				packs.Insert(pack)
			}
		}
		packCount := uint64(len(packs))

		if packCount < chkr.CountPacks() {
			Verbosef(fmt.Sprintf("read group #%d of %d data packs (out of total %d packs in %d groups)\n", opts.ReadDataSubset[0], packCount, chkr.CountPacks(), opts.ReadDataSubset[1]))
		} else {
			Verbosef("read all data\n")
		}

		p := newReadProgress(gopts, restic.Stat{Blobs: packCount})
		errChan := make(chan error)

		go chkr.ReadPacks(gopts.ctx, packs, p, errChan)

		for err := range errChan {
			errorsFound = true
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}

	switch {
	case opts.ReadData:
		doReadData(1, 1)
	case len(opts.ReadDataSubset) == 2:
		doReadData(opts.ReadDataSubset[0], opts.ReadDataSubset[1])
	}

	if errorsFound {
		return errors.Fatal("repository contains errors")
	}

	Verbosef("no errors were found\n")

	return nil
}
