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

	"github.com/dsnet/compress/bzip2"
	"rsc.io/getopt"
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

	ActualFlags []*flag.Flag
	stdin       bool // Indicates if reading from standard input
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

func parseActualFlags(args []string) {
	for s := 0; s < len(args); s++ {
		arg := args[s]
		switch arg[0] {
		case '-':
			if len(arg) == 1 {
				break
			} else if arg[1] != '-' { /* Vulgar UNIX command line options. */
				for f := 0; f < len(arg[1:]); f++ {
					sarg := arg[1:]
					ActualFlags = append(ActualFlags,
						getopt.CommandLine.Lookup(string(sarg[f])))
				}
				continue
			} else if arg[1] == '-' && len(arg) > 2 { /* GNU-style long options. */
				ActualFlags = append(ActualFlags,
					getopt.CommandLine.Lookup(arg[2:]))
			}
		default:
			continue
		}
	}
}

// setByUser checks whether a specific flag was explicitly set by the user
func setByUser(name string) bool {
	for _, f := range ActualFlags {
		if f.Name == name {
			return true
		}
	}
	return false
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
	if inFilePath == "-" { // read from stdin
		if *stdout != true {
			return fmt.Errorf("reading from stdin, can write only to stdout")
		}
		if setByUser("S") == true {
			return fmt.Errorf("reading from stdin, suffix not needed")
		}
		stdin = true
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
			if *decompress {
				outFileDir, outFileName := path.Split(inFilePath)
				if strings.HasSuffix(outFileName, "."+*suffix) {
					if len(outFileName) > len("."+*suffix) {
						nstr := strings.SplitN(outFileName, ".", len(outFileName))
						estr := strings.Join(nstr[0:len(nstr)-1], ".")
						outFilePath = outFileDir + estr
					} else {
						return fmt.Errorf("can't strip suffix .%s from file %s", *suffix, inFilePath)
					}
				} else {
					return fmt.Errorf("file %s doesn't have suffix .%s", inFilePath, *suffix)
				}
			} else {
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

			if *verbose {
				fmt.Fprintf(os.Stderr, "%s: ", inFile.Name())
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
			fmt.Fprintln(os.Stderr, "done")
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

			if *verbose {
				fmt.Fprintf(os.Stderr, "%s: ", inFile.Name())
			}

			_, err = io.Copy(z, inFile)
			if err != nil {
				pw.CloseWithError(err)
				return
			}

			if *verbose {
				compratio := (float64(z.InputOffset) / float64(z.OutputOffset))
				fmt.Fprintf(os.Stderr, "%6.3f:1, %6.3f bits/byte, %5.2f%% saved, %d in, %d out.\n",
					compratio, ((1 / compratio) * 8),
					(100 * (1 - (1 / compratio))),
					z.InputOffset, z.OutputOffset)
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
		"h", "help",
	)

	// Parse command-line flags
	getopt.Parse()

	// Workaround for https://github.com/rsc/getopt/issues/2.
	parseActualFlags(os.Args[1:])

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

	// From 'go doc runtime.GOMAXPROCS':
	// "It defaults to the value of runtime.NumCPU.
	// If n < 1, it does not change the current setting."
	// In fact, if the default value of cores is zero, it
	// will use all the cores of the machine.
	runtime.GOMAXPROCS(*cores)

	// Get list of files to process
	files := flag.Args()
	if len(files) == 0 {
		files = []string{"-"} // default to stdin
	}

	// Process each file
	hasErrors := false
	for _, file := range files {
		if file == "-" {
			err := processFile(file)
			if err != nil {
				log.Printf("%s: %v", file, err)
				hasErrors = true
			}
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			log.Printf("%s: %v", file, err)
			hasErrors = true
			continue
		}

		if info.IsDir() {
			if *recursive {
				err = filepath.Walk(file, func(path string, fi os.FileInfo, err error) error {
					if err != nil {
						log.Printf("%s: %v", path, err)
						hasErrors = true
						return nil
					}
					if !fi.IsDir() {
						if err := processFile(path); err != nil {
							log.Printf("%s: %v", path, err)
							hasErrors = true
						}
					}
					return nil
				})
				if err != nil {
					log.Printf("%s: %v", file, err)
					hasErrors = true
				}
			} else {
				log.Printf("%s is a directory (use -r to process recursively)", file)
				hasErrors = true
			}
		} else {
			err := processFile(file)
			if err != nil {
				log.Printf("%s: %v", file, err)
				hasErrors = true
			}
		}
	}

	// Exit with error code if any failures occurred
	if hasErrors {
		os.Exit(1)
	}
}
