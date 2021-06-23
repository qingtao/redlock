package redlock_test

import (
	"context"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"
	"github.com/qingtao/redlock"
	"github.com/qingtao/redlock/driver/redigo"
)

func ExampleNew() {
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
	taskName := func() string {
		return "task:" + uuid.NewString()
	}
	redLock := redlock.New(redigo.NewClient(p))
	lock1 := redLock.NewMutex(taskName(),
		redlock.WithExpires(60*time.Second),
		redlock.WithRetries(2),
	)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	if err := lock1.Exec(ctx1, func(c context.Context) {
		fmt.Println("ok")
	}); err != nil {
		fmt.Println(err.Error())
	}

	lock2 := redLock.NewMutex(taskName(),
		redlock.WithExpires(500*time.Millisecond),
		redlock.WithRetries(2),
	)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel2()
	exit := make(chan struct{})
	if err := lock2.Exec(ctx2, func(c context.Context) {
		defer close(exit)
		for i := 0; i < 5; i++ {
			select {
			case <-ctx2.Done():
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

	// Output:
	// ok
	// cancel
}
