package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/joyparty/gokit"
	"github.com/oklog/ulid/v2"
	"gitlab.haochang.tv/gopkg/nodehub/cluster"
	"gitlab.haochang.tv/gopkg/nodehub/logger"
	"gitlab.haochang.tv/gopkg/nodehub/proto/nh"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

const sizeLen = 4

var (
	bufPool = &sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}
)

// TCPAuthorizer tcp授权函数
type TCPAuthorizer func(sess Session) (userID string, md metadata.MD, ok bool)

// TCPServer tcp网关服务
type TCPServer struct {
	playground *Playground
	listenAddr string
	listener   net.Listener
	authorizer TCPAuthorizer
}

// NewTCPServer 构造函数
func NewTCPServer(playground *Playground, listenAddr string, authorizer TCPAuthorizer) *TCPServer {
	return &TCPServer{
		playground: playground,
		listenAddr: listenAddr,
		authorizer: authorizer,
	}
}

// Name 服务名称
func (ts *TCPServer) Name() string {
	return "tcpproxy"
}

// CompleteNodeEntry 补全节点信息
func (ts *TCPServer) CompleteNodeEntry(entry *cluster.NodeEntry) {
	entry.Entrance = fmt.Sprintf("tcp://%s", ts.listenAddr)
}

// Start 启动服务
func (ts *TCPServer) Start(ctx context.Context) error {
	l, err := net.Listen("tcp", ts.listenAddr)
	if err != nil {
		return fmt.Errorf("listen, %w", err)
	}

	go func() {
		for {
			conn, err := l.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			} else if err != nil {
				logger.Error("accept", err)
				continue
			}

			go ts.handle(conn)
		}
	}()

	return nil
}

// Stop 停止服务
func (ts *TCPServer) Stop(ctx context.Context) error {
	ts.listener.Close()
	ts.playground.Close()
	return nil
}

func (ts *TCPServer) handle(conn net.Conn) {
	sess, err := ts.newSession(conn)
	if err != nil {
		logger.Error("initialize session", "error", err, "remoteAddr", conn.RemoteAddr().String())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ts.playground.Handle(ctx, sess)
}

func (ts *TCPServer) newSession(conn net.Conn) (Session, error) {
	sess := newTCPSession(conn)

	userID, md, ok := ts.authorizer(sess)
	if !ok {
		return nil, fmt.Errorf("deny by authorizer")
	} else if userID == "" {
		return nil, fmt.Errorf("user id is empty")
	} else if md == nil {
		md = metadata.MD{}
	}

	sess.SetID(userID)
	sess.SetMetadata(md)
	return sess, nil
}

type tcpSession struct {
	id         string
	conn       net.Conn
	md         metadata.MD
	lastRWTime gokit.ValueOf[time.Time]
	closeOnce  sync.Once
}

func newTCPSession(conn net.Conn) *tcpSession {
	ts := &tcpSession{
		id:         ulid.Make().String(),
		conn:       conn,
		md:         metadata.New(nil),
		lastRWTime: gokit.NewValueOf[time.Time](),
	}
	ts.lastRWTime.Store(time.Now())

	return ts
}

func (ts *tcpSession) ID() string {
	return ts.id
}

func (ts *tcpSession) SetID(id string) {
	ts.id = id
}

func (ts *tcpSession) SetMetadata(md metadata.MD) {
	ts.md = md
}

func (ts *tcpSession) MetadataCopy() metadata.MD {
	return ts.md.Copy()
}

func (ts *tcpSession) Recv(req *nh.Request) (err error) {
	defer func() {
		if errors.Is(err, net.ErrClosed) {
			err = io.EOF
		}
	}()

	sizeFrame := make([]byte, sizeLen)

	for {
		if _, err := io.ReadFull(ts.conn, sizeFrame); err != nil {
			return fmt.Errorf("read size frame, %w", err)
		}

		size := int(binary.BigEndian.Uint32(sizeFrame))
		if size == 0 { // ping
			ts.lastRWTime.Store(time.Now())
			continue
		}

		data := make([]byte, size)
		if _, err := io.ReadFull(ts.conn, data); err != nil {
			return fmt.Errorf("read data frame, %w", err)
		}

		if err := proto.Unmarshal(data, req); err != nil {
			return fmt.Errorf("unmarshal request, %w", err)
		}
		ts.lastRWTime.Store(time.Now())
		return nil
	}
}

func (ts *tcpSession) Send(reply *nh.Reply) error {
	data, err := proto.Marshal(reply)
	if err != nil {
		return fmt.Errorf("marshal reply, %w", err)
	}

	buf := bufPool.Get().(*bytes.Buffer)
	defer bufPool.Put(buf)
	buf.Reset()

	if err := binary.Write(buf, binary.BigEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("write size frame, %w", err)
	} else if err := binary.Write(buf, binary.BigEndian, data); err != nil {
		return fmt.Errorf("write data frame, %w", err)
	}

	// ts.conn.SetWriteDeadline(time.Now().Add(writeWait))
	_, err = ts.conn.Write(buf.Bytes())
	if err == nil {
		ts.lastRWTime.Store(time.Now())
	}
	return err
}

func (ts *tcpSession) LocalAddr() string {
	return ts.conn.LocalAddr().String()
}

func (ts *tcpSession) RemoteAddr() string {
	return ts.conn.RemoteAddr().String()
}

func (ts *tcpSession) LastRWTime() time.Time {
	return ts.lastRWTime.Load()
}

func (ts *tcpSession) Close() (err error) {
	ts.closeOnce.Do(func() {
		err = ts.conn.Close()
	})
	return
}