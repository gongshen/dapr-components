package etcd

import (
	"context"
	"fmt"
	"github.com/dapr/components-contrib/nameresolution"
	"github.com/dapr/kit/logger"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type resolver struct {
	conf      *configSpec
	logger    logger.Logger
	cli       *clientv3.Client
	leaseId   clientv3.LeaseID
	keepAlive <-chan *clientv3.LeaseKeepAliveResponse
	key       string

	wch                endpoints.WatchChannel
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	deregisterListener net.Listener

	mu        sync.RWMutex
	newPicker func([]string) Picker
	pickers   map[string]Picker

	allUps map[string]*endpoints.Update
}

// NewResolver creates etcd name resolver.
func NewResolver(logger logger.Logger) nameresolution.Resolver {
	return &resolver{
		logger:  logger,
		pickers: make(map[string]Picker),
		allUps:  make(map[string]*endpoints.Update),
	}
}

// Init initializes Kubernetes name resolver.
func (k *resolver) Init(metadata nameresolution.Metadata) error {
	k.ctx, k.cancel = context.WithCancel(context.Background())
	conf, err := parseConfig(metadata.Configuration)
	if err != nil {
		return err
	}
	k.conf = conf
	cli, err := clientv3.New(clientv3.Config{
		Context:              k.ctx,
		Endpoints:            k.conf.Endpoints,
		DialTimeout:          time.Duration(k.conf.DialTimeout) * time.Second,
		DialKeepAliveTime:    time.Duration(k.conf.DialKeepAliveTime) * time.Second,
		DialKeepAliveTimeout: time.Duration(k.conf.DialKeepAliveTimeout) * time.Second,
		PermitWithoutStream:  true,
		DialOptions: []grpc.DialOption{
			grpc.WithBlock(),
			grpc.WithConnectParams(grpc.ConnectParams{
				Backoff: backoff.DefaultConfig,
			}),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to init etcd client: %w", err)
	}
	k.cli = cli
	resp, err := cli.Grant(k.ctx, k.conf.TTL)
	if err != nil {
		return err
	}
	k.leaseId = resp.ID
	manager, err := endpoints.NewManager(cli, k.conf.RegisterPrefix)
	if err != nil {
		return err
	}

	var (
		appID    string
		host     string
		httpPort string
		ok       bool
		daprPort string
	)

	if daprPort, ok = metadata.Properties[nameresolution.DaprPort]; !ok || daprPort == "" {
		return fmt.Errorf("metadata property missing: %s", nameresolution.DaprPort)
	}

	if appID, ok = metadata.Properties[nameresolution.AppID]; !ok {
		return fmt.Errorf("metadata property missing: %s", nameresolution.AppID)
	}

	if host, ok = metadata.Properties[nameresolution.HostAddress]; !ok {
		return fmt.Errorf("metadata property missing: %s", nameresolution.HostAddress)
	}

	if httpPort, ok = metadata.Properties[nameresolution.DaprHTTPPort]; !ok {
		return fmt.Errorf("metadata property missing: %s", nameresolution.DaprHTTPPort)
	} else if _, err = strconv.ParseUint(httpPort, 10, 0); err != nil {
		return fmt.Errorf("error parsing %s: %w", nameresolution.DaprHTTPPort, err)
	}
	var servicename string
	if k.conf.Namespace != "" {
		servicename = fmt.Sprintf("%s.%s", appID, k.conf.Namespace)
	} else {
		servicename = appID
	}
	key := fmt.Sprintf("%s/%s/%s:%s", k.conf.RegisterPrefix, servicename, host, daprPort)
	if err = manager.Update(k.ctx, []*endpoints.UpdateWithOpts{
		endpoints.NewAddUpdateOpts(key, endpoints.Endpoint{
			Addr: fmt.Sprintf("%s:%s", host, daprPort),
			Metadata: map[string]interface{}{
				"servicename": servicename,
			},
		}, clientv3.WithLease(k.leaseId)),
	}); err != nil {
		return err
	}
	k.key = key
	k.logger.Info("ETCD register success. key:", key)
	k.keepAlive, err = cli.KeepAlive(k.ctx, k.leaseId)
	if err != nil {
		return err
	}
	go k.keepalive()
	if k.conf.DeregisterAddr != "" {
		if err = k.StartDeregisterListener(); err != nil {
			return err
		}
	}

	k.wch, err = manager.NewWatchChannel(k.ctx)
	if err != nil {
		return err
	}
	// init picker
	switch k.conf.Picker {
	case randomPicker:
		k.newPicker = func(i []string) Picker {
			return newRPicker(i)
		}
	default:
		k.newPicker = func(i []string) Picker {
			return newRRPicker(i)
		}
	}
	k.watchOnce()
	k.wg.Add(1)
	go k.watch()
	return nil

}

func (k *resolver) keepalive() {
	for range k.keepAlive {
		// eat messages until keep alive channel closes
	}
}

func (k *resolver) ResolveID(req nameresolution.ResolveRequest) (string, error) {
	var servicename string
	if req.Namespace != "" {
		servicename = fmt.Sprintf("%s.%s", req.ID, req.Namespace)
	} else {
		servicename = req.ID
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	picker, ok := k.pickers[servicename]
	if !ok {
		return "", fmt.Errorf("no healthy services found with servicename '%s'", servicename)
	}
	addr := picker.Pick()
	if !ok {
		return "", fmt.Errorf("no healthy services found with servicename '%s'", servicename)
	}
	return addr, nil
}

func (k *resolver) Close() error {
	if k.cli != nil {
		ctx, cancel := context.WithTimeout(k.ctx, 3*time.Second)
		defer cancel()
		_, _ = k.cli.Revoke(ctx, k.leaseId)
		_ = k.cli.Close()
	}
	k.cancel()
	k.wg.Wait()
	if k.deregisterListener != nil {
		_ = k.deregisterListener.Close()
	}
	return nil
}

func (k *resolver) StartDeregisterListener() error {
	if l, err := net.Listen("tcp", k.conf.DeregisterAddr); err != nil {
		return err
	} else {
		k.deregisterListener = l
	}
	go http.Serve(k.deregisterListener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := k.cli.Delete(context.Background(), k.key, clientv3.WithIgnoreLease()); err != nil {
			k.logger.Error("delete Etcd key err:", err)
		}
	}))
	return nil
}

func (k *resolver) watchOnce() {
	select {
	case <-k.ctx.Done():
		return
	case ups, ok := <-k.wch:
		if !ok {
			return
		}
		for key, addrs := range k.watchHandler(ups) {
			k.pickers[key] = k.newPicker(addrs)
		}
	}
}

func (k *resolver) watchHandler(ups []*endpoints.Update) map[string][]string {
	upAppIDMap := make(map[string]bool)
	for _, up := range ups {
		switch up.Op {
		case endpoints.Add:
			k.allUps[up.Key] = up
		case endpoints.Delete:
			delete(k.allUps, up.Key)
		}
		arr := strings.Split(up.Key, "/")
		if len(arr) != 6 {
			k.logger.Error("invalid etcd key:", up.Key)
			continue
		}
		upAppIDMap[arr[4]] = true
	}
	ans := make(map[string][]string, len(upAppIDMap))
	for _, up := range k.allUps {
		arr := strings.Split(up.Key, "/")
		if len(arr) != 6 {
			k.logger.Error("invalid etcd key:", up.Key)
			continue
		}
		if !upAppIDMap[arr[4]] {
			continue
		}
		ans[arr[4]] = append(ans[arr[4]], up.Endpoint.Addr)
	}
	return ans
}

func (k *resolver) watch() {
	defer k.wg.Done()
	for {
		select {
		case <-k.ctx.Done():
			return
		case ups, ok := <-k.wch:
			if !ok {
				return
			}
			ans := k.watchHandler(ups)
			k.mu.Lock()
			for key, addrs := range ans {
				k.pickers[key] = k.newPicker(addrs)
			}
			k.mu.Unlock()
		}
	}
}
