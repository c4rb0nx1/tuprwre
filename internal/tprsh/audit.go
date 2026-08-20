// Package tprsh is a proof-of-concept hardened interception shell built on an
// in-process interpreter (mvdan.cc/sh) rather than a bash wrapper. Every
// command is vetted against a binary+argument allowlist before it can run, the
// accepted grammar is reduced to remove escape features, the child environment
// is reset, and every attempt is written to a tamper-evident, parent-side
// audit log the launched process cannot reach.
package tprsh

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Decision is the verdict recorded for one command attempt.
type Decision string

const (
	DecisionStart  Decision = "start"
	DecisionAllow  Decision = "allow"
	DecisionDeny   Decision = "deny"
	DecisionFinish Decision = "finish"
)

// Record is one entry in the audit chain. Hash chains over Prev so any edit,
// reorder, or deletion of an interior record breaks every later hash.
type Record struct {
	Seq      int      `json:"seq"`
	TS       string   `json:"ts"`
	Event    Decision `json:"event"`
	Cmd      string   `json:"cmd"`
	Args     []string `json:"args"`
	Cwd      string   `json:"cwd"`
	Reason   string   `json:"reason,omitempty"`
	ExitCode int      `json:"exit_code"`
	Prev     string   `json:"prev"`
	Hash     string   `json:"hash"`
}

// Auditor appends hash-chained records to an append-only log. The log path is
// held only by the parent process; children never receive a handle to it.
type Auditor struct {
	mu   sync.Mutex
	f    *os.File
	seq  int
	head string
	now  func() time.Time
}

// NewAuditor opens (creating if needed) an append-only log at path and
// continues the existing hash chain. Resuming the chain rather than starting a
// fresh one per process is what makes deleting a whole session detectable: the
// next session's first record would no longer link to the last surviving hash.
func NewAuditor(path string) (*Auditor, error) {
	seq, head, err := tailChain(path)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &Auditor{f: f, seq: seq, head: head, now: time.Now}, nil
}

// tailChain returns the sequence number and hash of the last record already in
// the log, or zero values when the log does not exist yet.
func tailChain(path string) (int, string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("read audit log: %w", err)
	}

	var last Record
	found := false
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var r Record
		if err := dec.Decode(&r); err != nil {
			return 0, "", fmt.Errorf("audit log is corrupt near record %d: %w", last.Seq+1, err)
		}
		last, found = r, true
	}
	if !found {
		return 0, "", nil
	}
	return last.Seq, last.Hash, nil
}

// hashRecord computes H(prev || canonical(record without hash)).
func hashRecord(r Record) string {
	r.Hash = ""
	body, _ := json.Marshal(r)
	sum := sha256.Sum256(append([]byte(r.Prev), body...))
	return hex.EncodeToString(sum[:])
}

// Append writes one record, chaining it onto the current head, and fsyncs so
// the entry is durable before the caller proceeds to exec.
func (a *Auditor) Append(event Decision, cmd string, args []string, cwd, reason string, exit int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.seq++
	r := Record{
		Seq:      a.seq,
		TS:       a.now().UTC().Format(time.RFC3339Nano),
		Event:    event,
		Cmd:      cmd,
		Args:     args,
		Cwd:      cwd,
		Reason:   reason,
		ExitCode: exit,
		Prev:     a.head,
	}
	r.Hash = hashRecord(r)

	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := a.f.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := a.f.Sync(); err != nil {
		return err
	}
	a.head = r.Hash
	return nil
}

// Close closes the underlying file.
func (a *Auditor) Close() error {
	if a.f == nil {
		return nil
	}
	return a.f.Close()
}

// LoadAndVerify reads a log and verifies the hash chain is intact. It returns
// the records and, if the chain is broken, the sequence number where
// verification first failed.
func LoadAndVerify(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []Record
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var r Record
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("decode audit record: %w", err)
		}
		records = append(records, r)
	}

	prev := ""
	for _, r := range records {
		if r.Prev != prev {
			return records, fmt.Errorf("audit chain broken at seq %d: prev mismatch", r.Seq)
		}
		if want := hashRecord(r); want != r.Hash {
			return records, fmt.Errorf("audit chain broken at seq %d: hash mismatch (record tampered)", r.Seq)
		}
		prev = r.Hash
	}
	return records, nil
}
