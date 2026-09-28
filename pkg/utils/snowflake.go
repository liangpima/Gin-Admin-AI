package utils

import (
	"sync"
	"time"
)

const (
	epoch          = 1288834974657
	machineBits    = 10
	sequenceBits   = 12
	maxMachineID   = (1 << machineBits) - 1
	maxSequence    = (1 << sequenceBits) - 1
	machineIDShift = sequenceBits
	timeShift      = machineBits + sequenceBits
)

type Snowflake struct {
	mu        sync.Mutex
	machineID int64
	sequence  int64
	lastTime  int64
}

var (
	sf     *Snowflake
	sfOnce sync.Once
)

// InitSnowflake 初始化雪花算法实例，重复调用只生效一次。
//
// 用 sync.Once 保护：早前的写法允许并发写全局变量 sf，
// 而 GenerateID 里的「判空 + 懒初始化」在并发首调时构成数据竞争。
func InitSnowflake(machineID int64) {
	if machineID < 0 || machineID > maxMachineID {
		machineID = 1
	}
	sfOnce.Do(func() {
		sf = &Snowflake{machineID: machineID}
	})
}

func (s *Snowflake) NextID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()

	if now < s.lastTime {
		now = s.lastTime
	}

	if now == s.lastTime {
		s.sequence = (s.sequence + 1) & maxSequence
		if s.sequence == 0 {
			// 同一毫秒内序列号用尽，等到下一毫秒。
			// 加 Sleep 而不是空转：时钟回拨时这个循环可能要等较久，
			// 紧循环会把一个核跑满。
			for now <= s.lastTime {
				time.Sleep(200 * time.Microsecond)
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.sequence = 0
	}

	s.lastTime = now

	return ((now - epoch) << timeShift) | (s.machineID << machineIDShift) | s.sequence
}

func GenerateID() int64 {
	// 用 sfOnce 而不是「先判空、再初始化」。
	//
	// 原写法是 `if sf == nil { InitSnowflake(1) }`：对全局变量 sf 的**读**
	// 发生在 sfOnce.Do 之外，与 Do 内部对 sf 的**写**没有任何同步关系 ——
	// 并发首次调用时构成数据竞争（-race 能报出来，而竞态下的表现是
	// 拿到一个 nil 的 *Snowflake 并 panic，或两个 goroutine 各建一个实例）。
	// 让 Once 独自负责初始化：Do 返回后读 sf 才有 happens-before 保证。
	//
	// 幂等：若 InitSnowflake 已经跑过，这里就是空操作，不会覆盖它的 machineID。
	sfOnce.Do(func() { sf = &Snowflake{machineID: 1} })
	return sf.NextID()
}
