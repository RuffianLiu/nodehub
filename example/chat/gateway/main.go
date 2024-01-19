package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/joyparty/gokit"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"gitlab.haochang.tv/gopkg/nodehub"
	"gitlab.haochang.tv/gopkg/nodehub/cluster"
	"gitlab.haochang.tv/gopkg/nodehub/component/gateway"
	"gitlab.haochang.tv/gopkg/nodehub/event"
	"gitlab.haochang.tv/gopkg/nodehub/logger"
	"gitlab.haochang.tv/gopkg/nodehub/multicast"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/metadata"
)

var (
	registry *cluster.Registry

	proxyListen string
	grpcListen  string
	redisAddr   string
	natsAddr    string
	useTCP      bool
)

func init() {
	flag.StringVar(&proxyListen, "proxy", "127.0.0.1:9000", "proxy listen address")
	flag.StringVar(&grpcListen, "grpc", "127.0.0.1:10000", "grpc listen address")
	flag.StringVar(&redisAddr, "redis", "127.0.0.1:6379", "redis address")
	flag.StringVar(&natsAddr, "nats", "127.0.0.1:4222", "nats address")
	flag.BoolVar(&useTCP, "tcp", false, "use tcp")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	logger.SetLogger(slog.Default())

	client := mustReturn(clientv3.New(clientv3.Config{
		Endpoints: []string{"127.0.0.1:2379"},
	}))

	registry = mustReturn(cluster.NewRegistry(client))
}

func main() {
	var transporter gateway.Transporter
	uid := &atomic.Int32{}
	if useTCP {
		transporter = gateway.NewTCPServer(
			proxyListen,
			func(sess gateway.Session) (userID string, md metadata.MD, ok bool) {
				userID = fmt.Sprintf("%d", uid.Add(1))
				md = metadata.MD{}
				ok = true
				return
			},
		)
	} else {
		transporter = gateway.NewWSServer(
			proxyListen,
			func(w http.ResponseWriter, r *http.Request) (userID string, md metadata.MD, ok bool) {
				userID = fmt.Sprintf("%d", uid.Add(1))
				md = metadata.MD{}
				ok = true
				return
			},
		)
	}

	// evBus, muBus := newRedisBus()
	evBus, muBus := newNatsBus()

	gwConfig := nodehub.GatewayConfig{
		Options: []gateway.Option{
			gateway.WithTransporter(transporter),
			gateway.WithRequestLogger(slog.Default()),
			gateway.WithEventBus(evBus),
			gateway.WithMulticast(muBus),
		},

		GRPCListen: grpcListen,
	}

	node := nodehub.NewGatewayNode(registry, gwConfig)

	logger.Info("gateway server start", "listen", proxyListen)
	mustDo(node.Serve(context.Background()))
}

func mustDo(err error) {
	if err != nil {
		panic(err)
	}
}

func mustReturn[T any](t T, er error) T {
	if er != nil {
		panic(er)
	}
	return t
}

func newRedisBus() (*event.Bus, *multicast.Bus) {
	client := redis.NewClient(&redis.Options{
		Network: "tcp",
		Addr:    redisAddr,
	})
	return event.NewRedisBus(client), multicast.NewRedisBus(client)
}

func newNatsBus() (*event.Bus, *multicast.Bus) {
	dsn := fmt.Sprintf("nats://%s", natsAddr)
	conn := gokit.MustReturn(nats.Connect(dsn))
	return event.NewNatsBus(conn), multicast.NewNatsBus(conn)
}
