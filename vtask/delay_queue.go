package vtask

import (
	"errors"
	"github.com/ville-vv/vilgo/vutil"

	//"github.com/ville-vv/vilgo/vutil"
	"sync"
	"time"
)

//type delayNode struct {
//	Value interface{}
//	Next  *delayNode
//	TmStp int64 // 时间戳
//}
//
//func (sel *delayNode) SetTmStp(duration time.Duration) {
//	sel.TmStp = time.Now().UnixNano() + int64(duration)
//}
//
//func newDelayNode(value interface{}, duration time.Duration) *delayNode {
//	nd := &delayNode{Value: value}
//	nd.SetTmStp(duration)
//	return nd
//}
//
//// DelayTask 延迟队列，使用环型的方式一直在循环检测队列中的数据是否超时
//// 如果超时了就取出来
//type DelayTask struct {
//	lock    sync.Mutex
//	prtNode *delayNode
//	head    *delayNode
//	rear    *delayNode
//	qLen    AtomicInt64
//}
//
//func NewDelayQueue() *DelayTask {
//	return &DelayTask{}
//}
//
//func (sel *DelayTask) Push(v interface{}, delay time.Duration) error {
//	sel.lock.Lock()
//	defer sel.lock.Unlock()
//	fmt.Println("Push:", v)
//	node := newDelayNode(v, delay)
//	if sel.head == nil || sel.qLen.Load() == 0 {
//		sel.head = newDelayNode(0, 0)
//		sel.head.Next = node
//		sel.rear = node
//		sel.prtNode = sel.head
//	}
//	sel.rear.Next = node
//	sel.rear = node
//	node.Next = sel.head.Next
//	sel.qLen.Inc()
//	return nil
//}
//
//func (sel *DelayTask) Loop(ctx context.Context, f func(interface{})) {
//	tmr := time.NewTicker(time.Millisecond * 10)
//	var dataNode *delayNode
//	for {
//		select {
//		case <-tmr.C:
//
//			sel.lock.Lock()
//			if sel.qLen.Load() <= 0 || sel.prtNode == nil {
//				// 空的和当前长度为 0 就 直接跳过
//				break
//			}
//			dataNode = sel.prtNode.Next
//			if dataNode == nil {
//				break
//			}
//			//
//			if dataNode.TmStp > time.Now().UnixNano() {
//				// 未超时就获取下一个
//				sel.prtNode = sel.prtNode.Next
//				break
//			}
//			f(dataNode.Value)
//			sel.prtNode.Next = sel.prtNode.Next.Next
//			dataNode.Next = nil
//			sel.qLen.Dec()
//			fmt.Println("当前队列大小：", sel.qLen.Load())
//		case <-ctx.Done():
//			tmr.Stop()
//			sel.lock.Unlock()
//			return
//		}
//		sel.lock.Unlock()
//	}
//}
//
//func (sel *DelayTask) del(prtNode *delayNode) {
//	sel.lock.Lock()
//	defer sel.lock.Unlock()
//	sel.prtNode.Next = prtNode.Next
//	prtNode = nil
//	sel.qLen.Dec()
//	return
//}
//
//func (sel *DelayTask) Length() int64 {
//	return sel.qLen.Load()
//}

type delayTaskHandler interface {
	Exec(interface{})
}

type delayTaskFunc func(interface{})

type delayTask struct {
	cycleNum int
	param    interface{}
	exec     delayTaskFunc
}

func (sel *delayTask) Dec() {
	sel.cycleNum--
}

func (sel *delayTask) IsExpire() bool {
	return sel.cycleNum <= 0
}

type DelayTask struct {
	slots       [60]*sync.Map
	slotTaskNum [60]*AtomicInt64
	curIndex    int
	maxTaskSize int64
	closed      bool
	stopCh      chan bool
	timeTick    *time.Ticker
	taskCh      chan delayTask
}

func NewDelayTask(size ...int64) *DelayTask {
	if len(size) == 0 {
		size = append(size, 1000)
	}
	dt := &DelayTask{
		slots:       [60]*sync.Map{},
		slotTaskNum: [60]*AtomicInt64{},
		curIndex:    0,
		maxTaskSize: size[0],
		stopCh:      make(chan bool),
		timeTick:    time.NewTicker(time.Second),
		taskCh:      make(chan delayTask, size[0]),
	}

	for i := 0; i < 60; i++ {
		dt.slots[i] = &sync.Map{}
	}

	for i := 0; i < 60; i++ {
		dt.slotTaskNum[i] = &AtomicInt64{}
	}

	go dt.loopExec()
	go dt.loopTask()

	return dt
}

func (sel *DelayTask) Close() {
	sel.closed = true
	close(sel.stopCh)
	close(sel.taskCh)
}

func (sel *DelayTask) loopExec() {
	for {
		select {
		case <-sel.stopCh:
			return
		case task, ok := <-sel.taskCh:
			if !ok {
				return
			}
			go task.exec(task.param)
		}
	}
}

func (sel *DelayTask) next() {
	sel.curIndex++
	if sel.curIndex >= 60 {
		sel.curIndex = 0
	}
}

func (sel *DelayTask) check() {
	tasks := sel.slots[sel.curIndex]
	taskNum := sel.slotTaskNum[sel.curIndex]
	tasks.Range(func(key, value interface{}) bool {
		task, _ := value.(*delayTask)
		if task.IsExpire() {
			//go task.exec(task.param)
			select {
			case sel.taskCh <- *task:
				tasks.Delete(key)
				taskNum.Dec()
			}
		} else {
			// 没有到期就轮询值减一个
			task.Dec()
		}
		return true
	})
}

func (sel *DelayTask) loopTask() {
	for {
		select {
		case <-sel.stopCh:
			sel.timeTick.Stop()
			return
		case <-sel.timeTick.C:
			sel.check()
			sel.next()
		}
	}
}

func (sel *DelayTask) slotIdx(subSecond int) int {
	idx := (subSecond)%60 + sel.curIndex
	if idx >= 60 {
		idx = idx - 60
	}
	return idx
}

func (sel *DelayTask) Push(name string, params interface{}, taskF delayTaskFunc, tm time.Time) error {
	if sel.closed {
		return nil
	}
	timeNow := time.Now()
	if tm.Before(timeNow) {
		return errors.New("time must be after than now")
	}
	subSecond := int(tm.Unix() - timeNow.Unix())
	idx := sel.slotIdx(subSecond)
	cycleNum := subSecond / 60
	if sel.slotTaskNum[idx].Load() > sel.maxTaskSize {
		return nil
	}
	sel.slotTaskNum[idx].Inc()
	sel.slots[idx].Store(name, &delayTask{
		cycleNum: cycleNum,
		param:    params,
		exec:     taskF,
	})
	return nil
}

type delayNode struct {
	cycleNum int
	value    interface{}
}

func (sel *delayNode) IsExpire() bool {
	return sel.cycleNum <= 0
}
func (sel *delayNode) Dec() {
	sel.cycleNum--
}

type DelayQueue struct {
	slots       [60]*sync.Map
	slotLen     [60]*AtomicInt64
	tempList    sync.Pool
	curIndex    int
	maxTaskSize int64
	started     bool
	stopCh      chan bool
	timeTick    *time.Ticker
	taskCh      chan delayNode
	workFunc    func([]interface{})
	once        sync.Once
}

func NewDelayQueue(workFunc func([]interface{}), size ...int64) *DelayQueue {
	if len(size) == 0 {
		size = append(size, 1000)
	}
	dt := &DelayQueue{
		slots:       [60]*sync.Map{},
		slotLen:     [60]*AtomicInt64{},
		curIndex:    0,
		maxTaskSize: size[0],
		stopCh:      make(chan bool),
		timeTick:    time.NewTicker(time.Second),
		taskCh:      make(chan delayNode, size[0]),
		workFunc:    workFunc,
		tempList: sync.Pool{New: func() interface{} {
			return make([]interface{}, 0, size[0])
		},
		},
	}
	for i := 0; i < 60; i++ {
		dt.slots[i] = &sync.Map{}
	}
	for i := 0; i < 60; i++ {
		dt.slotLen[i] = &AtomicInt64{}
	}
	return dt
}

func (sel *DelayQueue) Run() {
	sel.once.Do(func() {
		sel.started = true
		go sel.loopTask()
	})
}

func (sel *DelayQueue) Close() {
	sel.started = false
	close(sel.stopCh)
	close(sel.taskCh)
}

func (sel *DelayQueue) next() {
	sel.curIndex++
	if sel.curIndex >= 60 {
		sel.curIndex = 0
	}
}

func (sel *DelayQueue) check() {
	tasks := sel.slots[sel.curIndex]
	taskNum := sel.slotLen[sel.curIndex]
	tempList := sel.tempList.Get().([]interface{})
	tasks.Range(func(key, value interface{}) bool {
		task, _ := value.(*delayNode)
		if task.IsExpire() {
			tasks.Delete(key)
			taskNum.Dec()
			tempList = append(tempList, task.value)
		} else {
			task.Dec()
		}
		return true
	})
	if len(tempList) > 0 {
		sel.workFunc(tempList)
		tempList = nil
	}
	sel.tempList.Put(tempList)
}

func (sel *DelayQueue) loopTask() {
	for {
		select {
		case <-sel.stopCh:
			sel.timeTick.Stop()
			return
		case <-sel.timeTick.C:
			sel.check()
			sel.next()
		}
	}
}

func (sel *DelayQueue) slotIdx(subSecond int) int {
	idx := (subSecond)%60 + sel.curIndex
	if idx >= 60 {
		idx = idx - 60
	}
	return idx
}

func (sel *DelayQueue) Push(val interface{}, tm time.Time) error {
	return sel.push(vutil.RandStringBytesMask(16), val, tm)
}

func (sel *DelayQueue) push(name string, val interface{}, tm time.Time) error {
	if !sel.started {
		return ErrDelayNotStarted
	}
	timeNow := time.Now()
	if tm.Before(timeNow) {
		return ErrDelayTime
	}
	subSecond := int(tm.Unix() - timeNow.Unix())
	idx := sel.slotIdx(subSecond)
	if sel.slotLen[idx].Load() > sel.maxTaskSize {
		return ErrOverMaxSize
	}
	sel.slotLen[idx].Inc()
	sel.slots[idx].Store(name, &delayNode{
		cycleNum: subSecond / 60,
		value:    val,
	})
	return nil
}

type delayTaskV2 struct {
	Name  string
	param interface{}
	exec  delayTaskFunc
}

type DelayTaskV2 struct {
	delayQueue *DelayQueue
}

func NewDelayTaskV2() *DelayTaskV2 {
	tsk := &DelayTaskV2{}
	return tsk
}

func (sel *DelayTaskV2) Start() {
	sel.delayQueue = NewDelayQueue(sel.exec)
	sel.delayQueue.Run()
}

func (sel *DelayTaskV2) Close() {
	sel.delayQueue.Close()
}

func (sel *DelayTaskV2) exec(list []interface{}) {
	l := len(list)
	for i := 0; i < l; i++ {
		task, ok := list[i].(*delayTaskV2)
		if !ok {
			continue
		}
		task.exec(task.param)
	}
}

func (sel *DelayTaskV2) Push(name string, params interface{}, tm time.Duration, taskF delayTaskFunc) error {
	return sel.delayQueue.Push(&delayTaskV2{
		Name:  name,
		param: params,
		exec:  taskF,
	}, time.Now().Add(tm))
}