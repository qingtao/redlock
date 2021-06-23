package redlock

import (
	"context"
	"strings"
	"time"
)

// Conn redis连接
type Conn interface {
	// SetNX 存储锁的key和value, 如果需要设置连接超时,可以使用context.WithTimeout,
	// 如果超时时间比过期时间(expires)大,使用expires作为超时时间
	SetNX(ctx context.Context, key, value string, expires time.Duration) error
	// Delete 删除锁的key, 如果需要设置连接超时,可以使用context.WithTimeout
	Delete(ctx context.Context, key, value string) error
	// Extend 存储锁的key的延长过期时间, 如果需要设置连接超时,可以使用context.WithTimeout,
	// 如果超时时间比过期时间(expires)大,使用expires作为超时时间
	Extend(ctx context.Context, key, value string, expires time.Duration) error
	// Close 关闭redis的连接
	Close() error
}

// MultiError 包装多个错误
type MultiError []error

// Error 遍历多个错误, 以字符串格式返回
func (m MultiError) Error() string {
	if len(m) == 0 {
		return ""
	}
	var (
		buf      strings.Builder
		splitStr = "; "
	)
	for _, err := range m {
		if err == nil {
			continue
		}
		buf.WriteString(err.Error())
		buf.WriteString(splitStr)
	}
	return strings.TrimSuffix(buf.String(), splitStr)
}

// WorkerFunc 工作函数, 任务可以选择接收ctx.Done()
// 由更上层的上下文取消时, 执行退出操作, 也可以忽略此取消事件;
type WorkerFunc func(context.Context)

// Lock 锁的提供者
type Lock struct {
	// redis实例的列表
	clients []Conn
}

// New 使用初始化完成的客户端列表, 创建锁的提供者
func New(clients ...Conn) *Lock {
	if len(clients) == 0 {
		panic("must provider redis connections")
	}
	return &Lock{clients: clients}
}

// NewMutex 创建互斥锁
// 提供者已经初始化了客户端连接, 调用者不需要使用WithPool选项
func (l *Lock) NewMutex(name string, opts ...Option) *Mutex {
	opts = append(opts, WithConnections(l.clients...))
	return newMutex(name, applyOptions(opts...))
}
