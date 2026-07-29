package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/echotools/tape/pkg/codec"
	"github.com/echotools/tape/pkg/conversion"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

func newConvertCommand() *cobra.Command {
	var (
		outputDir string
		workers   int
		recursive bool
		glob      string
		overwrite bool
		dryRun    bool
		progress  bool
	)

	cmd := &cobra.Command{
		Use:   "convert [files or dirs...]",
		Short: "Convert legacy captures to tape format",
		Long: `Convert .echoreplay and .nevrcap files to the v2 tape format.

Accepts individual files, directories (with --recursive), or glob patterns.
Output files are written alongside the input with .tape extension unless
--output-dir is specified.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Collect input files.
			files, err := collectInputFiles(args, recursive, glob)
			if err != nil {
				return err
			}

			if len(files) == 0 {
				printf(cmd.OutOrStdout(), "no matching files found\n")
				return nil
			}

			// Dry run: print list and exit.
			if dryRun {
				for _, f := range files {
					outPath := outputPathFor(f, outputDir)
					printf(cmd.OutOrStdout(), "%s -> %s\n", f, outPath)
				}
				printf(cmd.OutOrStdout(), "\n%d file(s) would be converted\n", len(files))
				return nil
			}

			// Create output directory if specified.
			if outputDir != "" {
				if err := os.MkdirAll(outputDir, 0o750); err != nil {
					return fmt.Errorf("create output dir: %w", err)
				}
			}

			// Set up worker pool.
			if workers <= 0 {
				workers = runtime.NumCPU()
			}
			if workers > len(files) {
				workers = len(files)
			}

			type workItem struct {
				input  string
				output string
			}
			type workResult struct {
				result *conversion.ConvertResult
				err    error
				item   workItem
			}

			work := make(chan workItem, len(files))
			results := make(chan workResult, len(files))

			// Set up optional progress bar.
			var bar *progressbar.ProgressBar
			if progress {
				bar = progressbar.NewOptions(len(files),
					progressbar.OptionSetDescription("converting"),
					progressbar.OptionSetWriter(cmd.ErrOrStderr()),
					progressbar.OptionShowCount(),
					progressbar.OptionShowIts(),
					progressbar.OptionSetItsString("files"),
				)
			}

			// Start workers.
			var wg sync.WaitGroup
			for range workers {
				wg.Go(func() {
					for item := range work {
						res, convErr := convertOne(item.input, item.output, overwrite)
						results <- workResult{item: item, result: res, err: convErr}
					}
				})
			}

			// Enqueue work.
			for _, f := range files {
				work <- workItem{input: f, output: outputPathFor(f, outputDir)}
			}
			close(work)

			// Collect results in background.
			go func() {
				wg.Wait()
				close(results)
			}()

			var converted, skipped, failed atomic.Int32
			var totalFrames, totalEvents, unparsedLines atomic.Uint32
			var errs []string
			// Inputs whose lines the reader could not parse. A tape built from
			// such a file is not a complete record of its source, so it is
			// called out rather than folded into the frame count.
			var lossy []string
			// Engine strings no conversion table recognised, aggregated across
			// every input so a corpus pass reports the vocabulary gap once
			// rather than per file (VOCAB-001).
			unmapped := map[string]uint32{}

			for res := range results {
				if bar != nil {
					bar.Add(1) //nolint:errcheck // progress bar errors are non-fatal
				}
				if res.err != nil {
					failed.Add(1)
					errs = append(errs, fmt.Sprintf("  %s: %v", res.item.input, res.err))
					continue
				}
				if res.result == nil {
					skipped.Add(1)
					continue
				}
				converted.Add(1)
				totalFrames.Add(res.result.FrameCount)
				totalEvents.Add(res.result.EventCount)
				if n := res.result.SkippedLines; n > 0 {
					unparsedLines.Add(n)
					lossy = append(lossy, fmt.Sprintf("  %s: %d line(s) unparsed, %d frame(s) kept",
						res.item.input, n, res.result.FrameCount))
				}
				for _, u := range res.result.UnmappedValues {
					unmapped[u.Field+" = "+strconv.Quote(u.Value)] += u.Count
				}
			}

			if bar != nil {
				bar.Finish() //nolint:errcheck // progress bar errors are non-fatal
				println(cmd.ErrOrStderr())
			}

			// Print summary.
			printf(cmd.OutOrStdout(), "converted: %d, skipped: %d, failed: %d\n",
				converted.Load(), skipped.Load(), failed.Load())
			printf(cmd.OutOrStdout(), "total frames: %d, total events: %d\n",
				totalFrames.Load(), totalEvents.Load())

			if n := unparsedLines.Load(); n > 0 {
				printf(cmd.OutOrStdout(),
					"unparsed lines: %d — output is NOT a complete record of its source\n", n)
				println(cmd.ErrOrStderr(), "\nunparsed input:")
				for _, l := range lossy {
					println(cmd.ErrOrStderr(), l)
				}
			}

			if len(unmapped) > 0 {
				printf(cmd.OutOrStdout(),
					"unmapped engine values: %d distinct — these convert to UNSPECIFIED and are LOST\n",
					len(unmapped))
				println(cmd.ErrOrStderr(), "\nunmapped engine values:")
				for _, k := range slices.Sorted(maps.Keys(unmapped)) {
					println(cmd.ErrOrStderr(), fmt.Sprintf("  %s (%d occurrence(s))", k, unmapped[k]))
				}
			}

			if len(errs) > 0 {
				println(cmd.ErrOrStderr(), "\nerrors:")
				for _, e := range errs {
					println(cmd.ErrOrStderr(), e)
				}
				return fmt.Errorf("%d file(s) failed", failed.Load())
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&outputDir, "output-dir", "", "directory for output files (default: alongside input)")
	cmd.Flags().IntVar(&workers, "workers", runtime.NumCPU(), "number of parallel workers")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "recurse into directories")
	cmd.Flags().StringVar(&glob, "glob", "", "glob pattern to filter files (e.g. *.echoreplay)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite existing output files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list files without converting")
	cmd.Flags().BoolVar(&progress, "progress", false, "show progress bar")

	return cmd
}

// convertOne converts a single file, handling skip logic for existing outputs.
func convertOne(input, output string, overwrite bool) (*conversion.ConvertResult, error) {
	if !overwrite {
		if _, err := os.Stat(output); err == nil {
			return nil, nil // skip: output exists
		}
	}
	return conversion.ConvertFile(input, output)
}

// outputPathFor computes the output path for a given input file.
func outputPathFor(input, outputDir string) string {
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext) + codec.ExtTape

	if outputDir != "" {
		return filepath.Join(outputDir, name)
	}
	return filepath.Join(filepath.Dir(input), name)
}

// collectInputFiles expands arguments into a list of convertible file paths.
func collectInputFiles(args []string, recursive bool, globPattern string) ([]string, error) {
	var files []string

	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			// Try as glob pattern.
			matches, globErr := filepath.Glob(arg)
			if globErr != nil || len(matches) == 0 {
				return nil, fmt.Errorf("not found: %s", arg)
			}
			for _, m := range matches {
				if isConvertibleFile(m) {
					files = append(files, m)
				}
			}
			continue
		}

		if !info.IsDir() {
			if isConvertibleFile(arg) {
				files = append(files, arg)
			}
			continue
		}

		// Directory: walk if recursive, otherwise list top-level.
		if recursive {
			err := filepath.WalkDir(arg, func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !d.IsDir() && isConvertibleFile(path) {
					if matchesGlob(path, globPattern) {
						files = append(files, path)
					}
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", arg, err)
			}
		} else {
			entries, readErr := os.ReadDir(arg)
			if readErr != nil {
				return nil, fmt.Errorf("read dir %s: %w", arg, readErr)
			}
			for _, e := range entries {
				path := filepath.Join(arg, e.Name())
				if !e.IsDir() && isConvertibleFile(path) {
					if matchesGlob(path, globPattern) {
						files = append(files, path)
					}
				}
			}
		}
	}

	return files, nil
}

func isConvertibleFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".echoreplay" || ext == ".nevrcap" || ext == ".tape"
}

func matchesGlob(path, pattern string) bool {
	if pattern == "" {
		return true
	}
	matched, err := filepath.Match(pattern, filepath.Base(path))
	return err == nil && matched
}
