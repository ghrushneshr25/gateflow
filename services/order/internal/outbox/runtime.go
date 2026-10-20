package outbox

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/libs/go/kafkax"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

// Runtime supervises a single publisher and consumer with Kafka reconnect.
type Runtime struct {
	Store    *store.Store
	Brokers  []string
	Topic    string
	Group    string
	Log      *slog.Logger
	Metrics  *Metrics
	Pub      *Publisher
	Cons     *Consumer

	kafkaReady atomic.Bool
	started    atomic.Bool
	mu         sync.Mutex
	prod       *kafkax.Producer
	reader     *kafkax.Consumer
	workersCancel context.CancelFunc
}

func (r *Runtime) Ready() bool { return r.kafkaReady.Load() }

// Run blocks until ctx done. Retries Kafka connectivity with backoff.
func (r *Runtime) Run(ctx context.Context) {
	if r.Log == nil {
		r.Log = slog.Default()
	}
	if r.Metrics == nil {
		r.Metrics = &Metrics{}
	}
	if r.Topic == "" {
		r.Topic = kafkax.TopicOrderConfirmed
	}
	if r.Group == "" {
		if g := os.Getenv("GATEFLOW_KAFKA_GROUP"); g != "" {
			r.Group = g
		} else {
			r.Group = "order-confirmed-effects"
		}
	}
	backoff := 500 * time.Millisecond
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			r.shutdownWorkers()
			return
		}
		if err := r.ensureKafka(ctx); err != nil {
			r.kafkaReady.Store(false)
			r.Log.Warn("kafka not ready; retrying", "err", err, "backoff", backoff.String())
			select {
			case <-ctx.Done():
				r.shutdownWorkers()
				return
			case <-time.After(backoff + time.Duration(rand.Intn(250))*time.Millisecond):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		r.kafkaReady.Store(true)
		backoff = 500 * time.Millisecond
		r.startWorkersOnce(ctx)

		// Health probe loop; on sustained failure mark not ready and restart workers.
		select {
		case <-ctx.Done():
			r.shutdownWorkers()
			return
		case <-time.After(5 * time.Second):
			if err := kafkax.EnsureTopic(r.Brokers, r.Topic, 1); err != nil {
				r.Log.Warn("kafka health probe failed", "err", err)
				r.kafkaReady.Store(false)
				r.shutdownWorkers()
				// loop will reconnect
			}
		}
	}
}

func (r *Runtime) ensureKafka(ctx context.Context) error {
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		ch <- result{err: kafkax.EnsureTopic(r.Brokers, r.Topic, 1)}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		return res.err
	case <-time.After(5 * time.Second):
		return context.DeadlineExceeded
	}
}

func (r *Runtime) startWorkersOnce(parent context.Context) {
	if r.started.Swap(true) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	wctx, cancel := context.WithCancel(parent)
	r.workersCancel = cancel

	r.prod = kafkax.NewProducer(r.Brokers)
	r.reader = kafkax.NewConsumer(r.Brokers, r.Topic, r.Group)

	lease := 30 * time.Second
	if os.Getenv("GATEFLOW_TEST_HOOKS") == "1" {
		// Crash-recovery tests need fast reclaim after process death mid-publish.
		lease = 2 * time.Second
	}
	r.Pub = &Publisher{
		Store: r.Store, Producer: r.prod, WorkerID: ids.New("order-pub"),
		Log: r.Log, Metrics: r.Metrics, Topic: r.Topic, Interval: 300 * time.Millisecond,
		Lease: lease,
	}
	r.Cons = &Consumer{Store: r.Store, Reader: r.reader, Log: r.Log}

	pubOff := os.Getenv("GATEFLOW_OUTBOX_PUBLISHER") == "0" || os.Getenv("GATEFLOW_OUTBOX_PUBLISHER") == "false"
	consOff := os.Getenv("GATEFLOW_OUTBOX_CONSUMER") == "0" || os.Getenv("GATEFLOW_OUTBOX_CONSUMER") == "false"
	if !pubOff {
		go r.Pub.Run(wctx)
	}
	if !consOff {
		go r.Cons.Run(wctx)
	}
	r.Log.Info("kafka outbox workers started", "brokers", r.Brokers, "topic", r.Topic, "publisher", !pubOff, "consumer", !consOff)
}

func (r *Runtime) shutdownWorkers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workersCancel != nil {
		r.workersCancel()
		r.workersCancel = nil
	}
	if r.prod != nil {
		_ = r.prod.Close()
		r.prod = nil
	}
	if r.reader != nil {
		_ = r.reader.Close()
		r.reader = nil
	}
	r.Pub = nil
	r.Cons = nil
	r.started.Store(false)
}
