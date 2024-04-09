package gateway

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/joyparty/gokit"
	"github.com/joyparty/nodehub/cluster"
	"github.com/joyparty/nodehub/logger"
	"github.com/joyparty/nodehub/proto/nh"
	"github.com/oklog/ulid/v2"
	"github.com/panjf2000/ants/v2"
	"github.com/quic-go/quic-go"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

var (
	_ Session     = &quicSession{}
	_ Transporter = &quicServer{}
)

type quicServer struct {
	listenAddr     string
	tlsConfig      *tls.Config
	quicConfig     *quic.Config
	listener       *quic.Listener
	authorizer     Authorizer
	sessionHandler SessionHandler
}

// NewQUICServer 构造函数
func NewQUICServer(listenAddr string, authorizer Authorizer, tlsConfig *tls.Config, quicConfig *quic.Config) Transporter {
	return &quicServer{
		listenAddr: listenAddr,
		tlsConfig:  tlsConfig,
		quicConfig: quicConfig,
		authorizer: authorizer,
	}
}

func (qs *quicServer) CompleteNodeEntry(entry *cluster.NodeEntry) {
	entry.Entrance = fmt.Sprintf("quic://%s", qs.listenAddr)
}

func (qs *quicServer) SetSessionHandler(handler SessionHandler) {
	qs.sessionHandler = handler
}

func (qs *quicServer) Start(ctx context.Context) error {
	l, err := quic.ListenAddr(qs.listenAddr, qs.tlsConfig, qs.quicConfig)
	if err != nil {
		return fmt.Errorf("listen, %w", err)
	}
	qs.listener = l

	go func() {
		for {
			conn, err := l.Accept(context.Background())
			if err != nil {
				logger.Error("quic accept", "error", err)
				return
			}

			if err := ants.Submit(func() {
				qs.handleConn(ctx, conn)
			}); err != nil {
				logger.Error("handle quic connection", "error", err, "remoteAddr", conn.RemoteAddr().String())
				_ = conn.CloseWithError(quic.ApplicationErrorCode(quic.InternalError), "") // TODO: close with error code
			}
		}
	}()

	return nil
}

func (qs *quicServer) Stop(_ context.Context) error {
	return qs.listener.Close()
}

func (qs *quicServer) handleConn(ctx context.Context, conn quic.Connection) {
	sess, err := qs.newSession(ctx, conn)
	if err != nil {
		logger.Error("initialize quic session", "error", err, "remoteAddr", conn.RemoteAddr().String())

		if errors.Is(err, ErrDenyByAuthorizer) {
			_ = conn.CloseWithError(quic.ApplicationErrorCode(quic.ConnectionRefused), err.Error())
		} else {
			_ = conn.CloseWithError(quic.ApplicationErrorCode(quic.InternalError), "")
		}
		return
	}

	qs.sessionHandler(ctx, sess)
}

func (qs *quicServer) newSession(ctx context.Context, conn quic.Connection) (Session, error) {
	sess := newQuicSession(conn)

	userID, md, ok := qs.authorizer(ctx, sess)
	if !ok {
		return nil, ErrDenyByAuthorizer
	} else if userID == "" {
		return nil, fmt.Errorf("user id is empty")
	} else if md == nil {
		md = metadata.MD{}
	}

	sess.SetID(userID)
	sess.SetMetadata(md)
	return sess, nil
}

type quicSession struct {
	id         string
	conn       quic.Connection
	streams    *quicStreams
	reqC       chan []byte
	md         metadata.MD
	lastRWTime gokit.ValueOf[time.Time]
	closeOnce  sync.Once
	done       chan struct{}
}

func newQuicSession(conn quic.Connection) *quicSession {
	qs := &quicSession{
		id:         ulid.Make().String(),
		conn:       conn,
		streams:    newQuicStreams(),
		md:         metadata.New(nil),
		lastRWTime: gokit.NewValueOf[time.Time](),
		done:       make(chan struct{}),

		reqC: make(chan []byte),
	}
	qs.lastRWTime.Store(time.Now())

	go qs.handleRequest()
	return qs
}

func (qs *quicSession) ID() string {
	return qs.id
}

func (qs *quicSession) SetID(id string) {
	qs.id = id
}

func (qs *quicSession) SetMetadata(md metadata.MD) {
	qs.md = md
}

func (qs *quicSession) MetadataCopy() metadata.MD {
	return qs.md.Copy()
}

func (qs *quicSession) LocalAddr() string {
	return qs.conn.LocalAddr().String()
}

func (qs *quicSession) RemoteAddr() string {
	return qs.conn.RemoteAddr().String()
}

func (qs *quicSession) LastRWTime() time.Time {
	return qs.lastRWTime.Load()
}

func (qs *quicSession) Close() error {
	qs.closeOnce.Do(func() {
		close(qs.done)
		close(qs.reqC)

		qs.streams.CloseAll()
		_ = qs.conn.CloseWithError(0, "")
	})
	return nil
}

func (qs *quicSession) handleRequest() {
	for {
		s, err := qs.conn.AcceptStream(context.Background())
		if err != nil {
			logger.Error("accept quic stream", "error", err, "remoteAddr", qs.RemoteAddr())
			_ = qs.Close()
			return
		}
		qs.streams.Append(s)

		go func() (err error) {
			defer func() {
				if err != nil {
					logger.Error("handle quic stream", "error", err, "remoteAddr", qs.RemoteAddr())
				}

				s.CancelRead(0)
				qs.streams.Remove(s)

				// 如果所有stream都关闭了则关闭整个连接
				if qs.streams.Len() == 0 {
					_ = qs.Close()
				}
			}()

			for {
				select {
				case <-qs.done:
					return
				default:
					sizeFrame := make([]byte, sizeLen)
					if _, err := io.ReadFull(s, sizeFrame); err != nil {
						return fmt.Errorf("read size frame, %w", err)
					}

					size := int(binary.BigEndian.Uint32(sizeFrame))
					if size == 0 { // ping
						qs.lastRWTime.Store(time.Now())
						continue
					} else if size > MaxPayloadSize {
						return fmt.Errorf("payload size exceeds the limit, %d", size)
					}

					payload := make([]byte, size)
					if _, err := io.ReadFull(s, payload); err != nil {
						return fmt.Errorf("read data frame, %w", err)
					}

					qs.lastRWTime.Store(time.Now())
					qs.reqC <- payload
				}
			}
		}()
	}
}

func (qs *quicSession) Recv(req *nh.Request) error {
	payload, ok := <-qs.reqC
	if !ok {
		return fmt.Errorf("payload channel closed")
	} else if err := proto.Unmarshal(payload, req); err != nil {
		return fmt.Errorf("unmarshal request, %w", err)
	}
	return nil
}

func (qs *quicSession) Send(reply *nh.Reply) error {
	s, ok := qs.streams.Pick(reply.FromService)
	if !ok {
		return errors.New("no available stream")
	}

	return sendBy(reply, func(data []byte) error {
		s.SetWriteDeadline(time.Now().Add(writeWait))
		_, err := s.Write(data)
		return err
	})
}

type quicStreams struct {
	ss []quic.Stream
	l  sync.RWMutex
}

func newQuicStreams() *quicStreams {
	return &quicStreams{
		ss: []quic.Stream{},
	}
}

func (qs *quicStreams) Len() int {
	qs.l.RLock()
	defer qs.l.RUnlock()

	return len(qs.ss)
}

func (qs *quicStreams) Append(s quic.Stream) {
	qs.l.Lock()
	qs.ss = append(qs.ss, s)
	qs.l.Unlock()
}

func (qs *quicStreams) Remove(s quic.Stream) {
	qs.l.Lock()
	defer qs.l.Unlock()

	qs.ss = slices.DeleteFunc(qs.ss, func(v quic.Stream) bool {
		return s.StreamID() == v.StreamID()
	})
}

// Pick 根据service code分配，按id hash，确保同一个服务的下行消息都通过同一个stream下发
func (qs *quicStreams) Pick(serviceCode int32) (quic.Stream, bool) {
	qs.l.RLock()
	defer qs.l.RUnlock()

	if l := len(qs.ss); l == 0 {
		return nil, false
	} else if l == 1 {
		return qs.ss[0], true
	}

	return qs.ss[int(serviceCode)%len(qs.ss)], true
}

func (qs *quicStreams) CloseAll() {
	qs.l.Lock()
	defer qs.l.Unlock()

	for _, s := range qs.ss {
		_ = s.Close()
	}
}
