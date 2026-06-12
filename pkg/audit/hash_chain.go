// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/Bionic-AI-Solutions/cluster-health-autopilot/pkg/ai"
)

// Tamper-evident audit trail.
//
// The ai.AuditSink interface is content-addressable in spirit (you can
// hash the bytes after the fact), but a downstream attacker with write
// access to the sink can edit history without leaving a mark.
// Hash-chained entries close that gap:
//
//	entry_N.prev_hash = sha256( canonicalize(entry_{N-1}_with_prev_hash) )
//
// A verifier walking the chain catches any mutation: if entry M was
// edited, entry M's recomputed self-hash (or entry M+1's prev_hash
// link) will not match.
//
// This is NOT a cryptographic-signature scheme — it's tamper EVIDENCE,
// not tamper RESISTANCE. The chain detects mutation; SIEM ingestion
// alerts on the gap. For full tamper resistance, layer the chain over
// an append-only Vault audit device or an immutable log store. Signed
// checkpoints (CheckpointSigner) additionally anchor the chain TAIL
// against truncation, which the hash links alone cannot detect.

// EntryTimeKey is the Details key under which ChainedSink stamps the
// per-entry wall-clock time (RFC3339Nano). It lives inside the hashed
// payload, so the timestamp is tamper-evident.
const EntryTimeKey = "entry_time"

// CheckpointType is the AuditEvent.Type of a chain-anchoring checkpoint
// entry. Verifiers recognise it to locate the latest anchored tail.
const CheckpointType = "audit.checkpoint"

// defaultCheckpointEvery is the data-event cadence for automatic
// checkpoints when ChainOptions.CheckpointEvery is unset.
const defaultCheckpointEvery = 100

// nowFunc is the clock seam. Overridable in tests so timestamps are
// deterministic; production uses time.Now().
var nowFunc = time.Now

// CheckpointSigner signs the current chain head hash so the tail of the
// log is cryptographically anchored. Callers adapt their signer (for
// example an Ed25519 key) to this narrow shape; passing no signer keeps
// the chain working but leaves the tail anchor absent.
type CheckpointSigner interface {
	// SignCheckpoint returns an opaque (typically base64) signature over
	// data plus the identifier of the signing key.
	SignCheckpoint(data []byte) (sig string, keyID string, err error)
}

// ChainOptions configures NewChainedSinkResuming.
type ChainOptions struct {
	// Signer, when non-nil, signs periodic + caller-triggered CHECKPOINT
	// entries that anchor the chain tail. Nil = unsigned checkpoints (the
	// chain still continues; tail truncation past the last checkpoint is
	// undetectable without an external anchor).
	Signer CheckpointSigner

	// CheckpointEvery emits a checkpoint after this many data events.
	// Zero or negative selects the package default (100). Callers that
	// own a closeable store should also call WriteCheckpoint on close so
	// the final tail is anchored even when the cadence didn't fire.
	CheckpointEvery int
}

// ChainedSink wraps any ai.AuditSink and prepends a `prev_hash` field to
// each event's Details map before delegating. Thread-safe.
type ChainedSink struct {
	inner ai.AuditSink

	mu       sync.Mutex
	lastHash string // hex(sha256) of the last successfully chained event

	// Checkpoint anchoring. signer, when non-nil, signs the head hash
	// into periodic checkpoint entries so the tail is cryptographically
	// anchored. checkpointEvery is the data-event cadence;
	// sinceCheckpoint counts events since the last one.
	signer          CheckpointSigner
	checkpointEvery int
	sinceCheckpoint int
	loggedNoSigner  bool // ensures the "unsigned" notice logs once
}

// NewChainedSink constructs a ChainedSink delegating to inner. The
// initial prev_hash is the empty string ("") — the verifier treats that
// as the chain anchor. No automatic checkpoints are emitted; use
// NewChainedSinkResuming for checkpoint cadence and resumption.
func NewChainedSink(inner ai.AuditSink) *ChainedSink {
	if inner == nil {
		inner = ai.NoOpAuditSink{}
	}
	return &ChainedSink{inner: inner}
}

// NewChainedSinkResuming constructs a ChainedSink whose chain continues
// from resumeHash (the entry_hash of the last record already in the
// caller's store, or "" for a fresh chain) instead of the "" anchor. A
// restart therefore extends the existing chain rather than starting a
// fresh, independently-verifiable segment. Checkpoint cadence and
// signing are configured via opts.
func NewChainedSinkResuming(inner ai.AuditSink, resumeHash string, opts ChainOptions) *ChainedSink {
	if inner == nil {
		inner = ai.NoOpAuditSink{}
	}
	every := opts.CheckpointEvery
	if every <= 0 {
		every = defaultCheckpointEvery
	}
	return &ChainedSink{
		inner:           inner,
		lastHash:        resumeHash,
		signer:          opts.Signer,
		checkpointEvery: every,
	}
}

// Write injects the chain fields, computes the new hash, and delegates.
// If the inner sink returns an error, the chain state is NOT advanced
// (so a downstream retry continues from the same prev_hash).
func (s *ChainedSink) Write(ctx context.Context, e ai.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.Details == nil {
		e.Details = map[string]any{}
	}
	// Bind wall-clock time INSIDE the hashed payload so identical events
	// do not hash identically and time can't be edited without breaking
	// the chain.
	stampEntryTime(&e)
	e.Details["prev_hash"] = s.lastHash

	// Hash the canonical JSON of the event (with prev_hash already
	// embedded). The verifier reproduces this exactly.
	canon, err := canonicalJSON(e)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canon)
	newHash := hex.EncodeToString(sum[:])
	// Embed the new hash in the event so consumers see both the chain
	// link and the self-hash without recomputing.
	e.Details["entry_hash"] = newHash

	if err := s.inner.Write(ctx, e); err != nil {
		return err
	}
	s.lastHash = newHash

	// Periodic checkpoint cadence. Only data events advance the counter;
	// the checkpoint write itself goes through writeCheckpointLocked
	// which does NOT recurse here.
	if s.checkpointEvery > 0 {
		s.sinceCheckpoint++
		if s.sinceCheckpoint >= s.checkpointEvery {
			s.writeCheckpointLocked(ctx)
		}
	}
	return nil
}

// WriteCheckpoint emits a CHECKPOINT entry anchoring the current head.
// Callers that own a closeable store should invoke it on close so the
// tail is anchored even if the cadence didn't fire. Best-effort: a
// signer or sink error does not abort the audit stream.
func (s *ChainedSink) WriteCheckpoint(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeCheckpointLocked(ctx)
}

// writeCheckpointLocked writes a CHECKPOINT entry that chains over the
// current head and (when a signer is configured) carries a signature of
// the head hash. The checkpoint is itself a chained event, so a verifier
// validates it like any other entry; the signature additionally proves
// the head existed at checkpoint time, anchoring the tail against
// truncation.
//
// Caller MUST hold s.mu. Best-effort: a signer or sink error is logged
// once and does not abort the audit stream (audit is non-blocking).
func (s *ChainedSink) writeCheckpointLocked(ctx context.Context) {
	head := s.lastHash
	cp := ai.AuditEvent{
		Type:  CheckpointType,
		Actor: "cha/audit",
		Details: map[string]any{
			"checkpoint_head": head,
		},
	}
	if s.signer != nil {
		sig, kid, err := s.signer.SignCheckpoint([]byte(head))
		if err == nil {
			cp.Details["checkpoint_sig"] = sig
			cp.Details["checkpoint_kid"] = kid
		} else if !s.loggedNoSigner {
			log.Printf("audit: checkpoint signing failed (%v); chain continues unsigned", err)
			s.loggedNoSigner = true
		}
	} else if !s.loggedNoSigner {
		log.Printf("audit: no checkpoint signer configured; chain tail is unsigned (restart-continuing only)")
		s.loggedNoSigner = true
	}

	// Chain the checkpoint exactly like a data event (inline rather than
	// recursing through Write, which would re-enter the cadence counter).
	stampEntryTime(&cp)
	cp.Details["prev_hash"] = s.lastHash
	canon, err := canonicalJSON(cp)
	if err != nil {
		return
	}
	sum := sha256.Sum256(canon)
	newHash := hex.EncodeToString(sum[:])
	cp.Details["entry_hash"] = newHash
	if err := s.inner.Write(ctx, cp); err != nil {
		return
	}
	s.lastHash = newHash
	s.sinceCheckpoint = 0
}

// LastHash returns the most recently chained hash. Useful for tests,
// metrics, and persisting the resume point of an external store.
func (s *ChainedSink) LastHash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHash
}

// VerifyChain walks the given events in order and returns the index of
// the first event whose prev_hash link or recomputed self-hash does not
// match. Returns -1 when the chain is intact.
//
// The events must be in the same order they were written; reordering a
// chained event in storage is itself a tamper indicator.
func VerifyChain(events []ai.AuditEvent) (int, error) {
	prev := ""
	for i, e := range events {
		if e.Details == nil {
			return i, errors.New("chained event missing Details")
		}
		// Check the link from the previous entry.
		got, _ := e.Details["prev_hash"].(string)
		if got != prev {
			return i, nil
		}
		// Recompute the self-hash using the canonical bytes WITHOUT the
		// entry_hash field (which is the output, not part of the chained
		// content).
		clone := cloneAuditEvent(e)
		delete(clone.Details, "entry_hash")
		canon, err := canonicalJSON(clone)
		if err != nil {
			return i, err
		}
		sum := sha256.Sum256(canon)
		recomputed := hex.EncodeToString(sum[:])
		stored, _ := e.Details["entry_hash"].(string)
		if recomputed != stored {
			return i, nil
		}
		prev = stored
	}
	return -1, nil
}

// ChainVerification is the richer result of VerifyChainWithCheckpoints.
type ChainVerification struct {
	// BrokenIndex is the index of the first entry whose link or self-hash
	// fails, or -1 when every link is intact.
	BrokenIndex int

	// LastCheckpointIndex is the index of the last checkpoint entry, or
	// -1 when none is present.
	LastCheckpointIndex int

	// TailAnchored is true when a SIGNED checkpoint exists AND no data
	// events follow it (so the tail is cryptographically anchored). It is
	// false when the log was truncated past its last checkpoint, when the
	// last checkpoint is unsigned, or when there is no checkpoint at all.
	TailAnchored bool
}

// VerifyChainWithCheckpoints walks the chain like VerifyChain but also
// inspects checkpoint anchoring. The hash chain alone CANNOT detect
// tail-truncation (lopping off the last N entries leaves the surviving
// prefix internally consistent). Signed checkpoints close that gap: a
// truncation that removes data events written after the last checkpoint
// leaves the checkpoint as the tail (anchored), but truncating INTO the
// checkpoint-covered region — or removing the checkpoint itself — leaves
// the tail unanchored, which this reports.
//
// "Anchored" means the LAST entry is a signed checkpoint (no data event
// follows it). A signed checkpoint followed by further data events is
// not a tail anchor for those trailing events — only the next checkpoint
// would anchor them.
func VerifyChainWithCheckpoints(events []ai.AuditEvent) ChainVerification {
	res := ChainVerification{BrokenIndex: -1, LastCheckpointIndex: -1}
	if idx, _ := VerifyChain(events); idx >= 0 {
		res.BrokenIndex = idx
		return res
	}
	for i, e := range events {
		if e.Type == CheckpointType {
			res.LastCheckpointIndex = i
		}
	}
	if res.LastCheckpointIndex < 0 {
		return res // no checkpoint at all → tail unanchored
	}
	last := events[len(events)-1]
	// The tail is anchored only when the final entry IS a signed
	// checkpoint (no trailing data events) — that proves the head hash
	// at checkpoint time matches the surviving tail.
	if last.Type == CheckpointType && isSignedCheckpoint(last) {
		res.TailAnchored = true
	}
	return res
}

// isSignedCheckpoint reports whether e is a checkpoint carrying a
// non-empty signature (an unsigned checkpoint does not anchor the tail
// because it has no external proof of the head hash).
func isSignedCheckpoint(e ai.AuditEvent) bool {
	if e.Type != CheckpointType || e.Details == nil {
		return false
	}
	sig, _ := e.Details["checkpoint_sig"].(string)
	return sig != ""
}

// stampEntryTime writes the RFC3339Nano wall-clock time into e.Details
// under EntryTimeKey, unless one is already present (so a replayed /
// re-chained entry keeps its original time). Caller guarantees Details
// is non-nil.
func stampEntryTime(e *ai.AuditEvent) {
	if _, ok := e.Details[EntryTimeKey]; ok {
		return
	}
	e.Details[EntryTimeKey] = nowFunc().UTC().Format(time.RFC3339Nano)
}

// canonicalJSON marshals e for stable hashes. encoding/json sorts map keys
// alphabetically, so json.Marshal of AuditEvent — whose Details field is
// map[string]any — is deterministic; struct fields are always marshaled in
// declaration order. This is tamper-evidence, not cryptographic
// authentication; hash stability only requires ordering to be
// deterministic within one process/runtime.
func canonicalJSON(e ai.AuditEvent) ([]byte, error) {
	return json.Marshal(e)
}

// cloneAuditEvent returns a shallow-but-Details-deep copy. Details is
// deep-copied at the top level so verifier code can mutate it.
func cloneAuditEvent(e ai.AuditEvent) ai.AuditEvent {
	out := e
	if e.Details != nil {
		out.Details = make(map[string]any, len(e.Details))
		for k, v := range e.Details {
			out.Details[k] = v
		}
	}
	return out
}
