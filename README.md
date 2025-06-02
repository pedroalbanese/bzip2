# bzip2(2)
[![ISC License](http://img.shields.io/badge/license-ISC-blue.svg)](https://github.com/pedroalbanese/bzip2/blob/master/LICENSE.md)
[![GitHub downloads](https://img.shields.io/github/downloads/pedroalbanese/bzip2/total.svg?logo=github&logoColor=white)](https://github.com/pedroalbanese/bzip2/releases)
[![GoDoc](https://godoc.org/github.com/pedroalbanese/bzip2?status.png)](http://godoc.org/github.com/pedroalbanese/bzip2)
[![Go Report Card](https://goreportcard.com/badge/github.com/pedroalbanese/bzip2)](https://goreportcard.com/report/github.com/pedroalbanese/bzip2)
[![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/pedroalbanese/bzip2)](https://golang.org)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/pedroalbanese/bzip2)](https://github.com/pedroalbanese/bzip2/releases)
### Command:
<pre>Usage: bzip2 [OPTION]... [FILE]
Compress or uncompress FILE (by default, compress FILE in-place).

  -1    set block size to 100k
  [...]
  -9    set block size to 900k (default)
  -c    write on standard output, keep original files unchanged
  -cores int
        number of cores to use for parallelization
  -d    decompress; see also -c and -k
  -f    force overwrite of output file
  -h    print this help message
  -k    keep original files unchaned
  -l int
        compression level (1 = fastest, 9 = best) (default 9)
  -s string
        use provided suffix on compressed files (default "bz2")
  -t    test compressed file integrity
  -z    compress file(s) (default true)

With no FILE, or when FILE is -, read standard input.</pre>

## License

This project is licensed under the ISC License.

##### Copyright (c) 2020-2025 ALBANESE Research Lab & Projeto Pindorama.
