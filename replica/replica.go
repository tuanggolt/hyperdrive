package replica

import (
	"context"

	"github.com/renproject/hyperdrive/mq"
	"github.com/renproject/hyperdrive/process"
	"github.com/renproject/hyperdrive/scheduler"
	"github.com/renproject/hyperdrive/timer"
	"github.com/renproject/id"
)

// DidHandleMessage is called by the Replica after it has finished handling an
// input message (i.e. Propose, Prevote, or Precommit), or timeout. The message
// could have been either accepted and inserted into the processing queue, or
// filtered out and dropped. The callback is called even when the context
// within which the Replica runs gets cancelled.
type DidHandleMessage func()

// A Replica represents a process in a replicated state machine that
// participates in the Hyperdrive Consensus Algorithm. It encapsulates a
// Hyperdrive Process and exposes an interface for the Hyperdrive user to
// insert messages (propose, prevote, precommit, timeouts). A Replica then
// handles these messages in asynchronously after having enqueued them in
// increasing order of height and round. A Replica is instantiated by passing
// in the set of signatories participating in the consensus mechanism, and it
// filters out messages if they haven't been sent by the known set of allowed
// signatories.
type Replica struct {
	opts Options

	proc         process.Process
	procsAllowed map[id.Signatory]bool

	onProposeTimeout   chan timer.Timeout
	onPrevoteTimeout   chan timer.Timeout
	onPrecommitTimeout chan timer.Timeout

	onPropose   chan process.Propose
	onPrevote   chan process.Prevote
	onPrecommit chan process.Precommit
	mq          mq.MessageQueue

	didHandleMessage DidHandleMessage
}

// New instantiates and returns a pointer to a new Hyperdrive replica machine
func New(
	opts Options,
	whoami id.Signatory,
	signatories []id.Signatory,
	linearTimer process.Timer,
	propose process.Proposer,
	validate process.Validator,
	commit process.Committer,
	catch process.Catcher,
	broadcast process.Broadcaster,
	didHandleMessage DidHandleMessage,
) *Replica {
	f := len(signatories) / 3
	scheduler := scheduler.NewRoundRobin(signatories)
	proc := process.New(
		whoami,
		f,
		linearTimer,
		scheduler,
		propose,
		validate,
		broadcast,
		commit,
		catch,
	)

	procsAllowed := make(map[id.Signatory]bool)
	for _, signatory := range signatories {
		procsAllowed[signatory] = true
	}

	return &Replica{
		opts: opts,

		proc:         proc,
		procsAllowed: procsAllowed,

		onProposeTimeout:   make(chan timer.Timeout, opts.MessageQueueOpts.MaxCapacity),
		onPrevoteTimeout:   make(chan timer.Timeout, opts.MessageQueueOpts.MaxCapacity),
		onPrecommitTimeout: make(chan timer.Timeout, opts.MessageQueueOpts.MaxCapacity),

		onPropose:   make(chan process.Propose, opts.MessageQueueOpts.MaxCapacity),
		onPrevote:   make(chan process.Prevote, opts.MessageQueueOpts.MaxCapacity),
		onPrecommit: make(chan process.Precommit, opts.MessageQueueOpts.MaxCapacity),
		mq:          mq.New(opts.MessageQueueOpts),

		didHandleMessage: didHandleMessage,
	}
}

// Run starts the Hyperdrive replica's process
func (replica *Replica) Run(ctx context.Context) {
	replica.proc.Start()

	isRunning := true
	for isRunning {
		func() {
			defer func() {
				if replica.didHandleMessage != nil {
					replica.didHandleMessage()
				}
			}()

			select {
			case <-ctx.Done():
				isRunning = false
				return

			case timeout := <-replica.onProposeTimeout:
				replica.proc.OnTimeoutPropose(timeout.Height, timeout.Round)
			case timeout := <-replica.onPrevoteTimeout:
				replica.proc.OnTimeoutPrevote(timeout.Height, timeout.Round)
			case timeout := <-replica.onPrecommitTimeout:
				replica.proc.OnTimeoutPrecommit(timeout.Height, timeout.Round)

			case propose := <-replica.onPropose:
				if !replica.filterHeight(propose.Height) {
					return
				}
				if !replica.filterFrom(propose.From) {
					return
				}
				replica.mq.InsertPropose(propose)
			case prevote := <-replica.onPrevote:
				if !replica.filterHeight(prevote.Height) {
					return
				}
				if !replica.filterFrom(prevote.From) {
					return
				}
				replica.mq.InsertPrevote(prevote)
			case precommit := <-replica.onPrecommit:
				if !replica.filterHeight(precommit.Height) {
					return
				}
				if !replica.filterFrom(precommit.From) {
					return
				}
				replica.mq.InsertPrecommit(precommit)
			}

			replica.flush()
		}()
	}
}

// Propose adds a propose message to the replica. This message will be
// asynchronously inserted into the replica's message queue asynchronously,
// and consumed when the replica does not have any immediate task to do
func (replica *Replica) Propose(ctx context.Context, propose process.Propose) {
	select {
	case <-ctx.Done():
	case replica.onPropose <- propose:
	}
}

// Prevote adds a prevote message to the replica. This message will be
// asynchronously inserted into the replica's message queue asynchronously,
// and consumed when the replica does not have any immediate task to do
func (replica *Replica) Prevote(ctx context.Context, prevote process.Prevote) {
	select {
	case <-ctx.Done():
	case replica.onPrevote <- prevote:
	}
}

// Precommit adds a precommit message to the replica. This message will be
// asynchronously inserted into the replica's message queue asynchronously,
// and consumed when the replica does not have any immediate task to do
func (replica *Replica) Precommit(ctx context.Context, precommit process.Precommit) {
	select {
	case <-ctx.Done():
	case replica.onPrecommit <- precommit:
	}
}

// TimeoutPropose adds a propose timeout message to the replica. This message
// will be filtered based on the replica's consensus height, and inserted
// asynchronously into the replica's message queue. It will be consumed when
// the replica does not have any immediate task to do
func (replica *Replica) TimeoutPropose(ctx context.Context, timeout timer.Timeout) {
	select {
	case <-ctx.Done():
	case replica.onProposeTimeout <- timeout:
	}
}

// TimeoutPrevote adds a prevote timeout message to the replica. This message
// will be filtered based on the replica's consensus height, and inserted
// asynchronously into the replica's message queue. It will be consumed when
// the replica does not have any immediate task to do
func (replica *Replica) TimeoutPrevote(ctx context.Context, timeout timer.Timeout) {
	select {
	case <-ctx.Done():
	case replica.onPrevoteTimeout <- timeout:
	}
}

// TimeoutPrecommit adds a precommit timeout message to the replica. This message
// will be filtered based on the replica's consensus height, and inserted
// asynchronously into the replica's message queue. It will be consumed when
// the replica does not have any immediate task to do
func (replica *Replica) TimeoutPrecommit(ctx context.Context, timeout timer.Timeout) {
	select {
	case <-ctx.Done():
	case replica.onPrecommitTimeout <- timeout:
	}
}

// CurrentHeight returns the height (in terms of block number) that the replica
// is currently at
func (replica Replica) CurrentHeight() process.Height {
	return replica.proc.CurrentHeight
}

func (replica *Replica) filterHeight(height process.Height) bool {
	return height >= replica.proc.CurrentHeight
}

func (replica *Replica) filterFrom(from id.Signatory) bool {
	return replica.procsAllowed[from]
}

func (replica *Replica) flush() {
	for {
		n := replica.mq.Consume(
			replica.proc.CurrentHeight,
			replica.proc.Propose,
			replica.proc.Prevote,
			replica.proc.Precommit,
		)
		if n == 0 {
			return
		}
	}
}
