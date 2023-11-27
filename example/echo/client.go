package main

import (
	"flag"
	"fmt"
	serverpb "nodehub/example/echo/proto/server"
	pb "nodehub/example/echo/proto/server/echo"
	"nodehub/proto/client"
	clientpb "nodehub/proto/client"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

var (
	requestID  = &atomic.Uint32{}
	serverAddr string
)

func init() {
	flag.StringVar(&serverAddr, "server", "127.0.0.1:9000", "server address")
	flag.Parse()
}

func main() {
	endpoint := fmt.Sprintf("ws://%s/proxy", serverAddr)
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		panic(err)
	}

	req, err := newRequest(&pb.Msg{
		Message: "hello world!",
	})
	if err != nil {
		panic(err)
	}
	req.ServiceCode = int32(serverpb.Services_ECHO)
	req.Method = "Send"

	data, err := proto.Marshal(req)
	if err != nil {
		panic(err)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		panic(err)
	}

	messageType, data, err := conn.ReadMessage()
	if err != nil {
		panic(err)
	}

	if messageType != websocket.BinaryMessage {
		panic(fmt.Errorf("invalid message type, %d", messageType))
	}

	response := &client.Response{}
	if err := proto.Unmarshal(data, response); err != nil {
		panic(err)
	}
	fmt.Println(response.String())
}

func newRequest(message proto.Message) (*clientpb.Request, error) {
	data, err := proto.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal message, %w", err)
	}

	return &clientpb.Request{
		Id:   requestID.Add(1),
		Data: data,
	}, nil
}
