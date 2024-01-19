package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/joyparty/gokit"
	"gitlab.haochang.tv/gopkg/nodehub/component/gateway"
	"gitlab.haochang.tv/gopkg/nodehub/example/echo/proto/clusterpb"
	"gitlab.haochang.tv/gopkg/nodehub/example/echo/proto/echopb"
	"gitlab.haochang.tv/gopkg/nodehub/proto/nh"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

var (
	serverAddr      string
	echoServiceCode = int32(clusterpb.Services_ECHO)
)

func init() {
	flag.StringVar(&serverAddr, "server", "127.0.0.1:9000", "server address")
	flag.Parse()
}

func main() {
	endpoint := fmt.Sprintf("ws://%s/grpc", serverAddr)
	client := &echoClient{
		Client: gokit.MustReturn(gateway.NewClient(endpoint)),
	}
	defer client.Close()

	client.Client.SetDefaultHandler(func(resp *nh.Reply) {
		fmt.Printf("[%s] response: %s\n", time.Now().Format(time.RFC3339), resp.String())
	})

	client.Client.OnReceive(0, int32(nh.Protocol_RPC_ERROR), func(requestID uint32, reply *nh.RPCError) {
		fmt.Printf("[%s] #%03d ERROR, call %d.%s(), code = %s, message = %s\n",
			time.Now().Format(time.RFC3339),
			requestID,
			reply.GetRequestService(),
			reply.GetRequestMethod(),
			codes.Code(reply.Status.Code),
			reply.Status.Message,
		)
		os.Exit(1)
	})

	client.OnReceive(int32(echopb.Protocol_MSG), func(requestID uint32, reply *echopb.Msg) {
		fmt.Printf("[%s] #%03d receive: %s\n", time.Now().Format(time.RFC3339), requestID, reply.Message)
	})

	for {
		gokit.Must(
			client.Call("Send", &echopb.Msg{
				Message: "hello world!",
			}),
		)
		time.Sleep(1 * time.Second)
	}
}

type echoClient struct {
	*gateway.Client
}

func (c *echoClient) Call(method string, arg proto.Message, options ...gateway.CallOption) error {
	return c.Client.Call(echoServiceCode, method, arg, options...)
}

func (c *echoClient) OnReceive(messageType int32, handler any) {
	c.Client.OnReceive(echoServiceCode, messageType, handler)
}
