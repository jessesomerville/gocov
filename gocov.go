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
		os.Exit(2)
	}

	if err := displayCoverage(profiles); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

var (
	red    = rgb(48, 26, 31)
	green  = rgb(18, 38, 30)
	yellow = rgb(204, 136, 26)
	dark   = rgb(13, 17, 23)
)

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
		fmt.Printf("\n%s\n", yellow.Fg([]byte(fn)))

		colorlines(src, profile.Blocks)
	}
	return nil
}

func colorlines(src []byte, blocks []cover.ProfileBlock) {
	// Replace tabs with two spaces.
	src = bytes.ReplaceAll(src, []byte{9}, []byte("  "))
	prevEnd := 0
	var curr []byte
	for _, block := range blocks {
		curr, src = cutAfterIndexN(src, '\n', block.StartLine-prevEnd)
		fmt.Printf("%s", curr) // Uninstrumented lines.
		curr, src = cutAfterIndexN(src, '\n', block.EndLine-block.StartLine)
		if block.Count == 0 {
			fmt.Printf(red.Bg(curr))
		} else {
			fmt.Printf(green.Bg(curr))
		}
		prevEnd = block.EndLine
	}
	if len(src) != 0 {
		fmt.Printf("%s%s\n", reset, src)
	}
}

func cutAfterIndexN(s []byte, sep byte, n int) (before, after []byte) {
	if n <= 0 {
		return s, nil
	}
	i := 0
	for i < len(s)-1 {
		if s[i] == sep {
			n--
			if n == 0 {
				break
			}
		}
		i++
	}
	i++
	return s[:i], s[i:]
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

const reset = "\x1b[0m"

type trueColor struct {
	R, G, B uint8
}

func rgb(r, g, b uint8) trueColor {
	return trueColor{r, g, b}
}

func (tc trueColor) String() string {
	return fmt.Sprintf("rgb(%d, %d, %d)", tc.R, tc.G, tc.B)
}

func (tc trueColor) Bg(msg []byte) string {
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm%s%s", tc.R, tc.G, tc.B, msg, reset)
}

func (tc trueColor) Fg(msg []byte) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s%s", tc.R, tc.G, tc.B, msg, reset)
}
