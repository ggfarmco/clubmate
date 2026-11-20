package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"clubmate/pkg/report"
)

const (
	maxSignupBody = 1 << 10
	maxSelfSigned = 20_000
	newKeysBucket = "new"
)

var (
	errSignupClosed  = errors.New("sign-up closed")
	errTooManySignup = errors.New("too many sign-ups")
	errSignupBody    = errors.New("body must be a JSON object of at most 1 KiB")
)

type signupLimits struct {
	ipHour  *limiter
	ipDay   *limiter
	keyHour *limiter
	keyDay  *limiter
	newKeys *limiter
	cap     int
}

type signupRefusal struct {
	status int
	err    error
	retry  time.Duration
}

type signupReply struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

func newSignupLimits(now func() time.Time) *signupLimits {
	return &signupLimits{
		ipHour:  newLimiter(5.0/3600, 5, now),
		ipDay:   newLimiter(20.0/86400, 20, now),
		keyHour: newLimiter(3.0/3600, 3, now),
		keyDay:  newLimiter(6.0/86400, 6, now),
		newKeys: newLimiter(300.0/86400, 300, now),
		cap:     maxSelfSigned,
	}
}

func (l *signupLimits) all() []*limiter {
	return []*limiter{l.ipHour, l.ipDay, l.keyHour, l.keyDay, l.newKeys}
}

func (r *relay) postMac(w http.ResponseWriter, req *http.Request) {
	r.refresh()
	ip := clientIP(req)
	refuse := func(status int, err error, retry time.Duration) {
		r.log.Printf("signup refused %d %s", status, ip)
		if retry > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
		}
		http.Error(w, err.Error(), status)
	}
	if r.bans.ip(ip) {
		refuse(http.StatusForbidden, errBanned, 0)
		return
	}
	if wait := take(bucketKey(ip), r.signup.ipHour, r.signup.ipDay); wait > 0 {
		refuse(http.StatusTooManyRequests, errTooManySignup, wait)
		return
	}
	if req.ContentLength > maxSignupBody {
		refuse(http.StatusRequestEntityTooLarge, errSignupBody, 0)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxSignupBody))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			refuse(http.StatusRequestEntityTooLarge, errSignupBody, 0)
			return
		}
		refuse(http.StatusBadRequest, errBadBody, 0)
		return
	}
	now := r.now()
	pub, err := report.Verify(req, body, now)
	if err != nil {
		refuse(http.StatusUnauthorized, err, 0)
		return
	}
	sig, _ := base64.RawURLEncoding.DecodeString(req.Header.Get(report.HeaderSignature))
	if r.seen.used(string(sig), now) {
		refuse(http.StatusUnauthorized, errReplayed, 0)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		refuse(http.StatusBadRequest, errSignupBody, 0)
		return
	}
	hexKey := hex.EncodeToString(pub)
	if r.bannedKey(hexKey) {
		refuse(http.StatusForbidden, errBanned, 0)
		return
	}
	if wait := take(hexKey, r.signup.keyHour, r.signup.keyDay); wait > 0 {
		refuse(http.StatusTooManyRequests, errTooManySignup, wait)
		return
	}
	if err := r.seen.add(string(sig), now); err != nil {
		code := http.StatusTooManyRequests
		if errors.Is(err, errReplayed) {
			code = http.StatusUnauthorized
		}
		refuse(code, err, 0)
		return
	}
	name := macName(pub)
	token, fresh, err := r.tokens.signup(pub, now, func(fresh bool, self int) error {
		if !fresh {
			return nil
		}
		if wait := r.signup.newKeys.wait(newKeysBucket); wait > 0 {
			return &signupRefusal{status: http.StatusTooManyRequests, err: errTooManySignup, retry: wait}
		}
		if self >= r.signup.cap {
			return &signupRefusal{status: http.StatusServiceUnavailable, err: errSignupClosed}
		}
		if err := r.devices.set(name, nil); err != nil {
			return err
		}
		r.signup.newKeys.allow(newKeysBucket)
		return nil
	})
	if ref, ok := errors.AsType[*signupRefusal](err); ok {
		refuse(ref.status, ref.err, ref.retry)
		return
	}
	if errors.Is(err, errNameTaken) {
		refuse(http.StatusConflict, err, 0)
		return
	}
	if err != nil && token == "" {
		r.log.Printf("signup %s: %v", name, err)
		refuse(http.StatusInternalServerError, errNotStored, 0)
		return
	}
	if err != nil {
		r.log.Printf("reload tokens: %v", err)
	}
	how, status := "again", http.StatusOK
	if fresh {
		how, status = "new", http.StatusCreated
	}
	r.log.Printf("signup %s %s %s", name, how, ip)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(signupReply{Token: token, Name: name})
}

func (e *signupRefusal) Error() string {
	return e.err.Error()
}

func take(key string, ls ...*limiter) time.Duration {
	var wait time.Duration
	for _, l := range ls {
		wait = max(wait, l.wait(key))
	}
	if wait > 0 {
		return wait
	}
	for _, l := range ls {
		l.allow(key)
	}
	return 0
}

func (l *limiter) wait(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.bucket(key)
	if !ok {
		return reclaimEvery
	}
	if b.tokens >= 1 {
		return 0
	}
	return time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}
