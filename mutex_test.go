package redlock

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/qingtao/redlock/driver/redigo"
)

func createClients() []Conn {
	uri := `redis://:redis@127.0.0.1:6379/2`
	u, err := url.Parse(uri)
	if err != nil {
		panic(err)
	}
	opts := make([]redis.DialOption, 0)
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			panic(err)
		}
		opts = append(opts, redis.DialDatabase(n))
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
	return []Conn{redigo.NewClient(p)}
}

func TestMutex_tryLockOrExtend(t *testing.T) {
	cs := createClients()
	debug = &debugFlag{}
	t.Cleanup(func() {
		for _, c := range cs {
			debug = nil
			c.Close()
		}
	})
	type fields struct {
		name string
		opts *Options
	}
	type args struct {
		extend bool
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "Ok",
			fields: fields{
				name: "test-1",
				opts: &Options{
					timeout: 5 * time.Second,
					expires: 15 * time.Second,
					retries: 2,
					delay:   2 * time.Millisecond,
					conns:   cs,
				},
			},
			args:    args{extend: false},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMutex(tt.fields.name, tt.fields.opts)
			if err := m.tryLockOrExtend(context.TODO(), tt.args.extend); (err != nil) != tt.wantErr {
				t.Errorf("Mutex.tryLockOrExtend() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := m.Unlock(context.TODO()); err != nil {
				t.Errorf("Mutex.Unlock() error = %v", err)
			}
		})
	}
}
