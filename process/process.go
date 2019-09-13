package process

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/renproject/hyperdrive/block"
	"github.com/renproject/id"
	"github.com/sirupsen/logrus"
)

// Step in the consensus algorithm.
type Step uint8

// Define all Steps.
const (
	StepNil Step = iota
	StepPropose
	StepPrevote
	StepPrecommit
)

// A Blockchain defines a storage interface for Blocks that is based around
// Height.
type Blockchain interface {
	InsertBlockAtHeight(block.Height, block.Block)
	BlockAtHeight(block.Height) (block.Block, bool)
	BlockExistsAtHeight(block.Height) bool
}

// A Proposer builds a `block.Block` for proposals.
type Proposer interface {
	BlockProposal(block.Height, block.Round) block.Block
}

// A Validator validates a `block.Block` that has been proposed.
type Validator interface {
	IsBlockValid(block block.Block, checkHistory bool) bool
}

// An Observer is notified when note-worthy events happen for the first time.
type Observer interface {
	DidCommitBlock(block.Height)
}

// A Scheduler determines which `id.Signatory` should be broadcasting
// proposals in at a given `block.Height` and `block.Round`.
type Scheduler interface {
	Schedule(block.Height, block.Round) id.Signatory
}

// A Broadcaster sends a Message to as many Processes in the network as
// possible.
type Broadcaster interface {
	Broadcast(Message)
}

// A Timer determines the timeout duration at a given Step and `block.Round`.
type Timer interface {
	Timeout(step Step, round block.Round) time.Duration
}

// Processes defines a wrapper type around the []Process type.
type Processes []Process

// A Process defines a state machine in the distributed replicated state
// machine. See https://arxiv.org/pdf/1807.04938.pdf for more information.
type Process struct {
	logger logrus.FieldLogger
	mu     *sync.Mutex

	signatory  id.Signatory
	blockchain Blockchain
	state      State

	proposer    Proposer
	validator   Validator
	scheduler   Scheduler
	broadcaster Broadcaster
	timer       Timer
	observer    Observer
}

// New Process initialised to the default state, starting in the first round.
func New(logger logrus.FieldLogger, signatory id.Signatory, blockchain Blockchain, state State, proposer Proposer, validator Validator, observer Observer, broadcaster Broadcaster, scheduler Scheduler, timer Timer) *Process {
	p := &Process{
		logger: logger,
		mu:     new(sync.Mutex),

		signatory:  signatory,
		blockchain: blockchain,
		state:      state,

		proposer:    proposer,
		validator:   validator,
		observer:    observer,
		broadcaster: broadcaster,
		scheduler:   scheduler,
		timer:       timer,
	}
	return p
}

func (p Process) State() State {
	return p.state
}

// MarshalJSON implements the `json.Marshaler` interface for the Process type,
// by marshaling its isolated State.
func (p Process) MarshalJSON() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return json.Marshal(p.state)
}

// UnmarshalJSON implements the `json.Unmarshaler` interface for the Process
// type, by unmarshaling its isolated State.
func (p *Process) UnmarshalJSON(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return json.Unmarshal(data, &p.state)
}

// MarshalBinary implements the `encoding.BinaryMarshaler` interface for the
// Process type, by marshaling its isolated State.
func (p Process) MarshalBinary() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, p.state.CurrentHeight); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.CurrentHeight: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, p.state.CurrentRound); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.CurrentRound: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, p.state.CurrentStep); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.CurrentStep: %v", err)
	}
	lockedBlockData, err := p.state.LockedBlock.MarshalBinary()
	if err != nil {
		return buf.Bytes(), fmt.Errorf("cannot marshal p.state.LockedBlock: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint64(len(lockedBlockData))); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.LockedBlock len: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, lockedBlockData); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.LockedBlock data: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, p.state.LockedRound); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.LockedRound: %v", err)
	}
	validBlockData, err := p.state.ValidBlock.MarshalBinary()
	if err != nil {
		return buf.Bytes(), fmt.Errorf("cannot marshal p.state.ValidBlock: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint64(len(validBlockData))); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.ValidBlock len: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, validBlockData); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.ValidBlock data: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, p.state.ValidRound); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.ValidRound: %v", err)
	}
	proposalsData, err := p.state.Proposals.MarshalBinary()
	if err != nil {
		return buf.Bytes(), fmt.Errorf("cannot marshal p.state.Proposals: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint64(len(proposalsData))); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.Proposals len: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, proposalsData); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.Proposals data: %v", err)
	}
	prevotesData, err := p.state.Prevotes.MarshalBinary()
	if err != nil {
		return buf.Bytes(), fmt.Errorf("cannot marshal p.state.Prevotes: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint64(len(prevotesData))); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.Prevotes len: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, prevotesData); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.Prevotes data: %v", err)
	}
	precommitsData, err := p.state.Precommits.MarshalBinary()
	if err != nil {
		return buf.Bytes(), fmt.Errorf("cannot marshal p.state.Precommits: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint64(len(precommitsData))); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.Precommits len: %v", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, precommitsData); err != nil {
		return buf.Bytes(), fmt.Errorf("cannot write p.state.Precommits data: %v", err)
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary implements the `encoding.BinaryUnmarshaler` interface for the
// Process type, by unmarshaling its isolated State.
func (p *Process) UnmarshalBinary(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	buf := bytes.NewBuffer(data)
	if err := binary.Read(buf, binary.LittleEndian, &p.state.CurrentHeight); err != nil {
		return fmt.Errorf("cannot read p.state.CurrentHeight: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &p.state.CurrentRound); err != nil {
		return fmt.Errorf("cannot read p.state.CurrentRound: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &p.state.CurrentStep); err != nil {
		return fmt.Errorf("cannot read p.state.CurrentStep: %v", err)
	}
	var numBytes uint64
	if err := binary.Read(buf, binary.LittleEndian, &numBytes); err != nil {
		return fmt.Errorf("cannot read p.state.LockedBlock len: %v", err)
	}
	lockedBlockBytes := make([]byte, numBytes)
	if _, err := buf.Read(lockedBlockBytes); err != nil {
		return fmt.Errorf("cannot read p.state.LockedBlock data: %v", err)
	}
	if err := p.state.LockedBlock.UnmarshalBinary(lockedBlockBytes); err != nil {
		return fmt.Errorf("cannot unmarshal p.state.LockedBlock: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &p.state.LockedRound); err != nil {
		return fmt.Errorf("cannot read p.state.LockedRound: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &numBytes); err != nil {
		return fmt.Errorf("cannot read p.state.ValidBlock len: %v", err)
	}
	validBlockBytes := make([]byte, numBytes)
	if _, err := buf.Read(validBlockBytes); err != nil {
		return fmt.Errorf("cannot read p.state.ValidBlock data: %v", err)
	}
	if err := p.state.ValidBlock.UnmarshalBinary(validBlockBytes); err != nil {
		return fmt.Errorf("cannot unmarshal p.state.ValidBlock: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &p.state.ValidRound); err != nil {
		return fmt.Errorf("cannot read p.state.ValidRound: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &numBytes); err != nil {
		return fmt.Errorf("cannot read p.state.Proposals len: %v", err)
	}
	proposalsBytes := make([]byte, numBytes)
	if _, err := buf.Read(proposalsBytes); err != nil {
		return fmt.Errorf("cannot read p.state.Proposals data: %v", err)
	}
	if err := p.state.Proposals.UnmarshalBinary(proposalsBytes); err != nil {
		return fmt.Errorf("cannot unmarshal p.state.Proposals: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &numBytes); err != nil {
		return fmt.Errorf("cannot read p.state.Prevotes len: %v", err)
	}
	prevotesBytes := make([]byte, numBytes)
	if _, err := buf.Read(prevotesBytes); err != nil {
		return fmt.Errorf("cannot read p.state.Prevotes data: %v", err)
	}
	if err := p.state.Prevotes.UnmarshalBinary(prevotesBytes); err != nil {
		return fmt.Errorf("cannot unmarshal p.state.Prevotes: %v", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &numBytes); err != nil {
		return fmt.Errorf("cannot read p.state.Precommits len: %v", err)
	}
	precommitsBytes := make([]byte, numBytes)
	if _, err := buf.Read(precommitsBytes); err != nil {
		return fmt.Errorf("cannot read p.state.Precommits data: %v", err)
	}
	if err := p.state.Precommits.UnmarshalBinary(precommitsBytes); err != nil {
		return fmt.Errorf("cannot unmarshal p.state.Precommits: %v", err)
	}
	return nil
}

// Start the process
func (p *Process) Start() {
	p.startRound(p.state.CurrentRound)
}

// StartRound is safe for concurrent use. See
// https://arxiv.org/pdf/1807.04938.pdf for more information.
func (p *Process) StartRound(round block.Round) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.startRound(round)
}

// HandleMessage is safe for concurrent use. See
// https://arxiv.org/pdf/1807.04938.pdf for more information.
func (p *Process) HandleMessage(m Message) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch m := m.(type) {
	case *Propose:
		p.handlePropose(m)
	case *Prevote:
		p.handlePrevote(m)
	case *Precommit:
		p.handlePrecommit(m)
	}
}

func (p *Process) startRound(round block.Round) {
	p.state.CurrentRound = round
	p.state.CurrentStep = StepPropose

	// If process p is the proposer.
	if p.signatory.Equal(p.scheduler.Schedule(p.state.CurrentHeight, p.state.CurrentRound)) {
		var proposal block.Block
		if p.state.ValidBlock.Hash() != block.InvalidHash {
			proposal = p.state.ValidBlock
		} else {
			proposal = p.proposer.BlockProposal(p.state.CurrentHeight, p.state.CurrentRound)
		}
		propose := NewPropose(
			p.state.CurrentHeight,
			p.state.CurrentRound,
			proposal,
			p.state.ValidRound,
		)

		// Include the previous block for nodes to catch up
		previousBlock, ok := p.blockchain.BlockAtHeight(p.state.CurrentHeight - 1)
		if !ok {
			panic("fail to get previous block from storage")
		}
		messages := p.state.Precommits.QueryMessagesByHeightWithHigestRound(p.state.CurrentHeight - 1)
		commits := make([]Precommit, 0, len(messages))
		for _, message := range messages {
			commit := message.(*Precommit)
			if commit.blockHash.Equal(previousBlock.Hash()) {
				commits = append(commits, *commit)
			}
		}
		propose.latestCommit = LatestCommit{
			Block:      previousBlock,
			Precommits: commits,
		}

		// Always broadcast at the end
		p.logger.Infof("[🔊] Proposing a new block of height %v, round = %v", propose.Height(), propose.Round())
		p.broadcaster.Broadcast(propose)
	} else {
		p.scheduleTimeoutPropose(p.state.CurrentHeight, p.state.CurrentRound, p.timer.Timeout(StepPropose, p.state.CurrentRound))
	}
}

func (p *Process) handlePropose(propose *Propose) {
	p.syncLatestCommit(propose.latestCommit)

	p.logger.Debugf("Receive new propose of height %v", propose.height)
	n, firstTime, _, _ := p.state.Proposals.Insert(propose)

	// upon Propose{currentHeight, currentRound, block, -1}
	if propose.Height() == p.state.CurrentHeight && propose.Round() == p.state.CurrentRound && propose.ValidRound() == block.InvalidRound {
		// from Schedule{currentHeight, currentRound}
		if propose.Signatory().Equal(p.scheduler.Schedule(p.state.CurrentHeight, p.state.CurrentRound)) {
			// while currentStep = StepPropose
			if p.state.CurrentStep == StepPropose {
				var prevote *Prevote
				if p.validator.IsBlockValid(propose.Block(), true) && (p.state.LockedRound == block.InvalidRound || p.state.LockedBlock.Equal(propose.Block())) {
					prevote = NewPrevote(
						p.state.CurrentHeight,
						p.state.CurrentRound,
						propose.Block().Hash(),
					)
				} else {
					prevote = NewPrevote(
						p.state.CurrentHeight,
						p.state.CurrentRound,
						block.InvalidHash,
					)
				}
				p.state.CurrentStep = StepPrevote
				p.broadcaster.Broadcast(prevote)
			}
		}
	}

	// upon f+1 *{currentHeight, round, *, *} and round > currentRound
	if n > p.state.Prevotes.F() && propose.Height() == p.state.CurrentHeight && propose.Round() > p.state.CurrentRound {
		p.startRound(propose.Round())
	}

	p.checkProposeInCurrentHeightAndRoundWithPrevotes()
	if firstTime {
		p.checkProposeInCurrentHeightAndRoundWithPrevotesForTheFirstTime()
	}
	p.checkProposeInCurrentHeightWithPrecommits(propose.Round())
}

func (p *Process) handlePrevote(prevote *Prevote) {
	p.logger.Debugf("Receive new prevote of height = %v , IsNil= %v", prevote.height, prevote.blockHash.Equal(block.InvalidHash))
	n, _, _, firstTimeExceeding2F := p.state.Prevotes.Insert(prevote)
	if firstTimeExceeding2F && prevote.Height() == p.state.CurrentHeight && prevote.Round() == p.state.CurrentRound && p.state.CurrentStep == StepPrevote {
		// upon 2f+1 Prevote{currentHeight, currentRound, *} while step = StepPrevote for the first time
		p.scheduleTimeoutPrevote(p.state.CurrentHeight, p.state.CurrentRound, p.timer.Timeout(StepPrevote, p.state.CurrentRound))
	}

	// upon 2f+1 Prevote{currentHeight, currentRound, nil} while currentStep = StepPrevote
	if n := p.state.Prevotes.QueryByHeightRoundBlockHash(p.state.CurrentHeight, p.state.CurrentRound, block.InvalidHash); n > 2*p.state.Prevotes.F() && p.state.CurrentStep == StepPrevote {
		precommit := NewPrecommit(
			p.state.CurrentHeight,
			p.state.CurrentRound,
			block.InvalidHash,
		)
		p.state.CurrentStep = StepPrecommit
		// Always broadcast at the end
		p.broadcaster.Broadcast(precommit)
	}

	// upon f+1 *{currentHeight, round, *, *} and round > currentRound
	if n > p.state.Prevotes.F() && prevote.Height() == p.state.CurrentHeight && prevote.Round() > p.state.CurrentRound {
		p.startRound(prevote.Round())
	}

	p.checkProposeInCurrentHeightAndRoundWithPrevotes()
	if firstTimeExceeding2F {
		p.checkProposeInCurrentHeightAndRoundWithPrevotesForTheFirstTime()
	}
}

func (p *Process) handlePrecommit(precommit *Precommit) {
	p.logger.Debugf("Receive new precommit of height = %v, IsNil= %v", precommit.height, precommit.blockHash.Equal(block.InvalidHash))
	// upon 2f+1 Precommit{currentHeight, currentRound, *} for the first time
	n, _, _, firstTimeExceeding2F := p.state.Precommits.Insert(precommit)
	if firstTimeExceeding2F && precommit.Height() == p.state.CurrentHeight && precommit.Round() == p.state.CurrentRound {
		p.scheduleTimeoutPrecommit(p.state.CurrentHeight, p.state.CurrentRound, p.timer.Timeout(StepPrecommit, p.state.CurrentRound))
	}

	// upon f+1 *{currentHeight, round, *, *} and round > currentRound
	if n > p.state.Precommits.F() && precommit.Height() == p.state.CurrentHeight && precommit.Round() > p.state.CurrentRound {
		p.startRound(precommit.Round())
	}

	p.checkProposeInCurrentHeightWithPrecommits(precommit.Round())
}

// timeoutPropose checks if we have move to a new height, a new round or a new
// step after the timeout. If not, prevote for a invalid block and broadcast
// the vote, then move to prevote step.
func (p *Process) timeoutPropose(height block.Height, round block.Round) {
	if height == p.state.CurrentHeight && round == p.state.CurrentRound && p.state.CurrentStep == StepPropose {
		prevote := NewPrevote(
			p.state.CurrentHeight,
			p.state.CurrentRound,
			block.InvalidHash,
		)
		p.state.CurrentStep = StepPrevote
		// Always broadcast at the end
		p.broadcaster.Broadcast(prevote)
	}
}

func (p *Process) timeoutPrevote(height block.Height, round block.Round) {
	if height == p.state.CurrentHeight && round == p.state.CurrentRound && p.state.CurrentStep == StepPrevote {
		precommit := NewPrecommit(
			p.state.CurrentHeight,
			p.state.CurrentRound,
			block.InvalidHash,
		)
		p.state.CurrentStep = StepPrecommit
		// Always broadcast at the end
		p.broadcaster.Broadcast(precommit)
	}
}

func (p *Process) timeoutPrecommit(height block.Height, round block.Round) {
	if height == p.state.CurrentHeight && round == p.state.CurrentRound {
		p.startRound(p.state.CurrentRound + 1)
	}
}

func (p *Process) scheduleTimeoutPropose(height block.Height, round block.Round, duration time.Duration) {
	go func() {
		time.Sleep(duration)

		p.mu.Lock()
		defer p.mu.Unlock()

		p.timeoutPropose(height, round)
	}()
}

func (p *Process) scheduleTimeoutPrevote(height block.Height, round block.Round, duration time.Duration) {
	go func() {
		time.Sleep(duration)

		p.mu.Lock()
		defer p.mu.Unlock()

		p.timeoutPrevote(height, round)
	}()
}

func (p *Process) scheduleTimeoutPrecommit(height block.Height, round block.Round, duration time.Duration) {
	go func() {
		time.Sleep(duration)

		p.mu.Lock()
		defer p.mu.Unlock()

		p.timeoutPrecommit(height, round)
	}()
}

func (p *Process) checkProposeInCurrentHeightAndRoundWithPrevotes() {
	// upon Propose{currentHeight, currentRound, block, validRound} from Schedule(currentHeight, currentRound)
	m := p.state.Proposals.QueryByHeightRoundSignatory(p.state.CurrentHeight, p.state.CurrentRound, p.scheduler.Schedule(p.state.CurrentHeight, p.state.CurrentRound))
	if m == nil {
		return
	}
	propose := m.(*Propose)

	if propose.ValidRound() > block.InvalidRound {
		// and 2f+1 Prevote{currentHeight, validRound, blockHash}
		n := p.state.Prevotes.QueryByHeightRoundBlockHash(p.state.CurrentHeight, propose.ValidRound(), propose.BlockHash())
		if n > 2*p.state.Prevotes.F() {
			// while step = StepPropose and validRound >= 0 and validRound < currentRound
			if p.state.CurrentStep == StepPropose && propose.ValidRound() < p.state.CurrentRound {
				var prevote *Prevote
				if p.validator.IsBlockValid(propose.Block(), true) && (p.state.LockedRound <= propose.ValidRound() || p.state.LockedBlock.Equal(propose.Block())) {
					prevote = NewPrevote(
						p.state.CurrentHeight,
						p.state.CurrentRound,
						propose.Block().Hash(),
					)
				} else {
					prevote = NewPrevote(
						p.state.CurrentHeight,
						p.state.CurrentRound,
						block.InvalidHash,
					)
				}

				p.state.CurrentStep = StepPrevote
				// Always broadcast at the end
				p.broadcaster.Broadcast(prevote)
			}
		}
	}
}

// checkProposeInCurrentHeightAndRoundWithPrevotesForTheFirstTime must only be
// called when a Propose and 2f+1 Prevotes has been seen for the first time at
// the current `block.Height` and `block.Round`. This can happen when a Propose
// is seen for the first time at the current `block.Height` and `block.Round`,
// or, when a Prevote is seen for the first time at the current `block.Height`
// and `block.Round`.
func (p *Process) checkProposeInCurrentHeightAndRoundWithPrevotesForTheFirstTime() {
	// upon Propose{currentHeight, currentRound, block, *} from Schedule(currentHeight, currentRound)
	m := p.state.Proposals.QueryByHeightRoundSignatory(p.state.CurrentHeight, p.state.CurrentRound, p.scheduler.Schedule(p.state.CurrentHeight, p.state.CurrentRound))
	if m == nil {
		return
	}
	propose := m.(*Propose)

	// and 2f+1 Prevote{currentHeight, currentRound, blockHash} while Validate(block) and step >= StepPrevote for the first time
	n := p.state.Prevotes.QueryByHeightRoundBlockHash(p.state.CurrentHeight, p.state.CurrentRound, propose.BlockHash())
	if n > 2*p.state.Prevotes.F() {
		if p.state.CurrentStep >= StepPrevote && p.validator.IsBlockValid(propose.Block(), true) {
			p.state.ValidBlock = propose.Block()
			p.state.ValidRound = p.state.CurrentRound
			if p.state.CurrentStep == StepPrevote {
				p.state.LockedBlock = propose.Block()
				p.state.LockedRound = p.state.CurrentRound
				p.state.CurrentStep = StepPrecommit
				precommit := NewPrecommit(
					p.state.CurrentHeight,
					p.state.CurrentRound,
					propose.Block().Hash(),
				)

				// Always broadcast at the end
				p.broadcaster.Broadcast(precommit)
			}
		}
	}
}

func (p *Process) checkProposeInCurrentHeightWithPrecommits(round block.Round) {
	// upon Propose{currentHeight, round, block, *} from Schedule(currentHeight, round)
	m := p.state.Proposals.QueryByHeightRoundSignatory(p.state.CurrentHeight, round, p.scheduler.Schedule(p.state.CurrentHeight, round))
	if m == nil {
		return
	}
	propose := m.(*Propose)

	// and 2f+1 Precommits{currentHeight, round, blockHash}
	n := p.state.Precommits.QueryByHeightRoundBlockHash(p.state.CurrentHeight, round, propose.BlockHash())
	if n > 2*p.state.Precommits.F() {
		// while !BlockExistsAtHeight(currentHeight)
		if !p.blockchain.BlockExistsAtHeight(p.state.CurrentHeight) {
			if p.validator.IsBlockValid(propose.Block(), true) {
				p.blockchain.InsertBlockAtHeight(p.state.CurrentHeight, propose.Block())
				p.state.CurrentHeight++
				p.state.Reset(p.state.CurrentHeight - 1)
				if p.observer != nil {
					p.observer.DidCommitBlock(p.state.CurrentHeight - 1)
				}
				p.logger.Infof("[✅] block of height %v is finalized", propose.height)
				p.startRound(0)
			}
		}
	}
}

func (p *Process) syncLatestCommit(latestCommit LatestCommit) {
	// Check that the latest commit is from the future
	if latestCommit.Block.Header().Height() > p.state.CurrentHeight {
		return
	}

	// Check the proposed block and previous block with checking historical data..
	// It needs the validator to store the previous execute state.
	if !p.validator.IsBlockValid(latestCommit.Block, false) {
		return
	}

	// Validate the commits
	signatories := map[id.Signatory]struct{}{}
	baseBlock, ok := p.blockchain.BlockAtHeight(0) // FIXME : NEED TO CONSIDER HOW TO DO WHEN A REBASE
	if !ok {
		panic("no genesis block")
	}
	for _, sig := range baseBlock.Header().Signatories() {
		signatories[sig] = struct{}{}
	}
	for _, commit := range latestCommit.Precommits {
		if err := Verify(&commit); err != nil {
			return
		}
		if _, ok := signatories[commit.signatory]; !ok {
			return
		}
		if !commit.blockHash.Equal(latestCommit.Block.Hash()) {
			return
		}
		if commit.height != latestCommit.Block.Header().Height() {
			return
		}
		if commit.round != latestCommit.Block.Header().Round() {
			return
		}
	}

	// Check we have 2f+1 distinct commits
	signatories = map[id.Signatory]struct{}{}
	for _, commit := range latestCommit.Precommits {
		signatories[commit.Signatory()] = struct{}{}
	}
	if len(signatories) < 2*p.state.Proposals.f+1 {
		return
	}

	// if the commits are valid, store the block if we don't have one
	if !p.blockchain.BlockExistsAtHeight(latestCommit.Block.Header().Height()) {
		p.blockchain.InsertBlockAtHeight(latestCommit.Block.Header().Height(), latestCommit.Block)
	}
	p.state.CurrentHeight = latestCommit.Block.Header().Height() + 1
	p.state.CurrentRound = 0
	p.state.Reset(latestCommit.Block.Header().Height())
	p.startRound(p.state.CurrentRound)
}
