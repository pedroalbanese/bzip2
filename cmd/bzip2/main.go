// Copyright (c) 2010, Andrei Vieru. All rights reserved.
// Copyright (c) 2021, Pedro Albanese. All rights reserved.
// Copyright (c) 2025: Pindorama
//			Luiz Antônio Rangel (takusuman)
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
	"runtime"
	"strconv"
	"strings"

	"github.com/dsnet/compress/bzip2"
	"rsc.io/getopt"
)

var (
	stdout     = flag.Bool("c", false, "write on standard output, keep original files unchanged")
	decompress = flag.Bool("d", false, "decompress; see also -c and -k")
	force      = flag.Bool("f", false, "force overwrite of output file")
	help       = flag.Bool("h", false, "print this help message")
	keep       = flag.Bool("k", false, "keep original files unchaned")
	suffix     = flag.String("s", "bz2", "use provided suffix on compressed files")
	cores      = flag.Int("cores", 0, "number of cores to use for parallelization")
	test       = flag.Bool("t", false, "test compressed file integrity")
	compress   = flag.Bool("z", true, "compress file(s)")
	level      = flag.Int("l", 9, "compression level (1 = fastest, 9 = best)")

	stdin bool
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... [FILE]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Compress or uncompress FILE (by default, compress FILE in-place).\n\n")
	getopt.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nWith no FILE, or when FILE is -, read standard input.\n")
}

func exit(msg string) {
	usage()
	fmt.Fprintln(os.Stderr)
	log.Fatalf("%s: check args: %s\n\n", os.Args[0], msg)
}

func setByUser(name string) (isSet bool) {
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			isSet = true
		}
	})
	return
}

func main() {
	// Levels.
	// This is terrible. Don't blame it on me,
	// blame it on the flag package designers.
	// Yeah, this sort of spams usage().
	// Perhaps Pedro would like to review it further.
	for i := 1; i <= 9; i++ {
		explanation := fmt.Sprintf("set block size to %dk", (i * 100))
		if i == 9 {
			explanation += " (default)"
		}
		_ = flag.Bool(strconv.Itoa(i), false, explanation)
	}
	// Alias short flags with their long counterparts.
	getopt.Aliases(
		"1", "fast",
		"9", "best",
		"c", "stdout",
		"d", "decompress",
		"f", "force",
		"k", "keep",
		"t", "test",
		"z", "compress",
		"h", "help",
	)
	// Do the Bossa Nova --- I mean, parsing.
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

	if *level < 1 || *level > 9 {
		exit("invalid compression level: must be between 1 and 9")
	}

	if *help == true {
		usage()
		log.Fatal(0)
	}

	// Initial checks for whether conditions this program is being run.
	//if *stdout == true && *suffix != "bz2" {
	if *stdout == true && setByUser("s") == true {
		exit("stdout set, suffix not used")
	}
	if *stdout == true && *force == true {
		exit("stdout set, force not used")
	}
	if *stdout == true && *keep == true {
		exit("stdout set, keep is redundant")
	}
	if flag.NArg() > 1 {
		exit("too many file, provide at most one file at a time or check order of flags")
	}
	if setByUser("cores") && (*cores < 1 || *cores > 32) {
		exit("invalid number of cores")
	}

	// From 'go doc runtime.GOMAXPROCS':
	// "It defaults to the value of runtime.NumCPU.
	// If n < 1, it does not change the current setting."
	// In fact, if the default value of cores is zero, it
	// will use all the cores of the machine.
	runtime.GOMAXPROCS(*cores)

	var inFilePath string
	var outFilePath string

	// Code for testing the given file.
	if *test {
		if flag.NArg() == 1 {
			inFilePath = flag.Args()[0]
		}
		var inFile *os.File
		var err error
		if stdin {
			inFile = os.Stdin
		} else {
			inFile, err = os.Open(inFilePath)
			if err != nil {
				log.Fatal(err)
			}
			defer inFile.Close()
		}

		z, err := bzip2.NewReader(inFile, nil)
		if err != nil {
			log.Fatalf("corrupted file or format error: %v", err)
		}
		defer z.Close()

		_, err = io.Copy(io.Discard, z)
		if err != nil {
			log.Fatalf("test failed: %v", err)
		}

		fmt.Printf("%s: OK\n", inFilePath)
		return
	}

	if flag.NArg() == 0 || flag.NArg() == 1 && flag.Args()[0] == "-" { // parse args: read from stdin
		if *stdout != true {
			exit("reading from stdin, can write only to stdout")
		}
		//if *suffix != "bzip2" {
		if setByUser("s") == true {
			exit("reading from stdin, suffix not needed")
		}
		stdin = true
	} else if flag.NArg() == 1 { // parse args: read from file
		inFilePath = flag.Args()[0]
		f, err := os.Lstat(inFilePath)
		if err != nil {
			log.Fatal(err.Error())
		}
		if f == nil {
			exit(fmt.Sprintf("file %s not found", inFilePath))
		}
		if !!f.IsDir() {
			exit(fmt.Sprintf("%s is not a regular file", inFilePath))
		}

		if *stdout == false { // parse args: write to file
			if *suffix == "" {
				exit("suffix can't be an empty string")
			}

			if *decompress == true {
				outFileDir, outFileName := path.Split(inFilePath)
				if strings.HasSuffix(outFileName, "."+*suffix) {
					if len(outFileName) > len("."+*suffix) {
						nstr := strings.SplitN(outFileName, ".", len(outFileName))
						estr := strings.Join(nstr[0:len(nstr)-1], ".")
						outFilePath = outFileDir + estr
					} else {
						log.Fatalf("error: can't strip suffix .%s from file %s", *suffix, inFilePath)
					}
				} else {
					exit(fmt.Sprintf("file %s doesn't have suffix .%s", inFilePath, *suffix))
				}

			} else {
				outFilePath = inFilePath + "." + *suffix
			}

			f, err = os.Lstat(outFilePath)
			if err != nil && f != nil {
				// should be:
				// 	if err != nil && err != "file not found"
				// but i can't find the error's id
				//
				// taks quest.: Perhaps errors.Is()? If it
				// doesn't return a "not found" error, it is
				// the library's fault.
				log.Fatal(err.Error())
			}
			if f != nil && !f.IsDir() {
				if *force == true {
					err = os.Remove(outFilePath)
					if err != nil {
						log.Fatal(err.Error())
					}
				} else {
					exit(fmt.Sprintf("outFile %s exists. use force to overwrite", outFilePath))
				}
			} else if f != nil {
				exit(fmt.Sprintf("outFile %s exists and is not a regular file", outFilePath))
			}
		}
	}

	pr, pw := io.Pipe()
	//defer pr.Close()
	//defer pw.Close()

	if *decompress {
		// read from inFile into pw
		go func() {
			defer pw.Close()
			var inFile *os.File
			var err error
			if stdin == true {
				inFile = os.Stdin
			} else {
				inFile, err = os.Open(inFilePath)
			}
			defer inFile.Close()
			if err != nil {
				log.Fatal(err.Error())
			}

			_, err = io.Copy(pw, inFile)
			if err != nil {
				log.Fatal(err.Error())
			}

		}()

		// write into outFile from z
		defer pr.Close()
		z, _ := bzip2.NewReader(pr, nil)
		defer z.Close()
		var outFile *os.File
		var err error
		if *stdout == true {
			outFile = os.Stdout
		} else {
			outFile, err = os.Create(outFilePath)
		}
		defer outFile.Close()
		if err != nil {
			log.Fatal(err.Error())
		}

		_, err = io.Copy(outFile, z)
		if err != nil {
			log.Fatal(err.Error())
		}

	} else if *compress { // The default comportment.
		// read from inFile into z
		go func() {
			defer pw.Close()
			var z io.WriteCloser
			var inFile *os.File
			var err error
			if stdin == true {
				inFile = os.Stdin
				defer inFile.Close()
				z, _ = bzip2.NewWriter(pw, &bzip2.WriterConfig{Level: *level})
				defer z.Close()
			} else {
				inFile, err = os.Open(inFilePath)
				defer inFile.Close()
				if err != nil {
					log.Fatal(err.Error())
				}
				z, _ = bzip2.NewWriter(pw, &bzip2.WriterConfig{Level: *level})
				defer z.Close()
			}

			_, err = io.Copy(z, inFile)
			if err != nil {
				log.Fatal(err.Error())
			}
		}()

		// write into outFile from pr
		defer pr.Close()
		var outFile *os.File
		var err error
		if *stdout == true {
			outFile = os.Stdout
		} else {
			outFile, err = os.Create(outFilePath)
		}
		defer outFile.Close()
		if err != nil {
			log.Fatal(err.Error())
		}

		_, err = io.Copy(outFile, pr)
		if err != nil {
			log.Fatal(err.Error())
		}
	}

	if *stdout == false && *keep == false {
		err := os.Remove(inFilePath)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
}
