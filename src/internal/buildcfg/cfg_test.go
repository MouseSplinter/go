// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildcfg

import (
	"os"
	"testing"
)

func TestConfigFlags(t *testing.T) {
	os.Setenv("GOAMD64", "v1")
	if goamd64() != 1 {
		t.Errorf("Wrong parsing of GOAMD64=v1")
	}
	os.Setenv("GOAMD64", "v4")
	if goamd64() != 4 {
		t.Errorf("Wrong parsing of GOAMD64=v4")
	}
	Error = nil
	os.Setenv("GOAMD64", "1")
	if goamd64(); Error == nil {
		t.Errorf("Wrong parsing of GOAMD64=1")
	}
}

func TestRISCVConfigFlags(t *testing.T) {
	os.Setenv("GORISCV", "generic")
	if goriscv64().String() != "c" {
		t.Errorf("Wrong parsing of GORISCV=generic")
	}
	os.Setenv("GORISCV", "")
	if goriscv64().String() != "c" {
		t.Errorf("Wrong parsing of no GORISCV")
	}
	os.Setenv("GORISCV", "c")
	if goriscv64().String() != "c" {
		t.Errorf("Wrong parsing of GORISCV=c")
	}
}
