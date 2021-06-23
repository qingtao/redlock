package redigo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
)

func createRedisClient(uri string) (*Client, error) {
	if uri == "" {
		uri = `redis://:redis@127.0.0.1:6379/2?read_timeout=2s`
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, nil
	}
	opts := make([]redis.DialOption, 0)
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			return nil, err
		}
		opts = append(opts, redis.DialDatabase(n))
	}
	vs := u.Query()
	if timeout := vs.Get("read_timeout"); timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, err
		}
		opts = append(opts,
			redis.DialConnectTimeout(d),
			redis.DialWriteTimeout(d),
			redis.DialReadTimeout(d),
		)
	}
	if u.User != nil {
		if username := u.User.Username(); username != "" {
			opts = append(opts, redis.DialUsername(username))
		}
		if password, ok := u.User.Password(); ok && password != "" {
			opts = append(opts, redis.DialPassword(password))
		}
	}
	p := &redis.Pool{
		DialContext: func(ctx context.Context) (redis.Conn, error) {
			return redis.DialContext(ctx, "tcp", u.Host, opts...)
		},
		MaxIdle:     2,
		IdleTimeout: 2 * time.Minute,
	}
	return NewClient(p), nil
}

func TestSetNX(t *testing.T) {
	c, err := createRedisClient("")
	if err != nil {
		t.Fatal(err)
	}
	var (
		key     = "mykey-1"
		value   = "myvalue-1"
		timeout = 10 * time.Second
		expires = 60 * time.Second
	)
	ctx := context.TODO()
	t.Cleanup(func() {
		c.Delete(ctx, key, value)
		c.Close()
	})
	c.Delete(ctx, key, value)
	t.Run("Ok", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err := c.SetNX(ctx, key, value, expires); err != nil {
			t.Errorf("got error = %v", err)
		}
	})
	t.Run("ExpectFailed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, timeout+expires)
		defer cancel()
		if err := c.SetNX(ctx, key, value, expires); err == nil {
			t.Errorf("except error not nil")
		}
	})
	t.Run("Canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := c.SetNX(ctx, key, value, expires); err == nil {
			t.Errorf("except error not nil")
		}
	})
}

func TestDelete(t *testing.T) {
	c, err := createRedisClient("")
	if err != nil {
		t.Fatal(err)
	}
	var (
		key     = "mykey-1"
		value   = "myvalue-1"
		timeout = 10 * time.Second
		expires = 60 * time.Second
	)
	ctx := context.TODO()
	t.Cleanup(func() {
		c.Delete(ctx, key, value)
		c.Close()
	})
	c.Delete(ctx, key, value)
	c.SetNX(ctx, key, value, expires)
	for i := 0; i < 2; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if err := c.Delete(ctx, key, value); err != nil {
				t.Errorf("got error = %v", err)
			}
		})
	}
}

func TestExtent(t *testing.T) {
	c, err := createRedisClient("")
	if err != nil {
		t.Fatal(err)
	}
	var (
		key     = "mykey-1"
		value   = "myvalue-1"
		timeout = 20 * time.Second
		expires = 10 * time.Second
	)
	ctx := context.TODO()
	t.Cleanup(func() {
		c.Delete(ctx, key, value)
		c.Close()
	})
	c.Delete(ctx, key, value)
	c.SetNX(ctx, key, value, expires)
	t.Run("Ok", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.TODO(), timeout)
		defer cancel()
		if err := c.Extend(ctx, key, value, timeout); err != nil {
			t.Errorf("got error = %v, but expect nil", err)
		}
	})
	c.Delete(ctx, key, value)
	t.Run("ExpectFailed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.TODO(), timeout)
		defer cancel()
		if err := c.Extend(ctx, key, value, expires); err == nil {
			t.Error("expect error not nil")
		}
	})
}

func Test_execLuaScript(t *testing.T) {
	c, err := createRedisClient("")
	if err != nil {
		t.Fatal(err)
	}
	var (
		key     = "mykey-1"
		value   = "mykey"
		timeout = 10 * time.Second
		expires = 5 * time.Second
	)
	ctx := context.TODO()
	t.Cleanup(func() {
		c.Delete(ctx, key, value)
		c.Close()
	})
	if err := c.SetNX(ctx, key, value, expires); err != nil {
		t.Error(err)
	}
	// 模拟超时
	// t.Run("Timeout", func(t *testing.T) {
	// 	timeout = 200 * time.Nanosecond
	// 	ctx1, cancel := context.WithTimeout(ctx, timeout)
	// 	defer cancel()
	// 	if err := c.execLuaScript(ctx1, timeout, deleteScript, key, value); err == nil {
	// 		t.Error("expect error: timeout")
	// 	}
	// })
	t.Run("ExpectFailed", func(t *testing.T) {
		if err := c.execLuaScript(ctx, timeout, &luaScript{}, key, value); err == nil {
			t.Error("expect error not nil")
		}
	})
	t.Run("Canceled", func(t *testing.T) {
		ctx1, cancel := context.WithCancel(ctx)
		cancel()
		if err := c.execLuaScript(ctx1, timeout, deleteScript, key, value); err == nil {
			t.Error("expect error not nil")
		}
	})
}

func TestCancel(t *testing.T) {
	c, err := createRedisClient("redis://default:tomtom@127.0.0.1:6379/3")
	if err != nil {
		t.Fatal(err)
	}
	var (
		key     = "mykey-1"
		value   = "mykey"
		timeout = 10 * time.Nanosecond
		expires = 5 * time.Second
	)
	ctx := context.TODO()
	t.Cleanup(func() {
		c.Delete(ctx, key, value)
		c.Close()
	})
	t.Run("SetNX", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.TODO(), timeout)
		defer cancel()
		if err := c.SetNX(ctx, key, value, expires); err == nil {
			t.Error("err = nil, but expect = (context deadline exceeded)")
		}
	})
	t.Run("Extent", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.TODO(), timeout)
		defer cancel()
		if err := c.Extend(ctx, key, value, expires); err == nil {
			t.Error("err = nil, but expect = (context deadline exceeded)")
		}
	})
	t.Run("Delete", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.TODO(), timeout)
		defer cancel()
		if err := c.Delete(ctx, key, value); err == nil {
			t.Error("err = nil, but expect = (context deadline exceeded)")
		}
	})
}
