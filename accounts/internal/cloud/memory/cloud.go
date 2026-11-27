// Package memory implements the sandbox cloud.
package memory

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

// Sandbox latency, resource limits and instance identity.
const (
	BootDelay        = 20 * time.Second
	StopDelay        = 10 * time.Second
	TerminateDelay   = 10 * time.Second
	VolumeDelay      = 5 * time.Second
	ModifyDelay      = 10 * time.Second
	ChangeDelay      = 5 * time.Second
	PageSize         = 50
	MaxModifications = 4
	Account          = "000000000000"
	AMI              = "ami-sandbox"
)

// Config identifies the state file, injected clock and random source, and host settings.
type Config struct {
	Path  string
	Now   func() time.Time
	Rand  io.Reader
	Hosts settings.Hosts
}

// Cloud is a persistent sandbox cloud safe for concurrent use.
type Cloud struct {
	cfg         Config
	mu          sync.Mutex
	state       state
	backupScope [16]byte
}

var _ cloud.Cloud = (*Cloud)(nil)

const (
	// The sandbox rewrites one small JSON document. These limits bound
	// both persisted state and the new data a write can add to its snapshot.
	maxStateBytes    = 8 << 20
	maxMutationBytes = 128 << 10
)

var errStateCapacity = errors.New("sandbox cloud state capacity exceeded")

type instance struct {
	Value cloud.Instance
	Since time.Time
}

type volume struct {
	Value         cloud.Volume
	Created       time.Time
	Attached      time.Time
	DeleteStarted time.Time
	Modifications []modification
}

type modification struct {
	Target int32
	Since  time.Time
}

type launchToken struct {
	Spec cloud.Launch
	ID   string
}

type volumeToken struct {
	Spec cloud.VolumeSpec
	ID   string
}

type state struct {
	Key          []byte
	Instances    map[string]instance
	Volumes      map[string]volume
	Addresses    map[string]cloud.Address
	Records      map[string]cloud.Record
	Changes      map[string]time.Time
	LaunchTokens map[string]launchToken
	VolumeTokens map[string]volumeToken
	Associations map[string]bool
	Keys         []objectKey
}

func emptyState() state {
	return state{
		Key:          make([]byte, 32),
		Instances:    make(map[string]instance),
		Volumes:      make(map[string]volume),
		Addresses:    make(map[string]cloud.Address),
		Records:      make(map[string]cloud.Record),
		Changes:      make(map[string]time.Time),
		LaunchTokens: make(map[string]launchToken),
		VolumeTokens: make(map[string]volumeToken),
		Associations: make(map[string]bool),
	}
}

// New opens the state file or starts with an empty cloud.
func New(cfg Config) (*Cloud, error) {
	c := &Cloud{cfg: cfg, state: emptyState()}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cloud) load() error {
	s, err := readState(c.cfg.Path)
	if errors.Is(err, os.ErrNotExist) {
		_, err = io.ReadFull(c.cfg.Rand, c.state.Key)
		return err
	}
	if err != nil {
		return err
	}
	c.state = s
	return nil
}

func readState(path string) (state, error) {
	dir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return state{}, err
	}
	defer func() { _ = dir.Close() }()
	name := filepath.Base(path)
	info, err := dir.Stat(name)
	if err != nil {
		return state{}, err
	}
	if !info.Mode().IsRegular() {
		return state{}, errors.New("sandbox cloud state is not a regular file")
	}
	if info.Size() > maxStateBytes {
		return state{}, errStateCapacity
	}
	file, err := dir.Open(name)
	if err != nil {
		return state{}, err
	}
	defer func() { _ = file.Close() }()
	input := &io.LimitedReader{R: file, N: maxStateBytes + 1}
	decoder := json.NewDecoder(input)
	var s state
	if err := decoder.Decode(&s); err != nil {
		return state{}, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return state{}, errors.New("extra data after cloud state")
	}
	if input.N == 0 {
		return state{}, errStateCapacity
	}
	if len(s.Key) != 32 || s.Instances == nil || s.Volumes == nil || s.Addresses == nil ||
		s.Records == nil || s.Changes == nil || s.LaunchTokens == nil || s.VolumeTokens == nil || s.Associations == nil {
		return state{}, errors.New("invalid cloud state")
	}
	return s, nil
}

// snapshot copies the maps. Mutations also copy any nested collection they change.
func (s state) snapshot() state {
	s.Instances = maps.Clone(s.Instances)
	s.Volumes = maps.Clone(s.Volumes)
	s.Addresses = maps.Clone(s.Addresses)
	s.Records = maps.Clone(s.Records)
	s.Changes = maps.Clone(s.Changes)
	s.LaunchTokens = maps.Clone(s.LaunchTokens)
	s.VolumeTokens = maps.Clone(s.VolumeTokens)
	s.Associations = maps.Clone(s.Associations)
	return s
}

func (c *Cloud) save(next state, op operation) error {
	if err := writeState(c.cfg.Path, next); err != nil {
		return op.failure(cloud.CodeNoResponse, err)
	}
	c.state = next
	return nil
}

func writeState(path string, next state) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".sandbox-cloud-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	output := &stateWriter{dst: file, remaining: maxStateBytes}
	if err := json.NewEncoder(output).Encode(next); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

type stateWriter struct {
	dst       io.Writer
	remaining int
}

func (w *stateWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, errStateCapacity
	}
	n, err := w.dst.Write(p)
	w.remaining -= n
	return n, err
}

// mutationBudget counts input bytes and a minimum charge per tag. JSON can
// escape each input byte to at most six bytes. Counting before copying keeps
// a single caller from growing a mutation snapshot without bound.
type mutationBudget int

func (b *mutationBudget) take(n int) bool {
	if n > int(*b) {
		return false
	}
	*b -= mutationBudget(n)
	return true
}

func checkMutation(op operation, tagSets []cloud.Tags, values ...string) error {
	budget := mutationBudget(maxMutationBytes)
	for _, value := range values {
		if !budget.take(len(value)) {
			return op.failure(cloud.CodeNoResponse, errStateCapacity)
		}
	}
	for _, tags := range tagSets {
		for k, v := range tags {
			if !budget.take(8) || !budget.take(len(k)) || !budget.take(len(v)) {
				return op.failure(cloud.CodeNoResponse, errStateCapacity)
			}
		}
	}
	return nil
}

// ownedTags detaches new strings from their caller's backing storage.
func ownedTags(tags cloud.Tags) cloud.Tags {
	if tags == nil {
		return nil
	}
	out := make(cloud.Tags, len(tags))
	for k, v := range tags {
		out[strings.Clone(k)] = strings.Clone(v)
	}
	return out
}

type operation struct{ service, name, subject string }

func ec2(name, subject string) operation { return operation{"ec2", name, subject} }
func dns(name, subject string) operation { return operation{"route53", name, subject} }

var stsIdentity = operation{"sts", "GetCallerIdentity", ""}

func (op operation) failure(code string, err error) *cloud.Error {
	class := cloud.ClassRefused
	switch code {
	case "InvalidInstanceID.NotFound", "InvalidVolume.NotFound", "InvalidAllocationID.NotFound", "NoSuchChange", "NoSuchBucket":
		class = cloud.ClassNotFound
	case "IdempotentParameterMismatch":
		class = cloud.ClassMismatch
	case "VolumeModificationRateExceeded", "IncorrectInstanceState", "IncorrectState", cloud.CodeNoResponse:
		class = cloud.ClassTransient
	}
	return &cloud.Error{Service: op.service, Operation: op.name, Subject: op.subject, Code: code, Class: class, Err: err}
}

func (op operation) reject(code string) *cloud.Error { return op.failure(code, nil) }

func randomID[V any](c *Cloud, op operation, prefix string, ids map[string]V) (string, error) {
	for range 64 {
		var bytes [9]byte
		if _, err := io.ReadFull(c.cfg.Rand, bytes[:]); err != nil {
			return "", op.failure(cloud.CodeNoResponse, err)
		}
		suffix := hex.EncodeToString(bytes[:])[1:]
		if prefix == "C" {
			suffix = strings.ToUpper(suffix)
		}
		id := prefix + suffix
		if _, exists := ids[id]; !exists {
			return id, nil
		}
	}
	return "", op.failure(cloud.CodeNoResponse, fmt.Errorf("repeated random %s ids", prefix))
}

func forbiddenTags(tags cloud.Tags) bool {
	_, space := tags["Space"]
	_, domain := tags["Domain"]
	return space || domain
}

func hasKind(tags cloud.Tags) bool {
	_, ok := tags[cloud.TagKind]
	return ok
}

func allowedType(typ string) bool {
	family, _, found := strings.Cut(typ, ".")
	return found && slices.Contains([]string{"t4g", "m7g", "c7g", "r7g"}, family)
}

func matches(tags, filter cloud.Tags) bool {
	for k, v := range filter {
		if actual, ok := tags[k]; !ok || actual != v {
			return false
		}
	}
	return true
}

func find[V, T any](c *Cloud, op operation, filter cloud.Tags, data func(state) map[string]V, view func(state, V, time.Time) (T, cloud.Tags, bool), each func([]T) bool) error {
	if len(filter) == 0 || forbiddenTags(filter) {
		return op.reject(cloud.CodeTagContract)
	}
	c.mu.Lock()
	s, now := c.state, c.cfg.Now()
	c.mu.Unlock()
	values := data(s)
	page := make([]T, 0, PageSize)
	for _, id := range slices.Sorted(maps.Keys(values)) {
		value, tags, visible := view(s, values[id], now)
		if !visible || !matches(tags, filter) {
			continue
		}
		page = append(page, value)
		if len(page) == PageSize {
			if !each(page) {
				return nil
			}
			page = make([]T, 0, PageSize)
		}
	}
	if len(page) != 0 {
		each(page)
	}
	return nil
}
