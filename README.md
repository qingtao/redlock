# Redlock 实现一个基于redis的分布式锁

[redlock](https://redis.io/topics/distlock)

注意: 因为使用了`embed`包, 所以至少要求`Go`版本为**1.16**

示例:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/qingtao/redlock"
	"github.com/qingtao/redlock/driver/redigo"
)

func main() {
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
```
