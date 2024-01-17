package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joyparty/gokit"
	"github.com/oklog/ulid/v2"
	"github.com/panjf2000/ants/v2"
	"gitlab.haochang.tv/gopkg/nodehub/cluster"
	"gitlab.haochang.tv/gopkg/nodehub/component/rpc"
	"gitlab.haochang.tv/gopkg/nodehub/event"
	"gitlab.haochang.tv/gopkg/nodehub/logger"
	"gitlab.haochang.tv/gopkg/nodehub/multicast"
	"gitlab.haochang.tv/gopkg/nodehub/proto/nh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	requestPool = &sync.Pool{
		New: func() any {
			return &nh.Request{}
		},
	}

	replyPool = &sync.Pool{
		New: func() any {
			return &nh.Reply{}
		},
	}
)

// Session 连接会话
type Session interface {
	ID() string
	SetID(string)
	SetMetadata(metadata.MD)
	MetadataCopy() metadata.MD
	Recv(*nh.Request) error
	Send(*nh.Reply) error
	LocalAddr() string
	RemoteAddr() string
	LastRWTime() time.Time
	Close() error
}

// sessionHub 会话集合
type sessionHub struct {
	sessions *gokit.MapOf[string, Session]
	count    *atomic.Int32
	done     chan struct{}
	closed   *atomic.Bool
}

func newSessionHub() *sessionHub {
	hub := &sessionHub{
		sessions: gokit.NewMapOf[string, Session](),
		count:    &atomic.Int32{},
		done:     make(chan struct{}),
		closed:   &atomic.Bool{},
	}

	go hub.removeZombie()
	return hub
}

func (h *sessionHub) Count() int32 {
	return h.count.Load()
}

func (h *sessionHub) Store(c Session) {
	h.sessions.Store(c.ID(), c)
	h.count.Add(1)
}

func (h *sessionHub) Load(id string) (Session, bool) {
	if c, ok := h.sessions.Load(id); ok {
		return c.(Session), true
	}
	return nil, false
}

func (h *sessionHub) Delete(id string) {
	if _, ok := h.sessions.Load(id); ok {
		h.sessions.Delete(id)
		h.count.Add(-1)
	}
}

func (h *sessionHub) Range(f func(s Session) bool) {
	h.sessions.Range(func(_ string, value Session) bool {
		return f(value)
	})
}

func (h *sessionHub) Close() {
	if h.closed.CompareAndSwap(false, true) {
		close(h.done)

		h.Range(func(s Session) bool {
			s.Close()

			h.Delete(s.ID())
			return true
		})
	}
}

// 定时移除心跳超时的客户端
func (h *sessionHub) removeZombie() {
	for {
		select {
		case <-h.done:
			return
		case <-time.After(10 * time.Second):
			h.Range(func(s Session) bool {
				if time.Since(s.LastRWTime()) > DefaultHeartbeatTimeout {
					h.Delete(s.ID())
					s.Close()
				}
				return true
			})
		}
	}
}

// Playground 客户端会话运行环境
type Playground struct {
	nodeID        ulid.ULID
	registry      *cluster.Registry
	sessions      *sessionHub
	eventBus      *event.Bus
	multicast     multicast.Subscriber
	stateTable    *stateTable
	cleanJobs     *gokit.MapOf[string, *time.Timer]
	requestLogger logger.Logger
	done          chan struct{}

	requestInterceptor    RequestInterceptor
	connectInterceptor    ConnectInterceptor
	disconnectInterceptor DisconnectInterceptor
}

// NewPlayground 构造函数
func NewPlayground(nodeID ulid.ULID, registry *cluster.Registry, opt ...Option) *Playground {
	p := &Playground{
		nodeID:     nodeID,
		registry:   registry,
		sessions:   newSessionHub(),
		stateTable: newStateTable(),
		cleanJobs:  gokit.NewMapOf[string, *time.Timer](),
		done:       make(chan struct{}),

		requestInterceptor:    defaultRequestInterceptor,
		connectInterceptor:    defaultConnectInterceptor,
		disconnectInterceptor: defaultDisconnectInterceptor,
	}

	for _, fn := range opt {
		fn(p)
	}

	p.init(context.Background())
	return p
}

func (p *Playground) init(ctx context.Context) {
	// 有状态路由更新
	if p.eventBus != nil {
		p.eventBus.Subscribe(ctx, func(ev event.NodeAssign, _ time.Time) {
			if _, ok := p.sessions.Load(ev.SessionID); ok {
				p.stateTable.Store(ev.SessionID, ev.ServiceCode, ev.NodeID)
			}
		})
		p.eventBus.Subscribe(ctx, func(ev event.NodeUnassign, _ time.Time) {
			p.stateTable.Remove(ev.SessionID, ev.ServiceCode)
		})
	}

	// 处理主动下行消息
	if p.multicast != nil {
		p.multicast.Subscribe(ctx, func(msg *nh.Multicast) {
			// 只发送5分钟内的消息
			if time.Since(msg.GetTime().AsTime()) <= 5*time.Minute {
				for _, sessID := range msg.GetReceiver() {
					if sess, ok := p.sessions.Load(sessID); ok {
						ants.Submit(func() {
							sess.Send(msg.Content)
						})
					}
				}
			}
		})
	}

	p.registry.SubscribeDelete(func(entry cluster.NodeEntry) {
		p.stateTable.CleanNode(entry.ID)
	})
}

// NewGRPCService 网关管理服务
func (p *Playground) NewGRPCService() nh.GatewayServer {
	return &gwService{
		sessionHub: p.sessions,
		stateTable: p.stateTable,
	}
}

// Handle 处理客户端连接
func (p *Playground) Handle(ctx context.Context, sess Session) {
	if err := p.onConnect(ctx, sess); err != nil {
		logger.Error("on connect", "error", err, "sessionID", sess.ID(), "remoteAddr", sess.RemoteAddr())
		return
	}

	p.sessions.Store(sess)
	defer p.onDisconnect(ctx, sess)

	reqC := make(chan requestTask)
	defer close(reqC)
	go p.runPipeline(ctx, reqC)

	requestHandler := func(ctx context.Context, sess Session, req *nh.Request) {
		exec, pipeline := p.buildRequest(ctx, sess, req)
		fn := func() {
			exec()
			requestPool.Put(req)
		}

		if pipeline == "" {
			// 允许无序执行，并发处理
			ants.Submit(fn)
		} else {
			// 需要保证时序性，投递到队列处理
			select {
			case <-p.done:
				return
			case reqC <- requestTask{
				Pipeline: pipeline,
				Request:  fn,
			}:
			}
		}
	}

	for {
		select {
		case <-p.done:
			return
		default:
		}

		req := requestPool.Get().(*nh.Request)
		nh.ResetRequest(req)

		if err := sess.Recv(req); err != nil {
			requestPool.Put(req)

			if !errors.Is(err, io.EOF) {
				logger.Error("recv request", "error", err, "sessionID", sess.ID(), "remoteAddr", sess.RemoteAddr())
			}

			return
		}

		p.requestInterceptor(ctx, sess, req, requestHandler)
	}
}

func (p *Playground) onConnect(ctx context.Context, sess Session) error {
	if err := p.connectInterceptor(ctx, sess); err != nil {
		return err
	}

	md := sess.MetadataCopy()
	md.Set(rpc.MDSessID, sess.ID())
	md.Set(rpc.MDGateway, p.nodeID.String())
	sess.SetMetadata(md)

	// 放弃之前断线创造的清理任务
	if timer, ok := p.cleanJobs.Load(sess.ID()); ok {
		if !timer.Stop() {
			<-timer.C
		}
		p.cleanJobs.Delete(sess.ID())
	}

	if p.eventBus != nil {
		if err := p.eventBus.Publish(ctx, event.UserConnected{
			SessionID: sess.ID(),
			GatewayID: p.nodeID.String(),
		}); err != nil {
			return fmt.Errorf("publish event, %w", err)
		}
	}
	return nil
}

func (p *Playground) onDisconnect(ctx context.Context, sess Session) {
	p.disconnectInterceptor(ctx, sess)

	p.sessions.Delete(sess.ID())

	if p.eventBus != nil {
		p.eventBus.Publish(ctx, event.UserDisconnected{
			SessionID: sess.ID(),
			GatewayID: p.nodeID.String(),
		})
	}

	// 延迟5分钟之后，确认session不存在了，则清除相关数据
	p.cleanJobs.Store(sess.ID(), time.AfterFunc(5*time.Minute, func() {
		if _, ok := p.sessions.Load(sess.ID()); !ok {
			p.stateTable.CleanSession(sess.ID())
		}
		p.cleanJobs.Delete(sess.ID())
	}))

	sess.Close()
}

// Close 关闭服务
func (p *Playground) Close() {
	close(p.done)
	p.sessions.Close()
}

func (p *Playground) buildRequest(ctx context.Context, sess Session, req *nh.Request) (exec func(), pipeline string) {
	// 以status.Error()构造的错误，都会被下行通知到客户端
	var err error
	desc, ok := p.registry.GetGRPCDesc(req.ServiceCode)
	if !ok {
		err = status.Errorf(codes.NotFound, "service %d not found", req.ServiceCode)
	} else if !desc.Public {
		err = status.Errorf(codes.PermissionDenied, "request private service")
	}

	var (
		doRequest func() error
		start     = time.Now()
	)
	if err != nil {
		pipeline = ""

		doRequest = func() error {
			p.logRequest(ctx, sess, req, start, nil, err)
			return err
		}
	} else {
		pipeline = desc.Pipeline

		doRequest = func() (err error) {
			var conn *grpc.ClientConn
			defer func() {
				p.logRequest(ctx, sess, req, start, conn, err)
			}()

			conn, err = p.getUpstream(sess, req, desc)
			if err != nil {
				return
			}

			input, err := newEmptyMessage(req.Data)
			if err != nil {
				return fmt.Errorf("unmarshal request data: %w", err)
			}

			output := replyPool.Get().(*nh.Reply)
			defer replyPool.Put(output)
			nh.ResetReply(output)

			md := sess.MetadataCopy()
			md.Set(rpc.MDTransactionID, ulid.Make().String())
			ctx = metadata.NewOutgoingContext(ctx, md)

			method := path.Join(desc.Path, req.Method)
			if err = grpc.Invoke(ctx, method, input, output, conn); err != nil {
				return fmt.Errorf("invoke service: %w", err)
			}

			if req.GetNoReply() {
				return nil
			}

			output.RequestId = req.GetId()
			output.FromService = req.GetServiceCode()
			return sess.Send(output)
		}
	}

	exec = func() {
		if err := doRequest(); err != nil {
			if s, ok := status.FromError(err); ok {
				// unknown错误，不下行详细的错误描述，避免泄露信息到客户端
				if s.Code() == codes.Unknown {
					s = status.New(codes.Unknown, "unknown error")
				}

				reply, _ := nh.NewReply(int32(nh.Protocol_RPC_ERROR), &nh.RPCError{
					RequestService: req.GetServiceCode(),
					RequestMethod:  req.GetMethod(),
					Status:         s.Proto(),
				})
				reply.RequestId = req.GetId()

				sess.Send(reply)
			}
		}
	}

	return
}

func (p *Playground) getUpstream(sess Session, req *nh.Request, desc cluster.GRPCServiceDesc) (conn *grpc.ClientConn, err error) {
	var nodeID ulid.ULID
	// 无状态服务，根据负载均衡策略选择一个节点发送
	if !desc.Stateful {
		nodeID, err = p.registry.AllocGRPCNode(req.ServiceCode, sess)
		if err != nil {
			err = status.Errorf(codes.Unavailable, "pick grpc node, %v", err)
			return
		}

		goto FINISH
	}

	if desc.Allocation == cluster.ClientAllocate {
		// 每次客户端指定了节点，记录下来，后续使用
		if v := req.GetNodeId(); v != "" {
			nodeID, _ = ulid.Parse(v)
			defer func() {
				if err == nil {
					p.stateTable.Store(sess.ID(), req.ServiceCode, nodeID)
				}
			}()
			goto FINISH
		}
	}

	// 从状态路由表查询节点ID
	if v, ok := p.stateTable.Find(sess.ID(), req.ServiceCode); ok {
		nodeID = v
		goto FINISH
	}

	// 非自动分配策略，没有找到节点就中断请求
	if desc.Allocation != cluster.AutoAllocate && nodeID.Time() == 0 {
		err = status.Error(codes.PermissionDenied, "no node allocated")
		return
	}

	// 自动分配策略，根据负载均衡策略选择一个节点发送
	nodeID, err = p.registry.AllocGRPCNode(req.ServiceCode, sess)
	if err != nil {
		err = status.Errorf(codes.Unavailable, "pick grpc node, %v", err)
		return
	}
	defer func() {
		if err == nil {
			p.stateTable.Store(sess.ID(), req.ServiceCode, nodeID)
		}
	}()

FINISH:
	conn, err = p.registry.GetGRPCConn(nodeID)
	if err != nil {
		err = status.Errorf(codes.Unavailable, "get grpc conn, %v", err)
	}
	return
}

func (p *Playground) logRequest(
	ctx context.Context,
	sess Session,
	req *nh.Request,
	start time.Time,
	upstream *grpc.ClientConn,
	err error,
) {
	if err == nil && p.requestLogger == nil {
		return
	}

	logValues := []any{
		"reqID", req.Id,
		"sessID", sess.ID(),
		"remoteAddr", sess.RemoteAddr(),
		"serviceCode", req.ServiceCode,
		"method", req.Method,
		"duration", time.Since(start).String(),
	}

	if nodeID := req.GetNodeId(); nodeID != "" {
		logValues = append(logValues, "nodeID", nodeID)
	}

	if upstream != nil {
		logValues = append(logValues, "upstream", upstream.Target())
	}

	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if v := md.Get(rpc.MDTransactionID); len(v) > 0 {
			logValues = append(logValues, "transID", v[0])
		}
	}

	if err != nil {
		logValues = append(logValues, "error", err)

		if p.requestLogger == nil {
			logger.Error("handle request", logValues...)
		} else {
			p.requestLogger.Error("handle request", logValues...)
		}
	} else {
		p.requestLogger.Info("handle request", logValues...)
	}
}

type requestTask struct {
	Pipeline string
	Request  func()
}

// 把每个service的请求分发到不同的worker处理
// 确保对同一个service的请求是顺序处理的
func (p *Playground) runPipeline(ctx context.Context, reqC <-chan requestTask) {
	type worker struct {
		C          chan func()
		ActiveTime time.Time
	}

	workers := map[string]*worker{}
	defer func() {
		for _, w := range workers {
			close(w.C)
		}
	}()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 清除不活跃的worker
			for key, w := range workers {
				if time.Since(w.ActiveTime) > 5*time.Minute {
					close(w.C)
					delete(workers, key)
				}
			}
		case task, ok := <-reqC:
			if !ok {
				return
			}

			w, ok := workers[task.Pipeline]
			if !ok {
				w = &worker{
					C: make(chan func(), 100),
				}
				workers[task.Pipeline] = w

				go func() {
					for fn := range w.C {
						fn() // 错误会被打印到请求日志中，这里就不需要再处理
					}
				}()
			}

			w.C <- task.Request
			w.ActiveTime = time.Now()
		}
	}
}

// Option 网关配置选项
type Option func(p *Playground)

// RequestHandler 请求处理函数
type RequestHandler func(ctx context.Context, sess Session, req *nh.Request)

// RequestInterceptor 请求拦截器
//
// 拦截器提供了请求过程中注入自定义钩子的机制，拦截器需要在调用过程中执行handler函数来完成请求流程
type RequestInterceptor func(ctx context.Context, sess Session, req *nh.Request, next RequestHandler)

var defaultRequestInterceptor = func(ctx context.Context, sess Session, req *nh.Request, next RequestHandler) {
	next(ctx, sess, req)
}

// WithRequestInterceptor 设置请求拦截器
func WithRequestInterceptor(interceptor RequestInterceptor) Option {
	return func(p *Playground) {
		p.requestInterceptor = interceptor
	}
}

// ConnectInterceptor 在连接创建之后执行自定义操作，返回错误会中断连接
type ConnectInterceptor func(ctx context.Context, sess Session) error

var defaultConnectInterceptor = func(ctx context.Context, sess Session) error {
	return nil
}

// WithConnectInterceptor 设置连接拦截器
func WithConnectInterceptor(interceptor ConnectInterceptor) Option {
	return func(p *Playground) {
		p.connectInterceptor = interceptor
	}
}

// DisconnectInterceptor 在连接断开前执行自定操作
type DisconnectInterceptor func(ctx context.Context, sess Session)

var defaultDisconnectInterceptor = func(ctx context.Context, sess Session) {}

// WithDisconnectInterceptor 设置断开连接拦截器
func WithDisconnectInterceptor(interceptor DisconnectInterceptor) Option {
	return func(p *Playground) {
		p.disconnectInterceptor = interceptor
	}
}

// WithEventBus 设置事件总线
func WithEventBus(bus *event.Bus) Option {
	return func(p *Playground) {
		p.eventBus = bus
	}
}

// WithMulticast 设置广播组件
func WithMulticast(multicast multicast.Subscriber) Option {
	return func(p *Playground) {
		p.multicast = multicast
	}
}

// WithRequestLogger 设置请求日志记录器
func WithRequestLogger(logger logger.Logger) Option {
	return func(p *Playground) {
		p.requestLogger = logger
	}
}

func newEmptyMessage(data []byte) (msg *emptypb.Empty, err error) {
	msg = &emptypb.Empty{}
	if len(data) > 0 {
		err = proto.Unmarshal(data, msg)
	}
	return
}