package service

import (
	"crypto/rsa"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 平台证书缓存与「未知 serial 限速」的用例。
//
// 为什么值得写：待验签的 serial 直接来自请求头 `Wechatpay-Serial`，而支付回调
// 端点不需要鉴权（渠道侧发起、靠验签自证）。若「缓存里没有这个 serial」就无条件
// 重新拉取 /v3/certificates，匿名调用方每换一个随机 serial 就能换来一次带签名的
// 出网调用 —— 放大出网/CPU，还可能触发微信侧频率限制，反过来影响**正常**回调验签。
//
// 这段逻辑不依赖网络，所以能直接单测（真正出网的 fetchPlatformPublicKeys 按项目
// 既定边界不进单测：需要真实凭据与网络，测它只能 mock 掉整个网络层）。

// throttleAt 造一个用可控时钟的限速器，避免用例依赖 sleep。
func throttleAt(interval time.Duration, start time.Time) (*unknownSerialThrottle, func(time.Duration)) {
	current := start
	throttle := &unknownSerialThrottle{
		interval: interval,
		now:      func() time.Time { return current },
	}
	return throttle, func(d time.Duration) { current = current.Add(d) }
}

func TestUnknownSerialThrottle(t *testing.T) {
	t.Run("首次放行，间隔内重复请求被拒", func(t *testing.T) {
		throttle, advance := throttleAt(30*time.Second, time.Unix(1000, 0))

		if !throttle.allow() {
			t.Fatal("首次应当放行")
		}
		if throttle.allow() {
			t.Error("间隔内的第二次必须被拒 —— 否则每个请求都会换来一次出网调用")
		}
		if throttle.allow() {
			t.Error("间隔内的第三次必须被拒")
		}

		advance(29 * time.Second)
		if throttle.allow() {
			t.Error("间隔未满就被放行")
		}
	})

	t.Run("间隔届满后放行", func(t *testing.T) {
		throttle, advance := throttleAt(30*time.Second, time.Unix(1000, 0))

		if !throttle.allow() {
			t.Fatal("首次应当放行")
		}
		advance(30 * time.Second)

		if !throttle.allow() {
			t.Error("间隔届满后应当放行（证书轮换后新 serial 要靠这次拉取被认出来）")
		}
	})

	t.Run("限速是**全局**的，换 serial 绕不过", func(t *testing.T) {
		// 这是本限速器的核心语义。若做成「每个 serial 各记一次」，
		// 攻击者每次带一个新 serial 就能绕过 —— 而 serial 完全由他控制。
		// 这里连调 50 次模拟换 50 个 serial：只能放行 1 次。
		throttle, advance := throttleAt(30*time.Second, time.Unix(1000, 0))

		allowed := 0
		for i := 0; i < 50; i++ {
			if throttle.allow() {
				allowed++
			}
			advance(time.Millisecond)
		}
		if allowed != 1 {
			t.Errorf("50 次请求只应放行 1 次出网，实际 %d 次", allowed)
		}
	})
}

// TestUnknownSerialThrottleConcurrent 并发调用下，间隔内**恰好只有一个**请求获准。
//
// 上面那条串行用例只能证明「重复调用会被拒」，证明不了**锁**的作用：
// 把 allow() 里的 mu.Lock() 去掉（或改成「先读 last、判断、再赋值」的写法），
// 串行版本照样通过 —— 而并发时多个 goroutine 会同时看到「上一次为空」，
// 一起获准，出网就被放大成 N 倍。
//
// 而这正是限速器存在的唯一目的：触发它的 serial 来自**未鉴权的请求头**，
// 攻击者的默认打法就是并发轰炸，不是老老实实一次一次来。
// ⚠️ **本用例的区分力依赖 `-race`，本机无法验证**（Windows 无 MinGW，
// CGO_ENABLED=0）。写下这条是为了不让后人误以为它已经在本地验过：
//
// 竞态窗口是 `读 last → 判断 → 写 last` 这三步之间，而它们中间**没有可让出点**
// （实测过：把锁删掉后，64 个 goroutine × 50 轮、再加 Gosched 让出，
// 普通模式下依然从不交错 —— 把 Gosched 放在 `nowFn()` 里反而让所有
// goroutine 在「判断之前」对齐，执行更有序了）。
//
// 普通模式下这条用例只能捕获「逻辑性」失效（比如某次实现改成放行多个），
// 真正的 data race 检测由 CI 的 race job 完成 —— `-race` 靠 happens-before
// 分析发现 `t.last` 的读写无同步，**不需要真的交错**，因此能稳定捕获。
func TestUnknownSerialThrottleConcurrent(t *testing.T) {
	// 时钟返回固定时刻：整个用例落在同一个 interval 内，
	// 所以无论多少并发，能获准的都只该有一个。
	throttle, _ := throttleAt(30*time.Second, time.Unix(1000, 0))

	const goroutines = 64
	var allowed int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // 尽量让它们同时冲进临界区
			if throttle.allow() {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if allowed != 1 {
		t.Errorf("间隔内并发 %d 个请求只应放行 1 个，实际 %d —— 限速器在并发下失效，出网会被放大",
			goroutines, allowed)
	}
}

// TestCertCacheConcurrentAccess 并发读写证书缓存不得产生数据竞争。
//
// 读路径（cachedPlatformPublicKey / certCacheFresh）走 RLock，
// 写路径（拉取成功后整体替换 map）走 Lock。
//
// 这条用例配合 `go test -race` 才有完整意义：
//   · 普通模式下它能发现的是**极端**情况 —— map 并发读写会直接 panic
//     （"concurrent map read and map write"）
//   · -race 才能发现「读写交错但恰好没崩」的那些
//
// ⚠️ 本机是 Windows 且无 MinGW（CGO_ENABLED=0），-race 跑不了 ——
// 这条的竞态检测在 CI 的 race job 上完成（见 .github/workflows/ci.yml）。
// 这么写不是偷懒：缓存的读多写少、且写是「整体替换 map」，
// 一旦 RWMutex 用错（比如写路径漏了 Lock），普通模式下的 panic 是**偶发**的，
// 必须靠 -race 才能稳定复现。
func TestCertCacheConcurrentAccess(t *testing.T) {
	key := &rsa.PublicKey{}
	withCertCache(t, map[string]*rsa.PublicKey{"S1": key}, time.Now())

	const goroutines = 32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for j := 0; j < 50; j++ {
				if i%2 == 0 {
					_ = cachedPlatformPublicKey("S1")
					_ = certCacheFresh()
					continue
				}
				// 模拟「拉取成功后整体替换缓存」
				certCacheMu.Lock()
				certCache = map[string]*rsa.PublicKey{"S1": key}
				certCacheTime = time.Now()
				certCacheMu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()
}

// withCertCache 替换包级证书缓存，用例结束自动还原（用例之间不互相污染）。
func withCertCache(t *testing.T, keys map[string]*rsa.PublicKey, at time.Time) {
	t.Helper()

	certCacheMu.Lock()
	prevKeys, prevTime := certCache, certCacheTime
	certCache, certCacheTime = keys, at
	certCacheMu.Unlock()

	t.Cleanup(func() {
		certCacheMu.Lock()
		certCache, certCacheTime = prevKeys, prevTime
		certCacheMu.Unlock()
	})
}

func TestCertCacheFreshness(t *testing.T) {
	// 只比较指针身份，不做任何密码学运算，因此空结构体足够
	key := &rsa.PublicKey{}

	t.Run("缓存为空 → 不算新鲜，且取不到任何公钥", func(t *testing.T) {
		withCertCache(t, map[string]*rsa.PublicKey{}, time.Time{})

		if certCacheFresh() {
			t.Error("从未拉取过时不应算新鲜")
		}
		if pub := cachedPlatformPublicKey("any"); pub != nil {
			t.Error("空缓存不应返回公钥")
		}
	})

	t.Run("刚刷新 → 新鲜，命中的 serial 可取到", func(t *testing.T) {
		withCertCache(t, map[string]*rsa.PublicKey{"S1": key}, time.Now())

		if !certCacheFresh() {
			t.Error("刚刷新应当算新鲜")
		}
		if pub := cachedPlatformPublicKey("S1"); pub != key {
			t.Error("命中的 serial 应当返回缓存的公钥")
		}
		if pub := cachedPlatformPublicKey("未知"); pub != nil {
			t.Error("未命中的 serial 不应返回公钥")
		}
	})

	t.Run("过期 → 不算新鲜，命中也不返回（除非显式允许降级）", func(t *testing.T) {
		withCertCache(t, map[string]*rsa.PublicKey{"S1": key}, time.Now().Add(-certCacheTTL-time.Minute))

		if certCacheFresh() {
			t.Error("超过 TTL 不应算新鲜")
		}
		if pub := cachedPlatformPublicKey("S1"); pub != nil {
			t.Error("过期缓存不应参与正常取值")
		}
		// 拉取失败时允许用过期证书兜底：宁可用可能过期的证书，
		// 也不要让全部回调因一次出网失败而中断
		if pub := cachedPlatformPublicKey("S1", true); pub != key {
			t.Error("allowStale 时应当返回过期缓存")
		}
	})
}

// withCertThrottle 用可控时钟的限速器替换包级实例，用例结束自动还原。
func withCertThrottle(t *testing.T, interval time.Duration, start time.Time) *unknownSerialThrottle {
	t.Helper()

	throttle, _ := throttleAt(interval, start)
	prev := certUnknownThrottle
	certUnknownThrottle = throttle
	t.Cleanup(func() { certUnknownThrottle = prev })

	return throttle
}

// newCertCountingGateway 造一个出网次数可数的网关。
func newCertCountingGateway(keys map[string]*rsa.PublicKey) (*WechatPayGateway, *int) {
	fetches := 0
	return &WechatPayGateway{
		fetchCerts: func() (map[string]*rsa.PublicKey, error) {
			fetches++
			return keys, nil
		},
	}, &fetches
}

// TestGetPlatformPublicKeyRefetchThrottled 未知 serial 触发的出网必须被限速。
//
// 这条是 M8 的核心回归：serial 来自**未鉴权**的请求头，若「缓存里没有就重新拉取」，
// 匿名调用方每换一个随机 serial 就能换来一次带签名的出网调用。
// 断言落在**出网次数**上 —— 只看「返回了错误」是区分不出「限速生效」与
// 「每次都老老实实拉了一遍然后没找到」的。
func TestGetPlatformPublicKeyRefetchThrottled(t *testing.T) {
	known := &rsa.PublicKey{}
	withCertCache(t, map[string]*rsa.PublicKey{"KNOWN": known}, time.Now())
	withCertThrottle(t, certUnknownRefetchInterval, time.Unix(1000, 0))

	gw, fetches := newCertCountingGateway(map[string]*rsa.PublicKey{"KNOWN": known})

	// 100 个不同的未知 serial，全部都在 30 秒限速窗口内
	for i := 0; i < 100; i++ {
		if _, err := gw.getPlatformPublicKey(fmt.Sprintf("FAKE-%d", i)); err == nil {
			t.Fatalf("第 %d 次用未知 serial 竟然验签通过了", i)
		}
	}

	// 只放行 1 次：证书轮换后新 serial 要靠这一次被认出来，之后必须拦住
	if *fetches != 1 {
		t.Errorf("100 个未知 serial 只应触发 1 次出网，实际 %d 次（放大攻击面未收敛）", *fetches)
	}

	// 命中缓存的正常回调不应产生任何出网
	if _, err := gw.getPlatformPublicKey("KNOWN"); err != nil {
		t.Fatalf("已知 serial 应命中缓存: %v", err)
	}
	if *fetches != 1 {
		t.Errorf("命中缓存不应出网，实际累计 %d 次", *fetches)
	}
}

// TestGetPlatformPublicKeyStaleCacheAlwaysRefetches 缓存过期时的刷新不受限速影响。
//
// 反向对照：限速必须只约束「缓存新鲜时的未知 serial」。
// 若写成「一律限速」，一次出网失败后 30 秒内的正常回调会因为拿不到证书
// 而全部验签失败 —— 那才是真的影响收款。
func TestGetPlatformPublicKeyStaleCacheAlwaysRefetches(t *testing.T) {
	known := &rsa.PublicKey{}
	// 缓存已过期
	withCertCache(t, map[string]*rsa.PublicKey{"KNOWN": known},
		time.Now().Add(-certCacheTTL-time.Minute))

	throttle := withCertThrottle(t, certUnknownRefetchInterval, time.Unix(1000, 0))
	// 先把出网额度用光，证明下面的拉取不是靠「额度还够」
	if !throttle.allow() {
		t.Fatal("准备：首次额度应当可用")
	}
	if throttle.allow() {
		t.Fatal("准备：额度应当已被用光")
	}

	gw, fetches := newCertCountingGateway(map[string]*rsa.PublicKey{"KNOWN": known})

	if _, err := gw.getPlatformPublicKey("KNOWN"); err != nil {
		t.Fatalf("缓存过期后应当重新拉取并命中: %v", err)
	}
	if *fetches != 1 {
		t.Errorf("缓存过期触发的刷新不该被限速拦截，实际出网 %d 次", *fetches)
	}
}
