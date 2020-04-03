package replica

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/renproject/hyperdrive/process"
	"github.com/renproject/id"
)

// A Broadcaster is used to send signed, shard-specific, Messages to one or all
// Replicas in the network.
//
// For the consensus algorithm to work correctly, it is assumed that all honest
// replicas will eventually deliver all messages to all other honest replicas.
// The specific message ordering is not important. In practice, the Prevote
// messages are the only messages that must guarantee delivery when guaranteeing
// correctness.
type Broadcaster interface {
	Broadcast(Message)
	Cast(id.Signatory, Message)
}

type signer struct {
	broadcaster Broadcaster
	shard       Shard
	privKey     ecdsa.PrivateKey
}

// newSigner returns a `process.Broadcaster` that accepts `process.Messages`,
// signs them, associates them with a Shard, and re-broadcasts them.
func newSigner(broadcaster Broadcaster, shard Shard, privKey ecdsa.PrivateKey) process.Broadcaster {
	return &signer{
		broadcaster: broadcaster,
		shard:       shard,
		privKey:     privKey,
	}
}

// Broadcast implements the `process.Broadcaster` interface.
func (broadcaster *signer) Broadcast(m process.Message) {
	if err := process.Sign(m, broadcaster.privKey); err != nil {
		panic(fmt.Errorf("invariant violation: error broadcasting message: %v", err))
	}
	broadcaster.broadcaster.Broadcast(SignMessage(m, broadcaster.shard, broadcaster.privKey))
}

// Cast implements the `process.Broadcaster` interface.
func (broadcaster *signer) Cast(to id.Signatory, m process.Message) {
	if err := process.Sign(m, broadcaster.privKey); err != nil {
		panic(fmt.Errorf("invariant violation: error broadcasting message: %v", err))
	}
	broadcaster.broadcaster.Cast(to, SignMessage(m, broadcaster.shard, broadcaster.privKey))
}

// SignMessage with the Shard included. It is assumed that the `process.Message`
// is already signed.
func SignMessage(m process.Message, shard Shard, privKey ecdsa.PrivateKey) Message {
	mWithShard := Message{
		Message: m,
		Shard:   shard,
	}
	mWithShardHash := mWithShard.SigHash()
	signature, err := crypto.Sign(mWithShardHash[:], &privKey)
	if err != nil {
		panic(fmt.Errorf("invariant violation: error broadcasting message: %v", err))
	}
	copy(mWithShard.Signature[:], signature)
	return mWithShard
}
