package redigo

import (
	"context"
	"errors"
	"fmt"
	"time"

	_ "embed"

	"github.com/gomodule/redigo/redis"
)

type Client struct {
	p *redis.Pool
}

func NewClient(pool *redis.Pool) *Client {
	return &Client{p: pool}
}

func (c *Client) Close() error {
	return c.p.Close()
}

// SetNX 申请锁, 不存在时设置key和value, 可以通过context.WithTimeout指定请求的超时时间
func (c *Client) SetNX(ctx context.Context, key, value string, expires time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	var timeout time.Duration
	deadline, ok := ctx.Deadline()
	if ok {
		timeout = time.Until(deadline)
	}
	if timeout > expires {
		timeout = expires
	}
	conn := c.p.Get()
	defer conn.Close()
	s, err := redis.String(redis.DoWithTimeout(conn, timeout,
		"SET", key, value, "PX", expires.Milliseconds(), "NX",
	))
	if err != nil {
		return fmt.Errorf("SetNX failed: %w", err)
	}
	if s != "OK" {
		// 好像永远不会走到这个分支
		return fmt.Errorf("SetNX failed: result not OK")
	}
	return nil
}

type luaScript struct {
	keyCount int
	src      string
}

var (
	//go:embed script/delete.lua
	_deleteScript string
	//go:embed script/extend.lua
	_extendScript string
)

var (
	// Basically the random value is used,
	// in order to release the lock in a safe way,
	// with a script that tells Redis:
	// remove the key only if it exists,
	// and the value stored at the key is exactly the one I expect to be.
	// This is accomplished by the following Lua script:
	deleteScript = &luaScript{
		keyCount: 1,
		src:      _deleteScript,
	}

	// extend expires
	extendScript = &luaScript{
		keyCount: 1,
		src:      _extendScript,
	}
)

var ErrScriptResult = errors.New("lua script exec failed: result not 1")

// execLuaScript 运行脚本
func (c *Client) execLuaScript(ctx context.Context, timeout time.Duration, s *luaScript, args ...interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	conn := c.p.Get()
	defer conn.Close()
	argv := redis.Args{}.Add(s.src).
		Add(s.keyCount).
		AddFlat(args)
	n, err := redis.Int(redis.DoWithTimeout(conn, timeout,
		"EVAL", argv...,
	))
	if err != nil {
		return fmt.Errorf("lua script exec failed: %w", err)
	}
	if n == 1 {
		return nil
	}
	return ErrScriptResult
}

// Delete 删除设置的锁, 可以通过context.WithTimeout指定请求的超时时间
func (c *Client) Delete(ctx context.Context, key, value string) error {
	var timeout time.Duration
	deadline, ok := ctx.Deadline()
	if ok {
		timeout = time.Until(deadline)
	}
	if err := c.execLuaScript(ctx, timeout, deleteScript, key, value); err != nil {
		if !errors.Is(err, ErrScriptResult) {
			return err
		}
	}
	return nil
}

// Extend 延长锁的过期时间, 可以通过context.WithTimeout指定请求的超时时间
func (c *Client) Extend(ctx context.Context, key, value string, expires time.Duration) error {
	var timeout time.Duration
	deadline, ok := ctx.Deadline()
	if ok {
		timeout = time.Until(deadline)
	}
	if timeout > expires {
		timeout = expires
	}
	return c.execLuaScript(ctx, timeout, extendScript, key, value, expires.Milliseconds())
}
