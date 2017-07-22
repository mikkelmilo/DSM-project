package treadmarks

import (
	"DSM-project/memory"
	"fmt"
	"github.com/stretchr/testify/assert"
	"log"
	"testing"
	"time"
)

var _ = fmt.Print
var _ = log.Print

func setupTreadMarksStruct(nrProcs int) *TreadMarks {
	vm1 := memory.NewVmem(128, 8)
	tm1 := NewTreadMarks(vm1, nrProcs, 4, 4)
	return tm1
}

func TestTreadMarksInitialisation(t *testing.T) {
	/*managerHost, hosts := InitialiseTMSystem(4)
	fmt.Println(hosts[0])
	fmt.Println(managerHost)
	assert.NotNil(t, managerHost)
	assert.NotContains(t, hosts, nil)
	assert.Equal(t, byte(1), managerHost.ProcId)
	assert.Equal(t, byte(2), hosts[1].ProcId)*/

	managerHost := setupTreadMarksStruct(3)
	host2 := setupTreadMarksStruct(3)
	host3 := setupTreadMarksStruct(3)
	err := managerHost.Startup()
	assert.Nil(t, err)
	err = host2.Join("localhost:2000")
	assert.Nil(t, err)
	err = host3.Join("localhost:2000")
	assert.Nil(t, err)
	assert.NotNil(t, managerHost)

	assert.Equal(t, byte(1), managerHost.ProcId)
	assert.Equal(t, byte(2), host2.ProcId)
	assert.Equal(t, byte(3), host3.ProcId)

	host2.Shutdown()
	host3.Shutdown()
	managerHost.Shutdown()

}

func TestBarrier(t *testing.T) {
	managerHost := setupTreadMarksStruct(3)
	host2 := setupTreadMarksStruct(3)
	host3 := setupTreadMarksStruct(3)
	managerHost.Startup()
	host2.Join("localhost:2000")
	host3.Join("localhost:2000")

	started := make(chan bool, 3)
	done := make(chan bool)

	go func(host *TreadMarks, started chan<- bool, done chan<- bool) {
		started <- true
		host.Barrier(1)
		done <- true
	}(managerHost, started, done)
	<-started
	var failed bool
	select {
	case <-done:
		failed = true
	default:
		failed = false
	}
	assert.False(t, failed)

	go func(host *TreadMarks, started chan<- bool, done chan<- bool) {
		started <- true
		host.Barrier(1)
		done <- true
	}(host2, started, done)
	<-started
	select {
	case <-done:
		failed = true
	default:
		failed = false
	}
	assert.False(t, failed)

	go func(host *TreadMarks, started chan<- bool, done chan<- bool) {
		started <- true
		host.Barrier(1)
		done <- true
	}(host3, started, done)
	<-started

	<-done
	<-done
	<-done

	//add tests of validity of data structures

	host2.Shutdown()
	host3.Shutdown()
	managerHost.Shutdown()

}

func TestLocks(t *testing.T) {
	managerHost := setupTreadMarksStruct(3)
	host2 := setupTreadMarksStruct(3)
	host3 := setupTreadMarksStruct(3)
	managerHost.Startup()
	host2.Join("localhost:2000")
	host3.Join("localhost:2000")
	started := make(chan string)
	finished := make(chan string)

	go func() {
		started <- "ok"
		host2.AcquireLock(1)
		time.Sleep(500 * time.Millisecond)
		host2.ReleaseLock(1)
		finished <- "released"
	}()
	go func() {
		started <- "ok"
		host3.AcquireLock(1)
		finished <- "acquired"
	}()
	<-started
	<-started
	assert.Equal(t, "released", <-finished)
	assert.Equal(t, "acquired", <-finished)
}

func TestShouldGetCopyIfNoCopy(t *testing.T) {
	managerHost := setupTreadMarksStruct(2)
	host2 := setupTreadMarksStruct(2)
	managerHost.Startup()
	host2.Join("localhost:2000")
	res, _ := host2.Read(13) //in page 1
	assert.Equal(t, byte(0), res)
}
