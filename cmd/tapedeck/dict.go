package main

import (
	"fmt"
	"os"

	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/spf13/cobra"
)

// The operator's read path for a dictionary-compressed capture — F6.
//
// WHY THIS FILE EXISTS. codec grew dictionary compression; tapedeck could not
// open the result. Every command failed with "read header: unknown dictionary"
// and there was no flag to supply one — `tapedeck --help | grep -i dict` was
// empty. That is the defect class AGENTS.md §4 names: a write path with no
// operator read path, which makes a shipped feature unusable by the person who
// has to operate it.
//
// The refusal itself was CORRECT and is deliberately preserved. A capture
// written with a dictionary must fail loudly without it rather than return
// wrong bytes; that is the whole basis of the dictionary's permanent
// obligation. What was missing was the way to satisfy the obligation, not the
// obligation.
//
// One flag, defined in one place, added to every command that opens a capture,
// so the answer to "how do I read this" does not depend on which subcommand a
// person happened to reach for.

// dictFlagName is the flag every capture-reading command carries.
const dictFlagName = "dict"

// addDictFlag registers --dict on a command that opens a .tape.
func addDictFlag(cmd *cobra.Command) {
	cmd.Flags().String(dictFlagName, "",
		"path to the zstd dictionary the capture was written with (see `tape` docs; required "+
			"only for a dictionary-compressed capture, which fails loudly without it)")
}

// dictBytes loads the dictionary named by --dict, or nil when the flag is
// absent or empty. A named file that cannot be read is an error rather than a
// silent nil: falling back to "no dictionary" would turn a typo in a path into
// "unknown dictionary", which sends the reader looking in the wrong place.
func dictBytes(cmd *cobra.Command) ([]byte, error) {
	if cmd == nil {
		return nil, nil
	}
	f := cmd.Flags().Lookup(dictFlagName)
	if f == nil {
		return nil, nil
	}
	path := f.Value.String()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is an operator-supplied argument
	if err != nil {
		return nil, fmt.Errorf("read dictionary %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("dictionary %s is empty", path)
	}
	return data, nil
}

// openTape opens a capture for reading, supplying the dictionary named by
// --dict when there is one. It is what every command uses instead of
// codec.NewReader, so a dictionary capture is readable everywhere or nowhere
// rather than in whichever commands someone remembered.
func openTape(cmd *cobra.Command, path string, opts ...codec.ReaderOption) (*codec.Reader, error) {
	dict, err := dictBytes(cmd)
	if err != nil {
		return nil, err
	}
	return codec.NewReaderWithDictionary(path, dict, opts...)
}
