package main

import (
	"context"
	"crypto/sha256"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/hashing"
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/restic"
	"github.com/spf13/cobra"
)

var cmdAdhoc = &cobra.Command{
	Use:               "adhoc [...]",
	Short:             "adhoc command",
	Long:              `adhoc command`,
	DisableAutoGenTag: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAdhoc(globalOptions, args)
	},
}

var adhocOptions struct {
	target string
}

func init() {
	cmdRoot.AddCommand(cmdAdhoc)

	flags := cmdAdhoc.Flags()
	flags.StringVar(&adhocOptions.target, "target", "", "local directory to check against")
}

type adhocFileNode struct {
	path string
	node *restic.Node
}

func adhocFileCheck(ctx context.Context, repo *repository.Repository, path string, entry *restic.Node) error {
	path = filepath.Join(adhocOptions.target, path)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	for idx, id := range entry.Content {
		size, err := repo.LookupBlobSize(id, restic.DataBlob)
		if err != nil {
			return err
		}
		hrd := hashing.NewReader(io.LimitReader(file, int64(size)), sha256.New())
		io.Copy(ioutil.Discard, hrd)
		hash := restic.IDFromHash(hrd.Sum(nil))
		if !id.Equal(hash) {
			return errors.Errorf("ERR %s: chuck %d checksum %s != %s \n", path, idx, id, hash)
		}
	}

	return nil
}

func adhocTreeAction(ctx context.Context, repo *repository.Repository, id *restic.ID) error {
	wg := sync.WaitGroup{}
	ch := make(chan adhocFileNode)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range ch {
				err := adhocFileCheck(ctx, repo, entry.path, entry.node)
				if err == nil {
					Printf("%s\n", entry.path)
				} else {
					Printf("ERR %s: %v\n", entry.path, err)
				}
			}
		}()
	}
	err := adhocTreeAction0(ctx, repo, id, string(filepath.Separator), func(path string, node *restic.Node) {
		ch <- adhocFileNode{path: path, node: node}
	})
	close(ch)
	if err != nil {
		return err
	}
	wg.Wait()
	return nil
}

func adhocTreeAction0(ctx context.Context, repo *repository.Repository, id *restic.ID, prefix string, action func(path string, node *restic.Node)) error {
	tree, err := repo.LoadTree(ctx, *id)
	if err != nil {
		return err
	}

	for _, entry := range tree.Nodes {
		// Printf("%s\n", formatNode(prefix, entry, lsOptions.ListLong))

		path := filepath.Join(prefix, entry.Name)
		if entry.Type == "dir" && entry.Subtree != nil {
			if err = adhocTreeAction0(ctx, repo, entry.Subtree, path, action); err != nil {
				return err
			}
			continue
		}

		action(path, entry)
	}

	return nil
}

func runAdhoc(gopts GlobalOptions, args []string) error {
	if adhocOptions.target == "" {
		return errors.Errorf("--target is not specified")
	}

	repo, err := OpenRepository(gopts)
	if err != nil {
		return err
	}

	if err = repo.LoadIndex(gopts.ctx); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(gopts.ctx)
	defer cancel()
	for sn := range FindFilteredSnapshots(ctx, repo, "", restic.TagLists{}, []string{} /*paths*/, []string{"latest"}) {
		Verbosef("snapshot %s of %v at %s):\n", sn.ID().Str(), sn.Paths, sn.Time)

		if err = adhocTreeAction(gopts.ctx, repo, sn.Tree); err != nil {
			return err
		}
	}
	return nil
}
