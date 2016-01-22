package ezd

import (
	"errors"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/albertrdixon/gearbox/logger"
	"github.com/coreos/etcd/client"
)

var keyReadErr = errors.New("Key read error")

const (
	DefaultTimeout = 20 * time.Second
)

type Client interface {
	Exists(key string) error
	Keys(pre string) ([]string, error)
	Mkdir(path string) error

	Set(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

type etcd struct {
	client.KeysAPI
	*sync.Mutex
	timeout time.Duration
}

func New(endpoints []string, timeout time.Duration) (Client, error) {
	cl, er := client.New(client.Config{Endpoints: endpoints})
	if er != nil {
		return nil, er
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &etcd{
		KeysAPI: client.NewKeysAPI(cl),
		Mutex:   new(sync.Mutex),
		timeout: timeout,
	}, nil
}

func EnableDebug()               { client.EnablecURLDebug() }
func DisableDebug()              { client.DisablecURLDebug() }
func IsKeyNotFound(e error) bool { return client.IsKeyNotFound(e) }

func (s *etcd) Exists(key string) bool {
	_, e := s.Get(key)
	return e == nil
}

func (e *etcd) Keys(prefix string) ([]string, error) {
	var (
		c, q = context.WithTimeout(context.Background(), e.timeout)
		opts = &client.GetOptions{Sort: true, Quorum: true}
	)

	node, er := e.get(c, q, opts, prefix)
	if er != nil {
		return nil, er
	}
	logger.Debugf("From etcd: %v", node)
	return extractKeys(node)
}

func (e *etcd) Get(key string) (string, error) {
	var (
		c, q = context.WithTimeout(context.Background(), e.timeout)
		opts = &client.GetOptions{Quorum: true}
	)

	logger.Debugf("[etcd] GET %q", key)
	node, er := e.get(c, q, opts, key)
	if er != nil {
		return "", er
	}
	logger.Debugf("From etcd: %v", node)
	return extractNodeValue(node)
}

func (e *etcd) Set(key, value string) error {
	var (
		c, q = context.WithTimeout(context.Background(), e.timeout)
		o    = &client.SetOptions{PrevExist: client.PrevIgnore, TTL: 0}
	)

	logger.Debugf("[etcd] SET %q %q", key, value)
	return e.set(c, q, o, key, value)
}

func (e *etcd) Mkdir(path string) error {
	var (
		c, q = context.WithTimeout(context.Background(), e.timeout)
		o    = &client.SetOptions{PrevExist: client.PrevNoExist, Dir: true}
	)

	logger.Debugf("[etcd] MKDIR %q", path)
	return e.set(c, q, o, path, "")
}

func (e *etcd) Delete(key string) error {
	var (
		c, q = context.WithTimeout(context.Background(), e.timeout)
		o    = &client.DeleteOptions{Recursive: true}
	)

	logger.Debugf("[etcd] DELETE %q", key)
	return e.del(c, q, o, key)
}

func (e *etcd) get(c context.Context, fn context.CancelFunc, o *client.GetOptions, k string) (*client.Node, error) {
	e.Lock()
	defer e.Unlock()
	defer fn()

	if resp, er := e.KeysAPI.Get(c, k, o); er == nil {
		return resp.Node, nil
	} else {
		return nil, er
	}
}

func (e *etcd) set(c context.Context, fn context.CancelFunc, o *client.SetOptions, k, v string) (er error) {
	e.Lock()
	defer e.Unlock()
	defer fn()

	_, er = e.KeysAPI.Set(c, k, v, o)
	return
}

func (e *etcd) del(c context.Context, fn context.CancelFunc, o *client.DeleteOptions, k string) (er error) {
	e.Lock()
	defer e.Unlock()
	defer fn()

	_, er = e.KeysAPI.Delete(c, k, o)
	return
}

func extractNodeValue(node *client.Node) (string, error) {
	if node == nil {
		return "", KeyReadErr
	}
	return node.Value, nil
}

func extractKeys(node *client.Node) ([]string, error) {
	if node == nil {
		return nil, KeyReadErr
	}
	if !node.Dir {
		return []string{}, nil
	}
	keys := make([]string, 0, len(node.Nodes))
	for _, n := range node.Nodes {
		keys = append(keys, n.Value)
	}
	return keys, nil
}
