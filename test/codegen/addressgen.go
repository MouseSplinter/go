// asmcheck

// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen

func getIdxI64U32(arr []int64, base uint32, offset int64) int64 {
	// riscv64/rva22u64: `SH3ADDUW\t`
	return arr[base+uint32(offset)]
}

func getIdxI32U32(arr []int32, base uint32, offset int64) int32 {
	// riscv64/rva22u64: `SH2ADDUW\t`
	return arr[base+uint32(offset)]
}

func getIdxI16U32(arr []int16, base uint32, offset int64) int16 {
	// riscv64/rva22u64: `SH1ADDUW\t`
	return arr[base+uint32(offset)]
}

func getIdxI8U32(arr []int8, base uint32, offset int64) int8 {
	// riscv64/rva22u64: `ADDUW\t`
	return arr[base+uint32(offset)]
}

func getIdxI32(idx int) uint64 {
	// riscv64/rva22u64: `SLLIUW\t[$]6`
	return uint64(uint32(idx)) << 6
}

func getIdxI64I32(arr []int64, base int32, offset int32) int64 {
	// riscv64/rva22u64: `SH3ADD\t`
	return arr[base+offset]
}

func getIdxI32I32(arr []int32, base int32, offset int32) int32 {
	// riscv64/rva22u64: `SH2ADD\t`
	return arr[base+offset]
}

func getIdxI16I32(arr []int16, base int32, offset int32) int16 {
	// riscv64/rva22u64: `SH1ADD\t`
	return arr[base+offset]
}
