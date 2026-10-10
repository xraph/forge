package brokers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/extensions/events/core"
)

// Streams retain entries until every logical group passes them. Register durable
// groups before producing. Publication rejects a full retained history.
//
//nolint:dupword // Lua closes nested blocks with consecutive end tokens.
const streamPublishScript = `
local function less(a,b)
 local am,as=string.match(a,'(%d+)%-(%d+)'); local bm,bs=string.match(b,'(%d+)%-(%d+)')
 if tonumber(am)==tonumber(bm) then return tonumber(as)<tonumber(bs) end
 return tonumber(am)<tonumber(bm)
end
if redis.call('EXISTS',KEYS[1])==0 and redis.call('HGET',KEYS[2],'published')=='1' then return redis.error_reply('RECOVERY_REQUIRED: stream was deleted') end
if redis.call('EXISTS',KEYS[1])==1 then
 local info=redis.call('XINFO','STREAM',KEYS[1]); local added=0; local len=0; local last='0-0'
 for i=1,#info,2 do
  if info[i]=='entries-added' then added=info[i+1] elseif info[i]=='length' then len=info[i+1] elseif info[i]=='last-generated-id' then last=info[i+1] end
 end
 local recorded=tonumber(redis.call('HGET',KEYS[2],'last_added') or '0')
 if added<recorded then return redis.error_reply('RECOVERY_REQUIRED: stream counter was reset') end
 local published=redis.call('HGET',KEYS[2],'last_published')
 if (published and last~=published) or added-len~=tonumber(redis.call('HGET',KEYS[2],'safe_removed') or '0') then return redis.error_reply('RECOVERY_REQUIRED: stream history was reset or removed') end
 local groups=redis.call('XINFO','GROUPS',KEYS[1]); local floor=nil
 for _,g in ipairs(groups) do
  local name=nil; local id=nil
  for i=1,#g,2 do
   if g[i]=='name' then name=g[i+1] elseif g[i]=='last-delivered-id' then id=g[i+1] end
  end
  local pending=redis.call('XPENDING',KEYS[1],name)
  if pending[1]>0 then
   if less(pending[2],id) then id=pending[2] end
  elseif id~='0-0' then
   local ms,seq=string.match(id,'(%d+)%-(%d+)'); id=ms..'-'..string.format('%.0f',tonumber(seq)+1)
  end
  if floor==nil or less(id,floor) then floor=id end
 end
 if floor~=nil and floor~='0-0' and redis.call('XLEN',KEYS[1])>=tonumber(ARGV[1]) then
  local trimmed=redis.call('XTRIM',KEYS[1],'MINID',floor)
  if trimmed>0 then redis.call('HSET',KEYS[2],'trimmed','1'); redis.call('HINCRBY',KEYS[2],'safe_removed',trimmed) end
 end
 if redis.call('XLEN',KEYS[1])>=tonumber(ARGV[1]) then return redis.error_reply('STREAM_CAPACITY: retained history is full') end
end
local id=redis.call('XADD',KEYS[1],'*','data',ARGV[2])
local info=redis.call('XINFO','STREAM',KEYS[1]); local added=0
for i=1,#info,2 do if info[i]=='entries-added' then added=info[i+1] end end
redis.call('HSET',KEYS[2],'published','1','last_published',id,'last_added',added)
return id`

//nolint:dupword // Lua closes nested blocks with consecutive end tokens.
const streamGroupScript = `
local groups={}
if redis.call('EXISTS',KEYS[1])==1 then groups=redis.call('XINFO','GROUPS',KEYS[1]) end
for _,g in ipairs(groups) do
 for i=1,#g,2 do
  if g[i]=='name' and g[i+1]==ARGV[1] then return 'existing' end
 end
end
if redis.call('HGET',KEYS[2],'trimmed')=='1' or (redis.call('HGET',KEYS[2],'published')=='1' and redis.call('EXISTS',KEYS[1])==0) then return redis.error_reply('RECOVERY_REQUIRED: retained history is missing') end
if redis.call('EXISTS',KEYS[1])==1 then
 local info=redis.call('XINFO','STREAM',KEYS[1]);local len=0;local added=0
 for i=1,#info,2 do
  if info[i]=='entries-added' then added=info[i+1] elseif info[i]=='length' then len=info[i+1] end
 end
 if added~=len then return redis.error_reply('RECOVERY_REQUIRED: retained history has deletions') end
end
redis.call('XGROUP','CREATE',KEYS[1],ARGV[1],'0','MKSTREAM')
return 'created' `

const streamFinishScript = `
if redis.call('GET',KEYS[2])~=ARGV[2] then return redis.error_reply('LEASE_LOST') end
if ARGV[4]~='' then
 if redis.call('XLEN',KEYS[3])>=tonumber(ARGV[8]) then return redis.error_reply('DEAD_LETTER_CAPACITY') end
 redis.call('XADD',KEYS[3],'*','source_stream',KEYS[1],'source_id',ARGV[3],'group',ARGV[1],'data',ARGV[5],'error',ARGV[4],'attempts',ARGV[6],'event_id',ARGV[7])
end
redis.call('HDEL',KEYS[4],ARGV[3])
return redis.call('XACK',KEYS[1],ARGV[1],ARGV[3])`

func streamSubscriptionKey(topic, name string) string { return topic + "\x00" + name }
func streamGroup(namespace, name string) string {
	sum := sha256.Sum256([]byte(name))

	return namespace + ":" + hex.EncodeToString(sum[:])
}
func streamFailureKey(topic, group string) string { return topic + ":forge:failures:" + group }
func streamStateKey(topic string) string          { return topic + ":forge:history" }
func streamLockKey(topic, group string) string    { return topic + ":forge:lease:" + group }
func (rb *RedisBroker) publishToStream(ctx context.Context, client redis.UniversalClient, topic string, data []byte) error {
	return client.Eval(ctx, streamPublishScript, []string{topic, streamStateKey(topic)}, rb.config.StreamMaxLen, data).Err()
}

// subscribeStream is called with the broker lock held.
func (rb *RedisBroker) subscribeStream(ctx context.Context, topic string, handler core.EventHandler) error {
	name := handler.Name()
	if strings.TrimSpace(name) == "" || name == "anonymous-handler" {
		return errors.New("streams requires a stable named handler")
	}

	key := streamSubscriptionKey(topic, name)
	if _, ok := rb.subscriptions[key]; ok {
		return fmt.Errorf("handler %s is already subscribed to %s", name, topic)
	}

	group := streamGroup(rb.config.ConsumerGroup, name)
	if err := rb.client.Eval(ctx, streamGroupScript, []string{topic, streamStateKey(topic)}, group).Err(); err != nil {
		if strings.Contains(err.Error(), "RECOVERY_REQUIRED") {
			rb.recovery[key] = err.Error()
		}

		return err
	}

	reason, err := streamGap(ctx, rb.client, topic, group)
	if err != nil {
		return err
	}

	if reason != "" {
		rb.recovery[key] = reason

		return fmt.Errorf("recovery required: %s", reason)
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub := &RedisSubscription{channel: topic, handler: handler, group: group, cancel: cancel, broker: rb}
	rb.subscriptions[key] = sub
	rb.stats.Subscriptions++

	rb.wg.Add(1)
	//nolint:gosec // Lease release needs a timeout after the subscription context is canceled.
	go rb.listenStream(subCtx, rb.client, sub)

	return nil
}

// streamGap reads history and pending state atomically. External removal is
// ambiguous, so it requires a source-log rebuild even if a group passed the entry.
//
//nolint:dupword // Lua closes nested blocks with consecutive end tokens.
const streamGapScript = `
local function less(a,b)
 local am,as=string.match(a,'(%d+)%-(%d+)'); local bm,bs=string.match(b,'(%d+)%-(%d+)')
 if tonumber(am)==tonumber(bm) then return tonumber(as)<tonumber(bs) end
 return tonumber(am)<tonumber(bm)
end
if redis.call('EXISTS',KEYS[1])==0 then return 'stream was deleted; source log rebuild is required' end
local info=redis.call('XINFO','STREAM',KEYS[1]); local added=0; local len=0; local last='0-0'
for i=1,#info,2 do
 if info[i]=='entries-added' then added=info[i+1] elseif info[i]=='length' then len=info[i+1] elseif info[i]=='last-generated-id' then last=info[i+1] end
end
local recorded=tonumber(redis.call('HGET',KEYS[2],'last_added') or '0')
if added<recorded then return 'stream counter was reset; source log rebuild is required' end
local published=redis.call('HGET',KEYS[2],'last_published')
if published and last~=published then return 'stream sequence was reset; source log rebuild is required' end
local removed=tonumber(redis.call('HGET',KEYS[2],'safe_removed') or '0')
if added-len~=removed then return 'stream history was removed externally; source log rebuild is required' end
local groups=redis.call('XINFO','GROUPS',KEYS[1])
for _,g in ipairs(groups) do
 for i=1,#g,2 do
  if g[i]=='name' and g[i+1]==ARGV[1] then return '' end
 end
end
return 'consumer group was deleted; source log rebuild is required' `

func streamGap(ctx context.Context, client redis.UniversalClient, topic, group string) (string, error) {
	return client.Eval(ctx, streamGapScript, []string{topic, streamStateKey(topic)}, group).Text()
}

func streamPause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (rb *RedisBroker) listenStream(ctx context.Context, client redis.UniversalClient, sub *RedisSubscription) {
	defer rb.wg.Done()
	defer func() {
		rb.mu.Lock()
		defer rb.mu.Unlock()

		key := streamSubscriptionKey(sub.channel, sub.handler.Name())
		if rb.subscriptions[key] == sub {
			delete(rb.subscriptions, key)
			rb.stats.Subscriptions--
		}
	}()

	key := streamLockKey(sub.channel, sub.group)

	for ctx.Err() == nil {
		token := uuid.NewString()

		acquired, err := client.SetNX(ctx, key, token, rb.config.StreamLeaseDuration).Result()
		if err != nil {
			rb.recordReceive(false, sub.channel)
		}

		if !acquired {
			if !streamPause(ctx, rb.config.StreamPollInterval) {
				return
			}

			continue
		}

		leaseCtx, cancel := context.WithCancel(ctx)

		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)

			ticker := time.NewTicker(rb.config.StreamLeaseDuration / 3)
			defer ticker.Stop()

			for {
				select {
				case <-leaseCtx.Done():
					return
				case <-ticker.C:
					renewed, renewErr := client.Eval(leaseCtx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('PEXPIRE',KEYS[1],ARGV[2]) end return 0`, []string{key}, token, rb.config.StreamLeaseDuration.Milliseconds()).Int()
					if renewErr != nil || renewed != 1 {
						cancel()

						return
					}
				}
			}
		}()

		rb.consumeStream(leaseCtx, client, sub, token)
		cancel()
		<-heartbeatDone

		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), time.Second)
		_ = client.Eval(releaseCtx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{key}, token).Err()

		releaseCancel()

		if !streamPause(ctx, rb.config.StreamPollInterval) {
			return
		}

		rb.mu.RLock()
		_, failed := rb.recovery[streamSubscriptionKey(sub.channel, sub.handler.Name())]
		rb.mu.RUnlock()

		if failed {
			return
		}
	}
}

func (rb *RedisBroker) consumeStream(ctx context.Context, client redis.UniversalClient, sub *RedisSubscription, token string) {
	for ctx.Err() == nil {
		reason, err := streamGap(ctx, client, sub.channel, sub.group)
		if err != nil {
			rb.recordReceive(false, sub.channel)

			return
		}

		if reason != "" {
			rb.mu.Lock()
			rb.recovery[streamSubscriptionKey(sub.channel, sub.handler.Name())] = reason
			rb.mu.Unlock()

			return
		}

		pending, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: sub.channel, Group: sub.group, Start: "-", End: "+", Count: 1}).Result()
		if err != nil {
			rb.recordReceive(false, sub.channel)

			return
		}

		var (
			message  redis.XMessage
			attempts int64 = 1
		)

		if len(pending) > 0 {
			if pending[0].Idle < rb.config.StreamClaimIdle {
				if !streamPause(ctx, rb.config.StreamPollInterval) {
					return
				}

				continue
			}

			claimed, claimErr := client.XClaim(ctx, &redis.XClaimArgs{Stream: sub.channel, Group: sub.group, Consumer: rb.config.ConsumerName, MinIdle: rb.config.StreamClaimIdle, Messages: []string{pending[0].ID}}).Result()
			if claimErr != nil {
				rb.recordReceive(false, sub.channel)

				return
			}

			if len(claimed) == 0 {
				continue
			}

			message = claimed[0]
			attempts = pending[0].RetryCount + 1
		} else {
			streams, readErr := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: sub.group, Consumer: rb.config.ConsumerName, Streams: []string{sub.channel, ">"}, Count: 1, Block: -1}).Result()
			if errors.Is(readErr, redis.Nil) {
				if !streamPause(ctx, rb.config.StreamPollInterval) {
					return
				}

				continue
			}

			if readErr != nil {
				rb.recordReceive(false, sub.channel)

				return
			}

			if len(streams) == 0 || len(streams[0].Messages) == 0 {
				continue
			}

			message = streams[0].Messages[0]
		}

		if ctx.Err() != nil {
			return
		}

		owner, ownerErr := client.Get(ctx, streamLockKey(sub.channel, sub.group)).Result()
		if ownerErr != nil || owner != token {
			return
		}

		payload, _ := message.Values["data"].(string)

		var event core.Event

		deliveryErr := json.Unmarshal([]byte(payload), &event)

		if attempts > rb.config.StreamMaxDeliveries {
			failure, readErr := client.HGet(ctx, streamFailureKey(sub.channel, sub.group), message.ID).Result()
			if readErr != nil && !errors.Is(readErr, redis.Nil) {
				rb.recordReceive(false, sub.channel)

				return
			}

			if failure == "" {
				failure = "delivery attempts exhausted before committed acknowledgment"
			}

			deliveryErr = errors.New(failure)
		}

		if deliveryErr == nil {
			rb.recordReceive(true, sub.channel)

			deliveryErr = invokeStreamHandler(ctx, sub.handler, &event)
		}

		if ctx.Err() != nil {
			return
		}

		failure := ""

		if deliveryErr != nil {
			rb.recordReceive(false, sub.channel)

			if persistErr := client.HSet(ctx, streamFailureKey(sub.channel, sub.group), message.ID, deliveryErr.Error()).Err(); persistErr != nil {
				return
			}

			if attempts < rb.config.StreamMaxDeliveries {
				continue
			}

			failure = deliveryErr.Error()
		}

		err = client.Eval(ctx, streamFinishScript, []string{sub.channel, streamLockKey(sub.channel, sub.group), sub.channel + ":forge:dead-letter", streamFailureKey(sub.channel, sub.group)}, sub.group, token, message.ID, failure, payload, attempts, event.ID, rb.config.StreamMaxLen).Err()
		if err != nil {
			rb.recordReceive(false, sub.channel)

			return
		}
	}
}
func invokeStreamHandler(ctx context.Context, handler core.EventHandler, event *core.Event) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("handler panic: %v", value)
		}
	}()

	if !handler.CanHandle(event) {
		return nil
	}

	return handler.Handle(ctx, event)
}
