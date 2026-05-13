// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package assertions

import (
	"context"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/bold/containers/threadsafe"
	"github.com/offchainlabs/nitro/bold/protocol"
)

// TestRecordAgreedAssertionDoesNotDowngradeLatestAgreedAssertion pins the
// structural invariant that fixes the prod "honest validator self-challenge"
// incident: latestAgreedAssertion must never move backward, even when the
// catchup goroutine writes an assertion that is an ancestor of (rather than a
// direct child of) the current value.
//
// Failure mode this test guards against: sync advances latestAgreedAssertion
// to A2; the slow RPC-bound catchup loop later finishes its write for A1
// (which is A2's ancestor). An unconditional advance would set
// latestAgreedAssertion back to A1, causing the next sync chunk to start its
// agreement-cursor at A1 instead of A2 and therefore skip the agreement check
// for any assertion whose parent is A2. Skipped assertions hit
// respondToAnyInvalidAssertions' "canonical parent, non-canonical self" branch
// and trigger a self-challenge against our own canonical assertion.
func TestRecordAgreedAssertionDoesNotDowngradeLatestAgreedAssertion(t *testing.T) {
	a0 := protocol.AssertionHash{Hash: hashFromString("A0")}
	a1 := protocol.AssertionHash{Hash: hashFromString("A1")}
	a2 := protocol.AssertionHash{Hash: hashFromString("A2")}

	a0Info := &protocol.AssertionCreatedInfo{AssertionHash: a0, InboxMaxCount: big.NewInt(1)}
	a1Info := &protocol.AssertionCreatedInfo{AssertionHash: a1, ParentAssertionHash: a0, InboxMaxCount: big.NewInt(2)}
	a2Info := &protocol.AssertionCreatedInfo{AssertionHash: a2, ParentAssertionHash: a1, InboxMaxCount: big.NewInt(3)}

	m := managerWithCanonical(t, a2, map[protocol.AssertionHash]*protocol.AssertionCreatedInfo{
		a0: a0Info,
		a1: a1Info,
		a2: a2Info,
	})

	// Slow catchup belatedly applies A1 (an ancestor of latestAgreedAssertion).
	// A1's parent is A0, not A2, so the cursor must NOT move.
	m.applyRecordAgreedAssertion(a1Info)

	require.Equal(t,
		a2,
		m.assertionChainData.latestAgreedAssertion,
		"latestAgreedAssertion was downgraded from A2 back to an ancestor — "+
			"this is the bug that triggers honest-validator self-challenge")

	// canonicalAssertions/submittedAssertions must still be append-only.
	_, hasA1 := m.assertionChainData.canonicalAssertions[a1]
	require.True(t, hasA1, "A1 must still be present in canonicalAssertions")
	require.True(t, m.submittedAssertions.Has(a1), "A1 must be tracked in submittedAssertions")
}

// TestRecordAgreedAssertionAdvancesOnDirectChild pins the positive side of the
// invariant: when the supplied assertion IS a direct child of the current
// latestAgreedAssertion, the advance must succeed. Without this, the catchup
// loop can't make progress.
func TestRecordAgreedAssertionAdvancesOnDirectChild(t *testing.T) {
	a0 := protocol.AssertionHash{Hash: hashFromString("A0")}
	a1 := protocol.AssertionHash{Hash: hashFromString("A1")}

	a0Info := &protocol.AssertionCreatedInfo{AssertionHash: a0, InboxMaxCount: big.NewInt(1)}
	a1Info := &protocol.AssertionCreatedInfo{AssertionHash: a1, ParentAssertionHash: a0, InboxMaxCount: big.NewInt(2)}

	m := managerWithCanonical(t, a0, map[protocol.AssertionHash]*protocol.AssertionCreatedInfo{a0: a0Info})

	m.applyRecordAgreedAssertion(a1Info)

	require.Equal(t, a1, m.assertionChainData.latestAgreedAssertion,
		"latestAgreedAssertion should advance to a direct child of the current value")
	_, hasA1 := m.assertionChainData.canonicalAssertions[a1]
	require.True(t, hasA1)
}

// TestRecordAgreedAssertionAllowsOverflowAdvance pins the choice of parent-hash
// linkage (not numeric ordering on InboxMaxCount) for the advance check.
//
// Overflow assertions are created when the machine stops mid-batch because it
// hit the per-assertion block-height cap before consuming the next inbox
// position. Their InboxMaxCount equals their parent's. A numeric check like
// "child.InboxMaxCount > parent.InboxMaxCount" would refuse the advance and
// pin catchup at the overflow parent forever, never making progress.
//
// This test would FAIL under a numeric implementation and PASSES under the
// parent-hash implementation.
func TestRecordAgreedAssertionAllowsOverflowAdvance(t *testing.T) {
	parent := protocol.AssertionHash{Hash: hashFromString("parent")}
	child := protocol.AssertionHash{Hash: hashFromString("child-overflow")}

	const sharedInboxMaxCount = 42
	parentInfo := &protocol.AssertionCreatedInfo{
		AssertionHash: parent,
		InboxMaxCount: big.NewInt(sharedInboxMaxCount),
	}
	childInfo := &protocol.AssertionCreatedInfo{
		AssertionHash:       child,
		ParentAssertionHash: parent,
		// Same as parent — the overflow case. A numeric check would refuse.
		InboxMaxCount: big.NewInt(sharedInboxMaxCount),
	}

	m := managerWithCanonical(t, parent, map[protocol.AssertionHash]*protocol.AssertionCreatedInfo{parent: parentInfo})

	m.applyRecordAgreedAssertion(childInfo)

	require.Equal(t, child, m.assertionChainData.latestAgreedAssertion,
		"overflow child (same InboxMaxCount as parent) must be allowed to advance "+
			"the chain pointer — a numeric-ordering check would have left catchup stuck")
}

// TestNoSelfChallengeAfterCursorDowngradeAttempt is an end-to-end regression
// test that walks the full prod scenario: a slow catchup goroutine attempts
// to write an ancestor of the current latestAgreedAssertion (the downgrade
// race), and then a sync chunk arrives carrying a new canonical assertion.
// Without the parent-link guard in applyRecordAgreedAssertion, the cursor
// would downgrade, the new assertion would be skipped by
// findCanonicalAssertionBranch, and respondToAnyInvalidAssertions would fire
// the rival path against an honest assertion. This test asserts that
// end-to-end none of that happens.
//
// For finer-grained coverage of each fix half in isolation:
//   - applyRecordAgreedAssertion parent-link guard:
//     TestRecordAgreedAssertionDoesNotDowngradeLatestAgreedAssertion
//   - maybePostRivalAssertionAndChallenge same-hash short-circuit:
//     TestSelfChallengeBugWhenStateProviderReturnsTransientWrongRoot
func TestNoSelfChallengeAfterCursorDowngradeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := numToAssertionHash(0)
	a14 := numToAssertionHash(14)
	a15 := numToAssertionHash(15)

	gInfo := &protocol.AssertionCreatedInfo{
		AssertionHash: g,
		InboxMaxCount: big.NewInt(14),
		AfterState:    numToState(0, t),
	}
	a14Info := &protocol.AssertionCreatedInfo{
		AssertionHash:       a14,
		ParentAssertionHash: g,
		InboxMaxCount:       big.NewInt(15),
		AfterState:          numToState(14, t),
	}
	a15Info := &protocol.AssertionCreatedInfo{
		AssertionHash:       a15,
		ParentAssertionHash: a14,
		InboxMaxCount:       big.NewInt(16),
		AfterState:          numToState(15, t),
	}

	// State provider agrees with A15 when asked about parent=A14
	// (parent.InboxMaxCount == 15 is the lookup key in mockStateProvider).
	provider := &mockStateProvider{
		agreesWith: map[uint64]*protocol.AssertionCreatedInfo{
			15: a15Info,
		},
	}

	m := &Manager{
		execProvider:                provider,
		observedCanonicalAssertions: make(chan protocol.AssertionHash, 16),
		submittedAssertions: threadsafe.NewLruSet(
			1024,
			threadsafe.LruSetWithMetric[protocol.AssertionHash]("submittedAssertions"),
		),
		confirming: threadsafe.NewLruSet[protocol.AssertionHash](1024),
		assertionChainData: &assertionChainData{
			latestAgreedAssertion: a14,
			canonicalAssertions: map[protocol.AssertionHash]*protocol.AssertionCreatedInfo{
				g:   gInfo,
				a14: a14Info,
			},
		},
	}
	// Drain the confirmation queue so sendToConfirmationQueue doesn't block.
	go func() {
		for {
			select {
			case <-m.observedCanonicalAssertions:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Step 1: slow catchup belatedly tries to advance to G (an ancestor of A14).
	m.applyRecordAgreedAssertion(gInfo)

	// (a) Cursor must NOT move backward — the parent-link guard rejects the
	// ancestor write. Failure here means applyRecordAgreedAssertion regressed.
	require.Equal(
		t,
		a14,
		m.assertionChainData.latestAgreedAssertion,
		"cursor downgraded from A14 to ancestor G — applyRecordAgreedAssertion parent-link guard regressed",
	)

	// Step 2: a sync chunk arrives carrying A15 (canonical child of A14). With
	// cursor still at A14, findCanonicalAssertionBranch's cursor matches
	// A15's parent and the agreement check fires.
	chunk := []assertionAndParentCreationInfo{
		{parent: a14Info, assertion: a15Info},
	}
	require.NoError(t, m.findCanonicalAssertionBranch(ctx, chunk))

	// (b) A15 must be in canonicalAssertions. A regressed cursor would have
	// caused findCanonicalAssertionBranch to skip A15 entirely.
	_, hasA15 := m.assertionChainData.canonicalAssertions[a15]
	require.True(
		t,
		hasA15,
		"A15 not added to canonicalAssertions — findCanonicalAssertionBranch skipped it, "+
			"implying the cursor was downgraded by the catchup write",
	)

	// Step 3: respondToAnyInvalidAssertions runs on the same chunk. Because
	// A15 is now canonical, the "canonical parent, non-canonical self" branch
	// does not fire and no rival is posted.
	poster := &recordingMockRivalPoster{}
	require.NoError(t, m.respondToAnyInvalidAssertions(ctx, chunk, poster))

	// (c) Zero rival-path invocations — the spurious self-challenge that
	// drove the prod alert never fires when both fixes are in place.
	require.Empty(
		t,
		poster.calls,
		"rival path fired against a canonical assertion — this is the full integrated regression",
	)
}

// recordingMockRivalPoster captures every maybePostRivalAssertionAndChallenge
// invocation. Returns nil so the caller treats the call as a no-op.
type recordingMockRivalPoster struct {
	calls []protocol.AssertionHash
}

func (r *recordingMockRivalPoster) maybePostRivalAssertionAndChallenge(
	_ context.Context,
	args rivalPosterArgs,
) (*protocol.AssertionCreatedInfo, error) {
	r.calls = append(r.calls, args.invalidAssertion.AssertionHash)
	return nil, nil
}

// managerWithCanonical builds a Manager with only the fields recordAgreedAssertion
// touches, pre-seeded with the supplied canonical map and latestAgreedAssertion.
func managerWithCanonical(
	t *testing.T,
	latestAgreed protocol.AssertionHash,
	canonical map[protocol.AssertionHash]*protocol.AssertionCreatedInfo,
) *Manager {
	t.Helper()
	return &Manager{
		assertionChainData: &assertionChainData{
			latestAgreedAssertion: latestAgreed,
			canonicalAssertions:   canonical,
		},
		submittedAssertions: threadsafe.NewLruSet(
			1024,
			threadsafe.LruSetWithMetric[protocol.AssertionHash]("submittedAssertions"),
		),
	}
}

func hashFromString(s string) [32]byte {
	var h [32]byte
	copy(h[:], s)
	return h
}
