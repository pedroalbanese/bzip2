// Copyright (c) 2010, Andrei Vieru. All rights reserved.
// Copyright (c) 2021, Pedro Albanese. All rights reserved.
// Copyright (c) 2025: Pindorama
//		Luiz Antônio Rangel (takusuman)
// All rights reserved.
// Use of this source code is governed by a ISC license that
// can be found in the LICENSE file.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/dsnet/compress/bzip2"
	"github.com/mattn/go-isatty"
	"pindorama.net.br/getopt"
)

// Command-line flags
var (
	stdout     = flag.Bool("c", false, "write on standard output, keep original files unchanged")
	decompress = flag.Bool("d", false, "decompress; see also -c and -k")
	force      = flag.Bool("f", false, "force overwrite of output file")
	help       = flag.Bool("h", false, "print this help message")
	verbose    = flag.Bool("v", false, "be verbose")
	keep       = flag.Bool("k", false, "keep original files unchanged")
	suffix     = flag.String("S", "bz2", "use provided suffix on compressed files")
	cores      = flag.Int("cores", 0, "number of cores to use for parallelization")
	test       = flag.Bool("t", false, "test compressed file integrity")
	compress   = flag.Bool("z", true, "compress file(s)")
	level      = flag.Int("l", 9, "compression level (1 = fastest, 9 = best)")
	recursive  = flag.Bool("r", false, "operate recursively on directories")

	stdin bool // Indicates if reading from standard input
)

// usage displays program usage instructions
func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... [FILE]...\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Compress or uncompress FILEs (by default, compress FILEs in-place).\n\n")
	getopt.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nWith no FILE, or when FILE is -, read standard input.\n")
}

// exit shows an error message and exits the program with error code
func exit(msg string) {
	usage()
	fmt.Fprintln(os.Stderr)
	log.Fatalf("%s: check args: %s\n\n", os.Args[0], msg)
}

// setByUser checks whether a specific flag was explicitly set by the user
func setByUser(name string) (isSet bool) {
	getopt.Visit(func(f *flag.Flag) {
		if f.Name == name {
			isSet = true
		}
	})
	return
}

// processFile processes a single file (compression, decompression, or test)
// Returns an error if any issue occurs during processing
func processFile(inFilePath string) error {
	// Checks for conflicting flags
	if *stdout == true && setByUser("S") == true {
		return fmt.Errorf("stdout set, suffix not used")
	}
	if *stdout == true && *force == true {
		return fmt.Errorf("stdout set, force not used")
	}
	if *stdout == true && *keep == true {
		return fmt.Errorf("stdout set, keep is redundant")
	}

	var outFilePath string // Output file path

	// Test mode: verifies compressed file integrity
	if *test {
		var inFile *os.File
		var err error
		if inFilePath == "-" {
			inFile = os.Stdin
		} else {
			inFile, err = os.Open(inFilePath)
			if err != nil {
				return err
			}
			defer inFile.Close()
		}

		z, err := bzip2.NewReader(inFile, nil)
		if err != nil {
			return fmt.Errorf("corrupted file or format error: %v", err)
		}
		defer z.Close()

		_, err = io.Copy(io.Discard, z)
		if err != nil {
			return fmt.Errorf("test failed: %v", err)
		}

		if *verbose {
			fmt.Fprintf(os.Stderr, "%s: OK\n", inFilePath)
		}
		return nil
	}

	// Determines the input source (stdin or file)
	if stdin {
		if *stdout != true {
			return fmt.Errorf("reading from stdin, can write only to stdout")
		}
		if setByUser("S") == true {
			return fmt.Errorf("reading from stdin, suffix not needed")
		}
	} else { // read from file
		f, err := os.Lstat(inFilePath)
		if err != nil {
			return err
		}
		if f == nil {
			return fmt.Errorf("file %s not found", inFilePath)
		}
		if f.IsDir() {
			return fmt.Errorf("%s is a directory", inFilePath)
		}

		// Determines the output destination (file)
		if !*stdout { // write to file
			if *suffix == "" {
				return fmt.Errorf("suffix can't be an empty string")
			}

			// Generates output file name
			fext := ("." + *suffix)
			if *decompress {
				outFileDir, outFileName := path.Split(inFilePath)
				if strings.HasSuffix(outFileName, fext) {
					if len(outFileName) > len(fext) {
						nstr := strings.SplitN(outFileName, ".", len(outFileName))
						estr := strings.Join(nstr[0:len(nstr)-1], ".")
						outFilePath = (outFileDir + estr)
					} else {
						return fmt.Errorf("can't strip suffix .%s from file %s",
							*suffix, inFilePath)
					}
				} else {
					fmt.Fprintf(os.Stderr, "file %s doesn't have suffix .%s\n",
						inFilePath, *suffix)
					fmt.Fprintf(os.Stderr, "Can't guess original name for %s -- using %s.out\n",
						inFilePath, inFilePath)
					outFilePath = (outFileDir + outFileName + ".out")
				}
			} else {
				if strings.HasSuffix(inFilePath, fext) {
					return fmt.Errorf("Input file %s already has .%s suffix.",
						inFilePath, *suffix)
				}
				outFilePath = inFilePath + "." + *suffix
			}

			// Checks if output file already exists
			f, err = os.Lstat(outFilePath)
			if err == nil && f != nil {
				if !*force {
					return fmt.Errorf("outFile %s exists. use -f to overwrite", outFilePath)
				}
				if f.IsDir() {
					return fmt.Errorf("outFile %s is a directory", outFilePath)
				}
				err = os.Remove(outFilePath)
				if err != nil {
					return err
				}
			}
		}
	}

	// Creates a pipe for communication between goroutines
	pr, pw := io.Pipe()

	var logMu sync.Mutex

	// File decompression
	if *decompress {
		go func() {
			defer pw.Close()
			var inFile *os.File
			var err error
			if inFilePath == "-" {
				inFile = os.Stdin
			} else {
				inFile, err = os.Open(inFilePath)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				defer inFile.Close()
			}

			_, err = io.Copy(pw, inFile)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
		}()

		z, err := bzip2.NewReader(pr, nil)
		if err != nil {
			pr.Close()
			return err
		}
		defer z.Close()

		var outFile *os.File
		if *stdout {
			outFile = os.Stdout
		} else {
			outFile, err = os.Create(outFilePath)
			if err != nil {
				pr.Close()
				return err
			}
			defer outFile.Close()
		}

		_, err = io.Copy(outFile, z)
		pr.Close()
		if err != nil {
			return err
		}

		if *verbose && !*stdout {
			logMu.Lock()
			fmt.Fprintf(os.Stderr, "%s: done\n", inFilePath)
			logMu.Unlock()
		}
	} else { // File compression
		go func() {
			defer pw.Close()
			var inFile *os.File
			var err error
			if inFilePath == "-" {
				inFile = os.Stdin
			} else {
				inFile, err = os.Open(inFilePath)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				defer inFile.Close()
			}

			z, err := bzip2.NewWriter(pw, &bzip2.WriterConfig{Level: *level})
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			defer z.Close()

			_, err = io.Copy(z, inFile)
			if err != nil {
				pw.CloseWithError(err)
				return
			}

			if *verbose {
				var buf strings.Builder
				compratio := (float64(z.InputOffset) / float64(z.OutputOffset))
				fmt.Fprintf(&buf, "%s: %6.3f:1, %6.3f bits/byte, %5.2f%% saved, %d in, %d out.\n",
					inFilePath,
					compratio,
					((1 / compratio) * 8),
					(100 * (1 - (1 / compratio))),
					z.InputOffset, z.OutputOffset)

				logMu.Lock()
				fmt.Fprint(os.Stderr, buf.String())
				logMu.Unlock()
			}
		}()

		var outFile *os.File
		var err error
		if *stdout {
			outFile = os.Stdout
		} else {
			outFile, err = os.Create(outFilePath)
			if err != nil {
				pr.Close()
				return err
			}
			defer outFile.Close()
		}

		_, err = io.Copy(outFile, pr)
		pr.Close()
		if err != nil {
			return err
		}
	}

	// Removes the original file if needed
	if !*stdout && !*keep && inFilePath != "-" {
		err := os.Remove(inFilePath)
		if err != nil {
			return err
		}
	}

	return nil
}

// main is the program's entry point
func main() {
	// Configure flags for compression levels (1–9)
	for i := 1; i <= 9; i++ {
		levelValue := i
		explanation := fmt.Sprintf("set block size to %dk", (i * 100))
		if i == 9 {
			explanation += " (default)"
		}
		flag.BoolFunc(strconv.Itoa(i), explanation, func(string) error {
			*level = levelValue
			return nil
		})
	}
	_ = flag.Bool("s", false,
		"use less memory; bogus option for backwards compatibility")

	// Alias short flags with their long counterparts.
	getopt.Aliases(
		"1", "fast",
		"9", "best",
		"c", "stdout",
		"d", "decompress",
		"f", "force",
		"k", "keep",
		"r", "recursive",
		"t", "test",
		"v", "verbose",
		"z", "compress",
		"s", "small",
		"h", "help",
	)

	// Parse command-line flags
	getopt.Parse()

	// Check if someone has used '-#' for a compression level.
	if !setByUser("l") {
		for i := 1; i <= 9; i++ {
			if setByUser(strconv.Itoa(i)) {
				*level = i
				break
			}
		}
	}

	// Validate compression level
	if *level < 1 || *level > 9 {
		exit("invalid compression level: must be between 1 and 9")
	}

	// Show help if requested
	if *help {
		usage()
		os.Exit(0)
	}

	// Validate number of cores
	if setByUser("cores") && (*cores < 1 || *cores > 32) {
		exit("invalid number of cores")
	}

	// Get list of files to process
	files := flag.Args()
	if len(files) == 0 {
		files = []string{"-"} // default to stdin
		stdin = true          // read from stdin
	}

	// Make *stdout implicit if it is not a
	// terminal, but just if also using stdin.
	if !isatty.IsTerminal(os.Stdout.Fd()) &&
		stdin && !*stdout {
		*stdout = true
	}

	// From 'go doc runtime.GOMAXPROCS':
	// "It defaults to the value of runtime.NumCPU.
	// If n < 1, it does not change the current setting."
	// In fact, if the default value of cores is zero, it
	// will use all the cores of the machine.
	if *cores <= 0 {
		*cores = runtime.NumCPU()
	}

	// Process each file
	hasErrors := false
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, *cores)

	for _, file := range files {
		file := file
		wg.Add(1)

		go func(f string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if file == "-" {
				err := processFile(file)
				if err != nil {
					log.Printf("%s: %v", file, err)
					hasErrors = true
				}
				return
			}

			info, err := os.Stat(file)
			if err != nil {
				log.Printf("%s: %v", file, err)
				hasErrors = true
				return
			}

			if info.IsDir() {
				if *recursive {
					err = filepath.Walk(f, func(path string, fi os.FileInfo, err error) error {
						if err != nil {
							mu.Lock()
							log.Printf("%s: %v", path, err)
							hasErrors = true
							mu.Unlock()
							return nil
						}
						if !fi.IsDir() {
							if err := processFile(path); err != nil {
								mu.Lock()
								log.Printf("%s: %v", path, err)
								hasErrors = true
								mu.Unlock()
							}
						}
						return nil
					})
					if err != nil {
						mu.Lock()
						log.Printf("%s: %v", f, err)
						hasErrors = true
						mu.Unlock()
					}
				} else {
					mu.Lock()
					log.Printf("%s is a directory (use -r to process recursively)", f)
					hasErrors = true
					mu.Unlock()
				}
			} else {
				if err := processFile(f); err != nil {
					mu.Lock()
					log.Printf("%s: %v", f, err)
					hasErrors = true
					mu.Unlock()
				}
			}
		}(file)
	}

	wg.Wait()
	if hasErrors {
		os.Exit(1)
	}
}
