package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alecthomas/chroma/quick"
	"golang.org/x/tools/cover"
)

const usageMessage = "" +
	`Display Go test coverage

Given a coverage profile produced by 'go test':
	go test -coverprofile=c.out

Provide coverage profile with a flag:
	gocov c.out

Provide coverage profile on standard input:
	cat c.out | gocov
`

func usage() {
	fmt.Fprint(os.Stderr, usageMessage)
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	var rd io.Reader
	if flag.NArg() == 0 {
		rd = os.Stdin
	} else {
		infile := flag.Arg(0)
		f, err := os.Open(infile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open %q: %v\n", infile, err)
			os.Exit(2)
		}
		rd = f
	}

	profiles, err := cover.ParseProfilesFromReader(rd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	if err := displayCoverage(profiles); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func displayCoverage(profiles []*cover.Profile) error {
	dirs, err := findPkgs(profiles)
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		fn := profile.FileName
		file, err := findFile(dirs, fn)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("can't read %q: %v", fn, err)
		}
		border := strings.Repeat("-", len(fn))
		fmt.Printf("\n%s\n%s\n%s\n\n", border, fn, border)

		b := new(strings.Builder)
		quick.Highlight(b, string(src), "go", "terminal16", "base16-snazzy")
		colorlines(string(src), b.String(), profile.Blocks)
	}
	return nil
}

const (
	fg      = "\x1b[97m"
	redbg   = "\x1b[48;2;64;4;8m"
	greenbg = "\x1b[48;2;10;64;4m"
	reset   = "\x1b[0m"
)

func colorlines(src, hlsrc string, blocks []cover.ProfileBlock) {
	lines := strings.Split(src, "\n")
	hllines := strings.Split(hlsrc, "\n")
	outlines := make([]string, len(lines))
	for _, b := range blocks {
		c := redbg
		if b.Count > 0 {
			c = greenbg
		}
		for i := b.StartLine; i < b.EndLine-1; i++ {
			outlines[i] = fmt.Sprintf("%s%s%s%s", c, fg, lines[i], reset)
		}
	}
	for i := range outlines {
		if outlines[i] != "" {
			fmt.Println(outlines[i])
		} else {
			fmt.Println(hllines[i])
		}
	}
}

// Pkg describes a single package, compatible with JSON output from 'go list'.
type Pkg struct {
	ImportPath string
	Dir        string
	Error      *struct {
		Err string
	}
}

func findPkgs(profiles []*cover.Profile) (map[string]*Pkg, error) {
	// Run go list to find the location of every package we care about.
	pkgs := make(map[string]*Pkg)
	var list []string
	for _, profile := range profiles {
		if strings.HasPrefix(profile.FileName, ".") || filepath.IsAbs(profile.FileName) {
			// Relative or absolute path.
			continue
		}
		pkg := path.Dir(profile.FileName)
		if _, ok := pkgs[pkg]; !ok {
			pkgs[pkg] = nil
			list = append(list, pkg)
		}
	}

	if len(list) == 0 {
		return pkgs, nil
	}

	// Note: usually run as "go tool cover" in which case $GOROOT is set,
	// in which case runtime.GOROOT() does exactly what we want.
	goTool := filepath.Join(runtime.GOROOT(), "bin/go")
	cmd := exec.Command(goTool, append([]string{"list", "-e", "-json"}, list...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cannot run go list: %v\n%s", err, stderr.Bytes())
	}
	dec := json.NewDecoder(bytes.NewReader(stdout))
	for {
		var pkg Pkg
		err := dec.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decoding go list json: %v", err)
		}
		pkgs[pkg.ImportPath] = &pkg
	}
	return pkgs, nil
}

func findFile(pkgs map[string]*Pkg, file string) (string, error) {
	if strings.HasPrefix(file, ".") || filepath.IsAbs(file) {
		return file, nil
	}
	pkg := pkgs[path.Dir(file)]
	if pkg != nil {
		if pkg.Dir != "" {
			return filepath.Join(pkg.Dir, path.Base(file)), nil
		}
		if pkg.Error != nil {
			return "", errors.New(pkg.Error.Err)
		}
	}
	return "", fmt.Errorf("did not find package for %s in go list output", file)
}
