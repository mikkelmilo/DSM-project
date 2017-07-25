package Benchmarks

import (
	"DSM-project/memory"
	"DSM-project/treadmarks"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

func BenchmarkTreadMarks(b *testing.B) {
	// run the Fib function b.N times
	for n := 0; n < b.N; n++ {

	}
}

func TestJacobiProgram(t *testing.T) {
	//log.SetOutput(ioutil.Discard)
	group := sync.WaitGroup{}
	group.Add(2)
	go JacobiProgramTreadMarks(4, 2, true, group)
	go func() {
		time.Sleep(time.Millisecond * 200)
		JacobiProgramTreadMarks(4, 2, false, group)
	}()
	group.Wait()
}

func setupTreadMarksStruct(nrProcs, memsize, pagebytesize, nrlocks, nrbarriers int) *treadmarks.TreadMarks {
	vm1 := memory.NewVmem(memsize, pagebytesize)
	tm1 := treadmarks.NewTreadMarks(vm1, nrProcs, nrlocks, nrbarriers)
	return tm1
}

func JacobiProgramTreadMarks(nrIterations int, nrProcs int, isManager bool, group sync.WaitGroup) {
	const M = 256
	const N = 256
	const float32_BYTE_LENGTH = 4 //32 bits
	var privateArray [][]float32  //privateArray[M][N]
	privateArray = make([][]float32, M)
	for i := range privateArray {
		privateArray[i] = make([]float32, N)
	}
	gridEntryToAddr := func(m, n int) int {
		if m >= M || n >= N || m < 0 || n < 0 {
			//panic(fmt.Errorf("index out of bounds in scratch array:%v, %v", m, n))
			return -1
		}
		return (m * N * float32_BYTE_LENGTH) + (n * float32_BYTE_LENGTH)
	}

	tm := setupTreadMarksStruct(nrProcs, M*N*float32_BYTE_LENGTH, 4096, 1, 3)
	if isManager {
		tm.Startup()
	} else {
		tm.Join("localhost:2000")
		fmt.Println("joined with id:", tm.ProcId)
	}

	length := M / nrProcs
	begin := length * int(tm.ProcId-1)
	end := length * int(tm.ProcId)
	fmt.Println("begin, end:", begin, end)

	for iter := 0; iter <= nrIterations; iter++ {
		fmt.Println("in iteration nr", iter)
		for i := begin; i < end; i++ {
			for j := 0; j < N; j++ {
				divisionAmount := 4
				r1 := gridEntryToAddr(i-1, j)
				r2 := gridEntryToAddr(i+1, j)
				r3 := gridEntryToAddr(i, j-1)
				r4 := gridEntryToAddr(i, j+1)
				var g1 []byte = make([]byte, 8)
				var g2 []byte = make([]byte, 8)
				var g3 []byte = make([]byte, 8)
				var g4 []byte = make([]byte, 8)
				if r1 == -1 {
					divisionAmount--
				} else {
					g1, _ = tm.ReadBytes(gridEntryToAddr(i-1, j), float32_BYTE_LENGTH)
				}
				if r2 == -1 {
					divisionAmount--
				} else {
					g2, _ = tm.ReadBytes(gridEntryToAddr(i+1, j), float32_BYTE_LENGTH)
				}
				if r3 == -1 {
					divisionAmount--
				} else {
					g3, _ = tm.ReadBytes(gridEntryToAddr(i, j-1), float32_BYTE_LENGTH)
				}
				if r4 == -1 {
					divisionAmount--
				} else {
					g4, _ = tm.ReadBytes(gridEntryToAddr(i, j+1), float32_BYTE_LENGTH)
				}
				privateArray[i][j] = (bytesToFloat32(g1) + bytesToFloat32(g2) + bytesToFloat32(g3) + bytesToFloat32(g4)) / 4
			}
			tm.Barrier(1)
			for i := begin; i < end; i++ {
				for j := 0; j < N; j++ {
					addr := gridEntryToAddr(i, j)
					var valAsBytes []byte = float32ToBytes(privateArray[i][j])
					for r, b := range valAsBytes {
						tm.Write(addr+r, b)
					}
				}
			}
			tm.Barrier(2)
		}
	}
	defer func() {
		if isManager {
			tm.Shutdown()
		} else {
			tm.Close()
		}
		fmt.Println("exiting algorithm...")
		group.Done()
	}()

}

func bytesToFloat32(bytes []byte) float32 {
	bits := binary.LittleEndian.Uint32(bytes)
	float := math.Float32frombits(bits)
	return float
}

func float32ToBytes(float float32) []byte {
	bits := math.Float32bits(float)
	bytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(bytes, bits)
	return bytes
}