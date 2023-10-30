// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package asm

import (
	"bufio"
	"bytes"
	"fmt"
	"internal/buildcfg"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"cmd/asm/internal/lex"
	"cmd/internal/obj"
)

// An end-to-end test for the assembler: Do we print what we parse?
// Output is generated by, in effect, turning on -S and comparing the
// result against a golden file.

func testEndToEnd(t *testing.T, goarch, file string) {
	input := filepath.Join("testdata", file+".s")
	architecture, ctxt := setArch(goarch)
	architecture.Init(ctxt)
	lexer := lex.NewLexer(input)
	parser := NewParser(ctxt, architecture, lexer)
	pList := new(obj.Plist)
	var ok bool
	testOut = new(strings.Builder) // The assembler writes test output to this buffer.
	ctxt.Bso = bufio.NewWriter(os.Stdout)
	ctxt.IsAsm = true
	defer ctxt.Bso.Flush()
	failed := false
	ctxt.DiagFunc = func(format string, args ...interface{}) {
		failed = true
		t.Errorf(format, args...)
	}
	pList.Firstpc, ok = parser.Parse()
	if !ok || failed {
		t.Errorf("asm: %s assembly failed", goarch)
		return
	}
	output := strings.Split(testOut.String(), "\n")

	// Reconstruct expected output by independently "parsing" the input.
	data, err := os.ReadFile(input)
	if err != nil {
		t.Error(err)
		return
	}
	lineno := 0
	seq := 0
	hexByLine := map[string]string{}
	lines := strings.SplitAfter(string(data), "\n")
Diff:
	for _, line := range lines {
		lineno++

		// Ignore include of textflag.h.
		if strings.HasPrefix(line, "#include ") {
			continue
		}

		// Ignore GLOBL.
		if strings.HasPrefix(line, "GLOBL ") {
			continue
		}

		// The general form of a test input line is:
		//	// comment
		//	INST args [// printed form] [// hex encoding]
		parts := strings.Split(line, "//")
		printed := strings.TrimSpace(parts[0])
		if printed == "" || strings.HasSuffix(printed, ":") { // empty or label
			continue
		}
		seq++

		var hexes string
		switch len(parts) {
		default:
			t.Errorf("%s:%d: unable to understand comments: %s", input, lineno, line)
		case 1:
			// no comment
		case 2:
			// might be printed form or hex
			note := strings.TrimSpace(parts[1])
			if isHexes(note) {
				hexes = note
			} else {
				printed = note
			}
		case 3:
			// printed form, then hex
			printed = strings.TrimSpace(parts[1])
			hexes = strings.TrimSpace(parts[2])
			if !isHexes(hexes) {
				t.Errorf("%s:%d: malformed hex instruction encoding: %s", input, lineno, line)
			}
		}

		if hexes != "" {
			hexByLine[fmt.Sprintf("%s:%d", input, lineno)] = hexes
		}

		// Canonicalize spacing in printed form.
		// First field is opcode, then tab, then arguments separated by spaces.
		// Canonicalize spaces after commas first.
		// Comma to separate argument gets a space; comma within does not.
		var buf []byte
		nest := 0
		for i := 0; i < len(printed); i++ {
			c := printed[i]
			switch c {
			case '{', '[':
				nest++
			case '}', ']':
				nest--
			case ',':
				buf = append(buf, ',')
				if nest == 0 {
					buf = append(buf, ' ')
				}
				for i+1 < len(printed) && (printed[i+1] == ' ' || printed[i+1] == '\t') {
					i++
				}
				continue
			}
			buf = append(buf, c)
		}

		f := strings.Fields(string(buf))

		// Turn relative (PC) into absolute (PC) automatically,
		// so that most branch instructions don't need comments
		// giving the absolute form.
		if len(f) > 0 && strings.Contains(printed, "(PC)") {
			index := len(f) - 1
			suf := "(PC)"
			for !strings.HasSuffix(f[index], suf) {
				index--
				suf = "(PC),"
			}
			str := f[index]
			n, err := strconv.Atoi(str[:len(str)-len(suf)])
			if err == nil {
				f[index] = fmt.Sprintf("%d%s", seq+n, suf)
			}
		}

		if len(f) == 1 {
			printed = f[0]
		} else {
			printed = f[0] + "\t" + strings.Join(f[1:], " ")
		}

		want := fmt.Sprintf("%05d (%s:%d)\t%s", seq, input, lineno, printed)
		for len(output) > 0 && (output[0] < want || output[0] != want && len(output[0]) >= 5 && output[0][:5] == want[:5]) {
			if len(output[0]) >= 5 && output[0][:5] == want[:5] {
				t.Errorf("mismatched output:\nhave %s\nwant %s", output[0], want)
				output = output[1:]
				continue Diff
			}
			t.Errorf("unexpected output: %q", output[0])
			output = output[1:]
		}
		if len(output) > 0 && output[0] == want {
			output = output[1:]
		} else {
			t.Errorf("missing output: %q", want)
		}
	}
	for len(output) > 0 {
		if output[0] == "" {
			// spurious blank caused by Split on "\n"
			output = output[1:]
			continue
		}
		t.Errorf("unexpected output: %q", output[0])
		output = output[1:]
	}

	// Checked printing.
	// Now check machine code layout.

	top := pList.Firstpc
	var text *obj.LSym
	ok = true
	ctxt.DiagFunc = func(format string, args ...interface{}) {
		t.Errorf(format, args...)
		ok = false
	}
	obj.Flushplist(ctxt, pList, nil)

	for p := top; p != nil; p = p.Link {
		if p.As == obj.ATEXT {
			text = p.From.Sym
		}
		hexes := hexByLine[p.Line()]
		if hexes == "" {
			continue
		}
		delete(hexByLine, p.Line())
		if text == nil {
			t.Errorf("%s: instruction outside TEXT", p)
		}
		size := int64(len(text.P)) - p.Pc
		if p.Link != nil {
			size = p.Link.Pc - p.Pc
		} else if p.Isize != 0 {
			size = int64(p.Isize)
		}
		var code []byte
		if p.Pc < int64(len(text.P)) {
			code = text.P[p.Pc:]
			if size < int64(len(code)) {
				code = code[:size]
			}
		}
		codeHex := fmt.Sprintf("%x", code)
		if codeHex == "" {
			codeHex = "empty"
		}
		ok := false
		for _, hex := range strings.Split(hexes, " or ") {
			if codeHex == hex {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("%s: have encoding %s, want %s", p, codeHex, hexes)
		}
	}

	if len(hexByLine) > 0 {
		var missing []string
		for key := range hexByLine {
			missing = append(missing, key)
		}
		sort.Strings(missing)
		for _, line := range missing {
			t.Errorf("%s: did not find instruction encoding", line)
		}
	}

}

func isHexes(s string) bool {
	if s == "" {
		return false
	}
	if s == "empty" {
		return true
	}
	for _, f := range strings.Split(s, " or ") {
		if f == "" || len(f)%2 != 0 || strings.TrimLeft(f, "0123456789abcdef") != "" {
			return false
		}
	}
	return true
}

// It would be nice if the error messages always began with
// the standard file:line: prefix,
// but that's not where we are today.
// It might be at the beginning but it might be in the middle of the printed instruction.
var fileLineRE = regexp.MustCompile(`(?:^|\()(testdata[/\\][\da-z]+\.s:\d+)(?:$|\)|:)`)

// Same as in test/run.go
var (
	errRE       = regexp.MustCompile(`// ERROR ?(.*)`)
	errQuotesRE = regexp.MustCompile(`"([^"]*)"`)
)

func testErrors(t *testing.T, goarch, file string, flags ...string) {
	input := filepath.Join("testdata", file+".s")
	architecture, ctxt := setArch(goarch)
	architecture.Init(ctxt)
	lexer := lex.NewLexer(input)
	parser := NewParser(ctxt, architecture, lexer)
	pList := new(obj.Plist)
	var ok bool
	ctxt.Bso = bufio.NewWriter(os.Stdout)
	ctxt.IsAsm = true
	defer ctxt.Bso.Flush()
	failed := false
	var errBuf bytes.Buffer
	parser.errorWriter = &errBuf
	ctxt.DiagFunc = func(format string, args ...interface{}) {
		failed = true
		s := fmt.Sprintf(format, args...)
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		errBuf.WriteString(s)
	}
	for _, flag := range flags {
		switch flag {
		case "dynlink":
			ctxt.Flag_dynlink = true
		default:
			t.Errorf("unknown flag %s", flag)
		}
	}
	pList.Firstpc, ok = parser.Parse()
	obj.Flushplist(ctxt, pList, nil)
	if ok && !failed {
		t.Errorf("asm: %s had no errors", file)
	}

	errors := map[string]string{}
	for _, line := range strings.Split(errBuf.String(), "\n") {
		if line == "" || strings.HasPrefix(line, "\t") {
			continue
		}
		m := fileLineRE.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("unexpected error: %v", line)
			continue
		}
		fileline := m[1]
		if errors[fileline] != "" && errors[fileline] != line {
			t.Errorf("multiple errors on %s:\n\t%s\n\t%s", fileline, errors[fileline], line)
			continue
		}
		errors[fileline] = line
	}

	// Reconstruct expected errors by independently "parsing" the input.
	data, err := os.ReadFile(input)
	if err != nil {
		t.Error(err)
		return
	}
	lineno := 0
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		lineno++

		fileline := fmt.Sprintf("%s:%d", input, lineno)
		if m := errRE.FindStringSubmatch(line); m != nil {
			all := m[1]
			mm := errQuotesRE.FindAllStringSubmatch(all, -1)
			if len(mm) != 1 {
				t.Errorf("%s: invalid errorcheck line:\n%s", fileline, line)
			} else if err := errors[fileline]; err == "" {
				t.Errorf("%s: missing error, want %s", fileline, all)
			} else if !strings.Contains(err, mm[0][1]) {
				t.Errorf("%s: wrong error for %s:\n%s", fileline, all, err)
			}
		} else {
			if errors[fileline] != "" {
				t.Errorf("unexpected error on %s: %v", fileline, errors[fileline])
			}
		}
		delete(errors, fileline)
	}
	var extra []string
	for key := range errors {
		extra = append(extra, key)
	}
	sort.Strings(extra)
	for _, fileline := range extra {
		t.Errorf("unexpected error on %s: %v", fileline, errors[fileline])
	}
}

func Test386EndToEnd(t *testing.T) {
	testEndToEnd(t, "386", "386")
}

func TestARMEndToEnd(t *testing.T) {
	defer func(old int) { buildcfg.GOARM.Version = old }(buildcfg.GOARM.Version)
	for _, goarm := range []int{5, 6, 7} {
		t.Logf("GOARM=%d", goarm)
		buildcfg.GOARM.Version = goarm
		testEndToEnd(t, "arm", "arm")
		if goarm == 6 {
			testEndToEnd(t, "arm", "armv6")
		}
	}
}

func TestGoBuildErrors(t *testing.T) {
	testErrors(t, "amd64", "buildtagerror")
}

func TestGenericErrors(t *testing.T) {
	testErrors(t, "amd64", "duperror")
}

func TestARMErrors(t *testing.T) {
	testErrors(t, "arm", "armerror")
}

func TestARM64EndToEnd(t *testing.T) {
	testEndToEnd(t, "arm64", "arm64")
}

func TestARM64Encoder(t *testing.T) {
	testEndToEnd(t, "arm64", "arm64enc")
}

func TestARM64Errors(t *testing.T) {
	testErrors(t, "arm64", "arm64error")
}

func TestAMD64EndToEnd(t *testing.T) {
	testEndToEnd(t, "amd64", "amd64")
}

func Test386Encoder(t *testing.T) {
	testEndToEnd(t, "386", "386enc")
}

func TestAMD64Encoder(t *testing.T) {
	filenames := [...]string{
		"amd64enc",
		"amd64enc_extra",
		"avx512enc/aes_avx512f",
		"avx512enc/gfni_avx512f",
		"avx512enc/vpclmulqdq_avx512f",
		"avx512enc/avx512bw",
		"avx512enc/avx512cd",
		"avx512enc/avx512dq",
		"avx512enc/avx512er",
		"avx512enc/avx512f",
		"avx512enc/avx512pf",
		"avx512enc/avx512_4fmaps",
		"avx512enc/avx512_4vnniw",
		"avx512enc/avx512_bitalg",
		"avx512enc/avx512_ifma",
		"avx512enc/avx512_vbmi",
		"avx512enc/avx512_vbmi2",
		"avx512enc/avx512_vnni",
		"avx512enc/avx512_vpopcntdq",
	}
	for _, name := range filenames {
		testEndToEnd(t, "amd64", name)
	}
}

func TestAMD64Errors(t *testing.T) {
	testErrors(t, "amd64", "amd64error")
}

func TestAMD64DynLinkErrors(t *testing.T) {
	testErrors(t, "amd64", "amd64dynlinkerror", "dynlink")
}

func TestMIPSEndToEnd(t *testing.T) {
	testEndToEnd(t, "mips", "mips")
	testEndToEnd(t, "mips64", "mips64")
}

func TestLOONG64Encoder(t *testing.T) {
	testEndToEnd(t, "loong64", "loong64enc1")
	testEndToEnd(t, "loong64", "loong64enc2")
	testEndToEnd(t, "loong64", "loong64enc3")
	testEndToEnd(t, "loong64", "loong64")
}

func TestPPC64EndToEnd(t *testing.T) {
	defer func(old int) { buildcfg.GOPPC64 = old }(buildcfg.GOPPC64)
	for _, goppc64 := range []int{8, 9, 10} {
		t.Logf("GOPPC64=power%d", goppc64)
		buildcfg.GOPPC64 = goppc64
		// Some pseudo-ops may assemble differently depending on GOPPC64
		testEndToEnd(t, "ppc64", "ppc64")
		testEndToEnd(t, "ppc64", "ppc64_p10")
	}
}

func TestRISCVEndToEnd(t *testing.T) {
	buildcfg.GORISCV64.FeatureRVB = false
	testEndToEnd(t, "riscv64", "riscv64")
}

func TestRISCVErrors(t *testing.T) {
	buildcfg.GORISCV64.FeatureRVB = false
	testErrors(t, "riscv64", "riscv64error")
}

func TestRISCVRVBEndToEnd(t *testing.T) {
	buildcfg.GORISCV64.FeatureRVB = true
	testEndToEnd(t, "riscv64", "riscv64rvb")
}

// func TestRISCVRVBErrors(t *testing.T) {
// 	buildcfg.GORISCV64.FeatureRVB = true
// 	testErrors(t, "riscv64", "riscv64rvberror")
// }

func TestS390XEndToEnd(t *testing.T) {
	testEndToEnd(t, "s390x", "s390x")
}
