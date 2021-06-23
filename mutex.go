package redlock

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	// 默认的请求超时时间
	defaultTimeout = 50 * time.Millisecond
	// 默认的过期时间
	defaultExpires = 30 * time.Second
	// 默认的重试次数
	defaultRetries = 1
)

var logger *log.Logger = log.Default()

var (
	defaultDelay = randomDelay()
	once         sync.Once
)

// SetLogger 设置日志, 如果l==nil,则不打印日志
func SetLogger(l *log.Logger) {
	once.Do(func() {
		logger = l
	})
}

func printLog(err error) {
	if logger == nil {
		return
	}
	log.Printf("error: %+v", err)
}

func randomDelay() time.Duration {
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	n := random.Int63n(10)
	if n < 2 {
		n = 2
	}
	return time.Duration(n) * time.Millisecond
}

// Options 请求锁时的选项
type Options struct {
	// 请求超时时间
	timeout time.Duration
	// 有效时间
	expires time.Duration
	// 重试次数,
	retries int
	// 时间偏移
	delay time.Duration
	// 连接池
	conns []Conn
}

var optsionPool = sync.Pool{
	New: func() interface{} {
		return &Options{
			timeout: defaultTimeout,
			expires: defaultExpires,
			retries: defaultRetries,
			delay:   defaultDelay,
			conns:   nil,
		}
	},
}

// Option 选项函数
type Option func(*Options)

func (o *Options) reset() {
	o.timeout = defaultTimeout
	o.expires = defaultExpires
	o.retries = defaultRetries
	o.delay = defaultDelay
	o.conns = nil
}

// applyOptions 合并选项
func applyOptions(opts ...Option) *Options {
	v := optsionPool.Get()
	o := v.(*Options)
	o.reset()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithExpires 设置过期时间
func WithExpires(d time.Duration) Option {
	return func(o *Options) {
		if d > 0 {
			o.expires = d
		}
	}
}

// WithRetries 设置重试次数
func WithRetries(n int) Option {
	return func(o *Options) {
		if n >= 0 {
			o.retries = n
		}
	}
}

func WithConnections(conns ...Conn) Option {
	return func(o *Options) {
		o.conns = append(o.conns, conns...)
	}
}

// Mutex 互斥锁
type Mutex struct {
	// 申请锁的名称
	name string
	// 生成的值
	value string
	// 成功的数量
	success uint32
	// 成功获取锁的时间
	last time.Time

	opts *Options
}

type Debug struct{}

var debug *Debug

// newMutex 创建一个新的互斥锁,参数name是认证的唯一标识,如:
//	 userid:051c6b48-7a40-4826-850d-ea500adb7ebc
func NewMutex(name string, opts ...Option) (*Mutex, error) {
	o := applyOptions(opts...)
	if len(o.conns) == 0 {
		return nil, errors.New("must provider redis connections")
	}
	return newMutex(name, o), nil
}

func newMutex(name string, opts *Options) *Mutex {
	value := uuid.New()
	return &Mutex{
		name:  name,
		value: hex.EncodeToString(value[:]),
		opts:  opts,
	}
}

// getMinSuccess 计算锁请求的最小成功实例数量
func (m *Mutex) getMinSuccess() int {
	return len(m.opts.conns)/2 + 1
}

// tryLockOrExtend 尝试一次加锁或者延长锁的有效期
func (m *Mutex) tryLockOrExtend(ctx context.Context, extend bool) error {
	m.success = 0
	// 加锁成功的数量
	minSuccess := m.getMinSuccess()
	// 接收多个错误
	errs := make(MultiError, 0, len(m.opts.conns))
	// 设置超时时间
	ctx, cancel := context.WithTimeout(ctx, m.opts.timeout)
	defer cancel()
	// 并发执行加锁
	group, ctx1 := errgroup.WithContext(ctx)
	for i := 0; i < len(m.opts.conns); i++ {
		i, conn := i, m.opts.conns[i]
		group.Go(func() error {
			if conn == nil {
				return fmt.Errorf("redis connections index %d is nil", i)
			}
			var err error
			if extend {
				err = conn.Extend(ctx1, m.name, m.value, m.opts.expires)
			} else {
				err = conn.SetNX(ctx1, m.name, m.value, m.opts.expires)
			}
			if err != nil {
				errs = append(errs, err)
				return nil
			}
			// 成功数量加一
			n := int(atomic.AddUint32(&m.success, 1))
			// 如果满足最小成功数量,且成功数量小于实例数, 就尝试退出其他请求
			if n >= minSuccess && n < len(m.opts.conns) {
				cancel()
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	// 满足最小锁数量, 就认为加锁成功
	if m.success >= uint32(minSuccess) {
		// 打印日志
		if len(errs) > 0 {
			printLog(fmt.Errorf("tryLockOrExtend %w", errs))
		}
		if debug != nil {
			m.last = time.Now()
		}
		return nil
	}
	return append(errs, errors.New("lock failed"))
}

// lockOrExtend 尝试加锁或者延长有效期
func (m *Mutex) lockOrExtend(ctx context.Context, extend bool) error {
	// 开始加锁的时间
	start := time.Now()
	num := m.opts.retries + 1
	errs := make(MultiError, 0, num)
	// 最大尝试m.opts.retries+1次
	for i := 0; i < num; i++ {
		errs = errs[:0]
		err := m.tryLockOrExtend(ctx, extend)
		// 计算加锁使用的时间
		elapsed := time.Since(start)
		if err == nil && elapsed <= m.opts.expires-m.opts.delay {
			m.last = start
			return nil
		}
		errs = append(errs, err)
		// 全部解锁
		if err := m.Unlock(ctx); err != nil {
			if logger != nil {
				log.Printf("unlock error: %s", err)
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Lock 执行加锁
func (m *Mutex) Lock(ctx context.Context) error {
	return m.lockOrExtend(ctx, false)
}

// lock 延长锁的有效期, 只有在加锁成功后调用才能成功
func (m *Mutex) Extend(ctx context.Context) error {
	return m.lockOrExtend(ctx, true)
}

// Unlock 执行解锁
func (m *Mutex) Unlock(ctx context.Context) error {
	// 将选项放回缓冲池
	defer func() {
		optsionPool.Put(m.opts)
	}()
	// 锁超时了就执行退出
	if d := time.Since(m.last); d >= m.opts.expires {
		return nil
	}
	// 接收多个错误
	errs := make(MultiError, 0, len(m.opts.conns))
	// 设置超时时间
	ctx, cancel := context.WithTimeout(ctx, m.opts.timeout)
	defer cancel()
	// 并发执行解锁锁
	group, ctx1 := errgroup.WithContext(ctx)
	for i := 0; i < len(m.opts.conns); i++ {
		conn := m.opts.conns[i]
		group.Go(func() error {
			if conn == nil {
				return nil
			}
			if err := m.tryUnlock(ctx1, conn); err != nil {
				errs = append(errs, err)
			}
			return nil
		})
	}
	group.Wait()
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (m *Mutex) tryUnlock(ctx context.Context, conn Conn) error {
	var err error
	// 和加锁时一样, 最多执行m.opts.retries+1次
	// 每个实例都要重试, 直到成功或者达到最大次数
	for i := 0; i < m.opts.retries+1; i++ {
		if err = conn.Delete(ctx, m.name, m.value); err == nil {
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
	}
	return err
}

// LastTime 返回加锁成功的时间
func (m *Mutex) LastTime() time.Time {
	return m.last
}

// Exec 加锁并执行
// 传入的f函数如果执行时间过长, 会尝试延续锁的有效期,
// 延长有效期失败就退出,至于执行的操作是否有中断方法需要在f函数内部使用context
func (m *Mutex) Exec(ctx context.Context, f WorkerFunc) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("lock error: %v", e)
		}
	}()
	if err := m.Lock(ctx); err != nil {
		return fmt.Errorf("lock error: %w", err)
	}
	defer func() {
		if err = m.Unlock(ctx); err != nil {
			printLog(fmt.Errorf("unlock %w", err))
		}
	}()

	// 需要按需延长锁的有效期
	ctx1, cancel := context.WithCancel(ctx)
	defer cancel()
	// 在剩余有效期一半的时间延长锁
	refreshTime := (m.opts.expires - time.Since(m.last)) / 2
	done := make(chan struct{})
	go func() {
		f(ctx1)
		// 执行结束关闭通道
		close(done)
	}()
	tick := time.NewTicker(refreshTime)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx1.Done():
			return
		case <-tick.C:
			// 延长有效期失败就退出,至于执行的操作是否有中断方法需要在f函数内部使用context
			if err = m.Extend(ctx1); err != nil {
				return err
			}
			tick.Reset(refreshTime)
		}
	}
}
