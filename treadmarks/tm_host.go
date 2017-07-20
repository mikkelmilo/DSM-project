package treadmarks

import (
	"DSM-project/memory"
	"DSM-project/network"
	"errors"
	"fmt"
)

const (
	LOCK_ACQUIRE_REQUEST  = "l_acq_req"
	LOCK_ACQUIRE_RESPONSE = "l_acq_resp"
	BARRIER_REQUEST       = "b_req"
	BARRIER_RESPONSE      = "b_resp"
	LOCK_RELEASE          = "l_rel"
	DIFF_REQUEST          = "diff_req"
	DIFF_RESPONSE         = "diff_resp"
	MALLOC_REQUEST        = "mal_req"
	FREE_REQUEST          = "free_req"
	MALLOC_REPLY          = "mal_repl"
	FREE_REPLY            = "free_repl"
	WELCOME_MESSAGE       = "WELC"
	COPY_REQUEST          = "copy_req"
	COPY_RESPONSE         = "copy_resp"
)

type ITreadMarks interface {
	memory.VirtualMemory
	Startup() (func(msg network.Message) error, error)
	Shutdown(address string)
	AcquireLock(id int)
	ReleaseLock(id int)
	Barrier(id int)
}

type TM_Message struct {
	From      byte
	To        byte
	Type      string
	Diffs     []Diff
	Id        int
	PageNr    int
	VC        Vectorclock
	Intervals []Interval
	Event     *chan string
	Data      []byte
}

func (m TM_Message) GetFrom() byte {
	return m.From
}

func (m TM_Message) GetTo() byte {
	return m.To
}

func (m TM_Message) GetType() string {
	return m.Type
}

type TreadMarks struct {
	memory.VirtualMemory //embeds this interface type
	nrProcs              int
	procId               byte
	nrLocks              int
	nrBarriers           int
	TM_IDataStructures
	vc      Vectorclock
	twinMap map[int][]byte //contains twins since last sync.
	network.IClient
}

func NewTreadMarks(virtualMemory memory.VirtualMemory, nrProcs, nrLocks, nrBarriers int) *TreadMarks {
	pageArray := make(PageArray)
	procArray := make(ProcArray, nrProcs)
	diffPool := make(DiffPool, 0)
	tm := TreadMarks{
		VirtualMemory:      virtualMemory,
		TM_IDataStructures: &TM_DataStructures{diffPool, procArray, pageArray},
		vc:                 Vectorclock{make([]uint, nrProcs)},
		twinMap:            make(map[int][]byte),
		nrProcs:            nrProcs,
		nrLocks:            nrLocks,
		nrBarriers:         nrBarriers,
	}

	tm.VirtualMemory.AddFaultListener(func(addr int, faultType byte, accessType string, value byte) {
		//c := make(chan string)
		//do fancy protocol stuff here
		switch accessType {
		case "READ":
			pageNr := tm.GetPageAddr(addr) / tm.GetPageSize()
			//if no copy, get one. Else, create twin
			if entry := tm.GetPageEntry(pageNr); entry.CopySet == nil && entry.ProcArr == nil {
				tm.SetPageEntry(pageNr, *NewPageArrayEntry())
				tm.sendCopyRequest(pageNr, tm.procId)
			}
			tm.SetRights(addr, memory.READ_WRITE)
		case "WRITE":
			pageNr := tm.GetPageAddr(addr) / tm.GetPageSize()
			//if no copy, get one. Else, create twin
			if entry := tm.GetPageEntry(pageNr); entry.CopySet == nil && entry.ProcArr == nil {
				tm.SetPageEntry(pageNr, *NewPageArrayEntry())
				tm.sendCopyRequest(pageNr, tm.procId)
			} else {
				//create a twin
				val := tm.PrivilegedRead(tm.GetPageAddr(addr), tm.GetPageSize())
				tm.twinMap[pageNr] = val
				tm.PrivilegedWrite(addr, []byte{value})
			}
			tm.SetRights(addr, memory.READ_WRITE)
		}
	})
	return &tm
}

func (t *TreadMarks) Startup(address string) (func(msg network.Message) error, error) {
	c := make(chan bool)

	msgHandler := func(message network.Message) error {
		//handle incoming messages
		msg, ok := message.(TM_Message)
		if !ok {
			return errors.New("invalid message struct type")
		}
		switch msg.GetType() {
		case WELCOME_MESSAGE:
			t.procId = msg.To
			c <- true
		case LOCK_ACQUIRE_REQUEST:
			t.HandleLockAcquireRequest(&msg)
		case LOCK_ACQUIRE_RESPONSE:
			t.HandleLockAcquireResponse(&msg)
			*msg.Event <- "continue"
		case BARRIER_RESPONSE:
			*msg.Event <- "continue"
		case DIFF_REQUEST:
			//remember to create own diff and set read protection on page(s)
		case DIFF_RESPONSE:

		case COPY_REQUEST:
			//if we have a twin, send that. Else just send the current contents of page
			if pg, ok := t.twinMap[msg.PageNr]; ok {
				msg.Data = pg
			} else {
				t.PrivilegedRead(msg.PageNr*t.GetPageSize(), t.GetPageSize())
				msg.Data = pg
			}
			msg.From, msg.To = msg.To, msg.From
			err := t.Send(msg)
			panicOnErr(err)
		case COPY_RESPONSE:
			t.PrivilegedWrite(msg.PageNr*t.GetPageSize(), msg.Data)
			*msg.Event <- "continue"
		default:
			return errors.New("unrecognized message type value: " + msg.Type)
		}
		return nil
	}
	client := network.NewClient(msgHandler)
	t.IClient = client
	if err := t.Connect(address); err != nil {
		return msgHandler, err
	}
	<-c
	return msgHandler, nil
}

func (t *TreadMarks) HandleLockAcquireResponse(message *TM_Message) {
	//Here we need to add the incoming intervals to the correct write notices.
	t.incorporateIntervalsIntoDatastructures(message)
	t.vc = *t.vc.Merge(&message.VC)
}

func (t *TreadMarks) HandleLockAcquireRequest(msg *TM_Message) TM_Message {
	//send write notices back and stuff
	//start by incrementing vc
	t.vc.Increment(t.procId)
	//create new interval and make write notices for all twinned pages since last sync
	t.updateDatastructures()
	//find all the write notices to send
	t.MapProcArray(
		func(p *Pair, procNr byte) {
			if *p != (Pair{}) && p.car != nil {
				var iRecord *IntervalRecord = p.car.(*IntervalRecord)
				//loop through the interval records for this process
				for {
					if iRecord == nil {
						break
					}
					// if this record has older ts than the requester, break
					if iRecord.Timestamp.Compare(&msg.VC) == -1 {
						break
					}
					i := Interval{
						Proc: procNr,
						Vt:   iRecord.Timestamp,
					}
					for _, wn := range iRecord.WriteNotices {
						i.WriteNotices = append(i.WriteNotices, wn.WriteNotice)
					}
					msg.Intervals = append(msg.Intervals, i)

					iRecord = iRecord.NextIr
				}
			}
		})
	msg.From, msg.To = msg.To, msg.From
	msg.Type = LOCK_ACQUIRE_RESPONSE
	msg.VC = t.vc
	return *msg
}

func (t *TreadMarks) updateDatastructures() {
	interval := IntervalRecord{
		Timestamp:    t.vc,
		WriteNotices: make([]*WriteNoticeRecord, 0),
	}

	for key := range t.twinMap {
		//if entry doesn't exist yet, initialize it
		entry := t.GetPageEntry(int(key))
		if entry.ProcArr == nil && entry.CopySet == nil {
			t.SetPageEntry(int(key),
				PageArrayEntry{
					CopySet: []int{},
					ProcArr: make(map[byte]*WriteNoticeRecord),
				})
		}
		//add interval record to front of linked list in procArray
		wn := t.PrependWriteNotice(t.procId, WriteNotice{pageNr: int(key)})
		wn.Interval = &interval
		wn.WriteNotice = WriteNotice{int(key)}
		interval.WriteNotices = append(interval.WriteNotices, wn)

	}

	//We only actually add the interval if we have any writenotices
	if len(interval.WriteNotices) > 0 {
		t.PrependIntervalRecord(t.procId, &interval)
	}
}

func (t *TreadMarks) GenerateDiffRequests(pageNr int) []TM_Message {
	//First we check if we have the page already or need to request a copy.
	if t.twinMap[pageNr] == nil {
		//TODO We dont have a copy, so we need to request a new copy of the page.
	}
	messages := make([]TM_Message, 0)
	vc := make([]Vectorclock, t.nrProcs)
	// First we figure out what processes we actually need to send messages to.
	// To do this, we first find the largest interval for each process, where there is a write notice without
	// a diff.
	// During this, we also find the lowest timestamp for this process, where we are missing diffs.
	intrec := make([]*IntervalRecord, t.nrProcs)
	for proc := byte(0); proc < byte(t.nrProcs); proc = proc + byte(1) {
		wnr := t.TM_IDataStructures.GetWriteNoticeListHead(pageNr, proc)
		if wnr != nil && wnr.Diff == nil {
			intrec[int(proc)] = wnr.Interval
			for {
				vc[int(proc)] = wnr.Interval.Timestamp
				wnr = wnr.NextRecord
				if wnr == nil || wnr.Diff != nil {
					break
				}
			}
		}
	}
	// After that we remove the ones, that is overshadowed by others.
	for proc1, int1 := range intrec {
		if int1 == nil {
			continue
		}
		overshadowed := false
		for _, int2 := range intrec {
			if int2 == nil {
				continue
			}
			if int1.Timestamp.Compare(&int2.Timestamp) < 0 {
				overshadowed = true
				break
			}
		}
		if overshadowed == false {
			message := TM_Message{
				From:   t.procId,
				To:     byte(proc1),
				Type:   DIFF_REQUEST,
				VC:     vc[proc1],
				PageNr: pageNr,
			}
			messages = append(messages, message)
		}
	}
	return messages
}

func (t *TreadMarks) Shutdown() {
	t.Close()
}

func (t *TreadMarks) AcquireLock(id int) {
	c := make(chan string)
	msg := TM_Message{
		Type:  LOCK_ACQUIRE_REQUEST,
		To:    1,
		From:  t.procId,
		Diffs: nil,
		Id:    id,
		VC:    t.vc,
		Event: &c,
	}
	err := t.Send(msg)
	panicOnErr(err)
	<-c
}

func (t *TreadMarks) ReleaseLock(id int) {
	msg := TM_Message{
		Type:  LOCK_RELEASE,
		To:    1,
		From:  t.procId,
		Diffs: nil,
		Id:    id,
	}
	err := t.Send(msg)
	panicOnErr(err)
}

func (t *TreadMarks) Barrier(id int) {
	c := make(chan string)
	msg := TM_Message{
		Type:  BARRIER_REQUEST,
		To:    1,
		From:  t.procId,
		Diffs: nil,
		Id:    id,
		Event: &c,
	}
	err := t.Send(msg)
	panicOnErr(err)
	<-c
}

func (t *TreadMarks) incorporateIntervalsIntoDatastructures(msg *TM_Message) {
	for i := len(msg.Intervals) - 1; i >= 0; i-- {
		interval := msg.Intervals[i]
		ir := IntervalRecord{
			Timestamp: interval.Vt,
		}
		t.PrependIntervalRecord(interval.Proc, &ir)
		for _, wn := range interval.WriteNotices {
			//prepend to write notice list and update pointers
			var res *WriteNoticeRecord = t.PrependWriteNotice(interval.Proc, wn)
			res.Interval = &ir
			ir.WriteNotices = append(ir.WriteNotices, res)
			//check if I have a write notice for this page with no diff at head of list. If so, create diff.
			if myWn := t.GetWriteNoticeListHead(wn.pageNr, t.procId); myWn != nil && myWn.Diff == nil {
				pageVal, err := t.ReadBytes(wn.pageNr*t.GetPageSize(), t.GetPageSize())
				panicOnErr(err)
				diff := CreateDiff(t.twinMap[wn.pageNr], pageVal)
				t.twinMap[wn.pageNr] = nil
				myWn.Diff = &diff
			}
			//finally invalidate the page
			t.SetRights(wn.pageNr*t.GetPageSize(), memory.NO_ACCESS)
		}
	}
}

func (t *TreadMarks) sendCopyRequest(pageNr int, procNr byte) {
	c := make(chan string)
	msg := TM_Message{
		Type:   COPY_REQUEST,
		To:     procNr,
		From:   t.procId,
		Event:  &c,
		PageNr: pageNr,
	}
	err := t.Send(msg)
	panicOnErr(err)
	<-c
}

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}
