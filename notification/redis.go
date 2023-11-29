package notification

import (
	"context"
	"nodehub/logger"
	"nodehub/proto/gatewaypb"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

// RedisClient 实现了PubSub方法的客户端接口
type RedisClient interface {
	Publish(ctx context.Context, channel string, message any) *redis.IntCmd
	SPublish(ctx context.Context, channel string, message any) *redis.IntCmd
	Subscribe(ctx context.Context, channels ...string) *redis.PubSub
	SSubscribe(ctx context.Context, channels ...string) *redis.PubSub
}

// RedisMQ 是基于 Redis 的消息队列
type RedisMQ struct {
	client  RedisClient
	channel string
	shared  bool
	done    chan struct{}
}

// NewRedisMQ 构造函数
//
// client 可以使用 *redis.Client 或者 *redis.ClusterClient
//
// 当使用ClusterClient时，会采用shared模式
func NewRedisMQ(client RedisClient, channel string) *RedisMQ {
	// 如果是集群客户端，使用shared pubsub
	_, shared := client.(*redis.ClusterClient)

	return &RedisMQ{
		client:  client,
		channel: channel,
		shared:  shared,
		done:    make(chan struct{}),
	}
}

// Publish 把消息发布到消息队列
func (mq *RedisMQ) Publish(ctx context.Context, message *gatewaypb.Notification) error {
	payload, err := proto.Marshal(message)
	if err != nil {
		return err
	}

	var result *redis.IntCmd
	if mq.shared {
		result = mq.client.SPublish(ctx, mq.channel, payload)
	} else {
		result = mq.client.Publish(ctx, mq.channel, payload)
	}
	return result.Err()
}

// Subscribe 从消息队列订阅消息
func (mq *RedisMQ) Subscribe(ctx context.Context) (<-chan *gatewaypb.Notification, error) {
	var pubsub *redis.PubSub

	if mq.shared {
		pubsub = mq.client.SSubscribe(ctx, mq.channel)
	} else {
		pubsub = mq.client.Subscribe(ctx, mq.channel)
	}

	mc := pubsub.Channel()
	nc := make(chan *gatewaypb.Notification)

	go func() {
		defer func() {
			close(nc)

			if mq.shared {
				pubsub.SUnsubscribe(ctx)
			} else {
				pubsub.Unsubscribe(ctx)
			}
		}()

		for {
			select {
			case <-mq.done:
				return
			case msg, ok := <-mc:
				if !ok {
					logger.Error("redis MQ consumer unexpected closed")
					return
				}

				data := []byte(msg.Payload)

				n := &gatewaypb.Notification{}
				if err := proto.Unmarshal(data, n); err != nil {
					continue
				}

				select {
				case <-mq.done:
					return
				case nc <- n:
				}
			}
		}
	}()

	return nc, nil
}

// Close 关闭消息队列
func (mq *RedisMQ) Close() {
	close(mq.done)
}
