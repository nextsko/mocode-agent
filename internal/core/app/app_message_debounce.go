package app

import (
	"context"
	"sync"
	"time"

	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/util/pubsub"
)

const messageUpdateDebounce = 300 * time.Millisecond

type pendingMessageUpdate struct {
	msg      message.Message
	timer    *time.Timer
	deadline time.Time
}

type messageUpdateDebouncer struct {
	svc     *storeMessageService
	mu      sync.Mutex
	pending map[string]*pendingMessageUpdate
	delay   time.Duration
}

func newMessageUpdateDebouncer(svc *storeMessageService) *messageUpdateDebouncer {
	return &messageUpdateDebouncer{
		svc:     svc,
		pending: make(map[string]*pendingMessageUpdate),
		delay:   messageUpdateDebounce,
	}
}

func (d *messageUpdateDebouncer) Update(ctx context.Context, msg message.Message) error {
	if msg.FinishPart() != nil || len(msg.ToolCalls()) > 0 {
		d.flush(msg.ID)
		return d.svc.updateImmediate(ctx, msg)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if p, ok := d.pending[msg.ID]; ok {
		p.msg = msg
		p.deadline = time.Now().Add(d.delay)
		p.timer.Reset(d.delay)
		return nil
	}

	p := &pendingMessageUpdate{msg: msg, deadline: time.Now().Add(d.delay)}
	p.timer = time.AfterFunc(d.delay, func() {
		d.flush(msg.ID)
	})
	d.pending[msg.ID] = p
	return nil
}

func (d *messageUpdateDebouncer) flush(id string) {
	d.mu.Lock()
	p, ok := d.pending[id]
	if ok {
		delete(d.pending, id)
	}
	d.mu.Unlock()
	if !ok {
		return
	}
	p.timer.Stop()
	_ = d.svc.updateImmediate(context.Background(), p.msg)
}

func (s *storeMessageService) updateImmediate(ctx context.Context, msg message.Message) error {
	if err := s.store.Update(ctx, msg); err != nil {
		return err
	}
	s.Publish(pubsub.UpdatedEvent, msg.Clone())
	return nil
}
