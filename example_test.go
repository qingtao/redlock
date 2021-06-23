package redlock_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/qingtao/redlock"
	"github.com/qingtao/redlock/driver/redigo"
)

func ExampleMutex_Exec() {
	// 初始化redigo的连接
	p := &redis.Pool{
		DialContext: func(ctx context.Context) (redis.Conn, error) {
			return redis.DialContext(ctx, "tcp", "127.0.0.1:6379",
				redis.DialDatabase(2),
				redis.DialPassword("redis"),
			)
		},
		MaxIdle:     2,
		IdleTimeout: 2 * time.Minute,
	}
	taskName := func(id string) string {
		return fmt.Sprintf("task:%s", id)
	}
	// 创建一个工作任务锁
	workers := redlock.New(redigo.NewClient(p))
	lock := workers.NewMutex(taskName("exec"),
		// 请求超时时间
		redlock.WithTimeout(30*time.Second),
		// 锁的有效期
		redlock.WithExpires(60*time.Second),
		// 重试次数, 因此最大尝试测试是2+1
		redlock.WithRetries(2),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := lock.Exec(ctx, func(c context.Context) {
		fmt.Println("ok")
	}); err != nil {
		fmt.Println(err.Error())
	}

	// Output: ok
}

func ExampleMutex_Exec_extendCanceled() {
	// 初始化redigo的连接
	p := &redis.Pool{
		DialContext: func(ctx context.Context) (redis.Conn, error) {
			return redis.DialContext(ctx, "tcp", "127.0.0.1:6379",
				redis.DialDatabase(2),
				redis.DialPassword("redis"),
			)
		},
		MaxIdle:     2,
		IdleTimeout: 2 * time.Minute,
	}
	taskName := func(id string) string {
		return fmt.Sprintf("task:%s", id)
	}
	// 创建一个工作任务锁
	workers := redlock.New(redigo.NewClient(p))
	lock := workers.NewMutex(taskName("exec:extend:canceled"),
		// 锁的有效期
		redlock.WithExpires(500*time.Millisecond),
		// 重试次数, 因此最大尝试测试是2+1
		redlock.WithRetries(2),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	exit := make(chan struct{})
	if err := lock.Exec(ctx, func(c context.Context) {
		defer close(exit)
		for i := 0; i < 5; i++ {
			select {
			case <-ctx.Done():
				fmt.Println("cancel")
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}
		fmt.Println("ok")
	}); err != nil {
		fmt.Println(err.Error())
	}
	<-exit

	// Unordered output:
	// context deadline exceeded
	// cancel
}

func ExampleMutex_Exec_extendOk() {
	// 初始化redigo的连接
	redlock.SetLogger(log.Default())
	p := &redis.Pool{
		DialContext: func(ctx context.Context) (redis.Conn, error) {
			return redis.DialContext(ctx, "tcp", "127.0.0.1:6379",
				redis.DialDatabase(2),
				redis.DialPassword("redis"),
			)
		},
		MaxIdle:     2,
		IdleTimeout: 2 * time.Minute,
	}
	taskName := func(id string) string {
		return fmt.Sprintf("task:%s", id)
	}
	// 创建一个工作任务锁
	workers := redlock.New(redigo.NewClient(p))
	lock := workers.NewMutex(taskName("exec:extend:ok"),
		// 请求超时时间
		redlock.WithTimeout(2*time.Second),
		// 锁的有效期
		redlock.WithExpires(3*time.Second),
		// 重试次数, 因此最大尝试测试是2+1
		redlock.WithRetries(2),
	)
	ctx := context.Background()
	exit := make(chan struct{})
	if err := lock.Exec(ctx, func(ctx context.Context) {
		defer close(exit)
		for i := 0; i < 2; i++ {
			select {
			case <-ctx.Done():
				fmt.Println("cancel")
				return
			default:
				time.Sleep(1 * time.Second)
			}
		}
		fmt.Println("ok")
	}); err != nil {
		fmt.Println(err.Error())
	}
	<-exit

	// Output: ok
}
