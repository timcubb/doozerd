package paxos

import (
	"junta/assert"
	"junta/store"
	"testing"
)

func selfRefNewManager(extraNodes ...string) (*Manager, *store.Store) {
	p := make(FakePutter, 1)
	st := store.New()
	m := NewManager(uint64(len(extraNodes)+2), 1, st, p)
	st.Apply(uint64(1), mustEncodeSet(membersKey+"/"+m.Self, m.Self+"addr"))
	for i, node := range extraNodes {
		st.Apply(uint64(i+2), mustEncodeSet(membersKey+"/"+node, node+"addr"))
	}
	p[0] = m
	return m, st
}

func TestProposeAndLearn(t *testing.T) {
	exp := "foo"
	m, _ := selfRefNewManager()

	got, _ := m.Propose(exp)
	assert.Equal(t, exp, got, "")
}

func TestProposeAndRecv(t *testing.T) {
	exp := "foo"
	m, _ := selfRefNewManager()

	got, _ := m.Propose(exp)
	assert.Equal(t, exp, got, "")

	seqn, v := m.Recv()
	assert.Equal(t, uint64(2), seqn, "")
	assert.Equal(t, exp, v, "")
}

func TestProposeAndRecvAltStart(t *testing.T) {
	exp := "foo"
	m, _ := selfRefNewManager()

	got, _ := m.Propose(exp)
	assert.Equal(t, exp, got, "")

	seqn, v := m.Recv()
	assert.Equal(t, uint64(2), seqn, "")
	assert.Equal(t, exp, v, "")
}

func TestProposeAndRecvMultiple(t *testing.T) {
	exp := []string{"/foo", "/bar"}
	seqnexp := []uint64{2, 3}
	m, st := selfRefNewManager()

	got0, _ := m.Propose(exp[0])
	assert.Equal(t, exp[0], got0, "")

	seqn0, v0 := m.Recv()
	assert.Equal(t, seqnexp[0], seqn0, "seqn 1")
	assert.Equal(t, exp[0], v0, "")

	st.Apply(seqn0, v0)

	got1, _ := m.Propose(exp[1])
	assert.Equal(t, exp[1], got1, "")

	seqn1, v1 := m.Recv()
	assert.Equal(t, seqnexp[1], seqn1, "seqn 1")
	assert.Equal(t, exp[1], v1, "")
}

func TestNewInstanceBecauseOfMessage(t *testing.T) {
	exp := "foo"
	m, _ := selfRefNewManager()

	m.Put(newVoteFrom(1, 1, exp))
	seqn, v := m.Recv()
	assert.Equal(t, uint64(1), seqn, "")
	assert.Equal(t, exp, v, "")
}

func TestNewInstanceBecauseOfMessageTriangulate(t *testing.T) {
	exp := "bar"
	m, _ := selfRefNewManager()

	m.Put(newVoteFrom(1, 1, exp))
	seqn, v := m.Recv()
	assert.Equal(t, uint64(1), seqn, "")
	assert.Equal(t, exp, v, "")
}

func TestUnusedSeqn(t *testing.T) {
	exp1, exp2 := "foo", "bar"
	m, _ := selfRefNewManager()

	m.Put(newVoteFrom(1, 1, exp1))
	seqn, v := m.Recv()
	assert.Equal(t, uint64(1), seqn, "")
	assert.Equal(t, exp1, v, "")

	got, _ := m.Propose(exp2)
	assert.Equal(t, exp2, got, "")
	seqn, v = m.Recv()
	assert.Equal(t, uint64(2), seqn, "")
	assert.Equal(t, exp2, v, "")
}

func TestIgnoreMalformedMsg(t *testing.T) {
	m, _ := selfRefNewManager()

	m.Put(resize(newVoteFrom(1, 1, ""), -1))

	got, _ := m.Propose("y")
	assert.Equal(t, "y", got, "")

	seqn, v := m.Recv()
	assert.Equal(t, uint64(2), seqn, "")
	assert.Equal(t, "y", v, "")
}

func mustEncodeSet(k, v string) string {
	m, err := store.EncodeSet(k, v, store.Clobber)
	if err != nil {
		panic(err)
	}
	return m
}

func TestReadFromStore(t *testing.T) {
	// The cluster initially has 1 node (quorum of 1).
	st := store.New()
	p := make(ChanPutCloser)
	m := NewManager(1, 1, st, p)
	st.Apply(1, mustEncodeSet(membersKey+"/"+m.Self, ""))

	// Fire up a new instance with a vote message. This instance should block
	// trying to read the list of members. If it doesn't wait, it'll
	// immediately learn the value `x`.
	in := newVoteFrom(0, 1, "x")
	in.SetSeqn(3)
	m.Put(in)

	// Satisfy the sync read of data members above. After this, there will be
	// 2 nodes in the cluster, making the quorum 2.
	st.Apply(2, mustEncodeSet(membersKey+"/b", ""))

	// Now try to make it learn a new value with 2 votes to meet the new
	// quorum.
	exp := "y"
	in = newVoteFrom(0, 2, exp)
	in.SetSeqn(3)
	m.Put(in)
	in = newVoteFrom(1, 2, exp)
	in.SetSeqn(3)
	m.Put(in)

	seqn, v := m.Recv()
	assert.Equal(t, uint64(3), seqn, "")
	assert.Equal(t, exp, v, "")
}

type putFunc func(Msg)

func (pf putFunc) Put(msg Msg) {
	go pf(msg)
}

func (pf putFunc) Close() {}

func TestManagerPutFrom(t *testing.T) {
	exp := "bar"
	seqnExp := uint64(4)
	fromAddr := "y"
	fromIndex := 1 // [a, b, c].indexof(b) => 1

	p := make(FakePutter, 1)
	st := store.New()
	m := NewManager(4, 1, st, p)
	p[0] = m

	st.Apply(uint64(1), mustEncodeSet(membersKey+"/"+m.Self, "x"))
	st.Apply(uint64(2), mustEncodeSet(membersKey+"/b", "y"))
	st.Apply(uint64(3), mustEncodeSet(membersKey+"/c", "z"))

	froms := make(chan int)

	fp := putFunc(func (msg Msg) {
		froms <- msg.From()
	})

	seqn, it := m.getInstance(seqnExp)
	assert.Equal(t, seqnExp, seqn)
	it.cPutter = fp
	it.aPutter = fp
	it.lPutter = fp

	v1 := newVote(1, exp)
	v1.SetSeqn(seqnExp)
	m.PutFrom(fromAddr, v1)

	assert.Equal(t, fromIndex, <-froms, "")
	assert.Equal(t, fromIndex, <-froms, "")
	assert.Equal(t, fromIndex, <-froms, "")
}

func TestManagerAddrsFor(t *testing.T) {
	m, _ := selfRefNewManager()
	msg := newInvite(1)
	msg.SetSeqn(1)
	assert.Equal(t, []string{"aaddr"}, m.AddrsFor(msg))
}

func TestManagerGetInstanceForPropose(t *testing.T) {
	m, _ := selfRefNewManager()
	seqn, _ := m.getInstance(0)
	assert.Equal(t, uint64(2), seqn)
}
