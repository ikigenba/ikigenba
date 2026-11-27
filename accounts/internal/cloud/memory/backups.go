package memory

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/access"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	sandboxPartition = "aws"
	accessKeyPrefix  = "ASIA"
	accessKeyLength  = len(accessKeyPrefix) + 16
)

var stsAssumeRole = operation{"sts", "AssumeRole", ""}

var backupScopeSequence atomic.Uint64

// Identity returns the identity the sandbox uses to assume the backup role.
func (*Cloud) Identity(context.Context) (cloud.Caller, error) {
	return cloud.Caller{
		Account: Account,
		Arn:     "arn:aws:sts::" + Account + ":assumed-role/sandbox-accounts/sandbox",
		UserID:  "AROASANDBOX:sandbox",
	}, nil
}

// AssumeRole issues a temporary credential scoped to one backup prefix.
func (c *Cloud) AssumeRole(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
	if r.RoleARN != access.RoleARN(sandboxPartition, Account, c.cfg.Hosts) {
		return cloud.Credentials{}, stsAssumeRole.reject("AccessDenied")
	}
	if !validSessionName(r.SessionName) || r.Duration < 900*time.Second ||
		r.Duration > cloud.RoleSessionDuration || r.Duration%time.Second != 0 || len(r.Policy) > cloud.MaxPolicyBytes {
		return cloud.Credentials{}, stsAssumeRole.reject("ValidationError")
	}
	prefix, accepted := policyPrefix(c.cfg.Hosts.BackupBucket(), r.Policy)
	if !accepted {
		return cloud.Credentials{}, stsAssumeRole.reject("MalformedPolicyDocument")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var draw [8]byte
	if _, err := io.ReadFull(c.cfg.Rand, draw[:]); err != nil {
		return cloud.Credentials{}, stsAssumeRole.failure(cloud.CodeNoResponse, err)
	}
	if c.backupScope == [16]byte{} {
		// The sequence distinguishes receivers even with identical injected
		// readers. The first key draw also varies the scope across restarts.
		binary.BigEndian.PutUint64(c.backupScope[:8], backupScopeSequence.Add(1))
		copy(c.backupScope[8:], draw[:])
	}
	id := accessKeyPrefix + strings.ToUpper(hex.EncodeToString(draw[:]))
	expires := c.cfg.Now().UTC().Truncate(time.Second).Add(r.Duration)
	payload := binary.AppendVarint(make([]byte, 0, binary.MaxVarintLen64+len(prefix)+sha256.Size), expires.Unix())
	payload = append(payload, prefix...)
	secret := c.backupMAC("secret", id, payload)
	payload = append(payload, c.backupMAC("token", id, payload)...)
	return cloud.Credentials{
		AccessKeyID: id, SecretAccessKey: base64.RawURLEncoding.EncodeToString(secret),
		SessionToken: base64.RawURLEncoding.EncodeToString(payload), Expires: expires,
	}, nil
}

func validSessionName(name string) bool {
	if len(name) < 2 || len(name) > 64 {
		return false
	}
	for i := range len(name) {
		b := name[i]
		switch {
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9', strings.IndexByte("_+=,.@-", b) >= 0:
		default:
			return false
		}
	}
	return true
}

func policyPrefix(bucket, policy string) (string, bool) {
	// The policy inserts the same prefix twice. Measure the inserted span
	// from a marker policy, then verify the candidate with the policy builder.
	bare := access.Policy(bucket, sandboxPartition, "")
	marked := access.Policy(bucket, sandboxPartition, "\x00")
	added := len(policy) - len(bare)
	if added <= 0 || added%2 != 0 {
		return "", false
	}
	var start int
	for start < len(bare) && bare[start] == marked[start] {
		start++
	}
	prefix := policy[start : start+added/2]
	if !strings.HasSuffix(prefix, "/") || (!strings.HasPrefix(prefix, "accounts/") && !strings.HasPrefix(prefix, "hosts/")) {
		return "", false
	}
	return prefix, policy == access.Policy(bucket, sandboxPartition, prefix)
}

func (c *Cloud) backupMAC(kind, id string, payload []byte) []byte {
	mac := hmac.New(sha256.New, c.state.Key)
	_, _ = mac.Write(c.backupScope[:])
	_, _ = io.WriteString(mac, kind)
	_, _ = io.WriteString(mac, id)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

// backupSession checks all credential strings and returns their signed scope.
func (c *Cloud) backupSession(cred protocol.Credentials) (prefix string, expires time.Time, valid bool) {
	encoding := base64.RawURLEncoding
	if c.backupScope == [16]byte{} || len(cred.AccessKeyID) != accessKeyLength || len(cred.SecretAccessKey) != encoding.EncodedLen(sha256.Size) ||
		len(cred.SessionToken) > encoding.EncodedLen(binary.MaxVarintLen64+cloud.MaxPolicyBytes+sha256.Size) {
		return "", time.Time{}, false
	}
	token, err := encoding.DecodeString(cred.SessionToken)
	if err != nil || len(token) <= sha256.Size || encoding.EncodeToString(token) != cred.SessionToken {
		return "", time.Time{}, false
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	if !hmac.Equal(signature, c.backupMAC("token", cred.AccessKeyID, payload)) ||
		!hmac.Equal([]byte(cred.SecretAccessKey), []byte(encoding.EncodeToString(c.backupMAC("secret", cred.AccessKeyID, payload)))) {
		return "", time.Time{}, false
	}
	expiry, n := binary.Varint(payload)
	if n <= 0 || n == len(payload) {
		return "", time.Time{}, false
	}
	return string(payload[n:]), time.Unix(expiry, 0), true
}

// objectKey preserves the bytes of a key across JSON state rewrites.
type objectKey string

func (k objectKey) MarshalJSON() ([]byte, error) { return json.Marshal([]byte(k)) }

func (k *objectKey) UnmarshalJSON(data []byte) error {
	var key []byte
	if err := json.Unmarshal(data, &key); err != nil {
		return err
	}
	*k = objectKey(key)
	return nil
}

// Put records a key written by the host with its scoped backup credentials.
func (c *Cloud) Put(cred protocol.Credentials, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := operation{"s3", "PutObject", c.cfg.Hosts.BackupBucket()}
	prefix, expires, valid := c.backupSession(cred)
	if !valid {
		return op.reject("AccessDenied")
	}
	if !c.cfg.Now().Before(expires) {
		return op.reject("ExpiredToken")
	}
	if !strings.HasPrefix(key, prefix) {
		return op.reject("AccessDenied")
	}
	at, found := slices.BinarySearch(c.state.Keys, objectKey(key))
	if found {
		return nil
	}
	if len(key) > maxStateBytes {
		return op.failure(cloud.CodeNoResponse, errStateCapacity)
	}
	next := c.state
	next.Keys = make([]objectKey, len(c.state.Keys)+1)
	copy(next.Keys, c.state.Keys[:at])
	next.Keys[at] = objectKey(strings.Clone(key))
	copy(next.Keys[at+1:], c.state.Keys[at:])
	return c.save(next, op)
}

// ListKeys visits a stable view of the keys under prefix, in sorted pages.
func (c *Cloud) ListKeys(_ context.Context, bucket, prefix string, each func([]string) bool) error {
	keys, err := c.keys(bucket, prefix)
	if err != nil {
		return err
	}
	for page := range slices.Chunk(keys, cloud.MaxKeysPerPage) {
		out := make([]string, len(page))
		for i, key := range page {
			out[i] = string(key)
		}
		if !each(out) {
			break
		}
	}
	return nil
}

// HasKeys reports whether the bucket holds any key under prefix.
func (c *Cloud) HasKeys(_ context.Context, bucket, prefix string) (bool, error) {
	keys, err := c.keys(bucket, prefix)
	return len(keys) > 0, err
}

func (c *Cloud) keys(bucket, prefix string) ([]objectKey, error) {
	if bucket != c.cfg.Hosts.BackupBucket() {
		return nil, operation{"s3", "ListObjectsV2", bucket}.reject("NoSuchBucket")
	}
	c.mu.Lock()
	keys := c.state.Keys
	c.mu.Unlock()
	start, _ := slices.BinarySearch(keys, objectKey(prefix))
	keys = keys[start:]
	// Once the sorted set stops matching the prefix it cannot match again.
	end := sort.Search(len(keys), func(i int) bool {
		return !strings.HasPrefix(string(keys[i]), prefix)
	})
	return keys[:end], nil
}

// DeleteKeys deletes the supplied page of keys, ignoring missing keys.
func (c *Cloud) DeleteKeys(_ context.Context, bucket string, keys []string) error {
	op := operation{"s3", "DeleteObjects", bucket}
	if len(keys) == 0 || len(keys) > cloud.MaxKeysPerPage {
		return op.reject("MalformedXML")
	}
	if bucket != c.cfg.Hosts.BackupBucket() {
		return op.reject("NoSuchBucket")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	remove := make(map[objectKey]struct{}, len(keys))
	for _, key := range keys {
		if _, found := slices.BinarySearch(c.state.Keys, objectKey(key)); found {
			remove[objectKey(key)] = struct{}{}
		}
	}
	if len(remove) == 0 {
		return nil
	}
	next := c.state
	next.Keys = make([]objectKey, 0, len(c.state.Keys)-len(remove))
	for _, key := range c.state.Keys {
		if _, found := remove[key]; !found {
			next.Keys = append(next.Keys, key)
		}
	}
	return c.save(next, op)
}
