package nh

import (
	"log/slog"
	"reflect"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var replyTypes = map[[2]int32]reflect.Type{}

// RegisterReplyType 注册响应数据编码及类型
func RegisterReplyType(serviceCode int32, code int32, messageType reflect.Type) {
	replyTypes[[2]int32{serviceCode, code}] = messageType
}

// GetReplyType 根据编码获取对应的响应数据类型
func GetReplyType(serviceCode int32, code int32) (reflect.Type, bool) {
	if v, ok := replyTypes[[2]int32{serviceCode, code}]; ok {
		return v, true
	}
	return nil, false
}

// NewReply 把proto message打包Reply
func NewReply(code int32, msg proto.Message) (*Reply, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	return &Reply{
		Code: code,
		Data: data,
	}, nil
}

// NewMulticast 创建push消息
func NewMulticast(receiver []string, content *Reply) *Multicast {
	return &Multicast{
		Receiver: receiver,
		Time:     timestamppb.Now(),
		Content:  content,
	}
}

// ResetRequest 重置请求对象
func ResetRequest(req *Request) {
	req.Id = 0
	req.NodeId = ""
	req.ServiceCode = 0
	req.Method = ""

	if len(req.Data) > 0 {
		req.Data = req.Data[:0]
	}
}

// ResetReply 重置响应对象
func ResetReply(resp *Reply) {
	resp.RequestId = 0
	resp.ServiceCode = 0
	resp.Code = 0

	if len(resp.Data) > 0 {
		resp.Data = resp.Data[:0]
	}
}

// LogValue implements slog.LogValuer
func (x *Request) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.Int("id", int(x.GetId())),
		slog.Int("service", int(x.GetServiceCode())),
		slog.String("method", x.GetMethod()),
	}

	if nodeID := x.GetNodeId(); nodeID != "" {
		attrs = append(attrs, slog.String("nodeID", nodeID))
	}

	if x.GetNoReply() {
		attrs = append(attrs, slog.Bool("noReply", true))
	} else if x.GetServerStream() {
		attrs = append(attrs, slog.Bool("serverStream", true))
	}

	return slog.GroupValue(attrs...)
}

// LogValue implements slog.LogValuer
func (x *Reply) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("reqID", int(x.GetRequestId())),
		slog.Int("service", int(x.GetServiceCode())),
		slog.Int("code", int(x.GetCode())),
	)
}
